package users

import (
	"context"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// Repository задаёт узкий контракт хранения пользователей, ролей, сессий, токенов и доступа к ИИ-моделям.
type Repository interface {
	SyncPermissions(ctx context.Context, defs []platform.PermissionDef) error

	CreateUser(ctx context.Context, u *User) (int64, error)
	UpdateUser(ctx context.Context, u *User) error
	UpdatePasswordHash(ctx context.Context, userID int64, hash string) error
	GetUserByID(ctx context.Context, id int64) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)
	ListUsers(ctx context.Context, activeOnly bool) ([]User, error)
	CountActiveSuperAdmins(ctx context.Context, excludeUserID int64) (int, error)
	GetUserPermissions(ctx context.Context, userID int64) ([]string, error)

	ListRoles(ctx context.Context) ([]Role, error)
	GetRole(ctx context.Context, name string) (*Role, error)
	SaveRole(ctx context.Context, role *Role) error
	DeleteRole(ctx context.Context, name string) error
	ListPermissions(ctx context.Context) ([]platform.PermissionDef, error)

	CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error
	GetSessionUserID(ctx context.Context, tokenHash string, now time.Time) (int64, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteUserSessions(ctx context.Context, userID int64) error
	UpdateLastLogin(ctx context.Context, userID int64, at time.Time) error

	CreateAPIToken(ctx context.Context, tok *APIToken) (int64, error)
	ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error)
	GetAPITokenByHash(ctx context.Context, tokenHash string, now time.Time) (*APIToken, error)
	TouchAPIToken(ctx context.Context, tokenID int64, at time.Time) error
	RefreshAPITokenByHash(ctx context.Context, tokenHash string, newExpiresAt time.Time) (*APIToken, error)
	DeleteAPITokenByHash(ctx context.Context, tokenHash string) error
	DeleteAPIToken(ctx context.Context, tokenID int64, userID int64) error

	ListInventoryItems(ctx context.Context, category string) ([]InventoryItem, error)
	CreateInventoryItem(ctx context.Context, item *InventoryItem) error
	DeleteInventoryItem(ctx context.Context, id int64) error

	LogLoginEvent(ctx context.Context, ev *LoginEvent) error
	ListLoginEvents(ctx context.Context, userID *int64, event string, limit int) ([]LoginEvent, error)

	GetModelAccess(ctx context.Context, modelID int64) (*ModelAccessPolicy, error)
	SetModelAccess(ctx context.Context, policy ModelAccessPolicy) error
	ListModelAccess(ctx context.Context) ([]ModelAccessPolicy, error)

	ListUserIDsByPermission(ctx context.Context, perm string) ([]int64, error)
	CreateFile(ctx context.Context, f *StoredFile) error
	GetFile(ctx context.Context, id int64) (*StoredFile, error)
}
