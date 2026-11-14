package ai

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Module struct {
	Repo    Repository
	Gateway *Gateway
	Search  *SearchAndComposeService
	handler *Handler
}

func NewModule(
	repo Repository,
	auth platform.Authorizer,
	access ModelAccessPolicy,
	resolver platform.EntityResolver,
	docs DocFetcher,
	bus platform.EventBus,
	logger *slog.Logger,
	sources ...platform.IndexSource,
) *Module {
	gw := NewGateway(repo, access, logger)
	search := NewSearchAndComposeService(repo, gw, sources, resolver, docs, logger)
	if bus != nil {
		bus.Subscribe("docs.document.upserted", func(ctx context.Context, _ platform.Event) {
			_, _ = search.ReindexAll(ctx)
		})
		bus.Subscribe("docs.document.deleted", func(ctx context.Context, event platform.Event) {
			if payload, ok := event.Payload.(map[string]any); ok {
				if id, ok := payload["document_id"]; ok {
					_ = repo.DeleteSourceChunks(ctx, "doc", fmt.Sprint(id))
				}
			}
		})
		bus.Subscribe("docs.attachment.added", func(ctx context.Context, _ platform.Event) {
			_, _ = search.ReindexAll(ctx)
		})
	}
	return &Module{
		Repo:    repo,
		Gateway: gw,
		Search:  search,
		handler: NewHandler(repo, gw, search, auth, logger),
	}
}

func (m *Module) Name() string { return "ai" }

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
		{Key: "ai.use", Group: "ИИ", Resource: "ai", Action: "use", Description: "Доступ к ИИ-моделям"},
		{Key: "ai.search", Group: "ИИ", Resource: "search", Action: "read", Description: "Гибридный интеллектуальный поиск по базе знаний и CRM"},
		{Key: "ai.compose", Group: "ИИ", Resource: "compose", Action: "use", Description: "Генерация черновиков документов с ИИ"},
		{Key: "ai.admin", Group: "ИИ", Resource: "admin", Action: "manage", Description: "Настройка LLM-провайдеров, моделей и просмотр usage-логов"},
	}
}

func (m *Module) Workers() []platform.PeriodicJob {
	return []platform.PeriodicJob{
		{
			Name:     "ai_incremental_indexer",
			Interval: 5 * time.Minute,
			Run: func(ctx context.Context) error {
				_, err := m.Search.ReindexAll(ctx)
				return err
			},
		},
	}
}

func (m *Module) Tools() []platform.ToolDefinition {
	return []platform.ToolDefinition{
		{
			Name:        "ai_hybrid_search",
			Description: "Гибридный поиск (полнотекст + семантика) по регламентам, вложениям, контрагентам и оборудованию Теплосферы.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
					"limit": map[string]any{"type": "integer"},
				},
				"required": []string{"query"},
			},
			Execute: func(ctx context.Context, auth *platform.AuthContext, args json.RawMessage) (platform.ToolResult, error) {
				var in struct {
					Query string `json:"query"`
					Limit int    `json:"limit"`
				}
				_ = json.Unmarshal(args, &in)
				if in.Limit <= 0 || in.Limit > 15 {
					in.Limit = 6
				}
				items, err := m.Search.HybridSearch(ctx, auth, in.Query, in.Limit)
				if err != nil {
					return platform.ToolResult{}, err
				}
				if len(items) == 0 {
					return platform.ToolResult{Content: "Подходящие материалы не найдены."}, nil
				}
				var b strings.Builder
				var refs []platform.SourceRef
				for _, r := range items {
					fmt.Fprintf(&b, "- [%s] %s (%s): %s\n", r.SourceType, r.Title, r.Path, r.Snippet)
					refs = append(refs, platform.SourceRef{
						SourceType: r.SourceType,
						SourceID:   r.SourceID,
						Title:      r.Title,
						Anchor:     r.Anchor,
						Snippet:    r.Snippet,
						Path:       r.Path,
					})
				}
				return platform.ToolResult{Content: b.String(), Sources: refs}, nil
			},
		},
	}
}

func (m *Module) SearchSources() []platform.IndexSource { return nil }
