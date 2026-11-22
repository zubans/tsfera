package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

func (m *memAIRepo) GetTaskModelID(_ context.Context, task AITask) (*int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[task], nil
}

func (m *memAIRepo) ListTaskAssignments(context.Context) ([]TaskAssignment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []TaskAssignment
	for t, id := range m.tasks {
		out = append(out, TaskAssignment{Task: t, ModelID: id})
	}
	return out, nil
}

func (m *memAIRepo) SaveTaskAssignment(_ context.Context, task AITask, modelID *int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tasks == nil {
		m.tasks = map[AITask]*int64{}
	}
	m.tasks[task] = modelID
	return nil
}

// fakeProc имитирует консольного агента: запоминает запуск и отдаёт заготовленные строки stdout.
type fakeProc struct {
	specs  []cliSpec
	lines  []string
	stderr string
	err    error
}

func (f *fakeProc) Run(_ context.Context, spec cliSpec, onLine func([]byte)) (string, error) {
	f.specs = append(f.specs, spec)
	for _, l := range f.lines {
		onLine([]byte(l))
	}
	return f.stderr, f.err
}

func cliFixture(t *testing.T, kind ProviderKind, modelID string) (*Gateway, *memAIRepo, *fakeProc, *Model) {
	t.Helper()
	repo := newMemAIRepo()
	p := &Provider{Name: "Агент", Kind: kind, Active: true, APIKey: "sk-ant-oat-secret-token"}
	_ = repo.SaveProvider(context.Background(), p)
	m := &Model{ProviderID: p.ID, Name: "Агент · " + modelID, ModelID: modelID, Capability: CapChat, IsDefault: true, OpenToAll: true, Active: true}
	_ = repo.SaveModel(context.Background(), m)
	gw := NewGateway(repo, nil, nil)
	gw.SetCLIHome(t.TempDir())
	proc := &fakeProc{}
	gw.proc = proc
	return gw, repo, proc, m
}

func envValue(env []string, key string) string {
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			return strings.TrimPrefix(e, key+"=")
		}
	}
	return ""
}

var testTools = []platform.ToolDefinition{{Name: "find_contractor", Description: "Поиск контрагента", Parameters: map[string]any{"type": "object"}}}

func TestParseToolCalls(t *testing.T) {
	cases := []struct {
		name, text, visible string
		calls               int
	}{
		{"без инструментов", "Готово.", "Готово.", 0},
		{"список", `Ищу.<tool_calls>[{"name":"find_contractor","arguments":{"q":"Иванов"}},{"name":"x"}]</tool_calls>`, "Ищу.", 2},
		{"один объект", `<tool_calls>{"name":"find_contractor","arguments":{}}</tool_calls>`, "", 1},
		{"без закрывающего тега", `Ищу <tool_calls>[{"name":"find_contractor","arguments":{"q":"a"}}] <tool_result>выдумка`, "Ищу", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			visible, calls, err := parseToolCalls(c.text)
			if err != nil || visible != c.visible || len(calls) != c.calls {
				t.Fatalf("visible=%q calls=%d err=%v", visible, len(calls), err)
			}
			for _, call := range calls {
				if call.ID == "" || len(call.Arguments) == 0 {
					t.Fatalf("call without id/args: %+v", call)
				}
			}
		})
	}
	if _, _, err := parseToolCalls(`<tool_calls>[{"name": "x", "arguments": {</tool_calls>`); err == nil {
		t.Fatal("битый JSON должен давать ошибку")
	}
}

func TestClaudeCLIChatWithTools(t *testing.T) {
	gw, _, proc, m := cliFixture(t, ProviderClaudeCLI, "sonnet")
	proc.lines = []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"stream_event","event":{"delta":{"type":"text_delta","text":"Ищу"}}}`,
		`не json — пропускается`,
		`{"type":"result","is_error":false,"result":"Ищу контрагента.\n<tool_calls>[{\"name\":\"find_contractor\",\"arguments\":{\"q\":\"Иванов\"}}]</tool_calls>","usage":{"input_tokens":100,"cache_read_input_tokens":20,"output_tokens":7}}`,
	}
	resp, err := gw.CompleteChat(context.Background(), nil, CompletionRequest{
		Messages: []Message{
			{Role: "system", Content: "Ты помощник диспетчера."},
			{Role: "user", Content: "Найди Иванова"},
			{Role: "assistant", Content: "", ToolCalls: []ToolCall{{Name: "find_contractor", Arguments: json.RawMessage(`{"q":"Ив"}`)}}},
			{Role: "tool", ToolName: "find_contractor", Content: "ничего"},
		},
		Tools: testTools,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Ищу контрагента." || len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "find_contractor" {
		t.Fatalf("resp: %+v", resp)
	}
	if resp.PromptTokens != 120 || resp.CompletionTokens != 7 || resp.ModelID != m.ID {
		t.Fatalf("usage: %+v", resp)
	}
	spec := proc.specs[0]
	if spec.Bin != "claude" || !slices.Contains(spec.Args, "--no-session-persistence") || !slices.Contains(spec.Args, "sonnet") {
		t.Fatalf("args: %v", spec.Args)
	}
	if slices.Contains(spec.Args, "--bare") {
		t.Fatal("с токеном подписки --bare не передаётся")
	}
	if envValue(spec.Env, "CLAUDE_CODE_OAUTH_TOKEN") != "sk-ant-oat-secret-token" || envValue(spec.Env, "HOME") != spec.Dir {
		t.Fatalf("env: %v", spec.Env)
	}
	idx := slices.Index(spec.Args, "--system-prompt-file")
	raw, err := os.ReadFile(spec.Args[idx+1])
	if err != nil || !strings.Contains(string(raw), "Ты помощник диспетчера.") || !strings.Contains(string(raw), "find_contractor") {
		t.Fatalf("system prompt file: %q %v", raw, err)
	}
	for _, want := range []string{"<user>\\nНайди Иванова", "<tool_result name=\\\"find_contractor\\\">", "<tool_calls>", continuePrompt} {
		if !strings.Contains(spec.Stdin, want) {
			t.Fatalf("stdin has no %q: %s", want, spec.Stdin)
		}
	}
}

func TestClaudeCLIStreamsWithoutTools(t *testing.T) {
	gw, _, proc, _ := cliFixture(t, ProviderClaudeCLI, "haiku")
	proc.lines = []string{
		`{"type":"stream_event","event":{"delta":{"type":"text_delta","text":"При"}}}`,
		`{"type":"stream_event","event":{"delta":{"type":"text_delta","text":"вет"}}}`,
		`{"type":"result","result":"Привет","usage":{}}`,
	}
	var got []string
	resp, err := gw.StreamChat(context.Background(), nil, CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}, Operation: "compose"},
		func(d string) error { got = append(got, d); return nil })
	if err != nil || resp.Content != "Привет" || strings.Join(got, "") != "Привет" {
		t.Fatalf("resp=%+v deltas=%v err=%v", resp, got, err)
	}
}

func TestClaudeCLIErrors(t *testing.T) {
	gw, _, proc, _ := cliFixture(t, ProviderClaudeCLI, "sonnet")
	req := CompletionRequest{Messages: []Message{{Role: "user", Content: "hi"}}}

	proc.lines = []string{`{"type":"result","is_error":true,"result":"Invalid token sk-ant-oat-secret-token"}`}
	_, err := gw.CompleteChat(context.Background(), nil, req)
	if err == nil || strings.Contains(err.Error(), "secret-token") || !strings.Contains(err.Error(), "setup-token") {
		t.Fatalf("ошибка без токена и с подсказкой: %v", err)
	}

	proc.lines = nil
	proc.err = errCLINotFound
	_, err = gw.CompleteChat(context.Background(), nil, req)
	var de *platform.DomainError
	if !errors.As(err, &de) || !strings.Contains(err.Error(), "make build-ai-cli") {
		t.Fatalf("not found: %v", err)
	}
}

func TestCodexCLIChat(t *testing.T) {
	gw, _, proc, _ := cliFixture(t, ProviderCodexCLI, "gpt-5-codex")
	proc.lines = []string{
		`{"type":"thread.started","thread_id":"t1"}`,
		`{"type":"item.completed","item":{"type":"command_execution","command":"ls"}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"Ответ Codex"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":50,"cached_input_tokens":5,"output_tokens":9,"reasoning_output_tokens":1}}`,
	}
	resp, err := gw.CompleteChat(context.Background(), nil, CompletionRequest{Messages: []Message{{Role: "system", Content: "Правила"}, {Role: "user", Content: "Вопрос"}}})
	if err != nil || resp.Content != "Ответ Codex" || resp.PromptTokens != 55 || resp.CompletionTokens != 10 {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	spec := proc.specs[0]
	if spec.Bin != "codex" || spec.Args[0] != "exec" || !slices.Contains(spec.Args, "--ephemeral") || envValue(spec.Env, "CODEX_HOME") != spec.Dir {
		t.Fatalf("spec: %+v", spec)
	}
	if !strings.HasPrefix(spec.Stdin, "<system>\nПравила\n</system>") {
		t.Fatalf("stdin: %s", spec.Stdin)
	}

	proc.lines = []string{`{"type":"turn.failed","error":{"message":"usage limit reached"}}`}
	if _, err := gw.CompleteChat(context.Background(), nil, CompletionRequest{Messages: []Message{{Role: "user", Content: "x"}}}); err == nil || !strings.Contains(err.Error(), "usage limit") {
		t.Fatalf("turn.failed: %v", err)
	}
}

func TestAntigravityCLIChat(t *testing.T) {
	gw, _, proc, _ := cliFixture(t, ProviderAntigravityCLI, "1\tgemini-3-pro\tGemini 3 Pro")
	proc.lines = []string{
		`{"event":"step_update","step_update":{"step_type":"agent_response","text_delta":"Част"}}`,
		`{"event":"result","result":{"status":"SUCCESS","response":"Частичный ответ","usage":{"input_tokens":3,"output_tokens":4,"thinking_tokens":2}}}`,
	}
	resp, err := gw.CompleteChat(context.Background(), nil, CompletionRequest{Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil || resp.Content != "Частичный ответ" || resp.CompletionTokens != 6 {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	spec := proc.specs[0]
	if spec.Bin != "agy" || spec.Args[len(spec.Args)-1] != "gemini-3-pro" {
		t.Fatalf("args: %v", spec.Args)
	}
	raw, err := os.ReadFile(filepath.Join(spec.Dir, ".gemini", "antigravity-cli", "settings.json"))
	if err != nil || !strings.Contains(string(raw), `"toolPermission": "strict"`) {
		t.Fatalf("settings: %s %v", raw, err)
	}

	proc.lines = []string{`{"event":"result","result":{"status":"ERROR","error":"not logged in"}}`}
	if _, err := gw.CompleteChat(context.Background(), nil, CompletionRequest{Messages: []Message{{Role: "user", Content: "x"}}}); err == nil || !strings.Contains(err.Error(), "make agy-login") {
		t.Fatalf("login hint: %v", err)
	}
}

func TestListCLIModels(t *testing.T) {
	gw, _, proc, _ := cliFixture(t, ProviderCodexCLI, "x")
	p, _ := gw.repo.GetProvider(context.Background(), 1)
	proc.lines = []string{`{"models":[{"slug":"gpt-5-codex","visibility":"list"},{"slug":"hidden","visibility":"hide"},{"slug":"o4-mini"}]}`}
	got, err := gw.ListProviderModels(context.Background(), p)
	if err != nil || !slices.Equal(got, []string{"gpt-5-codex", "o4-mini"}) {
		t.Fatalf("codex: %v %v", got, err)
	}
	if !slices.Equal(parseAgyModels([]string{"Available models:", "1\tgemini-3-pro\tGemini", "2\tclaude-sonnet"}), []string{"gemini-3-pro", "claude-sonnet"}) {
		t.Fatal("agy models")
	}
}

func TestCLIGatewayChat(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gw-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"claude-1"},{"id":"codex-1"}]}`)
		case "/v1/chat/completions":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			bodies = append(bodies, body)
			if body["stream"] == true {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Пот\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"ок\"}}]}\n\ndata: [DONE]\n\n")
				return
			}
			_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Смотрю <tool_calls>[{\"name\":\"find_contractor\",\"arguments\":{}}]</tool_calls>"}}],"usage":{"prompt_tokens":11,"completion_tokens":2}}`)
		}
	}))
	defer srv.Close()

	repo := newMemAIRepo()
	p := &Provider{Name: "Шлюз", Kind: ProviderCLIGateway, BaseURL: srv.URL, APIKey: "gw-key", Active: true}
	_ = repo.SaveProvider(context.Background(), p)
	m := &Model{ProviderID: p.ID, Name: "claude-1", ModelID: "claude-1", Capability: CapChat, IsDefault: true, OpenToAll: true, Active: true}
	_ = repo.SaveModel(context.Background(), m)
	gw := NewGateway(repo, nil, nil)

	models, err := gw.ListProviderModels(context.Background(), p)
	if err != nil || !slices.Equal(models, []string{"claude-1", "codex-1"}) {
		t.Fatalf("models: %v %v", models, err)
	}

	resp, err := gw.CompleteChat(context.Background(), nil, CompletionRequest{
		Messages: []Message{
			{Role: "system", Content: "Правила"},
			{Role: "user", Content: "Найди"},
			{Role: "assistant", ToolCalls: []ToolCall{{Name: "find_contractor", Arguments: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolName: "find_contractor", Content: "a"},
			{Role: "tool", ToolName: "find_contractor", Content: "b"},
		},
		Tools:       testTools,
		Temperature: 0.2,
		MaxTokens:   500,
	})
	if err != nil || resp.Content != "Смотрю" || len(resp.ToolCalls) != 1 || resp.PromptTokens != 11 {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
	body := bodies[0]
	for _, forbidden := range []string{"tools", "temperature", "max_tokens"} {
		if _, ok := body[forbidden]; ok {
			t.Fatalf("шлюз отклоняет %s: %v", forbidden, body)
		}
	}
	msgs := body["messages"].([]any)
	var roles []string
	for _, raw := range msgs {
		roles = append(roles, raw.(map[string]any)["role"].(string))
	}
	if !slices.Equal(roles, []string{"system", "user", "assistant", "user"}) {
		t.Fatalf("roles: %v", roles)
	}
	last := msgs[3].(map[string]any)["content"].(string)
	if strings.Count(last, "<tool_result") != 2 {
		t.Fatalf("результаты инструментов склеены в одно сообщение: %s", last)
	}

	var deltas []string
	resp, err = gw.StreamChat(context.Background(), nil, CompletionRequest{Messages: []Message{{Role: "user", Content: "x"}}}, func(d string) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil || resp.Content != "Поток" || len(deltas) != 2 {
		t.Fatalf("stream: %+v %v %v", resp, deltas, err)
	}
}

func TestTaskAssignmentPicksModel(t *testing.T) {
	gw, repo, proc, defaultModel := cliFixture(t, ProviderClaudeCLI, "sonnet")
	other := &Model{ProviderID: defaultModel.ProviderID, Name: "opus", ModelID: "opus", Capability: CapChat, OpenToAll: true, Active: true}
	_ = repo.SaveModel(context.Background(), other)
	_ = repo.SaveTaskAssignment(context.Background(), TaskCompose, &other.ID)
	proc.lines = []string{`{"type":"result","result":"ok","usage":{}}`}

	resp, err := gw.StreamChat(context.Background(), nil, CompletionRequest{Messages: []Message{{Role: "user", Content: "x"}}, Operation: "compose"}, nil)
	if err != nil || resp.ModelID != other.ID {
		t.Fatalf("compose должна идти на назначенную модель: %+v %v", resp, err)
	}
	resp, err = gw.CompleteChat(context.Background(), nil, CompletionRequest{Messages: []Message{{Role: "user", Content: "x"}}, Operation: "chat_agent"})
	if err != nil || resp.ModelID != defaultModel.ID {
		t.Fatalf("чат без назначения — модель по умолчанию: %+v %v", resp, err)
	}
	if TaskForOperation("chat_rag") != TaskChat || TaskForOperation("compose") != TaskCompose {
		t.Fatal("task mapping")
	}
}

func TestCLIProxyOnlyForAgents(t *testing.T) {
	gw, _, proc, _ := cliFixture(t, ProviderCodexCLI, "gpt-5-codex")
	proc.lines = []string{`{"type":"item.completed","item":{"type":"agent_message","text":"ok"}}`}
	req := CompletionRequest{Messages: []Message{{Role: "user", Content: "x"}}}
	if _, err := gw.CompleteChat(context.Background(), nil, req); err != nil {
		t.Fatal(err)
	}
	if envValue(proc.specs[0].Env, "HTTPS_PROXY") != "" {
		t.Fatal("без настройки прокси не передаётся")
	}
	gw.SetCLIProxy("http://proxy.local:3128")
	if _, err := gw.CompleteChat(context.Background(), nil, req); err != nil {
		t.Fatal(err)
	}
	env := proc.specs[1].Env
	if envValue(env, "HTTPS_PROXY") != "http://proxy.local:3128" || envValue(env, "http_proxy") != "http://proxy.local:3128" {
		t.Fatalf("env: %v", env)
	}
}
