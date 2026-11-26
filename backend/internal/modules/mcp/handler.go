package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/internal/modules/docs"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

const (
	defaultProtocolVersion = "2025-06-18"
	maxMCPBodyBytes        = 4 << 20 // 4 МиБ
)

// Handler реализует MCP Streamable HTTP (JSON-RPC 2.0) и справку агента /api/agent-guide.md для модуля документации.
type Handler struct {
	docs   *docs.Service
	search Searcher
	chat   ChatAsker
	auth   platform.Authorizer
	logger *slog.Logger
	tools  []mcpTool
}

func NewHandler(
	docsSvc *docs.Service,
	search Searcher,
	chatSvc ChatAsker,
	auth platform.Authorizer,
	logger *slog.Logger,
) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{
		docs:   docsSvc,
		search: search,
		chat:   chatSvc,
		auth:   auth,
		logger: logger,
	}
	h.tools = h.buildTools()
	return h
}

func (h *Handler) Mount(r chi.Router) {
	r.Post("/mcp", h.handleMCP)
	r.Get("/mcp", h.handleMCPMethodNotAllowed)
	r.Get("/api/agent-guide.md", h.handleAgentGuide)
	r.Get("/api/v1/agent-guide.md", h.handleAgentGuide)
}

func (h *Handler) visibleTools(auth *platform.AuthContext) []mcpTool {
	if auth == nil {
		return h.tools
	}
	out := make([]mcpTool, 0, len(h.tools))
	for _, t := range h.tools {
		if t.VisibleTo == nil || t.VisibleTo(auth) {
			out = append(out, t)
		}
	}
	return out
}

func (h *Handler) findTool(name string) *mcpTool {
	for i := range h.tools {
		if h.tools[i].Name == name {
			return &h.tools[i]
		}
	}
	return nil
}

func (h *Handler) handleAgentGuide(w http.ResponseWriter, r *http.Request) {
	var auth *platform.AuthContext
	if h.auth != nil {
		if ac, err := h.auth.ResolveRequest(r); err == nil {
			auth = ac
		}
	}
	var spaces []docs.SpaceSummary
	if h.docs != nil && auth != nil && auth.HasOwnerPermission("docs.view") {
		spaces, _ = h.docs.ListSpacesForAuth(r.Context(), auth)
	}
	topic := r.URL.Query().Get("topic")
	if strings.TrimSpace(topic) == "" {
		topic = "all"
	}
	md := BuildAgentGuide(requestBaseURL(r), auth, spaces, h.visibleTools(auth), topic)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(md))
}

func (h *Handler) handleMCPMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Allow", "POST")
	w.WriteHeader(http.StatusMethodNotAllowed)
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func normalizeRPCID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

func writeRPC(w http.ResponseWriter, status int, resp rpcResponse) {
	resp.ID = normalizeRPCID(resp.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleMCP(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil {
		writeRPC(w, http.StatusUnauthorized, rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32001, Message: "Провайдер авторизации не настроен"},
		})
		return
	}
	auth, err := h.auth.ResolveRequest(r)
	if err != nil {
		writeRPC(w, http.StatusUnauthorized, rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32001, Message: "Требуется действительный API-токен (Authorization: Bearer <токен>)"},
		})
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxMCPBodyBytes))
	if err != nil {
		writeRPC(w, http.StatusBadRequest, rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "Ошибка чтения тела запроса"},
		})
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPC(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: -32700, Message: "Некорректный JSON-RPC запрос"},
		})
		return
	}
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		writeRPC(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32600, Message: "Некорректный формат JSON-RPC 2.0"},
		})
		return
	}

	// Любой JSON-RPC 2.0 запрос без id является уведомлением — подтверждаем 202 Accepted без тела.
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	baseURL := requestBaseURL(r)

	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &params)
		proto := defaultProtocolVersion
		switch params.ProtocolVersion {
		case "2024-11-05", "2025-03-26", "2025-06-18":
			proto = params.ProtocolVersion
		}
		writeRPC(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": proto,
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    "tsfera-docs",
					"version": "1.0.0",
				},
				"instructions": "Документация Теплосферы: регламенты, инструкции по котлам, взрыв-схемы, запчасти, ADR. В начале работы вызови get_agent_guide, чтобы узнать права токена и доступные инструменты. Для вопросов используй ask или search; итоги фиксируй через log_result.",
			},
		})
		return

	case "ping":
		writeRPC(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{},
		})
		return

	case "notifications/initialized":
		writeRPC(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{},
		})
		return

	case "tools/list":
		visible := h.visibleTools(auth)
		list := make([]map[string]any, 0, len(visible))
		for _, t := range visible {
			list = append(list, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
		writeRPC(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"tools": list,
			},
		})
		return

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil || strings.TrimSpace(params.Name) == "" {
			writeRPC(w, http.StatusOK, rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &rpcError{Code: -32602, Message: "Укажите name и arguments инструмента"},
			})
			return
		}
		tool := h.findTool(params.Name)
		if tool == nil {
			writeRPC(w, http.StatusOK, rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &rpcError{Code: -32602, Message: "Неизвестный инструмент: " + params.Name},
			})
			return
		}
		if tool.VisibleTo != nil && !tool.VisibleTo(auth) {
			writeRPC(w, http.StatusOK, rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"content": []map[string]any{{"type": "text", "text": "Инструмент недоступен для данного токена или роли владельца"}},
					"isError": true,
				},
			})
			return
		}
		args := params.Arguments
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		text, callErr := tool.Execute(r.Context(), auth, baseURL, args)
		if callErr != nil {
			msg := safeToolErrorMessage(callErr)
			if !isDomainError(callErr) {
				h.logger.Error("mcp tool execution failed", "tool", params.Name, "error", callErr)
			}
			writeRPC(w, http.StatusOK, rpcResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]any{
					"content": []map[string]any{{"type": "text", "text": msg}},
					"isError": true,
				},
			})
			return
		}
		writeRPC(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
				"isError": false,
			},
		})
		return

	default:
		writeRPC(w, http.StatusOK, rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: "Метод не поддерживается: " + req.Method},
		})
	}
}

func isDomainError(err error) bool {
	var de *platform.DomainError
	return errors.As(err, &de)
}

func safeToolErrorMessage(err error) string {
	var de *platform.DomainError
	if errors.As(err, &de) && strings.TrimSpace(de.Message) != "" {
		return de.Message
	}
	return "Внутренняя ошибка выполнения инструмента"
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		first := strings.ToLower(strings.TrimSpace(strings.Split(proto, ",")[0]))
		if first == "http" || first == "https" {
			scheme = first
		}
	} else if r.TLS != nil {
		scheme = "https"
	}
	host := sanitizeHost(r.Host)
	if host == "" {
		host = "localhost:8080"
	}
	return scheme + "://" + host
}

func sanitizeHost(raw string) string {
	host := strings.TrimSpace(raw)
	if host == "" || len(host) > 255 {
		return ""
	}
	for _, r := range host {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == ':' || r == '[' || r == ']') {
			return ""
		}
	}
	return host
}
