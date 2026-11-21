package ai

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Живая проверка: настоящий шлюз консольных агентов или установленный CLI. Тратит лимиты подписки,
// поэтому запускается только с переменными окружения.
//
//	AI_LIVE_GATEWAY_URL=http://localhost:7070 AI_LIVE_GATEWAY_KEY=... AI_LIVE_MODEL=claude-1 go test -run TestLive ./internal/modules/ai/
//	AI_LIVE_CLI_KIND=claude_cli AI_LIVE_CLI_HOME=/data/ai-cli AI_LIVE_MODEL=haiku go test -run TestLive ./internal/modules/ai/
func TestLiveAgent(t *testing.T) {
	model := os.Getenv("AI_LIVE_MODEL")
	var p *Provider
	switch {
	case os.Getenv("AI_LIVE_GATEWAY_URL") != "":
		p = &Provider{Name: "Шлюз", Kind: ProviderCLIGateway, BaseURL: os.Getenv("AI_LIVE_GATEWAY_URL"), APIKey: os.Getenv("AI_LIVE_GATEWAY_KEY"), Active: true, TimeoutSeconds: 300}
	case os.Getenv("AI_LIVE_CLI_KIND") != "":
		p = &Provider{Name: "Агент", Kind: ProviderKind(os.Getenv("AI_LIVE_CLI_KIND")), APIKey: os.Getenv("AI_LIVE_CLI_TOKEN"), Active: true, TimeoutSeconds: 300}
	default:
		t.Skip("set AI_LIVE_GATEWAY_URL or AI_LIVE_CLI_KIND to run against a real agent")
	}
	repo := newMemAIRepo()
	_ = repo.SaveProvider(context.Background(), p)
	gw := NewGateway(repo, nil, nil)
	gw.SetCLIHome(os.Getenv("AI_LIVE_CLI_HOME"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	models, err := gw.ListProviderModels(ctx, p)
	if err != nil {
		t.Fatalf("models: %v", err)
	}
	t.Logf("models: %v", models)
	if model == "" {
		model = models[0]
	}
	_ = repo.SaveModel(ctx, &Model{ProviderID: p.ID, Name: model, ModelID: model, Capability: CapChat, IsDefault: true, OpenToAll: true, Active: true})

	resp, err := gw.CompleteChat(ctx, nil, CompletionRequest{
		Messages: []Message{
			{Role: "system", Content: "Ты помощник диспетчера сервисной компании."},
			{Role: "user", Content: "Найди контрагента Иванов. Используй инструмент."},
		},
		Tools: testTools,
	})
	if err != nil {
		t.Fatalf("tool round: %v", err)
	}
	t.Logf("tool round: content=%q calls=%+v", resp.Content, resp.ToolCalls)
	if len(resp.ToolCalls) == 0 || resp.ToolCalls[0].Name != "find_contractor" {
		t.Fatalf("модель не вызвала инструмент: %+v", resp)
	}
	resp, err = gw.CompleteChat(ctx, nil, CompletionRequest{
		Messages: []Message{
			{Role: "user", Content: "Найди контрагента Иванов. Используй инструмент."},
			{Role: "assistant", ToolCalls: resp.ToolCalls},
			{Role: "tool", ToolName: "find_contractor", Content: `[{"id": 7, "name": "Иванов Пётр", "phone": "+7 999 111-22-33"}]`},
		},
		Tools: testTools,
	})
	if err != nil {
		t.Fatalf("final round: %v", err)
	}
	t.Logf("final: %q", resp.Content)
	if len(resp.ToolCalls) != 0 || !strings.Contains(resp.Content, "Иванов") {
		t.Fatalf("ожидался окончательный ответ с данными инструмента: %+v", resp)
	}
}
