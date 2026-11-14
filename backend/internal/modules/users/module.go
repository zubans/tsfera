package users

import (
	"context"
	"io/fs"
	"log/slog"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

// Module объединяет сервис, HTTP-хэндлеры и контракт platform.Module для подсистемы пользователей.
type Module struct {
	Service *Service
	Repo    Repository
	Handler *Handler
}

func NewModule(repo Repository, logger *slog.Logger, sessionTTL time.Duration) *Module {
	svc := NewService(repo, sessionTTL)
	return &Module{
		Service: svc,
		Repo:    repo,
		Handler: NewHandler(svc, logger),
	}
}

func (m *Module) Name() string                          { return "users" }
func (m *Module) Migrations() fs.FS                     { return migrationsFS }
func (m *Module) MountHTTP(r chi.Router)                { m.Handler.Mount(r) }
func (m *Module) Workers() []platform.PeriodicJob       { return nil }
func (m *Module) Tools() []platform.ToolDefinition      { return nil }
func (m *Module) SearchSources() []platform.IndexSource { return nil }

func (m *Module) Permissions() []platform.PermissionDef {
	return []platform.PermissionDef{
		{Key: "admin.user.view", Group: "admin", Resource: "user", Action: "view", Description: "Админ.Пользователи.Просмотр"},
		{Key: "admin.user.create", Group: "admin", Resource: "user", Action: "create", Description: "Админ.Пользователи.Создание"},
		{Key: "admin.user.update", Group: "admin", Resource: "user", Action: "update", Description: "Админ.Пользователи.Редактирование"},
		{Key: "admin.user.change_password", Group: "admin", Resource: "user", Action: "change_password", Description: "Админ.Пользователи.Смена пароля"},
		{Key: "admin.user.without_subdivision", Group: "admin", Resource: "user", Action: "without_subdivision", Description: "Админ.Пользователи.Без ограничения по подразделению"},
		{Key: "admin.role.view", Group: "admin", Resource: "role", Action: "view", Description: "Админ.Роли.Просмотр"},
		{Key: "admin.role.update", Group: "admin", Resource: "role", Action: "update", Description: "Админ.Роли.Редактирование"},
		{Key: "admin.role.delete", Group: "admin", Resource: "role", Action: "delete", Description: "Админ.Роли.Удаление"},
	}
}

func (m *Module) SyncPermissions(ctx context.Context, all []platform.PermissionDef) error {
	return m.Repo.SyncPermissions(ctx, all)
}
