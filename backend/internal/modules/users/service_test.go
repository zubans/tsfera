package users

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type stubRepo struct {
	users       map[int64]*User
	roles       map[string]*Role
	sessions    map[string]int64
	tokens      map[string]*APIToken
	loginEvents []LoginEvent
	modelAccess map[int64]*ModelAccessPolicy
	inventory   map[int64]InventoryItem
	files       map[int64]*StoredFile
	nextID      int64
}

func newStubRepo() *stubRepo {
	return &stubRepo{
		users:       make(map[int64]*User),
		roles:       map[string]*Role{SuperRoleName: {Name: SuperRoleName, Description: "Super", IsSystem: true}},
		sessions:    make(map[string]int64),
		tokens:      make(map[string]*APIToken),
		modelAccess: make(map[int64]*ModelAccessPolicy),
		inventory:   make(map[int64]InventoryItem),
		files:       make(map[int64]*StoredFile),
		nextID:      1,
	}
}

func (s *stubRepo) SyncPermissions(context.Context, []platform.PermissionDef) error { return nil }
func (s *stubRepo) CreateUser(_ context.Context, u *User) (int64, error) {
	s.nextID++
	cp := *u
	cp.ID = s.nextID
	s.users[cp.ID] = &cp
	return cp.ID, nil
}
func (s *stubRepo) UpdateUser(_ context.Context, u *User) error {
	cp := *u
	s.users[u.ID] = &cp
	return nil
}
func (s *stubRepo) UpdatePasswordHash(_ context.Context, uid int64, hash string) error {
	if u, ok := s.users[uid]; ok {
		u.PasswordHash = hash
	}
	return nil
}
func (s *stubRepo) GetUserByID(_ context.Context, id int64) (*User, error) {
	u, ok := s.users[id]
	if !ok {
		return nil, platform.NewNotFound("not found")
	}
	cp := *u
	return &cp, nil
}
func (s *stubRepo) GetUserByUsername(_ context.Context, username string) (*User, error) {
	for _, u := range s.users {
		if u.Username == username {
			cp := *u
			return &cp, nil
		}
	}
	return nil, platform.NewNotFound("not found")
}
func (s *stubRepo) ListUsers(_ context.Context, activeOnly bool) ([]User, error) {
	var out []User
	for _, u := range s.users {
		if !activeOnly || u.Active {
			out = append(out, *u)
		}
	}
	return out, nil
}
func (s *stubRepo) CountActiveSuperAdmins(_ context.Context, excludeUserID int64) (int, error) {
	cnt := 0
	for _, u := range s.users {
		if u.ID != excludeUserID && u.Active {
			for _, r := range u.Roles {
				if r == SuperRoleName {
					cnt++
				}
			}
		}
	}
	return cnt, nil
}
func (s *stubRepo) GetUserPermissions(_ context.Context, uid int64) ([]string, error) {
	u, ok := s.users[uid]
	if !ok {
		return nil, nil
	}
	var out []string
	for _, rn := range u.Roles {
		if role, ok := s.roles[rn]; ok {
			out = append(out, role.Permissions...)
		}
	}
	return out, nil
}
func (s *stubRepo) ListRoles(context.Context) ([]Role, error)             { return nil, nil }
func (s *stubRepo) GetRole(_ context.Context, name string) (*Role, error) { return s.roles[name], nil }
func (s *stubRepo) SaveRole(_ context.Context, role *Role) error {
	s.roles[role.Name] = role
	return nil
}
func (s *stubRepo) DeleteRole(_ context.Context, name string) error {
	delete(s.roles, name)
	return nil
}
func (s *stubRepo) ListPermissions(context.Context) ([]platform.PermissionDef, error) {
	return nil, nil
}
func (s *stubRepo) CreateSession(_ context.Context, h string, uid int64, _ time.Time) error {
	s.sessions[h] = uid
	return nil
}
func (s *stubRepo) GetSessionUserID(_ context.Context, h string, _ time.Time) (int64, error) {
	uid, ok := s.sessions[h]
	if !ok {
		return 0, platform.NewUnauthorized("no session")
	}
	return uid, nil
}
func (s *stubRepo) DeleteSession(_ context.Context, h string) error {
	delete(s.sessions, h)
	return nil
}
func (s *stubRepo) DeleteUserSessions(_ context.Context, uid int64) error {
	for k, v := range s.sessions {
		if v == uid {
			delete(s.sessions, k)
		}
	}
	return nil
}
func (s *stubRepo) UpdateLastLogin(context.Context, int64, time.Time) error { return nil }
func (s *stubRepo) CreateAPIToken(_ context.Context, tok *APIToken) (int64, error) {
	s.nextID++
	cp := *tok
	cp.ID = s.nextID
	s.tokens[cp.TokenHash] = &cp
	return cp.ID, nil
}
func (s *stubRepo) ListAPITokens(context.Context, int64) ([]APIToken, error) { return nil, nil }
func (s *stubRepo) GetAPITokenByHash(_ context.Context, h string, _ time.Time) (*APIToken, error) {
	t, ok := s.tokens[h]
	if !ok {
		return nil, platform.NewUnauthorized("invalid token")
	}
	return t, nil
}
func (s *stubRepo) TouchAPIToken(context.Context, int64, time.Time) error { return nil }
func (s *stubRepo) RefreshAPITokenByHash(_ context.Context, h string, newExp time.Time) (*APIToken, error) {
	t, ok := s.tokens[h]
	if !ok {
		return nil, platform.NewUnauthorized("invalid token")
	}
	t.ExpiresAt = &newExp
	cp := *t
	return &cp, nil
}
func (s *stubRepo) DeleteAPITokenByHash(_ context.Context, h string) error {
	delete(s.tokens, h)
	return nil
}
func (s *stubRepo) DeleteAPIToken(context.Context, int64, int64) error { return nil }
func (s *stubRepo) ListInventoryItems(_ context.Context, category string) ([]InventoryItem, error) {
	var out []InventoryItem
	for _, it := range s.inventory {
		if !it.Deleted && (category == "" || it.Category == category) {
			out = append(out, it)
		}
	}
	return out, nil
}
func (s *stubRepo) CreateInventoryItem(_ context.Context, item *InventoryItem) error {
	s.nextID++
	item.ID = s.nextID
	item.CreatedAt = time.Now()
	s.inventory[item.ID] = *item
	return nil
}
func (s *stubRepo) DeleteInventoryItem(_ context.Context, id int64) error {
	it, ok := s.inventory[id]
	if !ok || it.Deleted {
		return platform.NewNotFound("not found")
	}
	it.Deleted = true
	s.inventory[id] = it
	return nil
}
func (s *stubRepo) LogLoginEvent(_ context.Context, ev *LoginEvent) error {
	s.loginEvents = append(s.loginEvents, *ev)
	return nil
}
func (s *stubRepo) ListLoginEvents(context.Context, *int64, string, int) ([]LoginEvent, error) {
	return s.loginEvents, nil
}
func (s *stubRepo) GetModelAccess(_ context.Context, modelID int64) (*ModelAccessPolicy, error) {
	if p, ok := s.modelAccess[modelID]; ok {
		return p, nil
	}
	return &ModelAccessPolicy{ModelID: modelID}, nil
}
func (s *stubRepo) SetModelAccess(_ context.Context, pol ModelAccessPolicy) error {
	cp := pol
	s.modelAccess[pol.ModelID] = &cp
	return nil
}
func (s *stubRepo) ListModelAccess(context.Context) ([]ModelAccessPolicy, error) { return nil, nil }
func (s *stubRepo) ListUserIDsByPermission(_ context.Context, perm string) ([]int64, error) {
	var out []int64
	for _, u := range s.users {
		if !u.Active {
			continue
		}
		allowed := slices.Contains(u.Roles, SuperRoleName)
		if !allowed {
			for _, rn := range u.Roles {
				if r, ok := s.roles[rn]; ok && slices.Contains(r.Permissions, perm) {
					allowed = true
					break
				}
			}
		}
		if allowed {
			out = append(out, u.ID)
		}
	}
	slices.Sort(out)
	return out, nil
}
func (s *stubRepo) CreateFile(_ context.Context, f *StoredFile) error {
	s.nextID++
	f.ID = s.nextID
	f.CreatedAt = time.Now()
	cp := *f
	s.files[f.ID] = &cp
	return nil
}
func (s *stubRepo) GetFile(_ context.Context, id int64) (*StoredFile, error) {
	f, ok := s.files[id]
	if !ok {
		return nil, platform.NewNotFound("not found")
	}
	cp := *f
	return &cp, nil
}

func md5Double(s string) string {
	h1 := md5.Sum([]byte(s))
	s1 := hex.EncodeToString(h1[:])
	h2 := md5.Sum([]byte(s1))
	return hex.EncodeToString(h2[:])
}

func TestLoginUpgradesLegacyMD5AndAuditsFailure(t *testing.T) {
	repo := newStubRepo()
	repo.users[10] = &User{
		ID:           10,
		Username:     "legacy_eng",
		PasswordHash: md5Double("secret123"),
		Roles:        []string{"engineer"},
		Active:       true,
	}
	svc := NewService(repo, time.Hour)

	if _, err := svc.Login(context.Background(), LoginInput{Username: "legacy_eng", Password: "wrong"}); !errors.Is(err, platform.ErrUnauthorized) {
		t.Fatalf("wrong password err = %v, want ErrUnauthorized", err)
	}

	out, err := svc.Login(context.Background(), LoginInput{Username: "legacy_eng", Password: "secret123"})
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if out.SessionToken == "" {
		t.Fatal("expected non-empty session token")
	}
	if repo.users[10].PasswordHash[:2] != "$2" {
		t.Fatalf("expected legacy double-MD5 hash to upgrade to bcrypt, got %q", repo.users[10].PasswordHash)
	}
	if len(repo.loginEvents) != 2 || repo.loginEvents[0].Event != "failed" || repo.loginEvents[1].Event != "login" {
		t.Fatalf("unexpected login events: %+v", repo.loginEvents)
	}
}

func TestSuperAdminAndRoleProtections(t *testing.T) {
	repo := newStubRepo()
	repo.users[1] = &User{ID: 1, Username: "boss", Roles: []string{SuperRoleName}, Active: true}
	svc := NewService(repo, time.Hour)

	inactive := false
	_, err := svc.UpdateUser(context.Background(), 1, UpdateUserInput{
		FirstName: "Иван", LastName: "Иванов", Roles: []string{SuperRoleName}, Active: &inactive,
	})
	if !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("deactivating last superadmin err = %v, want ErrConflict", err)
	}
	if _, err := svc.SaveRole(context.Background(), SuperRoleName, "hack", nil); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("modifying superadmin role err = %v, want ErrForbidden", err)
	}
	if err := svc.DeleteRole(context.Background(), SuperRoleName); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("deleting superadmin role err = %v, want ErrForbidden", err)
	}
}

func TestModelAccessPolicy(t *testing.T) {
	repo := newStubRepo()
	svc := NewService(repo, time.Hour)
	eng := &platform.AuthContext{UserID: 5, Permissions: map[string]bool{}}
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}

	if ok, _ := svc.CanAccessModel(context.Background(), eng, 100, true); !ok {
		t.Fatal("free model with nil open_to_all should be open to all")
	}
	if ok, _ := svc.CanAccessModel(context.Background(), eng, 100, false); ok {
		t.Fatal("paid model with nil open_to_all must be restricted")
	}
	restricted := false
	_, _ = svc.SetModelAccess(context.Background(), ModelAccessPolicy{ModelID: 100, OpenToAll: &restricted, UserIDs: []int64{5}})
	if ok, _ := svc.CanAccessModel(context.Background(), eng, 100, false); !ok {
		t.Fatal("user in allowlist must access model")
	}
	if ok, _ := svc.CanAccessModel(context.Background(), admin, 999, false); !ok {
		t.Fatal("superadmin must access any model")
	}
}

func TestUserHandlerAuthAndReadOnlyTokenEnforcement(t *testing.T) {
	repo := newStubRepo()
	repo.roles["dispatcher"] = &Role{Name: "dispatcher", Permissions: []string{"admin.user.view"}}
	hash, _ := HashPassword("pass1234")
	repo.users[7] = &User{ID: 7, Username: "disp", PasswordHash: hash, Roles: []string{"dispatcher"}, Active: true}

	svc := NewService(repo, time.Hour)
	_, rawToken, err := svc.CreateAPIToken(context.Background(), 7, CreateTokenInput{Name: "agent-ro", CanRead: true, CanWrite: false})
	if err != nil {
		t.Fatal(err)
	}
	repo.users[7].Roles = []string{SuperRoleName}

	r := chi.NewRouter()
	NewHandler(svc, nil).Mount(r)

	// Read-only token can GET /users but is blocked with 403 on POST /admin/users
	reqGet := httptest.NewRequest(http.MethodGet, "/users", nil)
	reqGet.Header.Set("Authorization", "Bearer "+rawToken)
	recGet := httptest.NewRecorder()
	r.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET /users status = %d, body = %s", recGet.Code, recGet.Body.String())
	}

	body, _ := json.Marshal(CreateUserInput{Username: "newu", Password: "password1"})
	reqPost := httptest.NewRequest(http.MethodPost, "/admin/users", bytes.NewReader(body))
	reqPost.Header.Set("X-API-key", rawToken)
	recPost := httptest.NewRecorder()
	r.ServeHTTP(recPost, reqPost)
	if recPost.Code != http.StatusForbidden {
		t.Fatalf("POST /admin/users with read-only token status = %d, want 403, body = %s", recPost.Code, recPost.Body.String())
	}
}

func TestMobileAuthLifecycleSubdivisionScopeAndInventory(t *testing.T) {
	repo := newStubRepo()
	sub1, sub2 := int64(1), int64(2)
	repo.roles["disp"] = &Role{Name: "disp", Permissions: []string{"admin.user.view", "admin.user.update", "admin.inventory.work_clothes.view", "admin.inventory.work_clothes.create", "admin.inventory.work_clothes.delete"}}
	hash, _ := HashPassword("secret99")
	repo.users[10] = &User{ID: 10, Username: "mgr1", PasswordHash: hash, SubdivisionID: &sub1, Roles: []string{"disp"}, Active: true}
	repo.users[11] = &User{ID: 11, Username: "eng1", PasswordHash: hash, SubdivisionID: &sub1, Roles: []string{"disp"}, Active: true}
	repo.users[12] = &User{ID: 12, Username: "eng2", PasswordHash: hash, SubdivisionID: &sub2, Roles: []string{"disp"}, Active: true}

	svc := NewService(repo, time.Hour)
	h := NewHandler(svc, nil)
	r := chi.NewRouter()
	h.Mount(r)
	r.Route("/api/auth", h.MountMobileAuth)

	// 1. Mobile /api/auth/login -> /refresh -> /logout
	loginBody, _ := json.Marshal(map[string]string{"username": "mgr1", "password": "secret99"})
	recLogin := httptest.NewRecorder()
	r.ServeHTTP(recLogin, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody)))
	if recLogin.Code != http.StatusOK {
		t.Fatalf("mobile login status = %d, body = %s", recLogin.Code, recLogin.Body.String())
	}
	var loginEnv struct {
		Data struct {
			Token     string `json:"token"`
			ExpiredAt string `json:"expired_at"`
		} `json:"data"`
	}
	_ = json.Unmarshal(recLogin.Body.Bytes(), &loginEnv)
	if loginEnv.Data.Token == "" || loginEnv.Data.ExpiredAt == "" {
		t.Fatalf("expected token and expired_at in mobile login response: %s", recLogin.Body.String())
	}

	reqRef := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	reqRef.Header.Set("X-API-key", loginEnv.Data.Token)
	recRef := httptest.NewRecorder()
	r.ServeHTTP(recRef, reqRef)
	if recRef.Code != http.StatusOK {
		t.Fatalf("mobile refresh status = %d, body = %s", recRef.Code, recRef.Body.String())
	}

	// 2. Subdivision scoping without admin.user.without_subdivision
	reqList := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	reqList.Header.Set("X-API-key", loginEnv.Data.Token)
	recList := httptest.NewRecorder()
	r.ServeHTTP(recList, reqList)
	if recList.Code != http.StatusOK {
		t.Fatalf("list users status = %d", recList.Code)
	}
	var visibleUsers []User
	_ = json.Unmarshal(recList.Body.Bytes(), &visibleUsers)
	if len(visibleUsers) != 2 {
		t.Fatalf("expected only 2 subdivision=1 users visible, got %d", len(visibleUsers))
	}

	reqForeign := httptest.NewRequest(http.MethodGet, "/admin/users/12", nil)
	reqForeign.Header.Set("X-API-key", loginEnv.Data.Token)
	recForeign := httptest.NewRecorder()
	r.ServeHTTP(recForeign, reqForeign)
	if recForeign.Code != http.StatusForbidden {
		t.Fatalf("expected 403 reading user from foreign subdivision, got %d", recForeign.Code)
	}

	// 3. Engineer inventory catalog create/list/delete
	invBody, _ := json.Marshal(map[string]string{"category": InventoryWorkClothes, "name": "Куртка"})
	reqInv := httptest.NewRequest(http.MethodPost, "/admin/inventory", bytes.NewReader(invBody))
	reqInv.Header.Set("X-API-key", loginEnv.Data.Token)
	recInv := httptest.NewRecorder()
	r.ServeHTTP(recInv, reqInv)
	if recInv.Code != http.StatusCreated {
		t.Fatalf("create inventory status = %d, body = %s", recInv.Code, recInv.Body.String())
	}
	items, err := svc.ListInventoryItems(context.Background(), InventoryWorkClothes)
	if err != nil || len(items) != 1 || items[0].Name != "Куртка" {
		t.Fatalf("unexpected inventory list: %+v err=%v", items, err)
	}

	// 4. Mobile logout invalidates token
	reqOut := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	reqOut.Header.Set("X-API-key", loginEnv.Data.Token)
	recOut := httptest.NewRecorder()
	r.ServeHTTP(recOut, reqOut)
	if recOut.Code != http.StatusOK {
		t.Fatalf("mobile logout status = %d", recOut.Code)
	}
	reqAfter := httptest.NewRequest(http.MethodGet, "/me", nil)
	reqAfter.Header.Set("X-API-key", loginEnv.Data.Token)
	recAfter := httptest.NewRecorder()
	r.ServeHTTP(recAfter, reqAfter)
	if recAfter.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d", recAfter.Code)
	}
}

func TestFileUploadPermissionLookupAndNameValidation(t *testing.T) {
	repo := newStubRepo()
	repo.roles["disp"] = &Role{Name: "disp", Permissions: []string{"crm.application.update"}}
	repo.users[1] = &User{ID: 1, Username: "admin", Roles: []string{SuperRoleName}, Active: true}
	repo.users[2] = &User{ID: 2, Username: "disp1", Roles: []string{"disp"}, Active: true}
	repo.users[3] = &User{ID: 3, Username: "inactive", Roles: []string{"disp"}, Active: false}

	svc := NewService(repo, time.Hour)
	svc.SetFileStorageDir(t.TempDir())

	ids, err := svc.ListUserIDsByPermission(context.Background(), "crm.application.update")
	if err != nil || !slices.Equal(ids, []int64{1, 2}) {
		t.Fatalf("ListUserIDsByPermission = %v, err=%v; want [1 2]", ids, err)
	}

	if _, err := svc.CreateUser(context.Background(), CreateUserInput{
		Username:  "shortname",
		Password:  "secret99",
		FirstName: "А",
	}); !errors.Is(err, platform.ErrValidation) {
		t.Fatalf("expected ErrValidation for 1-char first_name, got %v", err)
	}

	_, rawTok, err := svc.CreateAPIToken(context.Background(), 2, CreateTokenInput{
		Name:     "up",
		CanRead:  true,
		CanWrite: true,
		TTLDays:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	NewHandler(svc, nil).Mount(r)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "qr.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(fw, "fake-png-content")
	_ = mw.Close()

	reqUp := httptest.NewRequest(http.MethodPost, "/files", &body)
	reqUp.Header.Set("Content-Type", mw.FormDataContentType())
	reqUp.Header.Set("Authorization", "Bearer "+rawTok)
	recUp := httptest.NewRecorder()
	r.ServeHTTP(recUp, reqUp)
	if recUp.Code != http.StatusCreated {
		t.Fatalf("POST /files status = %d, body = %s", recUp.Code, recUp.Body.String())
	}
	var uploaded StoredFile
	if err := json.Unmarshal(recUp.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	if uploaded.ID == 0 || uploaded.OriginalName != "qr.png" {
		t.Fatalf("unexpected uploaded file: %+v", uploaded)
	}

	reqDown := httptest.NewRequest(http.MethodGet, uploaded.URL[len("/api/v1"):], nil)
	reqDown.Header.Set("Authorization", "Bearer "+rawTok)
	recDown := httptest.NewRecorder()
	r.ServeHTTP(recDown, reqDown)
	if recDown.Code != http.StatusOK || recDown.Body.String() != "fake-png-content" {
		t.Fatalf("GET file status = %d, body = %q", recDown.Code, recDown.Body.String())
	}
}
