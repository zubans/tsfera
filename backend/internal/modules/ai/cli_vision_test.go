package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGatewayAnalyzeImageViaCLIGateway(t *testing.T) {
	var captured struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer gw-secret" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"content": "Клемма XT2: L черный, N синий [подтверждено на схеме]"}},
			},
			"usage": map[string]any{"prompt_tokens": 120, "completion_tokens": 25},
		})
	}))
	defer srv.Close()

	repo := newMemAIRepo()
	p := &Provider{
		Name:    "Шлюз agy",
		Kind:    ProviderCLIGateway,
		BaseURL: srv.URL,
		APIKey:  "gw-secret",
		Active:  true,
	}
	_ = repo.SaveProvider(context.Background(), p)
	m := &Model{
		ProviderID:   p.ID,
		ProviderKind: ProviderCLIGateway,
		Name:         "Шлюз agy · gemini-vision",
		ModelID:      "agy/gemini-vision",
		Capability:   CapChat,
		OpenToAll:    true,
		Active:       true,
	}
	_ = repo.SaveModel(context.Background(), m)
	_ = repo.SaveTaskAssignment(context.Background(), TaskVision, &m.ID)

	gw := NewGateway(repo, nil, nil)
	out, err := gw.AnalyzeImage(
		context.Background(),
		nil,
		"Отвечай строго по схеме.",
		"Какие провода на XT2?",
		[]ImageInput{{MimeType: "image/png", Data: []byte("fake-png-bytes")}},
		"schematic_inspect",
	)
	if err != nil {
		t.Fatalf("AnalyzeImage failed: %v", err)
	}
	if !strings.Contains(out, "XT2") {
		t.Fatalf("unexpected answer: %q", out)
	}
	if captured.Model != "agy/gemini-vision" || captured.Stream {
		t.Fatalf("unexpected model/stream: %q %v", captured.Model, captured.Stream)
	}
	if len(captured.Messages) != 2 || captured.Messages[1].Role != "user" {
		t.Fatalf("expected system+user messages, got %+v", captured.Messages)
	}
	if !strings.Contains(string(captured.Messages[1].Content), "data:image/png;base64,") {
		t.Fatalf("expected data URL image_url in user content, got %s", string(captured.Messages[1].Content))
	}
}
