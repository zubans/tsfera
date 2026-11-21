package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// ModelAccessPolicy проверяет доступ пользователя к конкретной модели (реализуется модулем users).
type ModelAccessPolicy interface {
	CanAccessModel(ctx context.Context, auth *platform.AuthContext, modelID int64, isOpenToAll bool) (bool, error)
}

type Gateway struct {
	repo       Repository
	access     ModelAccessPolicy
	httpClient *http.Client
	// longClient — для шлюза консольных агентов: длительность ограничивает контекст с таймаутом провайдера.
	longClient *http.Client
	logger     *slog.Logger
	// cliHome — каталог, в котором у каждого консольного агента своя папка входа.
	cliHome string
	proc    cliProcess
	semMu   sync.Mutex
	sems    map[int64]chan struct{}
}

// SetCLIHome задаёт каталог входа консольных агентов (по умолчанию ./data/ai-cli).
func (g *Gateway) SetCLIHome(dir string) {
	if strings.TrimSpace(dir) != "" {
		g.cliHome = dir
	}
}

func NewGateway(repo Repository, access ModelAccessPolicy, logger *slog.Logger) *Gateway {
	if logger == nil {
		logger = slog.Default()
	}
	return &Gateway{
		repo:       repo,
		access:     access,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		longClient: &http.Client{},
		logger:     logger,
		cliHome:    "./data/ai-cli",
		proc:       execProcess{},
		sems:       map[int64]chan struct{}{},
	}
}

// modelForTask — модель, назначенная задаче; без назначения — модель по умолчанию для возможности.
func (g *Gateway) modelForTask(ctx context.Context, task AITask) (*Model, error) {
	if id, err := g.repo.GetTaskModelID(ctx, task); err != nil {
		return nil, err
	} else if id != nil {
		m, err := g.repo.GetModel(ctx, *id)
		if err != nil {
			return nil, err
		}
		if m != nil && m.Active && m.Capability == task.Capability() {
			return m, nil
		}
	}
	return g.repo.GetDefaultModel(ctx, task.Capability())
}

func (g *Gateway) resolveChatModel(ctx context.Context, auth *platform.AuthContext, requestedModelID int64, task AITask) (*Model, *Provider, error) {
	var m *Model
	var err error
	if requestedModelID > 0 {
		m, err = g.repo.GetModel(ctx, requestedModelID)
	} else {
		m, err = g.modelForTask(ctx, task)
	}
	if err != nil {
		return nil, nil, err
	}
	if m == nil || !m.Active {
		return nil, nil, platform.NewNotConfigured("ИИ-модель не настроена. Добавьте провайдера и модель в настройках ИИ.")
	}
	if g.access != nil && auth != nil {
		ok, err := g.access.CanAccessModel(ctx, auth, m.ID, m.OpenToAll)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, platform.NewForbidden("Нет доступа к выбранной ИИ-модели")
		}
	}
	p, err := g.repo.GetProvider(ctx, m.ProviderID)
	if err != nil {
		return nil, nil, err
	}
	if p == nil || !p.Active {
		return nil, nil, platform.NewNotConfigured("Провайдер ИИ неактивен или удалён")
	}
	return m, p, nil
}

// CompleteChat выполняет синхронный вызов LLM (включая вызов инструментов) по диалекту выбранного провайдера.
func (g *Gateway) CompleteChat(ctx context.Context, auth *platform.AuthContext, req CompletionRequest) (*CompletionResponse, error) {
	started := time.Now()
	m, p, err := g.resolveChatModel(ctx, auth, req.ModelID, TaskForOperation(req.Operation))
	if err != nil {
		return nil, err
	}
	if req.Operation == "" {
		req.Operation = "chat"
	}

	var resp *CompletionResponse
	switch p.Kind {
	case ProviderOpenAI, ProviderOpenAICompat:
		resp, err = g.callOpenAIChat(ctx, p, m, req)
	case ProviderAnthropic:
		resp, err = g.callAnthropicChat(ctx, p, m, req)
	case ProviderGemini:
		resp, err = g.callGeminiChat(ctx, p, m, req)
	case ProviderOllama:
		resp, err = g.callOllamaChat(ctx, p, m, req)
	case ProviderClaudeCLI, ProviderCodexCLI, ProviderAntigravityCLI:
		resp, err = g.callCLIChat(ctx, p, m, req, nil)
	case ProviderCLIGateway:
		resp, err = g.callGatewayChat(ctx, p, m, req, nil)
	default:
		err = platform.NewValidation("Неподдерживаемый тип провайдера: "+string(p.Kind), nil)
	}

	g.recordUsage(ctx, auth, m, req.Operation, started, resp, err)
	if err != nil {
		return nil, err
	}
	resp.ModelID = m.ID
	resp.ModelName = m.Name
	return resp, nil
}

// StreamChat выполняет потоковую генерацию текста (используется для /ai/compose и финального ответа чата).
func (g *Gateway) StreamChat(ctx context.Context, auth *platform.AuthContext, req CompletionRequest, onDelta func(string) error) (*CompletionResponse, error) {
	started := time.Now()
	m, p, err := g.resolveChatModel(ctx, auth, req.ModelID, TaskForOperation(req.Operation))
	if err != nil {
		return nil, err
	}
	if req.Operation == "" {
		req.Operation = "stream"
	}

	var resp *CompletionResponse
	switch {
	case (p.Kind == ProviderOpenAI || p.Kind == ProviderOpenAICompat) && len(req.Tools) == 0:
		resp, err = g.streamOpenAIChat(ctx, p, m, req, onDelta)
	case p.Kind.IsCLI() && len(req.Tools) == 0:
		resp, err = g.callCLIChat(ctx, p, m, req, onDelta)
	case p.Kind == ProviderCLIGateway && len(req.Tools) == 0:
		resp, err = g.callGatewayChat(ctx, p, m, req, onDelta)
	default:
		// Для провайдеров без SSE или при активных инструментах — аккуратный fallback с отдачей результата в callback.
		resp, err = g.CompleteChat(ctx, auth, req)
		if err == nil && resp.Content != "" && onDelta != nil {
			err = onDelta(resp.Content)
		}
		return resp, err
	}

	g.recordUsage(ctx, auth, m, req.Operation, started, resp, err)
	if err != nil {
		return nil, err
	}
	resp.ModelID = m.ID
	resp.ModelName = m.Name
	return resp, nil
}

// Embed генерирует вектор эмбеддинга, если в системе настроена активная embedding-модель.
func (g *Gateway) Embed(ctx context.Context, text string) ([]float64, error) {
	m, err := g.modelForTask(ctx, TaskEmbedding)
	if err != nil || m == nil || !m.Active {
		return nil, nil
	}
	p, err := g.repo.GetProvider(ctx, m.ProviderID)
	if err != nil || p == nil || !p.Active {
		return nil, nil
	}
	switch p.Kind {
	case ProviderOpenAI, ProviderOpenAICompat:
		return g.embedOpenAI(ctx, p, m, text)
	case ProviderOllama:
		return g.embedOllama(ctx, p, m, text)
	case ProviderGemini:
		return g.embedGemini(ctx, p, m, text)
	default:
		return nil, nil
	}
}

// TranscribeAudio расшифровывает аудиозапись в текст через активную STT-модель (OpenAI / OpenAI-compatible /audio/transcriptions).
func (g *Gateway) TranscribeAudio(ctx context.Context, auth *platform.AuthContext, modelID int64, audio io.Reader, filename string, language string) (string, error) {
	var m *Model
	var err error
	if modelID > 0 {
		m, err = g.repo.GetModel(ctx, modelID)
	} else {
		m, err = g.modelForTask(ctx, TaskSTT)
	}
	if err != nil {
		return "", err
	}
	if m == nil || !m.Active || m.Capability != CapSTT {
		return "", platform.NewValidation("Не настроена активная STT-модель для распознавания речи", nil)
	}
	if g.access != nil {
		if auth == nil {
			if !m.OpenToAll {
				return "", platform.NewForbidden("Нет доступа к выбранной STT-модели")
			}
		} else {
			allowed, err := g.access.CanAccessModel(ctx, auth, m.ID, m.OpenToAll)
			if err != nil {
				return "", err
			}
			if !allowed {
				return "", platform.NewForbidden("Нет доступа к выбранной STT-модели")
			}
		}
	}
	p, err := g.repo.GetProvider(ctx, m.ProviderID)
	if err != nil {
		return "", err
	}
	if p == nil || !p.Active {
		return "", platform.NewValidation("Провайдер STT-модели отключён или не найден", nil)
	}
	if p.Kind != ProviderOpenAI && p.Kind != ProviderOpenAICompat {
		return "", platform.NewValidation("Распознавание речи поддерживается только для OpenAI и OpenAI-compatible провайдеров", nil)
	}

	if strings.TrimSpace(filename) == "" {
		filename = "audio.mp3"
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(fw, audio); err != nil {
		return "", err
	}
	_ = mw.WriteField("model", m.ModelID)
	if strings.TrimSpace(language) != "" {
		_ = mw.WriteField("language", strings.TrimSpace(language))
	}
	_ = mw.WriteField("response_format", "json")
	if err := mw.Close(); err != nil {
		return "", err
	}

	endpoint := defaultBaseURL(p.Kind, p.BaseURL) + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("STT-запрос не выполнен: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("STT HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBytes, &out); err != nil {
		return "", fmt.Errorf("некорректный ответ STT: %w", err)
	}
	return strings.TrimSpace(out.Text), nil
}

func (g *Gateway) recordUsage(ctx context.Context, auth *platform.AuthContext, m *Model, op string, started time.Time, resp *CompletionResponse, callErr error) {
	var uid int64
	if auth != nil {
		uid = auth.UserID
	}
	entry := &UsageLog{
		UserID:    uid,
		ModelID:   m.ID,
		ModelName: m.Name,
		Operation: op,
		LatencyMs: time.Since(started).Milliseconds(),
		Status:    "ok",
	}
	if resp != nil {
		entry.PromptTokens = resp.PromptTokens
		entry.CompletionTokens = resp.CompletionTokens
	}
	if callErr != nil {
		entry.Status = "error"
		entry.ErrorText = callErr.Error()
	}
	_ = g.repo.LogUsage(ctx, entry)
}

func defaultBaseURL(kind ProviderKind, raw string) string {
	if s := strings.TrimRight(strings.TrimSpace(raw), "/"); s != "" {
		return s
	}
	switch kind {
	case ProviderOpenAI:
		return "https://api.openai.com/v1"
	case ProviderAnthropic:
		return "https://api.anthropic.com/v1"
	case ProviderGemini:
		return "https://generativelanguage.googleapis.com/v1beta"
	case ProviderOllama:
		return "http://localhost:11434"
	default:
		return "http://localhost:8000/v1"
	}
}

func (g *Gateway) doJSON(ctx context.Context, method, url string, headers map[string]string, body any, dst any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d от провайдера ИИ: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}
	return json.Unmarshal(respBytes, dst)
}

// --- OpenAI / OpenAI-compatible ---

func buildOpenAIMessages(msgs []Message) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		item := map[string]any{"role": m.Role, "content": m.Content}
		if m.Role == "tool" {
			item["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			var tcs []map[string]any
			for _, tc := range m.ToolCalls {
				tcs = append(tcs, map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": string(tc.Arguments),
					},
				})
			}
			item["tool_calls"] = tcs
		}
		out = append(out, item)
	}
	return out
}

func (g *Gateway) callOpenAIChat(ctx context.Context, p *Provider, m *Model, req CompletionRequest) (*CompletionResponse, error) {
	payload := map[string]any{
		"model":    m.ModelID,
		"messages": buildOpenAIMessages(req.Messages),
	}
	if req.Temperature > 0 {
		payload["temperature"] = req.Temperature
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
				},
			})
		}
		payload["tools"] = tools
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	url := defaultBaseURL(p.Kind, p.BaseURL) + "/chat/completions"
	headers := map[string]string{"Authorization": "Bearer " + p.APIKey}
	if err := g.doJSON(ctx, http.MethodPost, url, headers, payload, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("пустой ответ от OpenAI-совместимого провайдера")
	}
	msg := parsed.Choices[0].Message
	out := &CompletionResponse{
		Content:          msg.Content,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
	}
	for _, tc := range msg.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(tc.Function.Arguments),
		})
	}
	return out, nil
}

func (g *Gateway) streamOpenAIChat(ctx context.Context, p *Provider, m *Model, req CompletionRequest, onDelta func(string) error) (*CompletionResponse, error) {
	payload := map[string]any{
		"model":    m.ModelID,
		"messages": buildOpenAIMessages(req.Messages),
		"stream":   true,
	}
	raw, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, defaultBaseURL(p.Kind, p.BaseURL)+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("HTTP %d stream: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err == nil && len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta.Content
			if delta != "" {
				full.WriteString(delta)
				if onDelta != nil {
					if err := onDelta(delta); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &CompletionResponse{Content: full.String()}, nil
}

// --- Anthropic ---

func (g *Gateway) callAnthropicChat(ctx context.Context, p *Provider, m *Model, req CompletionRequest) (*CompletionResponse, error) {
	var systemPrompt string
	var msgs []map[string]any
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			if systemPrompt != "" {
				systemPrompt += "\n"
			}
			systemPrompt += msg.Content
			continue
		}
		msgs = append(msgs, map[string]any{"role": msg.Role, "content": msg.Content})
	}
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = 2048
	}
	payload := map[string]any{
		"model":      m.ModelID,
		"max_tokens": maxTok,
		"messages":   msgs,
	}
	if systemPrompt != "" {
		payload["system"] = systemPrompt
	}
	if len(req.Tools) > 0 {
		var tools []map[string]any
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"name":         t.Name,
				"description":  t.Description,
				"input_schema": t.Parameters,
			})
		}
		payload["tools"] = tools
	}

	var parsed struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	url := defaultBaseURL(p.Kind, p.BaseURL) + "/messages"
	headers := map[string]string{
		"x-api-key":         p.APIKey,
		"anthropic-version": "2023-06-01",
	}
	if err := g.doJSON(ctx, http.MethodPost, url, headers, payload, &parsed); err != nil {
		return nil, err
	}
	out := &CompletionResponse{
		PromptTokens:     parsed.Usage.InputTokens,
		CompletionTokens: parsed.Usage.OutputTokens,
	}
	var textBuf strings.Builder
	for _, blk := range parsed.Content {
		switch blk.Type {
		case "text":
			textBuf.WriteString(blk.Text)
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, ToolCall{
				ID:        blk.ID,
				Name:      blk.Name,
				Arguments: blk.Input,
			})
		}
	}
	out.Content = textBuf.String()
	return out, nil
}

// --- Gemini ---

func (g *Gateway) callGeminiChat(ctx context.Context, p *Provider, m *Model, req CompletionRequest) (*CompletionResponse, error) {
	var sysParts []map[string]string
	var contents []map[string]any
	for _, msg := range req.Messages {
		if msg.Role == "system" {
			sysParts = append(sysParts, map[string]string{"text": msg.Content})
			continue
		}
		role := "user"
		if msg.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, map[string]any{
			"role":  role,
			"parts": []map[string]string{{"text": msg.Content}},
		})
	}
	payload := map[string]any{"contents": contents}
	if len(sysParts) > 0 {
		payload["systemInstruction"] = map[string]any{"parts": sysParts}
	}
	if len(req.Tools) > 0 {
		var fns []map[string]any
		for _, t := range req.Tools {
			fns = append(fns, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			})
		}
		payload["tools"] = []map[string]any{{"functionDeclarations": fns}}
	}

	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string          `json:"name"`
						Args json.RawMessage `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", defaultBaseURL(p.Kind, p.BaseURL), m.ModelID, p.APIKey)
	if err := g.doJSON(ctx, http.MethodPost, url, nil, payload, &parsed); err != nil {
		return nil, err
	}
	out := &CompletionResponse{
		PromptTokens:     parsed.UsageMetadata.PromptTokenCount,
		CompletionTokens: parsed.UsageMetadata.CandidatesTokenCount,
	}
	if len(parsed.Candidates) > 0 {
		var textBuf strings.Builder
		for idx, part := range parsed.Candidates[0].Content.Parts {
			if part.Text != "" {
				textBuf.WriteString(part.Text)
			}
			if part.FunctionCall != nil {
				out.ToolCalls = append(out.ToolCalls, ToolCall{
					ID:        fmt.Sprintf("gem_%d", idx+1),
					Name:      part.FunctionCall.Name,
					Arguments: part.FunctionCall.Args,
				})
			}
		}
		out.Content = textBuf.String()
	}
	return out, nil
}

// --- Ollama ---

func (g *Gateway) callOllamaChat(ctx context.Context, p *Provider, m *Model, req CompletionRequest) (*CompletionResponse, error) {
	payload := map[string]any{
		"model":    m.ModelID,
		"messages": buildOpenAIMessages(req.Messages),
		"stream":   false,
	}
	var parsed struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count"`
		EvalCount       int `json:"eval_count"`
	}
	url := defaultBaseURL(p.Kind, p.BaseURL) + "/api/chat"
	if err := g.doJSON(ctx, http.MethodPost, url, nil, payload, &parsed); err != nil {
		return nil, err
	}
	return &CompletionResponse{
		Content:          parsed.Message.Content,
		PromptTokens:     parsed.PromptEvalCount,
		CompletionTokens: parsed.EvalCount,
	}, nil
}

// --- Embeddings ---

func (g *Gateway) embedOpenAI(ctx context.Context, p *Provider, m *Model, text string) ([]float64, error) {
	var parsed struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	url := defaultBaseURL(p.Kind, p.BaseURL) + "/embeddings"
	headers := map[string]string{"Authorization": "Bearer " + p.APIKey}
	if err := g.doJSON(ctx, http.MethodPost, url, headers, map[string]any{"model": m.ModelID, "input": text}, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) == 0 {
		return nil, nil
	}
	return parsed.Data[0].Embedding, nil
}

func (g *Gateway) embedOllama(ctx context.Context, p *Provider, m *Model, text string) ([]float64, error) {
	var parsed struct {
		Embedding []float64 `json:"embedding"`
	}
	url := defaultBaseURL(p.Kind, p.BaseURL) + "/api/embeddings"
	if err := g.doJSON(ctx, http.MethodPost, url, nil, map[string]any{"model": m.ModelID, "prompt": text}, &parsed); err != nil {
		return nil, err
	}
	return parsed.Embedding, nil
}

func (g *Gateway) embedGemini(ctx context.Context, p *Provider, m *Model, text string) ([]float64, error) {
	var parsed struct {
		Embedding struct {
			Values []float64 `json:"values"`
		} `json:"embedding"`
	}
	url := fmt.Sprintf("%s/models/%s:embedContent?key=%s", defaultBaseURL(p.Kind, p.BaseURL), m.ModelID, p.APIKey)
	payload := map[string]any{"content": map[string]any{"parts": []map[string]string{{"text": text}}}}
	if err := g.doJSON(ctx, http.MethodPost, url, nil, payload, &parsed); err != nil {
		return nil, err
	}
	return parsed.Embedding.Values, nil
}
