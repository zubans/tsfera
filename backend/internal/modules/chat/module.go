package chat

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Module struct {
	Service *Service
	handler *Handler
}

func NewModule(
	repo Repository,
	auth platform.Authorizer,
	llm LLMRunner,
	searcher KnowledgeSearcher,
	resolver platform.EntityResolver,
	bus platform.EventBus,
	logger *slog.Logger,
	tools ...platform.ToolDefinition,
) *Module {
	svc := NewService(repo, llm, searcher, resolver, logger, tools...)
	if bus != nil {
		// Автоматически отзываем публичные ссылки на диалоги при деактивации сотрудника.
		bus.Subscribe("users.user.deactivated", func(ctx context.Context, ev platform.Event) {
			if m, ok := ev.Payload.(map[string]any); ok {
				if uid, ok := m["user_id"].(int64); ok && uid > 0 {
					_ = svc.RevokeOwnerShares(ctx, uid)
				}
			}
		})
	}
	return &Module{
		Service: svc,
		handler: NewHandler(svc, auth, logger),
	}
}

func (m *Module) Name() string { return "chat" }

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
		{Key: "chat.use", Group: "Чат ИИ", Resource: "chat", Action: "use", Description: "Диалоги с ИИ-ассистентом (RAG и агентский режим)"},
		{Key: "chat.share", Group: "Чат ИИ", Resource: "share", Action: "write", Description: "Создание read-only ссылок на диалоги"},
	}
}

func (m *Module) Workers() []platform.PeriodicJob       { return nil }
func (m *Module) Tools() []platform.ToolDefinition      { return nil }
func (m *Module) SearchSources() []platform.IndexSource { return nil }
