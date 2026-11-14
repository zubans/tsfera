package docs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

type Service struct {
	repo           Repository
	resolver       platform.EntityResolver
	bus            platform.EventBus
	storageDir     string
	logger         *slog.Logger
	clock          func() time.Time
	revisionMaxAge time.Duration
}

func NewService(
	repo Repository,
	resolver platform.EntityResolver,
	bus platform.EventBus,
	storageDir string,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if storageDir == "" {
		storageDir = filepath.Join(os.TempDir(), "tsfera-docs-attachments")
	}
	return &Service{
		repo:           repo,
		resolver:       resolver,
		bus:            bus,
		storageDir:     filepath.Clean(storageDir),
		logger:         logger,
		clock:          time.Now,
		revisionMaxAge: 24 * time.Hour,
	}
}

// CanViewDocument проверяет visibility документа с учётом ролей пользователя.
func CanViewDocument(auth *platform.AuthContext, doc *Document) bool {
	if doc == nil || doc.Deleted {
		return false
	}
	if auth == nil {
		return doc.Visibility == VisibilityPublic
	}
	if auth.IsSuperAdmin || auth.HasPermission("docs.manage") {
		return true
	}
	if !auth.HasPermission("docs.view") {
		return false
	}
	switch doc.Visibility {
	case "", VisibilityInternal, VisibilityPublic:
		return true
	case VisibilityEngineers:
		return auth.HasPermission("crm.application.view") || auth.HasPermission("crm.contractor.view_all")
	case VisibilityDispatchers:
		return auth.HasPermission("crm.contractor.view_all")
	case VisibilityAdmins:
		return false
	default:
		return true
	}
}

func (s *Service) publishChange(ctx context.Context, topic string, docID int64) {
	if s.bus != nil {
		s.bus.Publish(ctx, platform.Event{
			Topic:   topic,
			Payload: map[string]any{"document_id": docID},
		})
	}
}

func (s *Service) ListSpaces(ctx context.Context) ([]SpaceSummary, error) {
	return s.repo.ListSpaces(ctx)
}

func (s *Service) ListDocuments(ctx context.Context, auth *platform.AuthContext, f DocumentFilter) ([]Document, error) {
	items, err := s.repo.ListDocuments(ctx, f)
	if err != nil {
		return nil, err
	}
	visible := make([]Document, 0, len(items))
	for i := range items {
		if CanViewDocument(auth, &items[i]) {
			visible = append(visible, items[i])
		}
	}
	return visible, nil
}

func (s *Service) GetDocument(ctx context.Context, auth *platform.AuthContext, id int64) (*Document, error) {
	doc, err := s.repo.GetDocument(ctx, id)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, platform.NewNotFound("Документ не найден")
	}
	if !CanViewDocument(auth, doc) {
		return nil, platform.NewForbidden("Недостаточно прав для просмотра документа")
	}
	doc.Outline = ExtractOutline(doc.Content)
	links, _ := s.repo.ListEntityLinksByDocument(ctx, doc.ID)
	s.enrichEntityLinks(ctx, auth, links)
	doc.EntityLinks = links
	atts, _ := s.repo.ListAttachments(ctx, doc.ID)
	doc.Attachments = atts
	return doc, nil
}

func (s *Service) enrichEntityLinks(ctx context.Context, auth *platform.AuthContext, links []EntityLink) {
	if s.resolver == nil {
		return
	}
	for i := range links {
		if sum, err := s.resolver.ResolveEntity(ctx, auth, links[i].EntityType, links[i].EntityID); err == nil && sum != nil {
			links[i].EntityTitle = sum.Title
			links[i].EntityPath = sum.Path
		}
	}
}

type SaveDocumentInput struct {
	Space           string       `json:"space"`
	DocType         DocType      `json:"doc_type"`
	Title           string       `json:"title"`
	Slug            string       `json:"slug"`
	Content         string       `json:"content"`
	Status          string       `json:"status"`
	Tags            []string     `json:"tags"`
	Visibility      Visibility   `json:"visibility"`
	ExpectedVersion int          `json:"expected_version"`
	DiffSummary     string       `json:"diff_summary"`
	EntityLinks     []EntityLink `json:"entity_links,omitempty"`
}

func (s *Service) CreateDocument(ctx context.Context, auth *platform.AuthContext, in SaveDocumentInput) (*Document, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, platform.NewValidation("Укажите заголовок документа", map[string]string{"title": "Обязательное поле"})
	}
	space := strings.TrimSpace(in.Space)
	if space == "" {
		space = "general"
	}
	slug := strings.TrimSpace(in.Slug)
	if slug == "" {
		slug = slugifyTitle(title)
	}
	if existing, _ := s.repo.GetDocumentBySlug(ctx, space, slug); existing != nil {
		slug = slug + "-" + strconv.FormatInt(s.clock().Unix(), 10)
	}
	if in.DocType == "" {
		in.DocType = DocTypeArticle
	}
	if in.Status == "" {
		in.Status = "active"
	}
	if in.Visibility == "" {
		in.Visibility = VisibilityInternal
	}
	var uid int64
	if auth != nil {
		uid = auth.UserID
	}

	doc := &Document{
		Space:      space,
		DocType:    in.DocType,
		Title:      title,
		Slug:       slug,
		Content:    in.Content,
		Status:     in.Status,
		Tags:       normalizeTags(in.Tags),
		Version:    1,
		Visibility: in.Visibility,
		CreatedBy:  uid,
		UpdatedBy:  uid,
	}
	if err := s.repo.SaveDocumentRevision(ctx, doc, 0, &Revision{
		DiffSummary: firstString(in.DiffSummary, "Создание документа"), CreatedBy: uid,
	}); err != nil {
		return nil, err
	}

	for _, l := range in.EntityLinks {
		if l.EntityType != "" && l.EntityID > 0 {
			_ = s.repo.AddEntityLink(ctx, &EntityLink{
				DocumentID: doc.ID,
				EntityType: l.EntityType,
				EntityID:   l.EntityID,
				Note:       l.Note,
				CreatedBy:  uid,
			})
		}
	}
	s.publishChange(ctx, "docs.document.upserted", doc.ID)
	return s.GetDocument(ctx, auth, doc.ID)
}

func (s *Service) UpdateDocument(ctx context.Context, auth *platform.AuthContext, id int64, in SaveDocumentInput) (*Document, error) {
	existing, err := s.repo.GetDocument(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, platform.NewNotFound("Документ не найден")
	}
	if !CanViewDocument(auth, existing) {
		return nil, platform.NewForbidden("Нет прав на редактирование документа")
	}
	if in.ExpectedVersion <= 0 {
		return nil, platform.NewValidation("Для обновления необходимо указать expected_version", map[string]string{"expected_version": "Обязательное поле"})
	}
	if in.ExpectedVersion != existing.Version {
		return nil, platform.NewConflict(fmt.Sprintf("Документ изменён другим пользователем (текущая версия %d, передана %d)", existing.Version, in.ExpectedVersion))
	}

	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = existing.Title
	}
	var uid int64
	if auth != nil {
		uid = auth.UserID
	}

	diffSummary := strings.TrimSpace(in.DiffSummary)
	if diffSummary == "" {
		diffSummary = computeSimpleDiffSummary(existing.Content, in.Content)
	}

	if in.Space != "" {
		existing.Space = strings.TrimSpace(in.Space)
	}
	if in.DocType != "" {
		existing.DocType = in.DocType
	}
	existing.Title = title
	if strings.TrimSpace(in.Slug) != "" {
		existing.Slug = strings.TrimSpace(in.Slug)
	}
	existing.Content = in.Content
	if in.Status != "" {
		existing.Status = in.Status
		if in.Status == "archived" {
			now := s.clock()
			existing.ArchivedAt = &now
		} else {
			existing.ArchivedAt = nil
		}
	}
	if in.Tags != nil {
		existing.Tags = normalizeTags(in.Tags)
	}
	if in.Visibility != "" {
		existing.Visibility = in.Visibility
	}
	existing.UpdatedBy = uid

	if err := s.repo.SaveDocumentRevision(ctx, existing, in.ExpectedVersion, &Revision{
		DiffSummary: diffSummary, CreatedBy: uid,
	}); err != nil {
		return nil, err
	}

	s.publishChange(ctx, "docs.document.upserted", existing.ID)
	return s.GetDocument(ctx, auth, existing.ID)
}

func (s *Service) AppendDocument(ctx context.Context, auth *platform.AuthContext, id int64, addition string) (*Document, error) {
	if strings.TrimSpace(addition) == "" {
		return nil, platform.NewValidation("Добавляемый текст пуст", nil)
	}
	existing, err := s.repo.GetDocument(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, platform.NewNotFound("Документ не найден")
	}
	newContent := strings.TrimRight(existing.Content, "\n") + "\n\n" + strings.TrimSpace(addition)
	return s.UpdateDocument(ctx, auth, id, SaveDocumentInput{
		Title:           existing.Title,
		Content:         newContent,
		ExpectedVersion: existing.Version,
		DiffSummary:     "Дополнение документа",
	})
}

func (s *Service) DeleteDocument(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if _, err := s.GetDocument(ctx, auth, id); err != nil {
		return err
	}
	if err := s.repo.SoftDeleteDocument(ctx, id); err != nil {
		return err
	}
	s.publishChange(ctx, "docs.document.deleted", id)
	return nil
}

// --- Revisions ---

func (s *Service) ListRevisions(ctx context.Context, auth *platform.AuthContext, docID int64) ([]Revision, error) {
	doc, err := s.repo.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil || !CanViewDocument(auth, doc) {
		return nil, platform.NewForbidden("Документ недоступен")
	}
	return s.repo.ListRevisions(ctx, docID)
}

func (s *Service) GetRevision(ctx context.Context, auth *platform.AuthContext, docID, revID int64) (*Revision, error) {
	doc, err := s.repo.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil || !CanViewDocument(auth, doc) {
		return nil, platform.NewForbidden("Документ недоступен")
	}
	rev, err := s.repo.GetRevision(ctx, docID, revID)
	if err != nil {
		return nil, err
	}
	if rev == nil {
		return nil, platform.NewNotFound("Ревизия не найдена")
	}
	return rev, nil
}

func (s *Service) RestoreRevision(ctx context.Context, auth *platform.AuthContext, docID, revID int64) (*Document, error) {
	doc, err := s.repo.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, platform.NewNotFound("Документ не найден")
	}
	rev, err := s.repo.GetRevision(ctx, docID, revID)
	if err != nil {
		return nil, err
	}
	if rev == nil {
		return nil, platform.NewNotFound("Ревизия не найдена")
	}
	return s.UpdateDocument(ctx, auth, docID, SaveDocumentInput{
		Title:           rev.Title,
		Content:         rev.Content,
		ExpectedVersion: doc.Version,
		DiffSummary:     fmt.Sprintf("Восстановление версии #%d", rev.Version),
	})
}

func (s *Service) CompressOldRevisions(ctx context.Context) (int, error) {
	return s.repo.CompressRevisionsBefore(ctx, s.clock().Add(-s.revisionMaxAge))
}

// --- Entity Links ---

func (s *Service) LinkEntity(ctx context.Context, auth *platform.AuthContext, docID int64, entityType string, entityID int64, note string) (*EntityLink, error) {
	if entityType == "" || entityID <= 0 {
		return nil, platform.NewValidation("Укажите entity_type и entity_id", nil)
	}
	doc, err := s.repo.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, platform.NewNotFound("Документ не найден")
	}
	if !CanViewDocument(auth, doc) {
		return nil, platform.NewForbidden("Документ недоступен")
	}
	var uid int64
	if auth != nil {
		uid = auth.UserID
	}
	link := &EntityLink{
		DocumentID: docID,
		EntityType: strings.TrimSpace(entityType),
		EntityID:   entityID,
		Note:       strings.TrimSpace(note),
		CreatedBy:  uid,
	}
	if err := s.repo.AddEntityLink(ctx, link); err != nil {
		return nil, err
	}
	links := []EntityLink{*link}
	s.enrichEntityLinks(ctx, auth, links)
	return &links[0], nil
}

func (s *Service) UnlinkEntity(ctx context.Context, linkID int64) error {
	return s.repo.DeleteEntityLink(ctx, linkID)
}

func (s *Service) ListDocumentsByEntity(ctx context.Context, auth *platform.AuthContext, entityType string, entityID int64) ([]Document, error) {
	items, err := s.repo.ListDocumentsByEntity(ctx, entityType, entityID)
	if err != nil {
		return nil, err
	}
	out := make([]Document, 0, len(items))
	for i := range items {
		if CanViewDocument(auth, &items[i]) {
			out = append(out, items[i])
		}
	}
	return out, nil
}

// --- Inline Comments ---

type AddCommentInput struct {
	Body     string `json:"body"`
	Quote    string `json:"quote"`
	Anchor   string `json:"anchor"`
	LineFrom int    `json:"line_from"`
	LineTo   int    `json:"line_to"`
}

func (s *Service) ListComments(ctx context.Context, auth *platform.AuthContext, docID int64) ([]DocComment, error) {
	doc, err := s.repo.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil || !CanViewDocument(auth, doc) {
		return nil, platform.NewForbidden("Документ недоступен")
	}
	return s.repo.ListComments(ctx, docID)
}

func (s *Service) AddComment(ctx context.Context, auth *platform.AuthContext, docID int64, in AddCommentInput) (*DocComment, error) {
	doc, err := s.repo.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil || !CanViewDocument(auth, doc) {
		return nil, platform.NewForbidden("Документ недоступен")
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return nil, platform.NewValidation("Текст комментария не может быть пустым", map[string]string{"body": "Обязательное поле"})
	}
	if in.Quote != "" && !strings.Contains(doc.Content, in.Quote) {
		return nil, platform.NewValidation("Выделенная цитата не найдена в текущей версии документа", map[string]string{"quote": "Цитата устарела"})
	}
	var uid int64
	authorName := "Пользователь"
	if auth != nil {
		uid = auth.UserID
		if auth.DisplayName != "" {
			authorName = auth.DisplayName
		} else if auth.Username != "" {
			authorName = auth.Username
		}
	}
	c := &DocComment{
		DocumentID: docID,
		AuthorID:   uid,
		AuthorName: authorName,
		Body:       body,
		Quote:      strings.TrimSpace(in.Quote),
		Anchor:     strings.TrimSpace(in.Anchor),
		LineFrom:   in.LineFrom,
		LineTo:     in.LineTo,
	}
	if err := s.repo.AddComment(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) ResolveComment(ctx context.Context, auth *platform.AuthContext, docID, commentID int64, resolved bool) (*DocComment, error) {
	if _, err := s.GetDocument(ctx, auth, docID); err != nil {
		return nil, err
	}
	var uid int64
	if auth != nil {
		uid = auth.UserID
	}
	return s.repo.SetCommentResolved(ctx, docID, commentID, uid, resolved)
}

func (s *Service) DeleteComment(ctx context.Context, auth *platform.AuthContext, docID, commentID int64) error {
	if _, err := s.GetDocument(ctx, auth, docID); err != nil {
		return err
	}
	return s.repo.DeleteComment(ctx, docID, commentID)
}

// --- Attachments & DOCX Import & Context Export ---

func (s *Service) SaveAttachment(ctx context.Context, auth *platform.AuthContext, docID int64, filename, mimeType string, data []byte) (*Attachment, error) {
	doc, err := s.repo.GetDocument(ctx, docID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, platform.NewNotFound("Документ не найден")
	}
	if !CanViewDocument(auth, doc) {
		return nil, platform.NewForbidden("Документ недоступен")
	}
	cleanName := sanitizeFilename(filename)
	if cleanName == "" {
		return nil, platform.NewValidation("Некорректное имя файла", nil)
	}
	docDir := filepath.Join(s.storageDir, strconv.FormatInt(docID, 10))
	if err := os.MkdirAll(docDir, 0o750); err != nil {
		return nil, err
	}
	randTag := randomHex(6)
	diskPath := filepath.Clean(filepath.Join(docDir, randTag+"_"+cleanName))
	if !strings.HasPrefix(diskPath, s.storageDir+string(os.PathSeparator)) {
		return nil, platform.NewValidation("Недопустимый путь сохранения вложения", nil)
	}
	if err := os.WriteFile(diskPath, data, 0o600); err != nil {
		return nil, err
	}

	extracted := ExtractAttachmentText(cleanName, data)
	var uid int64
	if auth != nil {
		uid = auth.UserID
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	att := &Attachment{
		DocumentID:    docID,
		Filename:      cleanName,
		MimeType:      mimeType,
		SizeBytes:     int64(len(data)),
		StoragePath:   diskPath,
		ExtractedText: extracted,
		CreatedBy:     uid,
	}
	if err := s.repo.AddAttachment(ctx, att); err != nil {
		_ = os.Remove(diskPath)
		return nil, err
	}
	s.publishChange(ctx, "docs.attachment.added", docID)
	return att, nil
}

func (s *Service) GetAttachmentForDownload(ctx context.Context, auth *platform.AuthContext, attID int64) (*Attachment, []byte, error) {
	att, err := s.repo.GetAttachment(ctx, attID)
	if err != nil {
		return nil, nil, err
	}
	if att == nil {
		return nil, nil, platform.NewNotFound("Вложение не найдено")
	}
	doc, err := s.repo.GetDocument(ctx, att.DocumentID)
	if err != nil {
		return nil, nil, err
	}
	if !CanViewDocument(auth, doc) {
		return nil, nil, platform.NewForbidden("Нет прав на скачивание вложения")
	}
	cleanPath := filepath.Clean(att.StoragePath)
	if !strings.HasPrefix(cleanPath, s.storageDir+string(os.PathSeparator)) {
		return nil, nil, platform.NewForbidden("Недопустимый путь к файлу вложения")
	}
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, nil, platform.NewNotFound("Файл вложения отсутствует на диске")
	}
	return att, data, nil
}

func (s *Service) ImportDocx(ctx context.Context, auth *platform.AuthContext, space, title, docType string, data []byte) (*Document, error) {
	md, err := ConvertDocxToMarkdown(data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(title) == "" {
		title = "Импортированный документ"
	}
	dt := DocType(docType)
	if dt == "" {
		dt = DocTypeSOP
	}
	return s.CreateDocument(ctx, auth, SaveDocumentInput{
		Space:   space,
		DocType: dt,
		Title:   title,
		Content: md,
	})
}

func (s *Service) ExportContextMarkdown(ctx context.Context, auth *platform.AuthContext, space string, docID int64) (string, error) {
	var b strings.Builder
	if docID > 0 {
		doc, err := s.GetDocument(ctx, auth, docID)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "# %s (%s/%s)\n\n%s\n", doc.Title, doc.Space, doc.Slug, doc.Content)
		if len(doc.EntityLinks) > 0 {
			b.WriteString("\n## Связанные объекты Теплосферы\n")
			for _, l := range doc.EntityLinks {
				fmt.Fprintf(&b, "- %s #%d: %s (%s)\n", l.EntityType, l.EntityID, l.EntityTitle, l.Note)
			}
		}
		for _, att := range doc.Attachments {
			if att.ExtractedText != "" {
				fmt.Fprintf(&b, "\n## Вложение: %s\n\n%s\n", att.Filename, att.ExtractedText)
			}
		}
		return b.String(), nil
	}

	docs, err := s.ListDocuments(ctx, auth, DocumentFilter{Space: space, Limit: 200})
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "# База знаний Теплосферы (пространство: %s)\n\n", firstString(space, "все"))
	for _, d := range docs {
		fmt.Fprintf(&b, "---\n\n## [%s] %s (v%d)\n\n%s\n\n", d.DocType, d.Title, d.Version, d.Content)
	}
	return b.String(), nil
}

// --- helpers ---

func normalizeTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
	}
	return out
}

func slugifyTitle(title string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	res := strings.TrimRight(b.String(), "-")
	if res == "" {
		return "doc-" + randomHex(4)
	}
	return res
}

func sanitizeFilename(name string) string {
	base := filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	if base == "." || base == ".." || base == "/" {
		return ""
	}
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r < 32 {
			return '_'
		}
		return r
	}, base)
}

func computeSimpleDiffSummary(oldText, newText string) string {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")
	oldSet := make(map[string]int, len(oldLines))
	for _, l := range oldLines {
		oldSet[l]++
	}
	added := 0
	for _, l := range newLines {
		if oldSet[l] > 0 {
			oldSet[l]--
		} else {
			added++
		}
	}
	removed := 0
	for _, left := range oldSet {
		removed += left
	}
	return fmt.Sprintf("+%d / -%d строк", added, removed)
}

func firstString(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}
