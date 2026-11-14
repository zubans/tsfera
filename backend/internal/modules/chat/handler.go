package chat

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type Handler struct {
	svc    *Service
	auth   platform.Authorizer
	logger *slog.Logger
}

func NewHandler(svc *Service, auth platform.Authorizer, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{svc: svc, auth: auth, logger: logger}
}

func (h *Handler) Mount(r chi.Router) {
	r.Group(func(pr chi.Router) {
		pr.Use(platform.RequireAuth(h.auth, false))
		pr.Route("/api/v1/chat", func(cr chi.Router) {
			cr.With(platform.RequirePermission("chat.use")).Get("/conversations", h.listConversations)
			cr.With(platform.RequirePermission("chat.use")).Post("/conversations", h.createConversation)
			cr.With(platform.RequirePermission("chat.use")).Get("/conversations/{id}", h.getConversation)
			cr.With(platform.RequirePermission("chat.use")).Delete("/conversations/{id}", h.deleteConversation)
			cr.With(platform.RequirePermission("chat.use")).Post("/conversations/{id}/messages", h.postMessage)

			cr.With(platform.RequirePermission("chat.share")).Post("/conversations/{id}/share", h.createShare)
			cr.With(platform.RequirePermission("chat.share")).Delete("/conversations/{id}/share", h.revokeShare)
			cr.With(platform.RequirePermission("chat.use")).Get("/shared/{token}", h.getShared)
		})
	})
}

func parseID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	if err != nil || id <= 0 {
		return 0, platform.NewValidation("Некорректный ID", nil)
	}
	return id, nil
}

func (h *Handler) listConversations(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.svc.ListConversations(r.Context(), ac, limit)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) createConversation(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var in CreateConversationInput
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	conv, err := h.svc.CreateConversation(r.Context(), ac, in)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, conv)
}

func (h *Handler) getConversation(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	conv, err := h.svc.GetConversation(r.Context(), ac, id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, conv)
}

func (h *Handler) deleteConversation(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.DeleteConversation(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) postMessage(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in struct {
		Content string `json:"content"`
		Mode    Mode   `json:"mode"`
		ModelID int64  `json:"model_id"`
	}
	if err := platform.DecodeJSON(r, &in); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}

	wantsSSE := strings.Contains(r.Header.Get("Accept"), "text/event-stream") || r.URL.Query().Get("stream") == "true"
	if !wantsSSE {
		msg, err := h.svc.SendMessage(r.Context(), ac, id, in.Content, in.Mode, in.ModelID, nil)
		if err != nil {
			platform.WriteError(w, h.logger, err)
			return
		}
		platform.WriteJSON(w, http.StatusOK, msg)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)

	_, _ = h.svc.SendMessage(r.Context(), ac, id, in.Content, in.Mode, in.ModelID, func(ev SSEEvent) {
		raw, _ := json.Marshal(ev)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
		if flusher != nil {
			flusher.Flush()
		}
	})
}

func (h *Handler) createShare(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var in struct {
		TTLHours int `json:"ttl_hours"`
	}
	_ = platform.DecodeJSON(r, &in)
	var ttl time.Duration
	if in.TTLHours > 0 {
		ttl = time.Duration(in.TTLHours) * time.Hour
	}
	share, err := h.svc.CreateShare(r.Context(), ac, id, ttl)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, share)
}

func (h *Handler) revokeShare(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := parseID(r, "id")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.RevokeConversationShares(r.Context(), ac, id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusNoContent, nil)
}

func (h *Handler) getShared(w http.ResponseWriter, r *http.Request) {
	conv, err := h.svc.GetSharedConversation(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, conv)
}
