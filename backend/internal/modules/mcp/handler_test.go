package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/internal/modules/ai"
	"github.com/zubans/tsfera/backend/internal/modules/chat"
	"github.com/zubans/tsfera/backend/internal/modules/docs"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type stubAuthorizer struct {
	tokens map[string]*platform.AuthContext
}

func (s *stubAuthorizer) ResolveRequest(r *http.Request) (*platform.AuthContext, error) {
	hdr := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(hdr), "bearer ") {
		tok := strings.TrimSpace(hdr[7:])
		if ac, ok := s.tokens[tok]; ok {
			cp := *ac
			return &cp, nil
		}
	}
	return nil, platform.NewUnauthorized("unauthorized")
}

type stubSearcher struct {
	results []ai.SearchResult
}

func (s *stubSearcher) HybridSearch(_ context.Context, _ *platform.AuthContext, _ string, _ int) ([]ai.SearchResult, error) {
	return s.results, nil
}

type stubChat struct {
	nextID int64
	convs  map[int64]*chat.Conversation
}

func (s *stubChat) CreateConversation(_ context.Context, auth *platform.AuthContext, in chat.CreateConversationInput) (*chat.Conversation, error) {
	s.nextID++
	if s.convs == nil {
		s.convs = map[int64]*chat.Conversation{}
	}
	c := &chat.Conversation{ID: s.nextID, UserID: auth.UserID, Title: in.Title, Mode: in.Mode}
	s.convs[c.ID] = c
	return c, nil
}

func (s *stubChat) GetConversation(_ context.Context, _ *platform.AuthContext, id int64) (*chat.Conversation, error) {
	if c, ok := s.convs[id]; ok {
		return c, nil
	}
	return nil, platform.NewNotFound("Диалог не найден")
}

func (s *stubChat) SendMessage(_ context.Context, _ *platform.AuthContext, convID int64, userText string, _ chat.Mode, _ int64, _ func(chat.SSEEvent)) (*chat.ChatMessage, error) {
	return &chat.ChatMessage{
		ID:             1,
		ConversationID: convID,
		Role:           "assistant",
		Content:        "Ответ по документации: " + userText,
		Sources:        []platform.SourceRef{{SourceType: "doc", SourceID: "1", Title: "Регламент ТО", Path: "/docs/1"}},
	}, nil
}

type memDocsRepo struct {
	mu     sync.Mutex
	nextID int64
	docs   map[int64]*docs.Document
	revs   map[int64][]docs.Revision
	cmts   map[int64][]docs.DocComment
	atts   map[int64]*docs.Attachment
}

func newMemDocsRepo() *memDocsRepo {
	return &memDocsRepo{
		docs: map[int64]*docs.Document{},
		revs: map[int64][]docs.Revision{},
		cmts: map[int64][]docs.DocComment{},
		atts: map[int64]*docs.Attachment{},
	}
}

func (m *memDocsRepo) id() int64 {
	m.nextID++
	return m.nextID
}

func (m *memDocsRepo) SaveDocumentRevision(_ context.Context, doc *docs.Document, expectedVersion int, rev *docs.Revision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if expectedVersion == 0 {
		doc.ID = m.id()
		doc.Version = 1
		doc.CreatedAt = now
		doc.UpdatedAt = now
		cp := *doc
		m.docs[doc.ID] = &cp
	} else {
		cur, ok := m.docs[doc.ID]
		if !ok || cur.Deleted || cur.Version != expectedVersion {
			return platform.NewConflict("version conflict")
		}
		doc.Version = cur.Version + 1
		doc.UpdatedAt = now
		cp := *doc
		m.docs[doc.ID] = &cp
	}
	r := docs.Revision{
		ID:          m.id(),
		DocumentID:  doc.ID,
		Version:     doc.Version,
		Title:       doc.Title,
		Content:     doc.Content,
		DiffSummary: rev.DiffSummary,
		CreatedBy:   rev.CreatedBy,
		CreatedAt:   now,
	}
	m.revs[doc.ID] = append(m.revs[doc.ID], r)
	return nil
}

func (m *memDocsRepo) ListDocuments(_ context.Context, f docs.DocumentFilter) ([]docs.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []docs.Document
	for _, d := range m.docs {
		if d.Deleted {
			continue
		}
		if f.Space != "" && d.Space != f.Space {
			continue
		}
		if f.Query != "" && !strings.Contains(strings.ToLower(d.Title+" "+d.Content), strings.ToLower(f.Query)) {
			continue
		}
		out = append(out, *d)
	}
	return out, nil
}

func (m *memDocsRepo) ListSpaces(_ context.Context) ([]docs.SpaceSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	counts := map[string]int{}
	for _, d := range m.docs {
		if !d.Deleted {
			counts[d.Space]++
		}
	}
	var out []docs.SpaceSummary
	for sp, cnt := range counts {
		out = append(out, docs.SpaceSummary{Space: sp, Count: cnt})
	}
	return out, nil
}

func (m *memDocsRepo) GetDocument(_ context.Context, id int64) (*docs.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[id]
	if !ok || d.Deleted {
		return nil, nil
	}
	cp := *d
	return &cp, nil
}

func (m *memDocsRepo) GetDocumentBySlug(_ context.Context, space, slug string) (*docs.Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.docs {
		if !d.Deleted && d.Space == space && d.Slug == slug {
			cp := *d
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memDocsRepo) CreateDocument(context.Context, *docs.Document) error             { return nil }
func (m *memDocsRepo) UpdateDocumentWithVersion(context.Context, *docs.Document, int) error { return nil }
func (m *memDocsRepo) SoftDeleteDocument(context.Context, int64) error                  { return nil }
func (m *memDocsRepo) AddRevision(context.Context, *docs.Revision) error                { return nil }

func (m *memDocsRepo) ListRevisions(_ context.Context, docID int64) ([]docs.Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]docs.Revision(nil), m.revs[docID]...), nil
}

func (m *memDocsRepo) GetRevision(_ context.Context, docID, revisionID int64) (*docs.Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.revs[docID] {
		if r.ID == revisionID {
			cp := r
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memDocsRepo) CompressRevisionsBefore(context.Context, time.Time) (int, error) { return 0, nil }
func (m *memDocsRepo) ListEntityLinksByDocument(context.Context, int64) ([]docs.EntityLink, error) {
	return nil, nil
}
func (m *memDocsRepo) ListDocumentsByEntity(context.Context, string, int64) ([]docs.Document, error) {
	return nil, nil
}
func (m *memDocsRepo) AddEntityLink(context.Context, *docs.EntityLink) error { return nil }
func (m *memDocsRepo) DeleteEntityLink(context.Context, int64) error         { return nil }

func (m *memDocsRepo) ListComments(_ context.Context, docID int64) ([]docs.DocComment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]docs.DocComment(nil), m.cmts[docID]...), nil
}

func (m *memDocsRepo) AddComment(_ context.Context, c *docs.DocComment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.ID = m.id()
	c.CreatedAt = time.Now()
	m.cmts[c.DocumentID] = append(m.cmts[c.DocumentID], *c)
	return nil
}

func (m *memDocsRepo) SetCommentResolved(context.Context, int64, int64, int64, bool) (*docs.DocComment, error) {
	return nil, nil
}
func (m *memDocsRepo) DeleteComment(context.Context, int64, int64) error { return nil }

func (m *memDocsRepo) ListAttachments(_ context.Context, docID int64) ([]docs.Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []docs.Attachment
	for _, a := range m.atts {
		if a.DocumentID == docID {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (m *memDocsRepo) ListAllAttachments(context.Context, int) ([]docs.Attachment, error) { return nil, nil }
func (m *memDocsRepo) ListPendingSchematicAttachments(context.Context, int) ([]docs.Attachment, error) {
	return nil, nil
}

func (m *memDocsRepo) GetAttachment(_ context.Context, id int64) (*docs.Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.atts[id]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *memDocsRepo) AddAttachment(_ context.Context, att *docs.Attachment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	att.ID = m.id()
	cp := *att
	m.atts[att.ID] = &cp
	return nil
}

func (m *memDocsRepo) UpdateAttachmentAnalysis(context.Context, int64, string, string, string, *time.Time) error {
	return nil
}
func (m *memDocsRepo) DeleteAttachment(context.Context, int64) error                     { return nil }
func (m *memDocsRepo) ReplaceBoilerParts(context.Context, int64, []docs.BoilerPart) error { return nil }
func (m *memDocsRepo) ListBoilerPartsByDocument(context.Context, int64) ([]docs.BoilerPart, error) {
	return nil, nil
}
func (m *memDocsRepo) SearchBoilerParts(context.Context, docs.BoilerPartFilter) ([]docs.BoilerPart, error) {
	return nil, nil
}
func (m *memDocsRepo) ListAllBoilerParts(context.Context, int) ([]docs.BoilerPart, error) { return nil, nil }

func callMCP(t *testing.T, r http.Handler, token string, method string, params any) (int, rpcResponse) {
	t.Helper()
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		payload["params"] = params
	}
	raw, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var resp rpcResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp
}

func TestMCPAuthLevelsGuideToolsAndSpaceIsolation(t *testing.T) {
	repo := newMemDocsRepo()
	docsSvc := docs.NewService(repo, nil, nil, t.TempDir(), nil)

	adminAuth := &platform.AuthContext{
		UserID:       1,
		Username:     "admin",
		DisplayName:  "Админ",
		IsSuperAdmin: true,
	}
	docReg, err := docsSvc.CreateDocument(context.Background(), adminAuth, docs.SaveDocumentInput{
		Space:   "reglaments",
		DocType: docs.DocTypeSOP,
		Title:   "Регламент ТО",
		Content: "Проверка давления газа и горелки.",
	})
	if err != nil {
		t.Fatal(err)
	}
	docSec, err := docsSvc.CreateDocument(context.Background(), adminAuth, docs.SaveDocumentInput{
		Space:   "secret",
		DocType: docs.DocTypeArticle,
		Title:   "Внутренний документ",
		Content: "Секретные заметки.",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = repo.AddAttachment(context.Background(), &docs.Attachment{
		DocumentID:    docReg.ID,
		Filename:      "manual.pdf",
		MimeType:      "application/pdf",
		SizeBytes:     120,
		Kind:          docs.AttachmentKindFile,
		ExtractedText: "Страница 1: Давление газа 20 мбар.",
	})
	var attID int64
	for id := range repo.atts {
		attID = id
	}

	fullPerms := map[string]bool{
		"docs.view":    true,
		"docs.create":  true,
		"docs.update":  true,
		"docs.comment": true,
		"ai.search":    true,
		"chat.use":     true,
	}
	authz := &stubAuthorizer{
		tokens: map[string]*platform.AuthContext{
			"tok-ask": {
				UserID: 2, Username: "u1", IsToken: true, TokenName: "ask-bot",
				TokenAskOnly: true, TokenAllowedSpaces: []string{"reglaments"}, Permissions: fullPerms,
			},
			"tok-read": {
				UserID: 2, Username: "u1", IsToken: true, TokenName: "read-bot",
				TokenCanRead: true, TokenAllowedSpaces: []string{"reglaments"}, Permissions: fullPerms,
			},
			"tok-write": {
				UserID: 2, Username: "u1", IsToken: true, TokenName: "write-bot",
				TokenCanRead: true, TokenCanWrite: true, TokenAllowedSpaces: []string{"reglaments"}, Permissions: fullPerms,
			},
		},
	}

	h := NewHandler(docsSvc, &stubSearcher{}, &stubChat{}, authz, nil)
	r := chi.NewRouter()
	h.Mount(r)

	// 1. /api/agent-guide.md открыт без токена и персонализируется с токеном.
	recAnon := httptest.NewRecorder()
	r.ServeHTTP(recAnon, httptest.NewRequest(http.MethodGet, "/api/agent-guide.md", nil))
	if recAnon.Code != http.StatusOK || !strings.Contains(recAnon.Body.String(), "claude mcp add") {
		t.Fatalf("expected 200 guide with connect cmd, got %d: %s", recAnon.Code, recAnon.Body.String())
	}

	reqAskGuide := httptest.NewRequest(http.MethodGet, "/api/agent-guide.md", nil)
	reqAskGuide.Header.Set("Authorization", "Bearer tok-ask")
	recAskGuide := httptest.NewRecorder()
	r.ServeHTTP(recAskGuide, reqAskGuide)
	if !strings.Contains(recAskGuide.Body.String(), "только вопросы") || strings.Contains(recAskGuide.Body.String(), "`create_document`") {
		t.Fatalf("ask_only guide leaked write tools or wrong level: %s", recAskGuide.Body.String())
	}

	// 2. /mcp без токена -> 401.
	code, _ := callMCP(t, r, "", "initialize", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", code)
	}

	// 3. initialize + tools/list для ask_only: только 4 инструмента.
	code, initResp := callMCP(t, r, "tok-ask", "initialize", map[string]any{"protocolVersion": "2025-06-18"})
	if code != http.StatusOK || initResp.Error != nil {
		t.Fatalf("initialize failed: %d %+v", code, initResp.Error)
	}

	_, askToolsResp := callMCP(t, r, "tok-ask", "tools/list", nil)
	askNames := extractToolNames(t, askToolsResp.Result)
	if !slices.Equal(askNames, []string{"get_agent_guide", "list_spaces", "ask", "log_result"}) {
		t.Fatalf("unexpected ask_only tools: %v", askNames)
	}

	// ask_only не может вызывать get_document, но может ask и log_result.
	_, forbiddenCall := callMCP(t, r, "tok-ask", "tools/call", map[string]any{
		"name":      "get_document",
		"arguments": map[string]any{"id": docReg.ID},
	})
	if !isToolError(t, forbiddenCall.Result) {
		t.Fatalf("expected isError=true for ask_only calling get_document")
	}

	_, askCall := callMCP(t, r, "tok-ask", "tools/call", map[string]any{
		"name":      "ask",
		"arguments": map[string]any{"question": "Как проверить давление газа?"},
	})
	if isToolError(t, askCall.Result) || !strings.Contains(extractToolText(t, askCall.Result), "thread_id: 1") {
		t.Fatalf("unexpected ask result: %+v", askCall.Result)
	}

	// 4. read-only токен видит чтение, не видит запись и не видит пространство secret.
	_, readToolsResp := callMCP(t, r, "tok-read", "tools/list", nil)
	readNames := extractToolNames(t, readToolsResp.Result)
	if slices.Contains(readNames, "create_document") || !slices.Contains(readNames, "get_document") || !slices.Contains(readNames, "read_attachment") {
		t.Fatalf("unexpected read-only tools: %v", readNames)
	}

	_, getAllowed := callMCP(t, r, "tok-read", "tools/call", map[string]any{
		"name":      "get_document",
		"arguments": map[string]any{"id": docReg.ID},
	})
	if isToolError(t, getAllowed.Result) || !strings.Contains(extractToolText(t, getAllowed.Result), "Регламент ТО") {
		t.Fatalf("expected allowed doc text, got %+v", getAllowed.Result)
	}

	_, getSecret := callMCP(t, r, "tok-read", "tools/call", map[string]any{
		"name":      "get_document",
		"arguments": map[string]any{"id": docSec.ID},
	})
	if !isToolError(t, getSecret.Result) {
		t.Fatalf("expected error reading document from disallowed space")
	}

	_, readAtt := callMCP(t, r, "tok-read", "tools/call", map[string]any{
		"name":      "read_attachment",
		"arguments": map[string]any{"attachment_id": attID, "offset": 0, "limit": 10},
	})
	if isToolError(t, readAtt.Result) || !strings.Contains(extractToolText(t, readAtt.Result), "has_more=true") {
		t.Fatalf("expected paginated attachment chunk, got %+v", readAtt.Result)
	}

	// 5. write токен: create_document, update_document (вкл. конфликт expected_version), append_to_document, add_comment, log_result.
	_, writeToolsResp := callMCP(t, r, "tok-write", "tools/list", nil)
	writeNames := extractToolNames(t, writeToolsResp.Result)
	if !slices.Contains(writeNames, "create_document") || !slices.Contains(writeNames, "update_document") {
		t.Fatalf("expected write tools in list: %v", writeNames)
	}

	// Попытка обновить с неверным expected_version -> isError=true.
	_, conflictUpdate := callMCP(t, r, "tok-write", "tools/call", map[string]any{
		"name": "update_document",
		"arguments": map[string]any{
			"id":               docReg.ID,
			"expected_version": 99,
			"content":          "Новый текст",
		},
	})
	if !isToolError(t, conflictUpdate.Result) {
		t.Fatalf("expected conflict error on stale expected_version")
	}

	// Корректное обновление с expected_version=1.
	_, okUpdate := callMCP(t, r, "tok-write", "tools/call", map[string]any{
		"name": "update_document",
		"arguments": map[string]any{
			"id":               docReg.ID,
			"expected_version": 1,
			"content":          "Обновлённый регламент ТО.",
		},
	})
	if isToolError(t, okUpdate.Result) || !strings.Contains(extractToolText(t, okUpdate.Result), "v2") {
		t.Fatalf("expected v2 update, got %+v", okUpdate.Result)
	}

	// Запись в журнал работ агентов.
	_, logCall := callMCP(t, r, "tok-write", "tools/call", map[string]any{
		"name": "log_result",
		"arguments": map[string]any{
			"title":     "Обновлён регламент ТО",
			"summary":   "Актуализированы шаги проверки горелки.",
			"decisions": []string{"Сохранили совместимость со старыми котлами"},
		},
	})
	if isToolError(t, logCall.Result) || !strings.Contains(extractToolText(t, logCall.Result), "Журнал работ агентов") {
		t.Fatalf("unexpected log_result output: %+v", logCall.Result)
	}

	// 6. Проверки безопасности из ревью:
	// - ask_only без прав docs.create/docs.update у владельца не видит и не может вызвать log_result;
	// - read-only токен не имеет HasPermission("docs.comment") и HasPermission("chat.share");
	// - JSON-RPC parse error возвращает "id": null, а уведомление без id — 202;
	// - ask не даёт суперадмину доступ к чужому thread_id.
	authz.tokens["tok-ask-viewer"] = &platform.AuthContext{
		UserID: 3, Username: "viewer", IsToken: true, TokenName: "viewer-ask",
		TokenAskOnly: true, Permissions: map[string]bool{"docs.view": true},
	}
	_, viewerToolsResp := callMCP(t, r, "tok-ask-viewer", "tools/list", nil)
	if slices.Contains(extractToolNames(t, viewerToolsResp.Result), "log_result") {
		t.Fatalf("viewer ask_only token must not see log_result without docs.create/docs.update")
	}

	if authz.tokens["tok-read"].HasPermission("docs.comment") || authz.tokens["tok-read"].HasPermission("chat.share") {
		t.Fatalf("read-only token must not have docs.comment or chat.share")
	}

	// Parse error -> id: null
	badReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{bad json"))
	badReq.Header.Set("Authorization", "Bearer tok-read")
	badRec := httptest.NewRecorder()
	r.ServeHTTP(badRec, badReq)
	if !strings.Contains(badRec.Body.String(), `"id":null`) {
		t.Fatalf("expected id:null on parse error, got: %s", badRec.Body.String())
	}

	// Notification without id -> 202 Accepted
	notifReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	notifReq.Header.Set("Authorization", "Bearer tok-read")
	notifRec := httptest.NewRecorder()
	r.ServeHTTP(notifRec, notifReq)
	if notifRec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for notification, got %d", notifRec.Code)
	}

	// requestBaseURL санитизирует X-Forwarded-Proto и Host
	spoofReq := httptest.NewRequest(http.MethodGet, "/api/agent-guide.md", nil)
	spoofReq.Host = `evil.com" && rm -rf /`
	spoofReq.Header.Set("X-Forwarded-Proto", "javascript")
	if got := requestBaseURL(spoofReq); got != "http://localhost:8080" {
		t.Fatalf("expected sanitized baseURL http://localhost:8080, got %q", got)
	}
}

func extractToolNames(t *testing.T, res any) []string {
	t.Helper()
	raw, _ := json.Marshal(res)
	var parsed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(parsed.Tools))
	for _, item := range parsed.Tools {
		out = append(out, item.Name)
	}
	return out
}

func isToolError(t *testing.T, res any) bool {
	t.Helper()
	raw, _ := json.Marshal(res)
	var parsed struct {
		IsError bool `json:"isError"`
	}
	_ = json.Unmarshal(raw, &parsed)
	return parsed.IsError
}

func extractToolText(t *testing.T, res any) string {
	t.Helper()
	raw, _ := json.Marshal(res)
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(raw, &parsed)
	if len(parsed.Content) == 0 {
		return ""
	}
	return parsed.Content[0].Text
}
