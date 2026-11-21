package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// Шлюз консольных агентов (CLI AI Gateway, проект agy) отдаёт OpenAI-совместимый API, но отклоняет
// tools, temperature, max_tokens и роль tool. Поэтому запрос — только model, messages и stream,
// инструменты — тем же текстовым протоколом, что и у консольных агентов.

func gatewayBaseURL(p *Provider) string {
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		base = "http://host.docker.internal:7070"
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base
}

func (g *Gateway) gatewayRequest(ctx context.Context, p *Provider, method, path string, body any) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, gatewayBaseURL(p)+path, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key := strings.TrimSpace(p.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	// Ответ агента может идти минутами: ограничивает контекст с таймаутом провайдера, а не клиент.
	resp, err := g.longClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: шлюз недоступен (%s): %w", p.Name, gatewayBaseURL(p), err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(redact(string(b), p.APIKey))
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(b, &apiErr) == nil && apiErr.Error.Message != "" {
			msg = apiErr.Error.Message
		}
		return nil, fmt.Errorf("%s: шлюз ответил HTTP %d: %s", p.Name, resp.StatusCode, truncate(msg, 500))
	}
	return resp, nil
}

// callGatewayChat выполняет ход через шлюз; при onDelta без инструментов — потоково.
func (g *Gateway) callGatewayChat(ctx context.Context, p *Provider, m *Model, req CompletionRequest, onDelta func(string) error) (*CompletionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, providerTimeout(p))
	defer cancel()
	withTools := len(req.Tools) > 0
	system, dialog := splitSystem(req.Messages, req.Tools)
	stream := onDelta != nil && !withTools
	payload := map[string]any{
		"model":    m.ModelID,
		"messages": gatewayMessages(system, dialog),
		"stream":   stream,
	}
	resp, err := g.gatewayRequest(ctx, p, http.MethodPost, "/chat/completions", payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if !stream {
		var parsed struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&parsed); err != nil {
			return nil, fmt.Errorf("%s: некорректный ответ шлюза: %w", p.Name, err)
		}
		if len(parsed.Choices) == 0 {
			return nil, fmt.Errorf("%s: шлюз вернул пустой ответ", p.Name)
		}
		out, err := finishTextTools(parsed.Choices[0].Message.Content, withTools)
		if err != nil {
			return nil, err
		}
		out.PromptTokens = parsed.Usage.PromptTokens
		out.CompletionTokens = parsed.Usage.CompletionTokens
		return out, nil
	}

	var full strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
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
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(data), &chunk) != nil {
			continue
		}
		if chunk.Error != nil {
			return nil, fmt.Errorf("%s: %s", p.Name, truncate(chunk.Error.Message, 500))
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			d := chunk.Choices[0].Delta.Content
			full.WriteString(d)
			if err := onDelta(d); err != nil {
				return nil, err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return &CompletionResponse{Content: strings.TrimSpace(full.String())}, nil
}

// listGatewayModels возвращает модели, разрешённые ключу шлюза.
func (g *Gateway) listGatewayModels(ctx context.Context, p *Provider) ([]string, error) {
	resp, err := g.gatewayRequest(ctx, p, http.MethodGet, "/models", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("%s: некорректный список моделей шлюза: %w", p.Name, err)
	}
	var out []string
	for _, d := range parsed.Data {
		if d.ID != "" {
			out = append(out, d.ID)
		}
	}
	if len(out) == 0 {
		return nil, platform.NewValidation(p.Name+": шлюз не вернул ни одной модели — проверьте ключ и список разрешённых моделей", nil)
	}
	return out, nil
}

// ListProviderModels — модели провайдера для импорта: консольный агент или шлюз.
func (g *Gateway) ListProviderModels(ctx context.Context, p *Provider) ([]string, error) {
	switch {
	case p.Kind.IsCLI():
		return g.listCLIModels(ctx, p)
	case p.Kind == ProviderCLIGateway:
		return g.listGatewayModels(ctx, p)
	}
	return nil, platform.NewValidation("Загрузка списка моделей поддерживается для консольных агентов и шлюза", nil)
}
