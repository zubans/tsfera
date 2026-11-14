package platform

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// AuthContext описывает аутентифицированного пользователя или токен агента/мобильного клиента.
type AuthContext struct {
	UserID        int64
	Username      string
	DisplayName   string
	SubdivisionID *int64
	IsSuperAdmin  bool
	Permissions   map[string]bool

	// Поля для API-токенов (мобильный клиент или ИИ-агент).
	IsToken       bool
	TokenID       int64
	TokenName     string
	TokenCanRead  bool
	TokenCanWrite bool
}

// HasPermission проверяет наличие права (у супер-администратора есть все права; у токена учитывается флаг записи).
func (a *AuthContext) HasPermission(perm string) bool {
	if a == nil {
		return false
	}
	if a.IsToken {
		if !a.TokenCanRead && !a.TokenCanWrite {
			return false
		}
		if isWritePermission(perm) && !a.TokenCanWrite {
			return false
		}
	}
	if a.IsSuperAdmin {
		return true
	}
	return a.Permissions[perm]
}

func isWritePermission(perm string) bool {
	return len(perm) > 6 && (slices.Contains([]string{
		".create", ".update", ".rating", ".update_comment", ".update_executor", ".change_password", ".delete", ".write", ".close", ".execute", ".manage", ".admin", ".run",
	}, perm[max(0, lastDotIndex(perm)):]))
}

func lastDotIndex(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return 0
}

type authCtxKey struct{}

// WithAuth помещает AuthContext в context.Context.
func WithAuth(ctx context.Context, auth *AuthContext) context.Context {
	return context.WithValue(ctx, authCtxKey{}, auth)
}

// AuthFromContext извлекает AuthContext из context.Context.
func AuthFromContext(ctx context.Context) (*AuthContext, bool) {
	v, ok := ctx.Value(authCtxKey{}).(*AuthContext)
	return v, ok && v != nil
}

// Authorizer разрешает входящий HTTP-запрос (cookie сессии, Bearer-токен или legacy X-API-key).
type Authorizer interface {
	ResolveRequest(r *http.Request) (*AuthContext, error)
}

// RequireAuth возвращает middleware аутентификации; при optional=true пропускает анонимный запрос.
func RequireAuth(auth Authorizer, optional bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if auth == nil {
				if optional {
					next.ServeHTTP(w, r)
					return
				}
				WriteError(w, nil, NewUnauthorized("Провайдер авторизации не настроен"))
				return
			}
			ac, err := auth.ResolveRequest(r)
			if err != nil {
				if optional {
					next.ServeHTTP(w, r)
					return
				}
				WriteError(w, nil, NewUnauthorized("Требуется авторизация"))
				return
			}
			if ac.IsToken && !ac.TokenCanWrite && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
				WriteError(w, nil, NewForbidden("Токен не разрешает запись"))
				return
			}
			next.ServeHTTP(w, r.WithContext(WithAuth(r.Context(), ac)))
		})
	}
}

// RequirePermission проверяет конкретное RBAC-право на маршруте.
func RequirePermission(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ac, ok := AuthFromContext(r.Context())
			if !ok {
				WriteError(w, nil, NewUnauthorized("Требуется авторизация"))
				return
			}
			if !ac.HasPermission(perm) {
				WriteError(w, nil, NewForbidden("Недостаточно прав: требуется "+perm))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// PermissionDef описывает право доступа, которое модуль регистрирует в общем реестре RBAC.
type PermissionDef struct {
	Key         string `json:"key"`
	Group       string `json:"group"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	Description string `json:"description"`
}

// Event представляет внутреннее событие между модулями (например, звонок МТС, уведомление, индексация).
type Event struct {
	Topic      string    `json:"topic"`
	UserIDs    []int64   `json:"user_ids,omitempty"`
	Payload    any       `json:"payload"`
	OccurredAt time.Time `json:"occurred_at"`
}

// EventHandler подписывается на события EventBus.
type EventHandler func(ctx context.Context, ev Event)

// EventBus обеспечивает слабое связывание модулей без прямого импорта внутренних сервисов друг друга.
type EventBus interface {
	Publish(ctx context.Context, ev Event)
	Subscribe(topic string, fn EventHandler) (unsubscribe func())
}

// InMemoryEventBus — потокобезопасная шина событий для одного инстанса/модульного монолита.
type InMemoryEventBus struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[string]map[uint64]EventHandler
}

func NewInMemoryEventBus() *InMemoryEventBus {
	return &InMemoryEventBus{
		subscribers: make(map[string]map[uint64]EventHandler),
	}
}

func (b *InMemoryEventBus) Publish(ctx context.Context, ev Event) {
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now()
	}
	b.mu.RLock()
	var handlers []EventHandler
	for _, h := range b.subscribers[ev.Topic] {
		handlers = append(handlers, h)
	}
	for _, h := range b.subscribers["*"] {
		handlers = append(handlers, h)
	}
	b.mu.RUnlock()

	for _, h := range handlers {
		h(ctx, ev)
	}
}

func (b *InMemoryEventBus) Subscribe(topic string, fn EventHandler) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	if b.subscribers[topic] == nil {
		b.subscribers[topic] = make(map[uint64]EventHandler)
	}
	b.subscribers[topic][id] = fn
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subscribers[topic], id)
	}
}

// ToolDefinition описывает инструмент ИИ, поставляемый модулем (docs, crm и т.д.).
type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Execute     func(ctx context.Context, auth *AuthContext, args json.RawMessage) (ToolResult, error)
}

// ToolResult содержит текстовый результат работы инструмента и ссылки-источники для UI.
type ToolResult struct {
	Content string      `json:"content"`
	Sources []SourceRef `json:"sources,omitempty"`
}

// SourceRef — кликабельная ссылка на документ, вложение или карточку CRM.
type SourceRef struct {
	SourceType string `json:"source_type"`
	SourceID   string `json:"source_id"`
	Title      string `json:"title"`
	Anchor     string `json:"anchor,omitempty"`
	Snippet    string `json:"snippet,omitempty"`
	Path       string `json:"path,omitempty"`
}

// IndexItem — единица текста, поставляемая модулем в гибридный поисковый индекс ИИ.
type IndexItem struct {
	SourceType string            `json:"source_type"`
	SourceID   string            `json:"source_id"`
	Title      string            `json:"title"`
	Content    string            `json:"content"`
	Path       string            `json:"path"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// IndexSource описывает источник индексации и фильтр видимости по правам пользователя.
type IndexSource interface {
	SourceType() string
	ListForIndex(ctx context.Context, since *time.Time) ([]IndexItem, error)
	CanView(ctx context.Context, auth *AuthContext, sourceID string, meta map[string]string) (bool, error)
}

// EntitySummary — текстовая выжимка сущности (для Compose / контекстного чата / ссылок).
type EntitySummary struct {
	EntityType string `json:"entity_type"`
	EntityID   int64  `json:"entity_id"`
	Title      string `json:"title"`
	Markdown   string `json:"markdown"`
	Path       string `json:"path"`
}

// EntityResolver позволяет модулям docs/ai/chat запрашивать контекст сущностей CRM без жёсткой связности.
type EntityResolver interface {
	ResolveEntity(ctx context.Context, auth *AuthContext, entityType string, entityID int64) (*EntitySummary, error)
}

// Module — единый контракт подключения подсистемы к платформе или к большему Go-проекту.
type Module interface {
	Name() string
	Migrations() fs.FS
	Permissions() []PermissionDef
	MountHTTP(r chi.Router)
	Workers() []PeriodicJob
	Tools() []ToolDefinition
	SearchSources() []IndexSource
}
