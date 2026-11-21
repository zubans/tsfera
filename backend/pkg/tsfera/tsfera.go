package tsfera

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/zubans/tsfera/backend/internal/modules/ai"
	"github.com/zubans/tsfera/backend/internal/modules/chat"
	"github.com/zubans/tsfera/backend/internal/modules/crm"
	"github.com/zubans/tsfera/backend/internal/modules/docs"
	"github.com/zubans/tsfera/backend/internal/modules/users"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type BootstrapAdminConfig struct {
	Username string
	Password string
}

// Config описывает параметры подключения модуля Теплосфера к внешнему или standalone Go-сервису.
type Config struct {
	TelephonyProvider string
	MangoBaseURL      string
	MangoAPIKey       string
	MangoAPISecret    string
	SIPWebSocketURL   string

	Pool                   *pgxpool.Pool
	Authorizer             platform.Authorizer
	EventBus               platform.EventBus
	Logger                 *slog.Logger
	StorageDir             string
	SessionTTL             time.Duration
	MTSWebhookSecret       string
	MTSBaseURL             string
	MTSAPIToken            string
	DaDataAPIKey           string
	DaDataBaseURL          string
	CloudPaymentsPublicID  string
	CloudPaymentsAPISecret string
	CloudPaymentsBaseURL   string
	BootstrapAdmin         *BootstrapAdminConfig
	EnableDocsAndAI        bool
	// AICLIHome — каталог входа консольных агентов (claude, codex, agy): у каждого своя папка внутри.
	AICLIHome string
	// AICLIProxy — прокси для консольных агентов (например, http://user:pass@host:3128).
	AICLIProxy   string
	ExtraModules []platform.Module
	ExtraTools   []platform.ToolDefinition
	ExtraSources []platform.IndexSource
}

// Platform инкапсулирует все модули Теплосферы (users, crm, docs, ai, chat) и контракты расширения.
type Platform struct {
	cfg     Config
	Pool    *pgxpool.Pool
	Bus     platform.EventBus
	Logger  *slog.Logger
	Users   *users.Module
	CRM     *crm.Module
	Docs    *docs.Module
	AI      *ai.Module
	Chat    *chat.Module
	Modules []platform.Module
	Workers *platform.WorkerGroup
}

// OpenPool открывает и проверяет пул подключений PostgreSQL (pgxpool).
func OpenPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("DATABASE_URL is empty")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

// NewPlatform собирает полную модульную платформу (users + crm + docs + ai + chat) или только CRM-ядро (при EnableDocsAndAI=false).
func NewPlatform(ctx context.Context, cfg Config) (*Platform, error) {
	if cfg.Pool == nil {
		return nil, fmt.Errorf("tsfera.Config.Pool must not be nil")
	}
	return newPlatformWithRepos(ctx, cfg, repositories{
		users: users.NewPgRepository(cfg.Pool),
		crm:   crm.NewPgRepository(cfg.Pool),
		docs:  docs.NewPgRepository(cfg.Pool),
		ai:    ai.NewPgRepository(cfg.Pool),
		chat:  chat.NewPgRepository(cfg.Pool),
	})
}

// NewCRMOnly — удобный конструктор для встраивания только модуля CRM Теплосфера и пользователей в существующий Go-проект.
func NewCRMOnly(ctx context.Context, cfg Config) (*Platform, error) {
	cfg.EnableDocsAndAI = false
	return NewPlatform(ctx, cfg)
}

type repositories struct {
	users users.Repository
	crm   crm.Repository
	docs  docs.Repository
	ai    ai.Repository
	chat  chat.Repository
}

func newPlatformWithRepos(_ context.Context, cfg Config, repos repositories) (*Platform, error) {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	bus := cfg.EventBus
	if bus == nil {
		bus = platform.NewInMemoryEventBus()
	}

	usersMod := users.NewModule(repos.users, logger, cfg.SessionTTL)
	usersMod.Service.SetEventBus(bus)
	if cfg.StorageDir != "" {
		usersMod.Service.SetFileStorageDir(cfg.StorageDir)
	}

	auth := cfg.Authorizer
	if auth == nil {
		auth = usersMod.Service
	}

	userDir := &usersDirectoryAdapter{svc: usersMod.Service, repo: repos.users}
	crmMod := crm.NewModule(repos.crm, userDir, auth, bus, crm.Config{
		TelephonyProvider: cfg.TelephonyProvider, MangoBaseURL: cfg.MangoBaseURL, MangoAPIKey: cfg.MangoAPIKey, MangoAPISecret: cfg.MangoAPISecret, SIPWebSocketURL: cfg.SIPWebSocketURL,
		MTSWebhookSecret:       cfg.MTSWebhookSecret,
		MTSBaseURL:             cfg.MTSBaseURL,
		MTSAPIToken:            cfg.MTSAPIToken,
		DaDataAPIKey:           cfg.DaDataAPIKey,
		DaDataBaseURL:          cfg.DaDataBaseURL,
		CloudPaymentsPublicID:  cfg.CloudPaymentsPublicID,
		CloudPaymentsAPISecret: cfg.CloudPaymentsAPISecret,
		CloudPaymentsBaseURL:   cfg.CloudPaymentsBaseURL,
	}, logger)

	p := &Platform{
		cfg:     cfg,
		Pool:    cfg.Pool,
		Bus:     bus,
		Logger:  logger,
		Users:   usersMod,
		CRM:     crmMod,
		Workers: platform.NewWorkerGroup(logger),
	}
	p.Modules = []platform.Module{usersMod, crmMod}

	if cfg.EnableDocsAndAI {
		docsMod := docs.NewModule(repos.docs, auth, crmMod.Service, bus, cfg.StorageDir, logger)
		sources := collectSources([]platform.Module{usersMod, crmMod, docsMod}, cfg.ExtraModules, cfg.ExtraSources)
		docFetcher := &docsFetcherAdapter{svc: docsMod.Service}
		aiMod := ai.NewModule(repos.ai, auth, usersMod.Service, crmMod.Service, docFetcher, bus, logger, sources...)
		crmMod.Service.SetAudioTranscriber(aiMod.Gateway)
		aiMod.Gateway.SetCLIHome(cfg.AICLIHome)
		aiMod.Gateway.SetCLIProxy(cfg.AICLIProxy)

		baseMods := []platform.Module{usersMod, crmMod, docsMod, aiMod}
		tools := collectTools(baseMods, cfg.ExtraModules, cfg.ExtraTools)
		chatMod := chat.NewModule(repos.chat, auth, aiMod.Gateway, aiMod.Search, crmMod.Service, bus, logger, tools...)

		p.Docs = docsMod
		p.AI = aiMod
		p.Chat = chatMod
		p.Modules = append(p.Modules, docsMod, aiMod, chatMod)
	} else {
		p.Modules = append(p.Modules, cfg.ExtraModules...)
	}

	if cfg.EnableDocsAndAI && len(cfg.ExtraModules) > 0 {
		p.Modules = append(p.Modules, cfg.ExtraModules...)
	}

	for _, mod := range p.Modules {
		p.Workers.Add(mod.Workers()...)
	}
	return p, nil
}

// Migrate применяет встроенные SQL-миграции всех активных модулей, синхронизирует RBAC-права и создаёт первичного админа.
func (p *Platform) Migrate(ctx context.Context) error {
	if p.Pool == nil {
		return fmt.Errorf("nil pgx pool")
	}
	sources := make([]platform.MigrationSource, 0, len(p.Modules))
	for _, mod := range p.Modules {
		if fs := mod.Migrations(); fs != nil {
			sources = append(sources, platform.MigrationSource{Module: mod.Name(), FS: fs})
		}
	}
	if err := platform.Migrate(ctx, p.Pool, sources...); err != nil {
		return err
	}
	if err := p.Users.SyncPermissions(ctx, p.Permissions()); err != nil {
		return fmt.Errorf("sync permissions: %w", err)
	}
	if p.cfg.BootstrapAdmin != nil && p.cfg.BootstrapAdmin.Username != "" && p.cfg.BootstrapAdmin.Password != "" {
		if err := p.Users.Service.EnsureInitialAdmin(ctx, p.cfg.BootstrapAdmin.Username, p.cfg.BootstrapAdmin.Password); err != nil {
			return fmt.Errorf("bootstrap admin: %w", err)
		}
	}
	return nil
}

// Permissions возвращает объединение всех RBAC-ключей активных модулей.
func (p *Platform) Permissions() []platform.PermissionDef {
	seen := map[string]bool{}
	var out []platform.PermissionDef
	for _, mod := range p.Modules {
		for _, perm := range mod.Permissions() {
			if !seen[perm.Key] {
				seen[perm.Key] = true
				out = append(out, perm)
			}
		}
	}
	return out
}

// Mount подключает HTTP-маршруты и WebSocket-эндпоинты всех модулей к переданному chi.Router внешнего проекта.
func (p *Platform) Mount(r chi.Router) {
	for _, mod := range p.Modules {
		if mod.Name() == "users" {
			// Изолируем маршруты пользователей под /api/v1, чтобы не конфликтовать со SPA-маршрутами хоста,
			// и дополнительно монтируем legacy мобильную авторизацию /api/auth/{login,logout,refresh}.
			m := mod
			r.Route("/api/v1", func(vr chi.Router) {
				m.MountHTTP(vr)
			})
			if p.Users != nil && p.Users.Handler != nil {
				r.Route("/api/auth", func(ar chi.Router) {
					p.Users.Handler.MountMobileAuth(ar)
				})
			}
			continue
		}
		mod.MountHTTP(r)
	}
}

// Handler строит готовый http.Handler для standalone-запуска (с /healthz, /readyz и опциональной раздачей SPA).
func (p *Platform) Handler(spaDir string) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		platform.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if p.Pool != nil {
			if err := p.Pool.Ping(r.Context()); err != nil {
				platform.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "db_unavailable"})
				return
			}
		}
		platform.WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	p.Mount(r)

	if strings.TrimSpace(spaDir) != "" {
		mountSPA(r, spaDir)
	}
	return r
}

func (p *Platform) StartWorkers(ctx context.Context) {
	p.Workers.Start(ctx)
}

func (p *Platform) WaitWorkers() {
	p.Workers.Wait()
}

func collectSources(coreMods, extraMods []platform.Module, extra []platform.IndexSource) []platform.IndexSource {
	var out []platform.IndexSource
	for _, m := range append(append([]platform.Module(nil), coreMods...), extraMods...) {
		out = append(out, m.SearchSources()...)
	}
	return append(out, extra...)
}

func collectTools(coreMods, extraMods []platform.Module, extra []platform.ToolDefinition) []platform.ToolDefinition {
	var out []platform.ToolDefinition
	for _, m := range append(append([]platform.Module(nil), coreMods...), extraMods...) {
		out = append(out, m.Tools()...)
	}
	return append(out, extra...)
}

func mountSPA(r chi.Router, spaDir string) {
	cleanDir := filepath.Clean(spaDir)
	fsHandler := http.FileServer(http.Dir(cleanDir))
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") || strings.HasPrefix(req.URL.Path, "/ws/") || strings.HasPrefix(req.URL.Path, "/telephony/") {
			http.NotFound(w, req)
			return
		}
		rel := strings.TrimPrefix(filepath.Clean(req.URL.Path), string(os.PathSeparator))
		candidate := filepath.Join(cleanDir, rel)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			fsHandler.ServeHTTP(w, req)
			return
		}
		indexFile := filepath.Join(cleanDir, "index.html")
		if _, err := os.Stat(indexFile); err == nil {
			http.ServeFile(w, req, indexFile)
			return
		}
		http.NotFound(w, req)
	})
}

// --- Межмодульные адаптеры ---

type usersDirectoryAdapter struct {
	svc  *users.Service
	repo users.Repository
}

func (a *usersDirectoryAdapter) GetUserBrief(ctx context.Context, userID int64) (*crm.UserBrief, error) {
	u, err := a.svc.GetUser(ctx, userID)
	if err != nil || u == nil {
		return nil, err
	}
	return &crm.UserBrief{
		ID:            u.ID,
		Username:      u.Username,
		FullName:      u.FullName,
		Color:         u.Color,
		SubdivisionID: u.SubdivisionID,
		IsEngineer:    u.IsEngineer,
		IsDispatcher:  u.IsDispatcher,
		Active:        u.Active,
	}, nil
}

func (a *usersDirectoryAdapter) ListActiveWorkers(ctx context.Context, isEngineer, isDispatcher bool, subdivisionID *int64) ([]crm.UserBrief, error) {
	list, err := a.svc.ListUsers(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]crm.UserBrief, 0, len(list))
	for _, u := range list {
		if isEngineer && !u.IsEngineer {
			continue
		}
		if isDispatcher && !u.IsDispatcher {
			continue
		}
		if subdivisionID != nil && (u.SubdivisionID == nil || *u.SubdivisionID != *subdivisionID) {
			continue
		}
		out = append(out, crm.UserBrief{
			ID:            u.ID,
			Username:      u.Username,
			FullName:      u.FullName,
			Color:         u.Color,
			SubdivisionID: u.SubdivisionID,
			IsEngineer:    u.IsEngineer,
			IsDispatcher:  u.IsDispatcher,
			Active:        u.Active,
		})
	}
	return out, nil
}

func (a *usersDirectoryAdapter) ListUserIDsByPermission(ctx context.Context, perm string) ([]int64, error) {
	return a.svc.ListUserIDsByPermission(ctx, perm)
}

type docsFetcherAdapter struct {
	svc *docs.Service
}

func (a *docsFetcherAdapter) FetchDocumentMarkdown(ctx context.Context, auth *platform.AuthContext, docID int64) (string, error) {
	doc, err := a.svc.GetDocument(ctx, auth, docID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("# %s\n\n%s", doc.Title, doc.Content), nil
}
