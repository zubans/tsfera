package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

type memAIRepo struct {
	mu        sync.Mutex
	nextID    int64
	providers map[int64]*Provider
	models    map[int64]*Model
	chunks    []SearchChunk
	usage     []UsageLog
}

func newMemAIRepo() *memAIRepo {
	return &memAIRepo{
		providers: map[int64]*Provider{},
		models:    map[int64]*Model{},
	}
}

func (m *memAIRepo) id() int64 {
	m.nextID++
	return m.nextID
}

func (m *memAIRepo) ListProviders(context.Context) ([]Provider, error) { return nil, nil }
func (m *memAIRepo) GetProvider(_ context.Context, id int64) (*Provider, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.providers[id]
	if !ok {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}
func (m *memAIRepo) SaveProvider(_ context.Context, p *Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.ID == 0 {
		p.ID = m.id()
	}
	cp := *p
	m.providers[p.ID] = &cp
	return nil
}
func (m *memAIRepo) DeleteProvider(context.Context, int64) error { return nil }

func (m *memAIRepo) ListModels(_ context.Context, cap *ModelCapability) ([]Model, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Model
	for _, mdl := range m.models {
		if cap != nil && mdl.Capability != *cap {
			continue
		}
		out = append(out, *mdl)
	}
	return out, nil
}
func (m *memAIRepo) GetModel(_ context.Context, id int64) (*Model, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mdl, ok := m.models[id]
	if !ok {
		return nil, nil
	}
	cp := *mdl
	return &cp, nil
}
func (m *memAIRepo) GetDefaultModel(_ context.Context, capability ModelCapability) (*Model, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mdl := range m.models {
		if mdl.Active && mdl.Capability == capability && mdl.IsDefault {
			cp := *mdl
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memAIRepo) SaveModel(_ context.Context, mdl *Model) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mdl.ID == 0 {
		mdl.ID = m.id()
	}
	cp := *mdl
	m.models[mdl.ID] = &cp
	return nil
}
func (m *memAIRepo) DeleteModel(context.Context, int64) error { return nil }

func (m *memAIRepo) ListChunkHashes(_ context.Context, sourceType, sourceID string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for _, ch := range m.chunks {
		if ch.SourceType == sourceType && ch.SourceID == sourceID {
			out = append(out, ch.ContentHash)
		}
	}
	return out, nil
}
func (m *memAIRepo) ReplaceSourceChunks(_ context.Context, sourceType, sourceID string, chunks []SearchChunk) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept []SearchChunk
	for _, ch := range m.chunks {
		if ch.SourceType != sourceType || ch.SourceID != sourceID {
			kept = append(kept, ch)
		}
	}
	m.chunks = append(kept, chunks...)
	return nil
}
func (m *memAIRepo) DeleteSourceChunks(context.Context, string, string) error { return nil }
func (m *memAIRepo) SearchFTS(_ context.Context, query string, limit int) ([]SearchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	q := strings.ToLower(query)
	var out []SearchResult
	for _, ch := range m.chunks {
		if strings.Contains(strings.ToLower(ch.Title+" "+ch.Content), q) {
			out = append(out, SearchResult{
				SourceType: ch.SourceType,
				SourceID:   ch.SourceID,
				Title:      ch.Title,
				Anchor:     ch.Anchor,
				Content:    ch.Content,
				Snippet:    MakeSnippet(ch.Content, 120),
				Path:       ch.Path,
				Metadata:   ch.Metadata,
				Score:      1.0,
			})
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (m *memAIRepo) SearchVectors(_ context.Context, queryVec []float64, limit int) ([]SearchResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []SearchResult
	for _, ch := range m.chunks {
		if len(ch.Embedding) > 0 {
			sim := CosineSimilarity(queryVec, ch.Embedding)
			if sim > 0.2 {
				out = append(out, SearchResult{
					SourceType: ch.SourceType,
					SourceID:   ch.SourceID,
					Title:      ch.Title,
					Anchor:     ch.Anchor,
					Content:    ch.Content,
					Snippet:    MakeSnippet(ch.Content, 120),
					Path:       ch.Path,
					Metadata:   ch.Metadata,
					Score:      sim,
				})
			}
		}
	}
	return out, nil
}
func (m *memAIRepo) LogUsage(_ context.Context, entry *UsageLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.usage = append(m.usage, *entry)
	return nil
}
func (m *memAIRepo) ListUsageLogs(context.Context, int) ([]UsageLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]UsageLog(nil), m.usage...), nil
}

type mockIndexSource struct {
	items     []platform.IndexItem
	allowPred func(auth *platform.AuthContext, id string) bool
}

func (s *mockIndexSource) SourceType() string { return "doc" }
func (s *mockIndexSource) ListForIndex(context.Context, *time.Time) ([]platform.IndexItem, error) {
	return s.items, nil
}
func (s *mockIndexSource) CanView(_ context.Context, auth *platform.AuthContext, id string, _ map[string]string) (bool, error) {
	return s.allowPred(auth, id), nil
}

type denyPolicy struct {
	deniedModelID int64
}

func (p denyPolicy) CanAccessModel(_ context.Context, auth *platform.AuthContext, modelID int64, openToAll bool) (bool, error) {
	if auth != nil && auth.IsSuperAdmin {
		return true, nil
	}
	if modelID == p.deniedModelID {
		return false, nil
	}
	return openToAll, nil
}

type fakeDocFetcher struct{}

func (fakeDocFetcher) FetchDocumentMarkdown(_ context.Context, _ *platform.AuthContext, docID int64) (string, error) {
	return "# Регламент ТО #" + json.Number("1").String() + "\nДавление в РБ 1.5 бар.", nil
}

func TestMarkdownChunkingAndRRF(t *testing.T) {
	longText := "# Первый раздел\n" + strings.Repeat("Проверка давления газа. ", 90) + "\n## Второй раздел\nЗамена электрода розжига."
	chunks := ChunkItem(platform.IndexItem{
		SourceType: "doc",
		SourceID:   "7",
		Title:      "Котёл Baxi",
		Content:    longText,
		Path:       "/docs/7",
	}, 600)
	if len(chunks) < 3 {
		t.Fatalf("expected >=3 chunks for long doc, got %d", len(chunks))
	}
	if chunks[len(chunks)-1].Anchor != "второй-раздел" {
		t.Fatalf("expected anchor второго-раздела, got %q", chunks[len(chunks)-1].Anchor)
	}

	r1 := []SearchResult{{SourceType: "doc", SourceID: "1", Anchor: "a"}, {SourceType: "doc", SourceID: "2", Anchor: "b"}}
	r2 := []SearchResult{{SourceType: "doc", SourceID: "2", Anchor: "b"}, {SourceType: "doc", SourceID: "3", Anchor: "c"}}
	fused := ReciprocalRankFusion(r1, r2, 60)
	if fused[0].SourceID != "2" {
		t.Fatalf("expected item present in both lists to win RRF, got %+v", fused)
	}
}

func TestHybridSearchEnforcesRBAC(t *testing.T) {
	repo := newMemAIRepo()
	src := &mockIndexSource{
		items: []platform.IndexItem{
			{SourceType: "doc", SourceID: "1", Title: "Открытый регламент Baxi", Content: "Ошибка E01 — отсутствие пламени.", Path: "/docs/1"},
			{SourceType: "doc", SourceID: "2", Title: "Секретный скрипт Baxi", Content: "Ошибка E01 — внутренняя скидка.", Path: "/docs/2"},
		},
		allowPred: func(_ *platform.AuthContext, id string) bool {
			return id == "1"
		},
	}
	mod := NewModule(repo, nil, nil, nil, nil, nil, nil, src)
	ctx := context.Background()

	indexed, err := mod.Search.ReindexAll(ctx)
	if err != nil || indexed != 2 {
		t.Fatalf("expected 2 indexed docs, got %d err=%v", indexed, err)
	}
	// Повторная переиндексация без изменений не должна переписывать чанки (SHA-256 skip).
	indexed2, _ := mod.Search.ReindexAll(ctx)
	if indexed2 != 0 {
		t.Fatalf("expected unchanged docs to skip reindex, got %d", indexed2)
	}

	res, err := mod.Search.HybridSearch(ctx, &platform.AuthContext{UserID: 5}, "E01", 8)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(res) != 1 || res[0].SourceID != "1" {
		t.Fatalf("expected only doc #1 after RBAC filter, got %+v", res)
	}
}

func TestGatewayAllFourDialectsAndPolicy(t *testing.T) {
	ctx := context.Background()

	// Мокаем 4 провайдера сразу по заголовкам/путям.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{
					"message": map[string]any{
						"content": "Ответ OpenAI",
						"tool_calls": []map[string]any{{
							"id":       "tc_1",
							"function": map[string]any{"name": "crm_find_contractor", "arguments": `{"query":"Иванов"}`},
						}},
					},
				}},
				"usage": map[string]int{"prompt_tokens": 12, "completion_tokens": 5},
			})
		case r.URL.Path == "/v1/messages":
			if r.Header.Get("x-api-key") == "" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": []map[string]any{{"type": "text", "text": "Ответ Claude"}},
				"usage":   map[string]int{"input_tokens": 10, "output_tokens": 4},
			})
		case strings.Contains(r.URL.Path, ":generateContent"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"candidates": []map[string]any{{
					"content": map[string]any{"parts": []map[string]any{{"text": "Ответ Gemini"}}},
				}},
				"usageMetadata": map[string]int{"promptTokenCount": 8, "candidatesTokenCount": 3},
			})
		case r.URL.Path == "/api/chat":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message":           map[string]any{"content": "Ответ Ollama"},
				"prompt_eval_count": 7,
				"eval_count":        2,
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	repo := newMemAIRepo()
	kinds := []ProviderKind{ProviderOpenAI, ProviderAnthropic, ProviderGemini, ProviderOllama}
	expected := []string{"Ответ OpenAI", "Ответ Claude", "Ответ Gemini", "Ответ Ollama"}
	var modelIDs []int64

	for _, kind := range kinds {
		p := &Provider{Name: string(kind), Kind: kind, BaseURL: srv.URL + "/v1", APIKey: "k", Active: true}
		if kind == ProviderOllama {
			p.BaseURL = srv.URL
		}
		_ = repo.SaveProvider(ctx, p)
		m := &Model{ProviderID: p.ID, Name: string(kind) + "-chat", ModelID: "m", Capability: CapChat, OpenToAll: true, Active: true}
		_ = repo.SaveModel(ctx, m)
		modelIDs = append(modelIDs, m.ID)
	}

	gw := NewGateway(repo, denyPolicy{deniedModelID: modelIDs[1]}, nil)
	user := &platform.AuthContext{UserID: 77}

	// Доступ к запрещённой модели (Anthropic id=modelIDs[1]) должен вернуть ErrForbidden.
	if _, err := gw.CompleteChat(ctx, user, CompletionRequest{
		ModelID:  modelIDs[1],
		Messages: []Message{{Role: "user", Content: "Привет"}},
	}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("expected ErrForbidden on denied model, got %v", err)
	}

	// Проверяем все 4 диалекта как superadmin.
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}
	for i, mid := range modelIDs {
		resp, err := gw.CompleteChat(ctx, admin, CompletionRequest{
			ModelID:  mid,
			Messages: []Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "hi"}},
		})
		if err != nil {
			t.Fatalf("provider %s failed: %v", kinds[i], err)
		}
		if resp.Content != expected[i] {
			t.Fatalf("expected %q, got %q", expected[i], resp.Content)
		}
		if kinds[i] == ProviderOpenAI && (len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "crm_find_contractor") {
			t.Fatalf("expected parsed OpenAI tool call, got %+v", resp.ToolCalls)
		}
	}

	logs, _ := repo.ListUsageLogs(ctx, 20)
	if len(logs) != 4 {
		t.Fatalf("expected 4 usage logs, got %d", len(logs))
	}
}

func TestAIComposeSSEStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"# Черновик\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\" акта\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	repo := newMemAIRepo()
	ctx := context.Background()
	p := &Provider{Name: "OpenAI", Kind: ProviderOpenAI, BaseURL: srv.URL, APIKey: "k", Active: true, IsDefault: true}
	_ = repo.SaveProvider(ctx, p)
	_ = repo.SaveModel(ctx, &Model{ProviderID: p.ID, Name: "gpt-4o", ModelID: "gpt-4o", Capability: CapChat, IsDefault: true, OpenToAll: true, Active: true})

	mod := NewModule(repo, nil, nil, nil, fakeDocFetcher{}, nil, nil)
	var chunks []string
	resp, err := mod.Search.ComposeStream(ctx, &platform.AuthContext{UserID: 1, IsSuperAdmin: true}, ComposeRequest{
		Title:       "Инструкция",
		Goal:        "Составить памятку инженеру",
		SourceDocID: []int64{1},
	}, func(delta string) error {
		chunks = append(chunks, delta)
		return nil
	})
	if err != nil {
		t.Fatalf("compose stream failed: %v", err)
	}
	if !slices.Equal(chunks, []string{"# Черновик", " акта"}) || resp.Content != "# Черновик акта" {
		t.Fatalf("unexpected streamed output: chunks=%v content=%q", chunks, resp.Content)
	}
}

func TestMetadataChangeInvalidatesChunkHash(t *testing.T) {
	item := platform.IndexItem{Title: "Guide", Content: "Text", Metadata: map[string]string{"visibility": "internal"}}
	before := ChunkItem(item, 1000)[0].ContentHash
	item.Metadata = map[string]string{"visibility": "admins"}
	after := ChunkItem(item, 1000)[0].ContentHash
	if before == after {
		t.Fatal("visibility change did not invalidate index hash")
	}
}

func TestGatewayTranscribeAudio(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if r.FormValue("model") != "whisper-1" || r.FormValue("language") != "ru" {
			t.Fatalf("unexpected form values: model=%q lang=%q", r.FormValue("model"), r.FormValue("language"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"Здравствуйте, нужен выезд мастера"}`))
	}))
	defer srv.Close()

	repo := newMemAIRepo()
	p := &Provider{Name: "OpenAI", Kind: ProviderOpenAI, BaseURL: srv.URL, APIKey: "sk-test", Active: true, IsDefault: true}
	if err := repo.SaveProvider(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	m := &Model{ProviderID: p.ID, Name: "Whisper", ModelID: "whisper-1", Capability: CapSTT, IsDefault: true, OpenToAll: true, Active: true}
	if err := repo.SaveModel(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	gw := NewGateway(repo, nil, nil)
	got, err := gw.TranscribeAudio(context.Background(), nil, 0, strings.NewReader("fake-mp3-bytes"), "call.mp3", "ru")
	if err != nil {
		t.Fatalf("TranscribeAudio failed: %v", err)
	}
	if got != "Здравствуйте, нужен выезд мастера" {
		t.Fatalf("unexpected transcript: %q", got)
	}
}

type denyModelAccess struct{}

func (denyModelAccess) CanAccessModel(_ context.Context, _ *platform.AuthContext, _ int64, openToAll bool) (bool, error) {
	return openToAll, nil
}

func TestGatewayTranscribeAudioAccessDenied(t *testing.T) {
	repo := newMemAIRepo()
	p := &Provider{Name: "OpenAI", Kind: ProviderOpenAI, BaseURL: "http://127.0.0.1:1", APIKey: "sk-test", Active: true, IsDefault: true}
	if err := repo.SaveProvider(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	m := &Model{ProviderID: p.ID, Name: "Whisper", ModelID: "whisper-1", Capability: CapSTT, IsDefault: true, OpenToAll: false, Active: true}
	if err := repo.SaveModel(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	gw := NewGateway(repo, denyModelAccess{}, nil)
	_, err := gw.TranscribeAudio(context.Background(), &platform.AuthContext{UserID: 42}, 0, strings.NewReader("fake"), "call.mp3", "ru")
	if err == nil {
		t.Fatal("expected forbidden error, got nil")
	}
	if !strings.Contains(err.Error(), "Нет доступа") {
		t.Fatalf("expected forbidden error, got %v", err)
	}
}
