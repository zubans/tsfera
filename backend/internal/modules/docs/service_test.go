package docs

import (
	"archive/zip"
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type memDocsRepo struct {
	mu      sync.Mutex
	nextID  int64
	docs    map[int64]*Document
	revs    map[int64]*Revision
	links   map[int64]*EntityLink
	cmts    map[int64]*DocComment
	atts    map[int64]*Attachment
	parts   map[int64]*BoilerPart
	nowFunc func() time.Time
}

func newMemDocsRepo(clock func() time.Time) *memDocsRepo {
	return &memDocsRepo{
		docs:    map[int64]*Document{},
		revs:    map[int64]*Revision{},
		links:   map[int64]*EntityLink{},
		cmts:    map[int64]*DocComment{},
		atts:    map[int64]*Attachment{},
		parts:   map[int64]*BoilerPart{},
		nowFunc: clock,
	}
}

func (m *memDocsRepo) id() int64 {
	m.nextID++
	return m.nextID
}

func (m *memDocsRepo) ListDocuments(_ context.Context, f DocumentFilter) ([]Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Document
	for _, d := range m.docs {
		if d.Deleted {
			continue
		}
		if f.Space != "" && d.Space != f.Space {
			continue
		}
		if f.Query != "" && !strings.Contains(strings.ToLower(d.Title+d.Content), strings.ToLower(f.Query)) {
			continue
		}
		out = append(out, *d)
	}
	return out, nil
}

func (m *memDocsRepo) ListSpaces(context.Context) ([]SpaceSummary, error) {
	return []SpaceSummary{{Space: "reglaments", Count: 1}}, nil
}

func (m *memDocsRepo) GetDocument(_ context.Context, id int64) (*Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.docs[id]
	if !ok || d.Deleted {
		return nil, nil
	}
	cp := *d
	return &cp, nil
}

func (m *memDocsRepo) GetDocumentBySlug(_ context.Context, space, slug string) (*Document, error) {
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

func (m *memDocsRepo) CreateDocument(_ context.Context, doc *Document) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc.ID = m.id()
	doc.CreatedAt = m.nowFunc()
	doc.UpdatedAt = doc.CreatedAt
	cp := *doc
	m.docs[doc.ID] = &cp
	return nil
}

func (m *memDocsRepo) UpdateDocumentWithVersion(_ context.Context, doc *Document, expectedVersion int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cur, ok := m.docs[doc.ID]
	if !ok || cur.Deleted || cur.Version != expectedVersion {
		return platform.NewConflict("version conflict")
	}
	doc.Version = cur.Version + 1
	doc.UpdatedAt = m.nowFunc()
	cp := *doc
	m.docs[doc.ID] = &cp
	return nil
}

func (m *memDocsRepo) SoftDeleteDocument(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if d, ok := m.docs[id]; ok {
		d.Deleted = true
	}
	return nil
}

func (m *memDocsRepo) AddRevision(_ context.Context, rev *Revision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rev.ID = m.id()
	rev.CreatedAt = m.nowFunc()
	cp := *rev
	m.revs[rev.ID] = &cp
	return nil
}

func (m *memDocsRepo) ListRevisions(_ context.Context, docID int64) ([]Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Revision
	for _, r := range m.revs {
		if r.DocumentID == docID {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *memDocsRepo) GetRevision(_ context.Context, docID, revisionID int64) (*Revision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.revs[revisionID]
	if !ok || r.DocumentID != docID {
		return nil, nil
	}
	cp := *r
	if cp.Compressed {
		plain, err := gunzipBytes([]byte(cp.Content))
		if err == nil {
			cp.Content = plain
		}
	}
	return &cp, nil
}

func (m *memDocsRepo) CompressRevisionsBefore(_ context.Context, cutoff time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, r := range m.revs {
		if !r.Compressed && r.CreatedAt.Before(cutoff) {
			gz, err := gzipString(r.Content)
			if err == nil {
				r.Content = string(gz)
				r.Compressed = true
				count++
			}
		}
	}
	return count, nil
}

func (m *memDocsRepo) ListEntityLinksByDocument(_ context.Context, docID int64) ([]EntityLink, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []EntityLink
	for _, l := range m.links {
		if l.DocumentID == docID {
			out = append(out, *l)
		}
	}
	return out, nil
}

func (m *memDocsRepo) ListDocumentsByEntity(_ context.Context, entityType string, entityID int64) ([]Document, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Document
	for _, l := range m.links {
		if l.EntityType == entityType && l.EntityID == entityID {
			if d, ok := m.docs[l.DocumentID]; ok && !d.Deleted {
				out = append(out, *d)
			}
		}
	}
	return out, nil
}

func (m *memDocsRepo) AddEntityLink(_ context.Context, link *EntityLink) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	link.ID = m.id()
	cp := *link
	m.links[link.ID] = &cp
	return nil
}

func (m *memDocsRepo) DeleteEntityLink(_ context.Context, linkID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.links, linkID)
	return nil
}

func (m *memDocsRepo) ListComments(_ context.Context, docID int64) ([]DocComment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []DocComment
	for _, c := range m.cmts {
		if c.DocumentID == docID {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (m *memDocsRepo) AddComment(_ context.Context, c *DocComment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.ID = m.id()
	cp := *c
	m.cmts[c.ID] = &cp
	return nil
}

func (m *memDocsRepo) SetCommentResolved(_ context.Context, docID, commentID, userID int64, resolved bool) (*DocComment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.cmts[commentID]
	if !ok || c.DocumentID != docID {
		return nil, platform.NewNotFound("not found")
	}
	c.Resolved = resolved
	if resolved {
		c.ResolvedBy = &userID
	} else {
		c.ResolvedBy = nil
	}
	cp := *c
	return &cp, nil
}

func (m *memDocsRepo) DeleteComment(_ context.Context, _, commentID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cmts, commentID)
	return nil
}

func (m *memDocsRepo) ListAttachments(_ context.Context, docID int64) ([]Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Attachment
	for _, a := range m.atts {
		if a.DocumentID == docID {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (m *memDocsRepo) ListAllAttachments(context.Context, int) ([]Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Attachment
	for _, a := range m.atts {
		if a.ExtractedText != "" {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (m *memDocsRepo) ListPendingSchematicAttachments(_ context.Context, limit int) ([]Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Attachment
	for _, a := range m.atts {
		if a.AnalysisStatus == AnalysisStatusPending {
			out = append(out, *a)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *memDocsRepo) UpdateAttachmentAnalysis(_ context.Context, id int64, status, extractedText, errText string, analyzedAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.atts[id]
	if !ok {
		return platform.NewNotFound("Вложение не найдено")
	}
	a.AnalysisStatus = status
	if extractedText != "" {
		a.ExtractedText = extractedText
	}
	a.AnalysisError = errText
	a.AnalyzedAt = analyzedAt
	return nil
}

func (m *memDocsRepo) GetAttachment(_ context.Context, id int64) (*Attachment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.atts[id]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *memDocsRepo) AddAttachment(_ context.Context, att *Attachment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	att.ID = m.id()
	cp := *att
	m.atts[att.ID] = &cp
	return nil
}

func (m *memDocsRepo) DeleteAttachment(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.atts, id)
	return nil
}

func (m *memDocsRepo) ReplaceBoilerParts(_ context.Context, documentID int64, parts []BoilerPart) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, p := range m.parts {
		if p.DocumentID == documentID {
			delete(m.parts, id)
		}
	}
	for i := range parts {
		cp := parts[i]
		cp.ID = m.id()
		cp.DocumentID = documentID
		cp.CreatedAt = m.nowFunc()
		if d, ok := m.docs[documentID]; ok {
			cp.DocumentTitle = d.Title
		}
		m.parts[cp.ID] = &cp
		parts[i] = cp
	}
	return nil
}

func (m *memDocsRepo) ListBoilerPartsByDocument(_ context.Context, documentID int64) ([]BoilerPart, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []BoilerPart
	for _, p := range m.parts {
		if p.DocumentID == documentID {
			cp := *p
			if d, ok := m.docs[p.DocumentID]; ok {
				cp.DocumentTitle = d.Title
			}
			out = append(out, cp)
		}
	}
	return out, nil
}

func (m *memDocsRepo) SearchBoilerParts(_ context.Context, f BoilerPartFilter) ([]BoilerPart, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []BoilerPart
	for _, p := range m.parts {
		d, ok := m.docs[p.DocumentID]
		if !ok || d.Deleted {
			continue
		}
		if f.DocumentID > 0 && p.DocumentID != f.DocumentID {
			continue
		}
		if f.Brand != "" && !strings.Contains(strings.ToLower(p.Brand), strings.ToLower(f.Brand)) {
			continue
		}
		if f.Model != "" && !strings.Contains(strings.ToLower(p.BoilerModel+" "+d.Title), strings.ToLower(normalizeBlueprintText(f.Model))) {
			continue
		}
		if f.PartNumber != "" && !strings.Contains(strings.ToLower(p.PartNumber+" "+p.ReplacedBy), strings.ToLower(f.PartNumber)) {
			continue
		}
		if f.Assembly != "" && !strings.Contains(strings.ToLower(p.Assembly), strings.ToLower(f.Assembly)) {
			continue
		}
		if f.Query != "" {
			hay := strings.ToLower(p.Brand + " " + p.BoilerModel + " " + p.Assembly + " " + p.PartNumber + " " + p.Name + " " + p.Notes + " " + p.ReplacedBy)
			matched := true
			for _, tok := range strings.Fields(strings.ToLower(normalizeBlueprintText(f.Query))) {
				if !strings.Contains(hay, tok) {
					matched = false
					break
				}
			}
			if !matched {
				continue
			}
		}
		cp := *p
		cp.DocumentTitle = d.Title
		out = append(out, cp)
		if f.Limit > 0 && len(out) >= f.Limit {
			break
		}
	}
	return out, nil
}

func (m *memDocsRepo) ListAllBoilerParts(_ context.Context, limit int) ([]BoilerPart, error) {
	return m.SearchBoilerParts(context.Background(), BoilerPartFilter{Limit: limit})
}

type fakeEntityResolver struct{}

func (fakeEntityResolver) ResolveEntity(_ context.Context, _ *platform.AuthContext, entityType string, entityID int64) (*platform.EntitySummary, error) {
	return &platform.EntitySummary{
		EntityType: entityType,
		EntityID:   entityID,
		Title:      "Котёл Baxi #1",
		Path:       "/crm/contractors/1",
	}, nil
}

type staticAuth struct{ ac *platform.AuthContext }

func (a staticAuth) ResolveRequest(*http.Request) (*platform.AuthContext, error) {
	return a.ac, nil
}

func TestOptimisticLockingConflictAndRevisionGzipRestore(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	repo := newMemDocsRepo(clock)
	svc := NewService(repo, fakeEntityResolver{}, platform.NewInMemoryEventBus(), t.TempDir(), nil)
	svc.clock = clock
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}

	doc, err := svc.CreateDocument(ctx, admin, SaveDocumentInput{
		Space:   "manuals",
		Title:   "Инструкция Baxi Eco Four",
		Content: "# Раздел 1\nИсходный текст.",
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if len(doc.Outline) != 1 || doc.Outline[0].Anchor != "раздел-1" {
		t.Fatalf("unexpected outline: %+v", doc.Outline)
	}

	// Попытка обновления с неактуальной версией -> 409 Conflict.
	if _, err := svc.UpdateDocument(ctx, admin, doc.ID, SaveDocumentInput{
		Title:           doc.Title,
		Content:         "Перетёрто",
		ExpectedVersion: 99,
	}); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	updated, err := svc.UpdateDocument(ctx, admin, doc.ID, SaveDocumentInput{
		Title:           doc.Title,
		Content:         "# Раздел 1\nНовая редакция.",
		ExpectedVersion: 1,
	})
	if err != nil || updated.Version != 2 {
		t.Fatalf("expected v2, got %v (err=%v)", updated, err)
	}

	// Перематываем время на +25 часов и сжимаем старые ревизии в gzip.
	now = now.Add(25 * time.Hour)
	compressed, err := svc.CompressOldRevisions(ctx)
	if err != nil || compressed != 2 {
		t.Fatalf("expected 2 compressed revisions, got %d (err=%v)", compressed, err)
	}

	revs, _ := svc.ListRevisions(ctx, admin, doc.ID)
	var v1RevID int64
	for _, r := range revs {
		if r.Version == 1 {
			v1RevID = r.ID
		}
	}
	restored, err := svc.RestoreRevision(ctx, admin, doc.ID, v1RevID)
	if err != nil {
		t.Fatalf("restore revision failed: %v", err)
	}
	if restored.Version != 3 || !strings.Contains(restored.Content, "Исходный текст.") {
		t.Fatalf("unexpected restored doc: v=%d content=%q", restored.Version, restored.Content)
	}
}

func buildTestZip(files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func TestNativeDocxAndXlsxConverters(t *testing.T) {
	docxBytes := buildTestZip(map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Паспорт котла</w:t></w:r></w:p>
    <w:p><w:pPr><w:numPr/></w:pPr><w:r><w:t>Проверить тягу</w:t></w:r></w:p>
    <w:tbl>
      <w:tr><w:tc><w:p><w:r><w:t>Параметр</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>Норма</w:t></w:r></w:p></w:tc></w:tr>
      <w:tr><w:tc><w:p><w:r><w:t>Давление</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>1.5 бар</w:t></w:r></w:p></w:tc></w:tr>
    </w:tbl>
  </w:body>
</w:document>`,
	})
	md, err := ConvertDocxToMarkdown(docxBytes)
	if err != nil {
		t.Fatalf("convert docx: %v", err)
	}
	if !strings.Contains(md, "# Паспорт котла") || !strings.Contains(md, "- Проверить тягу") || !strings.Contains(md, "| Давление | 1.5 бар |") {
		t.Fatalf("unexpected converted markdown:\n%s", md)
	}

	xlsxBytes := buildTestZip(map[string]string{
		"xl/sharedStrings.xml": `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <si><t>Услуга</t></si>
  <si><t>ТО котла</t></si>
</sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row><c t="s"><v>0</v></c><c><v>Цена</v></c></row>
    <row><c t="s"><v>1</v></c><c><v>6500</v></c></row>
  </sheetData>
</worksheet>`,
	})
	txt, err := ExtractXlsxText(xlsxBytes)
	if err != nil {
		t.Fatalf("extract xlsx: %v", err)
	}
	if !strings.Contains(txt, "| ТО котла | 6500 |") {
		t.Fatalf("unexpected xlsx output:\n%s", txt)
	}
}

func TestInlineCommentsEntityLinksAndAttachmentGuard(t *testing.T) {
	storage := t.TempDir()
	repo := newMemDocsRepo(time.Now)
	mod := NewModule(repo, staticAuth{ac: &platform.AuthContext{UserID: 1, IsSuperAdmin: true}}, fakeEntityResolver{}, nil, storage, nil)
	ctx := context.Background()

	engAuth := &platform.AuthContext{
		UserID:      10,
		DisplayName: "Инженер Пётр",
		Permissions: map[string]bool{"docs.view": true, "docs.comment": true, "crm.application.view": true},
	}
	dispOnlyDoc, _ := mod.Service.CreateDocument(ctx, &platform.AuthContext{UserID: 1, IsSuperAdmin: true}, SaveDocumentInput{
		Space:      "internal",
		Title:      "Скрипт диспетчера",
		Content:    "Текст с важной фразой о давлении газа.",
		Visibility: VisibilityDispatchers,
	})
	if _, err := mod.Service.GetDocument(ctx, engAuth, dispOnlyDoc.ID); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("expected engineer to be denied dispatcher-only doc, got %v", err)
	}

	pubDoc, _ := mod.Service.CreateDocument(ctx, &platform.AuthContext{UserID: 1, IsSuperAdmin: true}, SaveDocumentInput{
		Space:      "manuals",
		Title:      "Общий регламент",
		Content:    "Перед запуском проверить давление 1.5 бар.",
		Visibility: VisibilityEngineers,
	})

	// Цитата отсутствует в документе -> ошибка валидации.
	if _, err := mod.Service.AddComment(ctx, engAuth, pubDoc.ID, AddCommentInput{
		Body:  "Уточнить",
		Quote: "несуществующая фраза",
	}); !errors.Is(err, platform.ErrValidation) {
		t.Fatalf("expected quote validation error, got %v", err)
	}

	cmt, err := mod.Service.AddComment(ctx, engAuth, pubDoc.ID, AddCommentInput{
		Body:  "Иногда требуется 1.8 бар для двухконтурных",
		Quote: "давление 1.5 бар",
	})
	if err != nil || cmt.AuthorName != "Инженер Пётр" {
		t.Fatalf("unexpected comment result: %+v err=%v", cmt, err)
	}
	resCmt, err := mod.Service.ResolveComment(ctx, engAuth, pubDoc.ID, cmt.ID, true)
	if err != nil || !resCmt.Resolved {
		t.Fatalf("resolve failed: %+v err=%v", resCmt, err)
	}

	// Привязка к CRM-сущности и выгрузка context.md + безопасное сохранение вложения.
	_, err = mod.Service.LinkEntity(ctx, engAuth, pubDoc.ID, "contractor", 1, "Связанный клиент")
	if err != nil {
		t.Fatalf("link entity: %v", err)
	}
	att, err := mod.Service.SaveAttachment(ctx, engAuth, pubDoc.ID, "../../etc/passwd.txt", "text/plain", []byte("содержимое памятки"))
	if err != nil {
		t.Fatalf("save attachment: %v", err)
	}
	if !strings.HasPrefix(att.StoragePath, filepath.Clean(storage)+string(filepath.Separator)) {
		t.Fatalf("path traversal check failed: %s", att.StoragePath)
	}
	if att.Filename != "passwd.txt" {
		t.Fatalf("expected sanitized basename, got %s", att.Filename)
	}

	ctxMD, err := mod.Service.ExportContextMarkdown(ctx, engAuth, "", pubDoc.ID)
	if err != nil || !strings.Contains(ctxMD, "Котёл Baxi #1") || !strings.Contains(ctxMD, "содержимое памятки") {
		t.Fatalf("unexpected context.md:\n%s", ctxMD)
	}

	// HTTP-проверка эндпоинта context.md.
	r := chi.NewRouter()
	mod.handler.auth = staticAuth{ac: engAuth}
	mod.MountHTTP(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/docs/context.md?document_id="+json.Number("1").String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on doc #1 via HTTP for engineer, got %d", rec.Code)
	}
}

func (m *memDocsRepo) SaveDocumentRevision(ctx context.Context, doc *Document, expected int, rev *Revision) error {
	var err error
	if expected == 0 {
		err = m.CreateDocument(ctx, doc)
	} else {
		err = m.UpdateDocumentWithVersion(ctx, doc, expected)
	}
	if err != nil {
		return err
	}
	rev.DocumentID, rev.Version, rev.Title, rev.Content = doc.ID, doc.Version, doc.Title, doc.Content
	return m.AddRevision(ctx, rev)
}

func buildTestPdf(streams []struct {
	dict     string
	body     []byte
	compress bool
}) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	for idx, s := range streams {
		data := s.body
		dict := s.dict
		if s.compress {
			var zbuf bytes.Buffer
			zw := zlib.NewWriter(&zbuf)
			_, _ = zw.Write(data)
			_ = zw.Close()
			data = zbuf.Bytes()
			dict += " /Filter /FlateDecode"
		}
		fmt.Fprintf(&buf, "%d 0 obj\n<< /Length %d %s >>\nstream\n", idx+1, len(data), dict)
		buf.Write(data)
		buf.WriteString("\nendstream\nendobj\n")
	}
	buf.WriteString("%%EOF\n")
	return buf.Bytes()
}

func withNativePdfOnly(t *testing.T) {
	t.Helper()
	orig := pdfTextLookPath
	pdfTextLookPath = func(string) (string, error) {
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { pdfTextLookPath = orig })
}

func TestPdfConverterNative(t *testing.T) {
	withNativePdfOnly(t)
	ctx := context.Background()

	t.Run("plain and multipage pdf", func(t *testing.T) {
		pdfBytes := buildTestPdf([]struct {
			dict     string
			body     []byte
			compress bool
		}{
			{body: []byte("BT /F1 12 Tf (Boiler manual Baxi) Tj T* [(Pressure: ) -200 (1.5 bar)] TJ ET")},
			{body: []byte("BT /F1 12 Tf (Page 2 checklist) Tj ET"), compress: true},
		})
		md, err := ConvertPdfToMarkdown(ctx, pdfBytes)
		if err != nil {
			t.Fatalf("convert pdf failed: %v", err)
		}
		if !strings.Contains(md, "Boiler manual Baxi") || !strings.Contains(md, "Pressure:  1.5 bar") && !strings.Contains(md, "Pressure: 1.5 bar") {
			t.Fatalf("unexpected page 1 text:\n%s", md)
		}
		if !strings.Contains(md, "---") || !strings.Contains(md, "Page 2 checklist") {
			t.Fatalf("expected page separator and page 2 text:\n%s", md)
		}
	})

	t.Run("compressed cyrillic with ToUnicode CMap", func(t *testing.T) {
		// Коды 0001..0005 -> Котёл (U+041A, U+043E, U+0442, U+0451, U+043B)
		cmap := []byte(`begincmap
5 beginbfchar
<0001> <041A>
<0002> <043E>
<0003> <0442>
<0004> <0451>
<0005> <043B>
endbfchar
endcmap`)
		content := []byte("BT /F1 12 Tf <00010002000300040005> Tj ET")
		pdfBytes := buildTestPdf([]struct {
			dict     string
			body     []byte
			compress bool
		}{
			{body: cmap, compress: true},
			{body: content, compress: true},
		})
		md, err := ConvertPdfToMarkdown(ctx, pdfBytes)
		if err != nil {
			t.Fatalf("convert cyrillic pdf failed: %v", err)
		}
		if !strings.Contains(md, "Котёл") {
			t.Fatalf("expected cyrillic word Котёл, got:\n%s", md)
		}
	})

	t.Run("scan pdf without text layer", func(t *testing.T) {
		pdfBytes := buildTestPdf([]struct {
			dict     string
			body     []byte
			compress bool
		}{
			{dict: "/Subtype /Image", body: []byte{0xFF, 0xD8, 0xFF, 0xE0}},
		})
		_, err := ConvertPdfToMarkdown(ctx, pdfBytes)
		if !errors.Is(err, platform.ErrValidation) {
			t.Fatalf("expected ErrValidation for scan pdf, got %v", err)
		}
		if !strings.Contains(err.Error(), "OCR") {
			t.Fatalf("expected OCR hint in error, got %q", err.Error())
		}
	})

	t.Run("invalid non-pdf bytes", func(t *testing.T) {
		if _, err := ConvertPdfToMarkdown(ctx, []byte("not a pdf")); !errors.Is(err, platform.ErrValidation) {
			t.Fatalf("expected ErrValidation, got %v", err)
		}
	})
}

func TestImportFileHTTPAndPdfAttachment(t *testing.T) {
	withNativePdfOnly(t)
	storage := t.TempDir()
	repo := newMemDocsRepo(time.Now)
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}
	mod := NewModule(repo, staticAuth{ac: admin}, fakeEntityResolver{}, nil, storage, nil)

	r := chi.NewRouter()
	mod.MountHTTP(r)

	uploadImport := func(endpoint, filename string, payload []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("file", filename)
		_, _ = fw.Write(payload)
		_ = mw.WriteField("space", "manuals")
		_ = mw.WriteField("doc_type", "equipment_manual")
		_ = mw.Close()

		req := httptest.NewRequest(http.MethodPost, endpoint, &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	pdfBytes := buildTestPdf([]struct {
		dict     string
		body     []byte
		compress bool
	}{
		{body: []byte("BT /F1 12 Tf (Baxi Eco Four Passport) Tj ET")},
	})

	xlsxBytes := buildTestZip(map[string]string{
		"xl/sharedStrings.xml": `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <si><t>Услуга</t></si>
  <si><t>ТО котла</t></si>
</sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row><c t="s"><v>0</v></c><c><v>Цена</v></c></row>
    <row><c t="s"><v>1</v></c><c><v>6500</v></c></row>
  </sheetData>
</worksheet>`,
	})

	// Импорт PDF через /api/v1/docs/import.
	recPdf := uploadImport("/api/v1/docs/import", "Паспорт Baxi.pdf", pdfBytes)
	if recPdf.Code != http.StatusCreated {
		t.Fatalf("expected 201 for pdf import, got %d: %s", recPdf.Code, recPdf.Body.String())
	}
	var pdfDoc Document
	if err := json.Unmarshal(recPdf.Body.Bytes(), &pdfDoc); err != nil {
		t.Fatalf("decode pdf doc: %v", err)
	}
	if pdfDoc.Title != "Паспорт Baxi" || !strings.Contains(pdfDoc.Content, "Baxi Eco Four Passport") {
		t.Fatalf("unexpected imported pdf doc: title=%q content=%q", pdfDoc.Title, pdfDoc.Content)
	}

	// Импорт XLSX через обратный алиас /api/v1/docs/import-docx.
	recXlsx := uploadImport("/api/v1/docs/import-docx", "Прайс.xlsx", xlsxBytes)
	if recXlsx.Code != http.StatusCreated {
		t.Fatalf("expected 201 for xlsx import, got %d: %s", recXlsx.Code, recXlsx.Body.String())
	}
	var xlsxDoc Document
	_ = json.Unmarshal(recXlsx.Body.Bytes(), &xlsxDoc)
	if xlsxDoc.Title != "Прайс" || !strings.Contains(xlsxDoc.Content, "| --- | --- |") || !strings.Contains(xlsxDoc.Content, "| ТО котла | 6500 |") {
		t.Fatalf("unexpected imported xlsx doc: title=%q content=%q", xlsxDoc.Title, xlsxDoc.Content)
	}

	// Неподдерживаемый формат -> 422 / 400.
	recBad := uploadImport("/api/v1/docs/import", "notes.txt", []byte("plain text"))
	if recBad.Code != http.StatusUnprocessableEntity && recBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400/422 for unsupported format, got %d: %s", recBad.Code, recBad.Body.String())
	}

	// PDF-вложение извлекает текст для поиска и context.md.
	att, err := mod.Service.SaveAttachment(context.Background(), admin, pdfDoc.ID, "scheme.pdf", "application/pdf", pdfBytes)
	if err != nil {
		t.Fatalf("save pdf attachment: %v", err)
	}
	if !strings.Contains(att.ExtractedText, "Baxi Eco Four Passport") {
		t.Fatalf("expected extracted text in pdf attachment, got %q", att.ExtractedText)
	}
}
