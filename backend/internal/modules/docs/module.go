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
				return platform.ToolResult{
					Content: fmt.Sprintf("# %s (v%d)\n\n%s", doc.Title, doc.Version, doc.Content),
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
		out = append(out, platform.IndexItem{
			SourceType: "doc_attachment",
			SourceID:   strconv.FormatInt(a.ID, 10),
			Title:      a.Filename,
			Content:    a.ExtractedText,
			Path:       fmt.Sprintf("/docs/%d", a.DocumentID),
			Metadata: map[string]string{
				"document_id": strconv.FormatInt(a.DocumentID, 10),
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
