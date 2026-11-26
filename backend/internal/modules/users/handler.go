package users

import (
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

// Handler — тонкий HTTP-слой модуля пользователей (не держит репозиториев).
type Handler struct {
	svc    *Service
	logger *slog.Logger
}

func NewHandler(svc *Service, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, logger: logger}
}

func (h *Handler) Mount(r chi.Router) {
	r.Post("/auth/login", h.login)
	r.Post("/auth/logout", h.logout)
	// Legacy-совместимость для мобильного API: {data: {token, expired_at}, errors}
	r.Post("/doc-mobile-auth/login", h.legacyMobileLogin)

	r.Group(func(pr chi.Router) {
		pr.Use(platform.RequireAuth(h.svc, false))
		pr.Post("/doc-mobile-auth/logout", h.legacyMobileLogout)
		pr.Post("/doc-mobile-auth/refresh", h.legacyMobileRefresh)
		pr.Get("/me", h.me)
		pr.Put("/me/password", h.changeMyPassword)
		pr.Get("/users", h.listActiveUsers)
		pr.Post("/files", h.uploadFile)
		pr.Get("/files/{id}", h.downloadFile)

		pr.Get("/tokens", h.listMyTokens)
		pr.Post("/tokens", h.createMyToken)
		pr.Delete("/tokens/{id}", h.deleteMyToken)

		pr.Group(func(admin chi.Router) {
			admin.Use(platform.RequirePermission("admin.user.view"))
			admin.Get("/admin/users", h.listAllUsers)
			admin.Get("/admin/users/{id}", h.getUser)
			admin.With(platform.RequirePermission("admin.user.create")).Post("/admin/users", h.createUser)
			admin.With(platform.RequirePermission("admin.user.update")).Put("/admin/users/{id}", h.updateUser)
			admin.With(platform.RequirePermission("admin.user.change_password")).Post("/admin/users/{id}/password", h.adminChangePassword)
			admin.Get("/admin/inventory", h.listInventory)
			admin.Post("/admin/inventory", h.createInventory)
			admin.Delete("/admin/inventory/{id}", h.deleteInventory)

			admin.With(platform.RequirePermission("admin.role.view")).Get("/admin/roles", h.listRoles)
			admin.With(platform.RequirePermission("admin.role.view")).Get("/admin/permissions", h.listPermissions)
			admin.With(platform.RequireAnyPermission("admin.role.create", "admin.role.update")).Put("/admin/roles/{name}", h.saveRole)
			admin.With(platform.RequirePermission("admin.role.delete")).Delete("/admin/roles/{name}", h.deleteRole)

			admin.Get("/admin/logins", h.listLogins)
			admin.With(platform.RequirePermission("admin.model_access.manage")).Get("/admin/model-access", h.listModelAccess)
			admin.With(platform.RequirePermission("admin.model_access.manage")).Put("/admin/model-access/{modelId}", h.setModelAccess)
		})
	})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := platform.DecodeJSON(r, &req); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	out, err := h.svc.Login(r.Context(), LoginInput{
		Username:  req.Username,
		Password:  req.Password,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    out.SessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
	})
	platform.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) legacyMobileLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := platform.DecodeJSON(r, &req); err != nil {
		platform.WriteJSON(w, http.StatusBadRequest, platform.LegacyAPIEnvelope{Errors: err.Error()})
		return
	}
	out, err := h.svc.Login(r.Context(), LoginInput{
		Username: req.Username, Password: req.Password, IP: clientIP(r), UserAgent: r.UserAgent(),
	})
	if err != nil {
		platform.WriteJSON(w, platform.HTTPStatusForError(err), platform.LegacyAPIEnvelope{Errors: err.Error()})
		return
	}
	tok, raw, err := h.svc.CreateAPIToken(r.Context(), out.User.ID, CreateTokenInput{
		Name: "mobile", CanRead: true, CanWrite: true, TTLDays: 30,
	})
	if err != nil {
		platform.WriteJSON(w, http.StatusInternalServerError, platform.LegacyAPIEnvelope{Errors: err.Error()})
		return
	}
	platform.WriteJSON(w, http.StatusOK, platform.LegacyAPIEnvelope{
		Data: map[string]any{"token": raw, "expired_at": tok.ExpiresAt},
	})
}

// MountMobileAuth монтирует legacy-маршруты мобильного API (/api/auth/{login,logout,refresh}).
func (h *Handler) MountMobileAuth(r chi.Router) {
	r.Post("/login", h.legacyMobileLogin)
	r.Group(func(pr chi.Router) {
		pr.Use(platform.RequireAuth(h.svc, false))
		pr.Post("/logout", h.legacyMobileLogout)
		pr.Post("/refresh", h.legacyMobileRefresh)
	})
}

func extractAPITokenRaw(r *http.Request) string {
	if raw := strings.TrimSpace(r.Header.Get("X-API-key")); raw != "" {
		return raw
	}
	if hdr := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(hdr), "bearer ") {
		return strings.TrimSpace(hdr[7:])
	}
	return ""
}

func (h *Handler) legacyMobileLogout(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	raw := extractAPITokenRaw(r)
	if err := h.svc.RevokeAPITokenByRaw(r.Context(), raw, clientIP(r), r.UserAgent(), ac); err != nil {
		platform.WriteJSON(w, platform.HTTPStatusForError(err), platform.LegacyAPIEnvelope{Errors: err.Error()})
		return
	}
	platform.WriteJSON(w, http.StatusOK, platform.LegacyAPIEnvelope{Data: map[string]bool{"ok": true}})
}

func (h *Handler) legacyMobileRefresh(w http.ResponseWriter, r *http.Request) {
	raw := extractAPITokenRaw(r)
	tok, refreshed, err := h.svc.RefreshAPITokenByRaw(r.Context(), raw)
	if err != nil {
		platform.WriteJSON(w, platform.HTTPStatusForError(err), platform.LegacyAPIEnvelope{Errors: err.Error()})
		return
	}
	platform.WriteJSON(w, http.StatusOK, platform.LegacyAPIEnvelope{
		Data: map[string]any{"token": refreshed, "expired_at": tok.ExpiresAt},
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	raw := ""
	if c, err := r.Cookie(SessionCookieName); err == nil {
		raw = c.Value
	}
	ac, _ := h.svc.ResolveRequest(r)
	_ = h.svc.Logout(r.Context(), raw, clientIP(r), r.UserAgent(), ac)
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	u, err := h.svc.GetUser(r.Context(), ac.UserID)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	perms := make([]string, 0, len(ac.Permissions))
	for k, ok := range ac.Permissions {
		if ok {
			perms = append(perms, k)
		}
	}
	platform.WriteJSON(w, http.StatusOK, map[string]any{
		"user":          u,
		"is_superadmin": ac.IsSuperAdmin,
		"permissions":   perms,
		"is_token":      ac.IsToken,
	})
}

func (h *Handler) changeMyPassword(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var req struct {
		Password string `json:"password"`
	}
	if err := platform.DecodeJSON(r, &req); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.ChangePassword(r.Context(), ac.UserID, req.Password); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) listActiveUsers(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListUsers(r.Context(), true)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, list)
}

func (h *Handler) listAllUsers(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	list, err := h.svc.ListUsersForCaller(r.Context(), ac, false)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, list)
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Некорректный ID", nil))
		return
	}
	u, err := h.svc.GetUser(r.Context(), id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.CheckSubdivisionAccess(ac, u.SubdivisionID); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	var req CreateUserInput
	if err := platform.DecodeJSON(r, &req); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.CheckSubdivisionAccess(ac, req.SubdivisionID); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	u, err := h.svc.CreateUser(r.Context(), req)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, u)
}

func (h *Handler) updateUser(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Некорректный ID", nil))
		return
	}
	existing, err := h.svc.GetUser(r.Context(), id)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.CheckSubdivisionAccess(ac, existing.SubdivisionID); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	var req UpdateUserInput
	if err := platform.DecodeJSON(r, &req); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.CheckSubdivisionAccess(ac, req.SubdivisionID); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	u, err := h.svc.UpdateUser(r.Context(), id, req)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, u)
}

func (h *Handler) listInventory(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListInventoryItems(r.Context(), r.URL.Query().Get("category"))
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	ac, _ := platform.AuthFromContext(r.Context())
	visible := items[:0]
	for _, item := range items {
		if ac.HasPermission("admin.inventory." + item.Category + ".view") {
			visible = append(visible, item)
		}
	}
	items = visible

	platform.WriteJSON(w, http.StatusOK, items)
}

func (h *Handler) createInventory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Category string `json:"category"`
		Name     string `json:"name"`
	}
	if err := platform.DecodeJSON(r, &req); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	ac, _ := platform.AuthFromContext(r.Context())
	if !ac.HasPermission("admin.inventory." + req.Category + ".create") {
		platform.WriteError(w, h.logger, platform.NewForbidden("Нет права создавать записи этого справочника"))
		return
	}

	item, err := h.svc.CreateInventoryItem(r.Context(), req.Category, req.Name)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, item)
}

func (h *Handler) deleteInventory(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Некорректный ID", nil))
		return
	}
	ac, _ := platform.AuthFromContext(r.Context())
	items, err := h.svc.ListInventoryItems(r.Context(), "")
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	allowed := false
	for _, item := range items {
		if item.ID == id {
			allowed = ac.HasPermission("admin.inventory." + item.Category + ".delete")
		}
	}
	if !allowed {
		platform.WriteError(w, h.logger, platform.NewForbidden("Нет права удалять запись этого справочника"))
		return
	}

	if err := h.svc.DeleteInventoryItem(r.Context(), id); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) adminChangePassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Некорректный ID", nil))
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := platform.DecodeJSON(r, &req); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	if err := h.svc.ChangePassword(r.Context(), id, req.Password); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handler) listRoles(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListRoles(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, list)
}

func (h *Handler) listPermissions(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListPermissions(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, list)
}

func (h *Handler) saveRole(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ac, _ := platform.AuthFromContext(r.Context())
	roles, err := h.svc.ListRoles(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	action := "create"
	for _, role := range roles {
		if role.Name == name {
			action = "update"
		}
	}
	if !ac.HasPermission("admin.role." + action) {
		platform.WriteError(w, h.logger, platform.NewForbidden("Нет права на операцию с ролью"))
		return
	}

	var req struct {
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := platform.DecodeJSON(r, &req); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	role, err := h.svc.SaveRole(r.Context(), name, req.Description, req.Permissions)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, role)
}

func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.svc.DeleteRole(r.Context(), name); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listMyTokens(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	list, err := h.svc.ListAPITokens(r.Context(), ac.UserID)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, list)
}

func (h *Handler) createMyToken(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	if ac.IsToken {
		platform.WriteError(w, h.logger, platform.NewForbidden("Управление токенами доступно только через пользовательскую сессию"))
		return
	}
	var req CreateTokenInput
	if err := platform.DecodeJSON(r, &req); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	tok, raw, err := h.svc.CreateAPIToken(r.Context(), ac.UserID, req)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusCreated, map[string]any{
		"token":  tok,
		"secret": raw,
	})
}

func (h *Handler) deleteMyToken(w http.ResponseWriter, r *http.Request) {
	ac, _ := platform.AuthFromContext(r.Context())
	if ac.IsToken {
		platform.WriteError(w, h.logger, platform.NewForbidden("Управление токенами доступно только через пользовательскую сессию"))
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Некорректный ID", nil))
		return
	}
	if err := h.svc.DeleteAPIToken(r.Context(), id, ac.UserID); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listLogins(w http.ResponseWriter, r *http.Request) {
	var uid *int64
	if raw := r.URL.Query().Get("userId"); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			uid = &id
		}
	}
	limit := 300
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	list, err := h.svc.ListLoginEvents(r.Context(), uid, r.URL.Query().Get("event"), limit)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, list)
}

func (h *Handler) listModelAccess(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListModelAccess(r.Context())
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, list)
}

func (h *Handler) setModelAccess(w http.ResponseWriter, r *http.Request) {
	modelID, err := strconv.ParseInt(chi.URLParam(r, "modelId"), 10, 64)
	if err != nil {
		platform.WriteError(w, h.logger, platform.NewValidation("Некорректный modelId", nil))
		return
	}
	var req ModelAccessPolicy
	if err := platform.DecodeJSON(r, &req); err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	req.ModelID = modelID
	pol, err := h.svc.SetModelAccess(r.Context(), req)
	if err != nil {
		platform.WriteError(w, h.logger, err)
		return
	}
	platform.WriteJSON(w, http.StatusOK, pol)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
