package docs

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Module struct {
	Service *Service
	handler *Handler
	repo    Repository
}

func NewModule(
	repo Repository,
	auth platform.Authorizer,
	resolver platform.EntityResolver,
	bus platform.EventBus,
	storageDir string,
	logger *slog.Logger,
) *Module {
	svc := NewService(repo, resolver, bus, storageDir, logger)
	return &Module{
		Service: svc,
		handler: NewHandler(svc, auth, logger),
		repo:    repo,
	}
}

func (m *Module) Name() string { return "docs" }

func (m *Module) Migrations() fs.FS {
	sub, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return migrationsFS
	}
	return sub
}

func (m *Module) MountHTTP(r chi.Router) {
	m.handler.Mount(r)
}

func (m *Module) Permissions() []platform.PermissionDef {
	return []platform.PermissionDef{
		{Key: "docs.view", Group: "Документация", Resource: "document", Action: "view", Description: "Чтение базы знаний, инструкций и вложений"},
		{Key: "docs.create", Group: "Документация", Resource: "document", Action: "create", Description: "Создание и импорт документов"},
		{Key: "docs.update", Group: "Документация", Resource: "document", Action: "update", Description: "Редактирование, восстановление версий и привязка к CRM"},
		{Key: "docs.delete", Group: "Документация", Resource: "document", Action: "delete", Description: "Удаление документов"},
		{Key: "docs.comment", Group: "Документация", Resource: "comment", Action: "write", Description: "Инлайн-комментарии и их разрешение"},
		{Key: "docs.manage", Group: "Документация", Resource: "document", Action: "manage", Description: "Доступ ко всем пространствам и приватным документам"},
	}
}

func (m *Module) Workers() []platform.PeriodicJob {
	return []platform.PeriodicJob{
		{
			Name:     "docs_revision_compressor",
			Interval: 1 * time.Hour,
			Run: func(ctx context.Context) error {
				_, err := m.Service.CompressOldRevisions(ctx)
				return err
			},
		},
		{
			Name:     "docs_schematic_analyzer",
			Interval: 30 * time.Second,
			Run: func(ctx context.Context) error {
				_, err := m.Service.ProcessPendingSchematics(ctx, 3)
				return err
			},
		},
	}
}

func (m *Module) Tools() []platform.ToolDefinition {
	return []platform.ToolDefinition{
		{
			Name:        "docs_search",
			Description: "Поиск регламентов, инструкций по котлам, чеклистов и ADR в модуле документации.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"space": map[string]any{"type": "string"},
					"limit": map[string]any{"type": "integer"},
				},
				"required": []string{"query"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				var in struct {
					Query string `json:"query"`
					Space string `json:"space"`
					Limit int    `json:"limit"`
				}
				_ = json.Unmarshal(args, &in)
				if in.Limit <= 0 || in.Limit > 20 {
					in.Limit = 8
				}
				items, err := m.Service.ListDocuments(ctx, auth, DocumentFilter{
					Space: in.Space,
					Query: in.Query,
					Limit: in.Limit,
				})
				if err != nil {
					return platform.ToolResult{}, err
				}
				if len(items) == 0 {
					return platform.ToolResult{Content: "Документы по запросу не найдены."}, nil
				}
				var b strings.Builder
				var refs []platform.SourceRef
				for _, d := range items {
					fmt.Fprintf(&b, "- [%s] %s (id=%d, space=%s, v%d)\n", d.DocType, d.Title, d.ID, d.Space, d.Version)
					refs = append(refs, platform.SourceRef{
						SourceType: "doc",
						SourceID:   strconv.FormatInt(d.ID, 10),
						Title:      d.Title,
						Path:       fmt.Sprintf("/docs/%d", d.ID),
					})
				}
				return platform.ToolResult{Content: b.String(), Sources: refs}, nil
			},
		},
		{
			Name:        "docs_get",
			Description: "Получить полный текст документа по ID (с оглавлением и привязками к CRM).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"document_id": map[string]any{"type": "integer"},
				},
				"required": []string{"document_id"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				var in struct {
					DocumentID int64 `json:"document_id"`
				}
				_ = json.Unmarshal(args, &in)
				doc, err := m.Service.GetDocument(ctx, auth, in.DocumentID)
				if err != nil {
					return platform.ToolResult{}, err
				}
				var content strings.Builder
				fmt.Fprintf(&content, "# %s (v%d)\n\n%s", doc.Title, doc.Version, doc.Content)
				if len(doc.Attachments) > 0 {
					content.WriteString("\n\n## Вложения и схемы документа\n")
					for _, a := range doc.Attachments {
						if a.Kind == AttachmentKindSchematic {
							fmt.Fprintf(&content, "- [Схема] attachment_id=%d, файл=%s, стр.=%d, статус=%s (для точного ответа по схеме вызови docs_inspect_schematic)\n",
								a.ID, a.Filename, a.PageNumber, a.AnalysisStatus)
						} else {
							fmt.Fprintf(&content, "- [Файл] attachment_id=%d, файл=%s (%s)\n", a.ID, a.Filename, a.MimeType)
						}
					}
				}
				return platform.ToolResult{
					Content: content.String(),
					Sources: []platform.SourceRef{{
						SourceType: "doc",
						SourceID:   strconv.FormatInt(doc.ID, 10),
						Title:      doc.Title,
						Path:       fmt.Sprintf("/docs/%d", doc.ID),
					}},
				}, nil
			},
		},
		{
			Name:        "docs_list_attachments",
			Description: "Получить список вложений и электрических/гидравлических схем документа (attachment_id, имя файла, страница, статус индексации).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"document_id": map[string]any{"type": "integer"},
				},
				"required": []string{"document_id"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				var in struct {
					DocumentID int64 `json:"document_id"`
				}
				_ = json.Unmarshal(args, &in)
				doc, err := m.Service.GetDocument(ctx, auth, in.DocumentID)
				if err != nil {
					return platform.ToolResult{}, err
				}
				if len(doc.Attachments) == 0 {
					return platform.ToolResult{Content: "У документа нет вложений и схем."}, nil
				}
				var b strings.Builder
				for _, a := range doc.Attachments {
					fmt.Fprintf(&b, "- attachment_id=%d | kind=%s | файл=%s | mime=%s | стр.=%d | analysis=%s\n",
						a.ID, a.Kind, a.Filename, a.MimeType, a.PageNumber, a.AnalysisStatus)
				}
				return platform.ToolResult{
					Content: b.String(),
					Sources: []platform.SourceRef{{
						SourceType: "doc",
						SourceID:   strconv.FormatInt(doc.ID, 10),
						Title:      doc.Title,
						Path:       fmt.Sprintf("/docs/%d", doc.ID),
					}},
				}, nil
			},
		},
		{
			Name:        "docs_inspect_schematic",
			Description: "Проверить оригинал электрической/технической схемы (вложения-изображения) по конкретному вопросу без галлюцинаций. При мелких клеммах можно передать region: full, top_left, top_right, bottom_left, bottom_right, center или координаты x1,y1,x2,y2 (0..1).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"attachment_id": map[string]any{"type": "integer", "description": "ID вложения-схемы из поиска или docs_list_attachments"},
					"question":      map[string]any{"type": "string", "description": "Конкретный вопрос по схеме (клеммы, контакты, провода, подключения)"},
					"region":        map[string]any{"type": "string", "description": "Необязательно: область увеличения (full, top_left, top_right, bottom_left, bottom_right, center или x1,y1,x2,y2)"},
				},
				"required": []string{"attachment_id", "question"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				var in struct {
					AttachmentID int64  `json:"attachment_id"`
					Question     string `json:"question"`
					Region       string `json:"region"`
				}
				_ = json.Unmarshal(args, &in)
				att, answer, err := m.Service.InspectSchematic(ctx, auth, in.AttachmentID, in.Question, in.Region)
				if err != nil {
					return platform.ToolResult{}, err
				}
				return platform.ToolResult{
					Content: answer,
					Sources: []platform.SourceRef{{
						SourceType: "doc_attachment",
						SourceID:   strconv.FormatInt(att.ID, 10),
						Title:      fmt.Sprintf("Схема: %s (стр. %d)", att.Filename, att.PageNumber),
						Path:       fmt.Sprintf("/docs/%d", att.DocumentID),
					}},
				}, nil
			},
		},
		{
			Name:        "docs_search_boiler_parts",
			Description: "Поиск запчастей, артикулов (Material No.), позиций на взрыв-схемах и замен артикулов по проиндексированным чертежам котлов. Используй для любых вопросов о запчастях, узлах, артикулах и комплектности конкретного котла.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"brand":       map[string]any{"type": "string", "description": "Производитель котла (например: Vaillant, Kentatsu, BAXI, Protherm)"},
					"model":       map[string]any{"type": "string", "description": "Модель котла (например: VK INT 246, VK 36/4-1 XE, VSC 266/4-5 200)"},
					"part_number": map[string]any{"type": "string", "description": "Артикул / Material No. запчасти или старый артикул для поиска замены"},
					"assembly":    map[string]any{"type": "string", "description": "Узел котла (например: Горелка, Теплообменник, Гидравлические части, Блок электроники)"},
					"query":       map[string]any{"type": "string", "description": "Название запчасти или ключевые слова (например: газовая арматура, электрод, датчик давления, насос, NTC)"},
					"limit":       map[string]any{"type": "integer"},
				},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				var in struct {
					Brand      string `json:"brand"`
					Model      string `json:"model"`
					PartNumber string `json:"part_number"`
					Assembly   string `json:"assembly"`
					Query      string `json:"query"`
					Limit      int    `json:"limit"`
				}
				_ = json.Unmarshal(args, &in)
				if in.Limit <= 0 || in.Limit > 50 {
					in.Limit = 25
				}
				items, err := m.Service.SearchBoilerParts(ctx, auth, BoilerPartFilter{
					Brand:      in.Brand,
					Model:      in.Model,
					PartNumber: in.PartNumber,
					Assembly:   in.Assembly,
					Query:      in.Query,
					Limit:      in.Limit,
				})
				if err != nil {
					return platform.ToolResult{}, err
				}
				if len(items) == 0 {
					return platform.ToolResult{Content: "Запчасти в проиндексированных чертежах котлов по указанным параметрам не найдены."}, nil
				}
				var b strings.Builder
				seenDocs := map[int64]bool{}
				var refs []platform.SourceRef
				for _, p := range items {
					b.WriteString("- ")
					b.WriteString(FormatBoilerPartLine(p))
					b.WriteByte('\n')
					if !seenDocs[p.DocumentID] {
						seenDocs[p.DocumentID] = true
						refs = append(refs, platform.SourceRef{
							SourceType: "doc",
							SourceID:   strconv.FormatInt(p.DocumentID, 10),
							Title:      p.DocumentTitle,
							Path:       fmt.Sprintf("/docs/%d", p.DocumentID),
						})
					}
				}
				return platform.ToolResult{Content: b.String(), Sources: refs}, nil
			},
		},
		{
			Name:        "docs_diagnose_boiler",
			Description: "Диагностика неисправности котла по проиндексированным чертежам и документации: находит релевантные документы котла, связанные с симптомом/ошибкой узлы, артикулы запчастей (с позициями и заменами) и вложения-схемы для визуальной проверки.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"brand":   map[string]any{"type": "string", "description": "Производитель котла (необязательно)"},
					"model":   map[string]any{"type": "string", "description": "Модель котла (например: VK INT 246, NOBBY SMART)"},
					"symptom": map[string]any{"type": "string", "description": "Описание неисправности, симптом или код ошибки (например: не разжигается, F.28, падает давление, перегрев)"},
				},
				"required": []string{"symptom"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				var in struct {
					Brand   string `json:"brand"`
					Model   string `json:"model"`
					Symptom string `json:"symptom"`
				}
				_ = json.Unmarshal(args, &in)
				diag, err := m.Service.DiagnoseBoiler(ctx, auth, in.Brand, in.Model, in.Symptom)
				if err != nil {
					return platform.ToolResult{}, err
				}
				var b strings.Builder
				var refs []platform.SourceRef
				seenDocs := map[int64]bool{}

				if len(diag.MatchedDocs) > 0 {
					b.WriteString("## Документы по котлу\n")
					for _, d := range diag.MatchedDocs {
						fmt.Fprintf(&b, "- [%s] %s (id=%d, space=%s)\n", d.DocType, d.Title, d.ID, d.Space)
						if !seenDocs[d.ID] {
							seenDocs[d.ID] = true
							refs = append(refs, platform.SourceRef{
								SourceType: "doc",
								SourceID:   strconv.FormatInt(d.ID, 10),
								Title:      d.Title,
								Path:       fmt.Sprintf("/docs/%d", d.ID),
							})
						}
					}
					b.WriteByte('\n')
				}
				if len(diag.RelatedParts) > 0 {
					b.WriteString("## Связанные узлы и запчасти из чертежей\n")
					for _, p := range diag.RelatedParts {
						b.WriteString("- ")
						b.WriteString(FormatBoilerPartLine(p))
						b.WriteByte('\n')
						if !seenDocs[p.DocumentID] {
							seenDocs[p.DocumentID] = true
							refs = append(refs, platform.SourceRef{
								SourceType: "doc",
								SourceID:   strconv.FormatInt(p.DocumentID, 10),
								Title:      p.DocumentTitle,
								Path:       fmt.Sprintf("/docs/%d", p.DocumentID),
							})
						}
					}
					b.WriteByte('\n')
				}
				if len(diag.Schematics) > 0 {
					b.WriteString("## Взрыв-схемы и чертежи для визуальной проверки (docs_inspect_schematic)\n")
					for _, a := range diag.Schematics {
						fmt.Fprintf(&b, "- attachment_id=%d | документ #%d | файл=%s | стр.=%d | анализ=%s\n",
							a.ID, a.DocumentID, a.Filename, a.PageNumber, a.AnalysisStatus)
					}
				}
				if b.Len() == 0 {
					return platform.ToolResult{Content: "По указанному котлу и неисправности не найдено проиндексированных чертежей и документов."}, nil
				}
				return platform.ToolResult{Content: b.String(), Sources: refs}, nil
			},
		},
		{
			Name:        "docs_get_by_entity",
			Description: "Найти все документы и инструкции, привязанные к сущности CRM (contractor, equipment, application, contract, task).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"entity_type": map[string]any{"type": "string"},
					"entity_id":   map[string]any{"type": "integer"},
				},
				"required": []string{"entity_type", "entity_id"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				var in struct {
					EntityType string `json:"entity_type"`
					EntityID   int64  `json:"entity_id"`
				}
				_ = json.Unmarshal(args, &in)
				items, err := m.Service.ListDocumentsByEntity(ctx, auth, in.EntityType, in.EntityID)
				if err != nil {
					return platform.ToolResult{}, err
				}
				if len(items) == 0 {
					return platform.ToolResult{Content: "Связанные документы не найдены."}, nil
				}
				var b strings.Builder
				var refs []platform.SourceRef
				for _, d := range items {
					fmt.Fprintf(&b, "- [%s] %s (id=%d)\n", d.DocType, d.Title, d.ID)
					refs = append(refs, platform.SourceRef{
						SourceType: "doc",
						SourceID:   strconv.FormatInt(d.ID, 10),
						Title:      d.Title,
						Path:       fmt.Sprintf("/docs/%d", d.ID),
					})
				}
				return platform.ToolResult{Content: b.String(), Sources: refs}, nil
			},
		},
		{
			Name:        "docs_create",
			Description: "Создать новый документ в базе знаний (требуется право docs.create и токен с записью).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"space":    map[string]any{"type": "string"},
					"doc_type": map[string]any{"type": "string"},
					"title":    map[string]any{"type": "string"},
					"content":  map[string]any{"type": "string"},
				},
				"required": []string{"title", "content"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				if auth == nil || !auth.HasPermission("docs.create") {
					return platform.ToolResult{}, platform.NewForbidden("Нет права docs.create")
				}
				var in SaveDocumentInput
				_ = json.Unmarshal(args, &in)
				doc, err := m.Service.CreateDocument(ctx, auth, in)
				if err != nil {
					return platform.ToolResult{}, err
				}
				return platform.ToolResult{
					Content: fmt.Sprintf("Создан документ #%d «%s»", doc.ID, doc.Title),
					Sources: []platform.SourceRef{{
						SourceType: "doc",
						SourceID:   strconv.FormatInt(doc.ID, 10),
						Title:      doc.Title,
						Path:       fmt.Sprintf("/docs/%d", doc.ID),
					}},
				}, nil
			},
		},
		{
			Name:        "docs_append",
			Description: "Дополнить существующий документ в конце (требуется право docs.update).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"document_id": map[string]any{"type": "integer"},
					"addition":    map[string]any{"type": "string"},
				},
				"required": []string{"document_id", "addition"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				if auth == nil || !auth.HasPermission("docs.update") {
					return platform.ToolResult{}, platform.NewForbidden("Нет права docs.update")
				}
				var in struct {
					DocumentID int64  `json:"document_id"`
					Addition   string `json:"addition"`
				}
				_ = json.Unmarshal(args, &in)
				doc, err := m.Service.AppendDocument(ctx, auth, in.DocumentID, in.Addition)
				if err != nil {
					return platform.ToolResult{}, err
				}
				return platform.ToolResult{
					Content: fmt.Sprintf("Документ #%d обновлён до версии v%d", doc.ID, doc.Version),
				}, nil
			},
		},
		{
			Name:        "docs_add_comment",
			Description: "Добавить инлайн-примечание или рецензию к документу.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"document_id": map[string]any{"type": "integer"},
					"body":        map[string]any{"type": "string"},
					"quote":       map[string]any{"type": "string"},
				},
				"required": []string{"document_id", "body"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				if auth == nil || !auth.HasPermission("docs.comment") {
					return platform.ToolResult{}, platform.NewForbidden("Нет права docs.comment")
				}
				var in struct {
					DocumentID int64  `json:"document_id"`
					Body       string `json:"body"`
					Quote      string `json:"quote"`
				}
				_ = json.Unmarshal(args, &in)
				c, err := m.Service.AddComment(ctx, auth, in.DocumentID, AddCommentInput{
					Body:  in.Body,
					Quote: in.Quote,
				})
				if err != nil {
					return platform.ToolResult{}, err
				}
				return platform.ToolResult{Content: fmt.Sprintf("Добавлено примечание #%d", c.ID)}, nil
			},
		},
	}
}

func (m *Module) SearchSources() []platform.IndexSource {
	return []platform.IndexSource{
		&docIndexSource{repo: m.repo},
		&attachmentIndexSource{repo: m.repo},
		&boilerPartsIndexSource{repo: m.repo},
	}
}

type docIndexSource struct {
	repo Repository
}

func (s *docIndexSource) SourceType() string { return "doc" }

func (s *docIndexSource) ListForIndex(ctx context.Context, _ *time.Time) ([]platform.IndexItem, error) {
	items, err := s.repo.ListDocuments(ctx, DocumentFilter{Limit: 1000})
	if err != nil {
		return nil, err
	}
	out := make([]platform.IndexItem, 0, len(items))
	for _, d := range items {
		out = append(out, platform.IndexItem{
			SourceType: "doc",
			SourceID:   strconv.FormatInt(d.ID, 10),
			Title:      d.Title,
			Content:    d.Content,
			Path:       fmt.Sprintf("/docs/%d", d.ID),
			Metadata: map[string]string{
				"space":      d.Space,
				"doc_type":   string(d.DocType),
				"visibility": string(d.Visibility),
			},
			UpdatedAt: d.UpdatedAt,
		})
	}
	return out, nil
}

func (s *docIndexSource) CanView(ctx context.Context, auth *platform.AuthContext, sourceID string, _ map[string]string) (bool, error) {
	id, err := strconv.ParseInt(sourceID, 10, 64)
	if err != nil {
		return false, nil
	}
	doc, err := s.repo.GetDocument(ctx, id)
	if err != nil {
		return false, err
	}
	return CanViewDocument(auth, doc), nil
}

type attachmentIndexSource struct {
	repo Repository
}

func (s *attachmentIndexSource) SourceType() string { return "doc_attachment" }

func (s *attachmentIndexSource) ListForIndex(ctx context.Context, _ *time.Time) ([]platform.IndexItem, error) {
	items, err := s.repo.ListAllAttachments(ctx, 1000)
	if err != nil {
		return nil, err
	}
	out := make([]platform.IndexItem, 0, len(items))
	for _, a := range items {
		content := a.ExtractedText
		title := a.Filename
		if a.Kind == AttachmentKindSchematic {
			title = fmt.Sprintf("Схема: %s (стр. %d)", a.Filename, a.PageNumber)
			content = fmt.Sprintf("[Схема документа #%d, attachment_id=%d, стр. %d]\n\n%s",
				a.DocumentID, a.ID, a.PageNumber, a.ExtractedText)
		}
		out = append(out, platform.IndexItem{
			SourceType: "doc_attachment",
			SourceID:   strconv.FormatInt(a.ID, 10),
			Title:      title,
			Content:    content,
			Path:       fmt.Sprintf("/docs/%d", a.DocumentID),
			Metadata: map[string]string{
				"document_id":   strconv.FormatInt(a.DocumentID, 10),
				"attachment_id": strconv.FormatInt(a.ID, 10),
				"kind":          a.Kind,
				"page_number":   strconv.Itoa(a.PageNumber),
			},
			UpdatedAt: a.CreatedAt,
		})
	}
	return out, nil
}

func (s *attachmentIndexSource) CanView(ctx context.Context, auth *platform.AuthContext, _ string, meta map[string]string) (bool, error) {
	docID, err := strconv.ParseInt(meta["document_id"], 10, 64)
	if err != nil {
		return false, nil
	}
	doc, err := s.repo.GetDocument(ctx, docID)
	if err != nil {
		return false, err
	}
	return CanViewDocument(auth, doc), nil
}

// boilerPartsIndexSource индексирует структурированные спецификации запчастей из чертежей котлов в единый ИИ-поиск.
type boilerPartsIndexSource struct {
	repo Repository
}

func (s *boilerPartsIndexSource) SourceType() string { return "boiler_parts" }

func (s *boilerPartsIndexSource) ListForIndex(ctx context.Context, _ *time.Time) ([]platform.IndexItem, error) {
	parts, err := s.repo.ListAllBoilerParts(ctx, 5000)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, nil
	}
	// Группируем запчасти по (document_id, assembly), чтобы каждый узел котла стал компактным и точным чанком поиска.
	type groupKey struct {
		docID    int64
		assembly string
	}
	order := make([]groupKey, 0)
	groups := map[groupKey][]BoilerPart{}
	for _, p := range parts {
		k := groupKey{docID: p.DocumentID, assembly: p.Assembly}
		if _, exists := groups[k]; !exists {
			order = append(order, k)
		}
		groups[k] = append(groups[k], p)
	}

	out := make([]platform.IndexItem, 0, len(order))
	for _, k := range order {
		items := groups[k]
		first := items[0]
		var b strings.Builder
		title := fmt.Sprintf("Запчасти %s %s", first.Brand, first.BoilerModel)
		title = strings.TrimSpace(title)
		if first.Assembly != "" {
			title += " — " + first.Assembly
		}
		fmt.Fprintf(&b, "# %s\nДокумент #%d «%s»\n\n", title, first.DocumentID, first.DocumentTitle)
		var updatedAt time.Time
		for _, p := range items {
			b.WriteString("- ")
			b.WriteString(FormatBoilerPartLine(p))
			b.WriteByte('\n')
			if p.CreatedAt.After(updatedAt) {
				updatedAt = p.CreatedAt
			}
		}
		out = append(out, platform.IndexItem{
			SourceType: "boiler_parts",
			SourceID:   fmt.Sprintf("%d:%s", first.DocumentID, first.Assembly),
			Title:      title,
			Content:    b.String(),
			Path:       fmt.Sprintf("/docs/%d", first.DocumentID),
			Metadata: map[string]string{
				"document_id":  strconv.FormatInt(first.DocumentID, 10),
				"brand":        first.Brand,
				"boiler_model": first.BoilerModel,
				"assembly":     first.Assembly,
			},
			UpdatedAt: updatedAt,
		})
	}
	return out, nil
}

func (s *boilerPartsIndexSource) CanView(ctx context.Context, auth *platform.AuthContext, _ string, meta map[string]string) (bool, error) {
	docID, err := strconv.ParseInt(meta["document_id"], 10, 64)
	if err != nil {
		return false, nil
	}
	doc, err := s.repo.GetDocument(ctx, docID)
	if err != nil {
		return false, err
	}
	return CanViewDocument(auth, doc), nil
}
