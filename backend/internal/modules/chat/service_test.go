package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/internal/modules/ai"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type memChatRepo struct {
	mu     sync.Mutex
	nextID int64
	convs  map[int64]*Conversation
	msgs   map[int64][]ChatMessage
	shares map[string]*ConversationShare
}

func newMemChatRepo() *memChatRepo {
	return &memChatRepo{
		convs:  map[int64]*Conversation{},
		msgs:   map[int64][]ChatMessage{},
		shares: map[string]*ConversationShare{},
	}
}

func (m *memChatRepo) id() int64 {
	m.nextID++
	return m.nextID
}

func (m *memChatRepo) ListConversations(_ context.Context, userID int64, _ int) ([]Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Conversation
	for _, c := range m.convs {
		if !c.Deleted && c.UserID == userID {
			out = append(out, *c)
		}
	}
	return out, nil
}
func (m *memChatRepo) GetConversation(_ context.Context, id int64) (*Conversation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.convs[id]
	if !ok || c.Deleted {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}
func (m *memChatRepo) CreateConversation(_ context.Context, conv *Conversation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	conv.ID = m.id()
	cp := *conv
	m.convs[conv.ID] = &cp
	return nil
}
func (m *memChatRepo) TouchConversation(context.Context, int64) error { return nil }
func (m *memChatRepo) SoftDeleteConversation(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.convs[id]; ok {
		c.Deleted = true
	}
	return nil
}
func (m *memChatRepo) ListMessages(_ context.Context, convID int64) ([]ChatMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ChatMessage(nil), m.msgs[convID]...), nil
}
func (m *memChatRepo) AddMessage(_ context.Context, msg *ChatMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg.ID = m.id()
	m.msgs[msg.ConversationID] = append(m.msgs[msg.ConversationID], *msg)
	return nil
}
func (m *memChatRepo) CreateShare(_ context.Context, s *ConversationShare) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.ID = m.id()
	cp := *s
	m.shares[s.ShareToken] = &cp
	return nil
}
func (m *memChatRepo) GetShareByToken(_ context.Context, token string) (*ConversationShare, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.shares[token]
	if !ok {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}
func (m *memChatRepo) RevokeSharesByConversation(_ context.Context, convID, ownerID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.shares {
		if s.ConversationID == convID && s.OwnerID == ownerID {
			s.Revoked = true
		}
	}
	return nil
}
func (m *memChatRepo) RevokeSharesByOwner(_ context.Context, ownerID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.shares {
		if s.OwnerID == ownerID {
			s.Revoked = true
		}
	}
	return nil
}

type fakeLLM struct {
	mu       sync.Mutex
	round    int
	seenReqs []ai.CompletionRequest
	handler  func(round int, req ai.CompletionRequest) (*ai.CompletionResponse, error)
}

func (f *fakeLLM) CompleteChat(_ context.Context, _ *platform.AuthContext, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
	f.mu.Lock()
	r := f.round
	f.round++
	f.seenReqs = append(f.seenReqs, req)
	f.mu.Unlock()
	return f.handler(r, req)
}

func (f *fakeLLM) StreamChat(ctx context.Context, auth *platform.AuthContext, req ai.CompletionRequest, onDelta func(string) error) (*ai.CompletionResponse, error) {
	resp, err := f.CompleteChat(ctx, auth, req)
	if err != nil {
		return nil, err
	}
	if onDelta != nil && resp.Content != "" {
		_ = onDelta(resp.Content)
	}
	return resp, nil
}

type fakeSearcher struct {
	results []ai.SearchResult
}

func (s fakeSearcher) HybridSearch(context.Context, *platform.AuthContext, string, int) ([]ai.SearchResult, error) {
	return s.results, nil
}

type fakeResolver struct{}

func (fakeResolver) ResolveEntity(_ context.Context, _ *platform.AuthContext, t string, id int64) (*platform.EntitySummary, error) {
	return &platform.EntitySummary{
		EntityType: t,
		EntityID:   id,
		Title:      "Контрагент Иванов",
		Markdown:   "# Контрагент Иванов\nКотёл: Baxi Eco Four 24F",
		Path:       "/crm/contractors/1",
	}, nil
}

type staticAuth struct{ ac *platform.AuthContext }

func (a staticAuth) ResolveRequest(*http.Request) (*platform.AuthContext, error) {
	return a.ac, nil
}

func TestAgenticLoopMultiRoundAndMax6Rounds(t *testing.T) {
	repo := newMemChatRepo()
	llm := &fakeLLM{
		handler: func(round int, _ ai.CompletionRequest) (*ai.CompletionResponse, error) {
			if round == 0 {
				return &ai.CompletionResponse{
					ToolCalls: []ai.ToolCall{{
						ID:        "call_1",
						Name:      "crm_find_contractor",
						Arguments: json.RawMessage(`{"query":"Иванов"}`),
					}},
					PromptTokens: 20, CompletionTokens: 10,
				}, nil
			}
			return &ai.CompletionResponse{
				Content:      "Найден контрагент Иванов (#10).",
				PromptTokens: 30, CompletionTokens: 12,
			}, nil
		},
	}

	toolCalledWithAuth := false
	tools := []platform.ToolDefinition{{
		Name: "crm_find_contractor",
		Execute: func(_ context.Context, auth *platform.AuthContext, _ json.RawMessage) (platform.ToolResult, error) {
			toolCalledWithAuth = auth != nil && auth.UserID == 7
			return platform.ToolResult{
				Content: "Иванов И.И. (#10)",
				Sources: []platform.SourceRef{{SourceType: "crm_contractor", SourceID: "10", Title: "Иванов", Path: "/crm/contractors/10"}},
			}, nil
		},
	}}

	svc := NewService(repo, llm, nil, nil, nil, tools...)
	ctx := context.Background()
	user := &platform.AuthContext{UserID: 7, Permissions: map[string]bool{"chat.use": true}}
	conv, _ := svc.CreateConversation(ctx, user, CreateConversationInput{Title: "Тест", Mode: ModeAgent})

	var sseTypes []string
	msg, err := svc.SendMessage(ctx, user, conv.ID, "Найди Иванова", "", 0, func(ev SSEEvent) {
		sseTypes = append(sseTypes, ev.Type)
	})
	if err != nil {
		t.Fatalf("send message failed: %v", err)
	}
	if !toolCalledWithAuth {
		t.Fatal("expected tool to execute under caller's AuthContext")
	}
	if len(msg.Steps) != 1 || len(msg.Sources) != 1 {
		t.Fatalf("expected 1 step and 1 source, got steps=%d sources=%d", len(msg.Steps), len(msg.Sources))
	}
	if strings.Join(sseTypes, ",") != "user,step,delta,sources,done" {
		t.Fatalf("unexpected SSE event sequence: %v", sseTypes)
	}

	// Проверка потолка MaxAgentRounds (6 раундов -> затем принудительный итог без инструментов).
	infiniteLLM := &fakeLLM{
		handler: func(round int, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
			if len(req.Tools) > 0 {
				return &ai.CompletionResponse{
					ToolCalls: []ai.ToolCall{{ID: "loop", Name: "crm_find_contractor", Arguments: json.RawMessage(`{}`)}},
				}, nil
			}
			return &ai.CompletionResponse{Content: "Принудительный итог после 6 шагов"}, nil
		},
	}
	svcLoop := NewService(repo, infiniteLLM, nil, nil, nil, tools...)
	conv2, _ := svcLoop.CreateConversation(ctx, user, CreateConversationInput{Title: "Лимит", Mode: ModeAgent})
	msg2, err := svcLoop.SendMessage(ctx, user, conv2.ID, "Цикл", "", 0, nil)
	if err != nil {
		t.Fatalf("loop guard failed: %v", err)
	}
	if len(msg2.Steps) != MaxAgentRounds || !strings.Contains(msg2.Content, "Принудительный итог") {
		t.Fatalf("expected exactly %d steps and fallback summary, got %d steps content=%q", MaxAgentRounds, len(msg2.Steps), msg2.Content)
	}
}

func TestRAGModeAndEntityContextInjection(t *testing.T) {
	repo := newMemChatRepo()
	llm := &fakeLLM{
		handler: func(_ int, req ai.CompletionRequest) (*ai.CompletionResponse, error) {
			return &ai.CompletionResponse{Content: "Очистите горелку согласно инструкции."}, nil
		},
	}
	searcher := fakeSearcher{
		results: []ai.SearchResult{
			{SourceType: "doc", SourceID: "5", Title: "Baxi E01", Content: "Проверить газовый клапан", Path: "/docs/5"},
		},
	}
	svc := NewService(repo, llm, searcher, fakeResolver{}, nil)
	ctx := context.Background()
	user := &platform.AuthContext{UserID: 3}

	conv, _ := svc.CreateConversation(ctx, user, CreateConversationInput{
		Title:      "Помощь по контрагенту",
		Mode:       ModeRAG,
		EntityType: "contractor",
		EntityID:   1,
	})
	msg, err := svc.SendMessage(ctx, user, conv.ID, "Почему ошибка E01?", "", 0, nil)
	if err != nil {
		t.Fatalf("RAG failed: %v", err)
	}
	if len(msg.Sources) != 1 || msg.Sources[0].Title != "Baxi E01" {
		t.Fatalf("unexpected RAG sources: %+v", msg.Sources)
	}
	sysMsg := llm.seenReqs[0].Messages[0].Content
	if !strings.Contains(sysMsg, "Baxi Eco Four 24F") {
		t.Fatalf("expected CRM entity context to be injected into system prompt, got:\n%s", sysMsg)
	}
}

func TestShareLinkAndAutoRevokeOnDeactivation(t *testing.T) {
	repo := newMemChatRepo()
	bus := platform.NewInMemoryEventBus()
	auth := staticAuth{ac: &platform.AuthContext{
		UserID:      12,
		Permissions: map[string]bool{"chat.use": true, "chat.share": true},
	}}
	mod := NewModule(repo, auth, &fakeLLM{
		handler: func(int, ai.CompletionRequest) (*ai.CompletionResponse, error) {
			return &ai.CompletionResponse{Content: "Ок"}, nil
		},
	}, nil, nil, bus, nil)
	ctx := context.Background()

	conv, _ := mod.Service.CreateConversation(ctx, auth.ac, CreateConversationInput{Title: "Публичный"})
	_, _ = mod.Service.SendMessage(ctx, auth.ac, conv.ID, "Вопрос", "", 0, nil)
	share, err := mod.Service.CreateShare(ctx, auth.ac, conv.ID, 24*time.Hour)
	if err != nil || share.ShareToken == "" {
		t.Fatalf("create share failed: %+v err=%v", share, err)
	}

	got, err := mod.Service.GetSharedConversation(ctx, share.ShareToken)
	if err != nil || len(got.Messages) != 2 {
		t.Fatalf("expected shared conversation with 2 messages, got %+v err=%v", got, err)
	}

	// Событие деактивации пользователя автоматически отзывает все его share-ссылки.
	bus.Publish(ctx, platform.Event{
		Topic:   "users.user.deactivated",
		Payload: map[string]any{"user_id": int64(12)},
	})
	if _, err := mod.Service.GetSharedConversation(ctx, share.ShareToken); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("expected share to be revoked after user deactivation, got %v", err)
	}

	// Проверка SSE по HTTP.
	r := chi.NewRouter()
	mod.MountHTTP(r)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat/conversations/1/messages", bytes.NewBufferString(`{"content":"Привет"}`))
	req.Header.Set("Accept", "text/event-stream")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(rec.Body.String(), `"type":"done"`) {
		t.Fatalf("unexpected SSE HTTP response code=%d body=%s", rec.Code, rec.Body.String())
	}
}
