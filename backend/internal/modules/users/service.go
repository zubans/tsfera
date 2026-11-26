package users

import (
	"context"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

const SessionCookieName = "tsfera_session"

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9._@-]{2,100}$`)

// Service реализует бизнес-логику пользователей, аутентификации, RBAC и доступа к ИИ-моделям.
type Service struct {
	repo           Repository
	bus            platform.EventBus
	sessionTTL     time.Duration
	fileStorageDir string
	nowFn          func() time.Time
}

func (s *Service) SetEventBus(bus platform.EventBus) {
	s.bus = bus
}

func NewService(repo Repository, sessionTTL time.Duration) *Service {
	if sessionTTL <= 0 {
		sessionTTL = 30 * 24 * time.Hour
	}
	return &Service{
		repo:       repo,
		sessionTTL: sessionTTL,
		nowFn:      time.Now,
	}
}

type LoginInput struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	IP        string `json:"-"`
	UserAgent string `json:"-"`
}

type LoginOutput struct {
	User         User     `json:"user"`
	SessionToken string   `json:"token,omitempty"`
	Permissions  []string `json:"permissions"`
}

func (s *Service) Login(ctx context.Context, in LoginInput) (*LoginOutput, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" || in.Password == "" {
		return nil, platform.NewValidation("Логин и пароль обязательны", map[string]string{
			"username": "Укажите логин",
			"password": "Укажите пароль",
		})
	}

	u, err := s.repo.GetUserByUsername(ctx, username)
	if err != nil {
		_ = s.repo.LogLoginEvent(ctx, &LoginEvent{
			Event: "failed", Username: username, IP: in.IP, UserAgent: in.UserAgent, Reason: "пользователь не найден",
		})
		return nil, platform.NewUnauthorized("Неверный логин или пароль")
	}

	if !u.Active {
		_ = s.repo.LogLoginEvent(ctx, &LoginEvent{
			Event: "failed", Username: u.Username, UserID: &u.ID, IP: in.IP, UserAgent: in.UserAgent, Reason: "пользователь отключен",
		})
		return nil, platform.NewForbidden("Учетная запись отключена")
	}

	ok, upgrade := VerifyPassword(u.PasswordHash, in.Password)
	if !ok {
		_ = s.repo.LogLoginEvent(ctx, &LoginEvent{
			Event: "failed", Username: u.Username, UserID: &u.ID, IP: in.IP, UserAgent: in.UserAgent, Reason: "неверный пароль",
		})
		return nil, platform.NewUnauthorized("Неверный логин или пароль")
	}

	if upgrade {
		if newHash, err := HashPassword(in.Password); err == nil {
			_ = s.repo.UpdatePasswordHash(ctx, u.ID, newHash)
			u.PasswordHash = newHash
		}
	}

	raw, hash, err := GenerateRandomToken()
	if err != nil {
		return nil, err
	}
	now := s.nowFn()
	if err := s.repo.CreateSession(ctx, hash, u.ID, now.Add(s.sessionTTL)); err != nil {
		return nil, err
	}
	_ = s.repo.UpdateLastLogin(ctx, u.ID, now)
	_ = s.repo.LogLoginEvent(ctx, &LoginEvent{
		Event: "login", Username: u.Username, UserID: &u.ID, IP: in.IP, UserAgent: in.UserAgent,
	})

	perms, err := s.repo.GetUserPermissions(ctx, u.ID)
	if err != nil {
		return nil, err
	}

	return &LoginOutput{
		User:         *u,
		SessionToken: raw,
		Permissions:  perms,
	}, nil
}

func (s *Service) Logout(ctx context.Context, rawToken, ip, userAgent string, auth *platform.AuthContext) error {
	if rawToken != "" {
		_ = s.repo.DeleteSession(ctx, HashToken(rawToken))
	}
	if auth != nil {
		_ = s.repo.LogLoginEvent(ctx, &LoginEvent{
			Event: "logout", Username: auth.Username, UserID: &auth.UserID, IP: ip, UserAgent: userAgent,
		})
	}
	return nil
}

// ResolveRequest реализует platform.Authorizer: проверяет cookie сессии, Authorization: Bearer и legacy X-API-key.
func (s *Service) ResolveRequest(r *http.Request) (*platform.AuthContext, error) {
	ctx := r.Context()
	now := s.nowFn()

	if apiKey := strings.TrimSpace(r.Header.Get("X-API-key")); apiKey != "" {
		return s.resolveAPIToken(ctx, apiKey, now)
	}
	if authHdr := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(authHdr), "bearer ") {
		token := strings.TrimSpace(authHdr[7:])
		if uid, err := s.repo.GetSessionUserID(ctx, HashToken(token), now); err == nil {
			return s.buildUserAuthContext(ctx, uid)
		}
		return s.resolveAPIToken(ctx, token, now)
	}
	if c, err := r.Cookie(SessionCookieName); err == nil && strings.TrimSpace(c.Value) != "" {
		uid, err := s.repo.GetSessionUserID(ctx, HashToken(strings.TrimSpace(c.Value)), now)
		if err != nil {
			return nil, err
		}
		return s.buildUserAuthContext(ctx, uid)
	}
	return nil, platform.NewUnauthorized("Отсутствует токен или сессия")
}

func (s *Service) resolveAPIToken(ctx context.Context, raw string, now time.Time) (*platform.AuthContext, error) {
	tok, err := s.repo.GetAPITokenByHash(ctx, HashToken(raw), now)
	if err != nil {
		return nil, err
	}
	ac, err := s.buildUserAuthContext(ctx, tok.UserID)
	if err != nil {
		return nil, err
	}
	ac.IsToken = true
	ac.TokenID = tok.ID
	ac.TokenName = tok.Name
	ac.TokenCanRead = tok.CanRead
	ac.TokenCanWrite = tok.CanWrite
	ac.TokenAskOnly = tok.AskOnly
	ac.TokenAllowedSpaces = tok.AllowedSpaces
	_ = s.repo.TouchAPIToken(ctx, tok.ID, now)
	return ac, nil
}

func (s *Service) buildUserAuthContext(ctx context.Context, userID int64) (*platform.AuthContext, error) {
	u, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !u.Active {
		return nil, platform.NewUnauthorized("Пользователь отключен")
	}
	perms, err := s.repo.GetUserPermissions(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	permMap := make(map[string]bool, len(perms))
	for _, p := range perms {
		permMap[p] = true
	}
	display := u.FullName
	if display == "" {
		display = u.Username
	}
	return &platform.AuthContext{
		UserID:        u.ID,
		Username:      u.Username,
		DisplayName:   display,
		SubdivisionID: u.SubdivisionID,
		IsSuperAdmin:  slices.Contains(u.Roles, SuperRoleName),
		Permissions:   permMap,
	}, nil
}

type CreateUserInput struct {
	Username             string   `json:"username"`
	Password             string   `json:"password"`
	Email                *string  `json:"email"`
	FirstName            string   `json:"first_name"`
	SecondName           string   `json:"second_name"`
	LastName             string   `json:"last_name"`
	SubdivisionID        *int64   `json:"subdivision_id"`
	IsEngineer           bool     `json:"is_engineer"`
	IsDispatcher         bool     `json:"is_dispatcher"`
	Color                string   `json:"color"`
	CorporatePhoneNumber string   `json:"corporate_phone_number"`
	OfficeEquipment      string   `json:"office_equipment"`
	Training             string   `json:"training"`
	CarRubber            string   `json:"car_rubber"`
	CarModel             string   `json:"car_model"`
	CarFuelCardNumber    string   `json:"car_fuel_card_number"`
	CarGovernmentNumber  string   `json:"car_government_number"`
	CarStsNumber         string   `json:"car_sts_number"`
	CarContractNumber    string   `json:"car_contract_number"`
	WorkClothes          []string `json:"work_clothes"`
	Instruments          []string `json:"instruments"`
	Equipments           []string `json:"equipments"`
	AvatarPath           string   `json:"avatar_path"`
	Roles                []string `json:"roles"`
}

func (s *Service) CreateUser(ctx context.Context, in CreateUserInput) (*User, error) {
	fields := map[string]string{}
	username := strings.TrimSpace(in.Username)
	if !usernameRe.MatchString(username) {
		fields["username"] = "Логин может содержать латиницу, цифры и . _ - @"
	}
	if len(in.Password) < 6 {
		fields["password"] = "Пароль должен быть не короче 6 символов"
	}
	if fn := strings.TrimSpace(in.FirstName); fn != "" && utf8.RuneCountInString(fn) < 2 {
		fields["first_name"] = "Имя должно содержать не менее 2 символов"
	}
	if ln := strings.TrimSpace(in.LastName); ln != "" && utf8.RuneCountInString(ln) < 2 {
		fields["last_name"] = "Фамилия должна содержать не менее 2 символов"
	}
	if in.CorporatePhoneNumber != "" {
		parsed, err := platform.ParsePhone10(in.CorporatePhoneNumber)
		if err != nil {
			fields["corporate_phone_number"] = "Некорректный телефон"
		} else {
			in.CorporatePhoneNumber = parsed
		}
	}
	if len(fields) > 0 {
		return nil, platform.NewValidation("Ошибка валидации пользователя", fields)
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	color := in.Color
	if color == "" {
		color = "#3b82f6"
	}
	u := &User{
		Username:             username,
		PasswordHash:         hash,
		Email:                in.Email,
		FirstName:            strings.TrimSpace(in.FirstName),
		SecondName:           strings.TrimSpace(in.SecondName),
		LastName:             strings.TrimSpace(in.LastName),
		FullName:             BuildFullName(in.LastName, in.FirstName, in.SecondName),
		SubdivisionID:        in.SubdivisionID,
		IsEngineer:           in.IsEngineer,
		IsDispatcher:         in.IsDispatcher,
		Color:                color,
		CorporatePhoneNumber: in.CorporatePhoneNumber,
		OfficeEquipment:      in.OfficeEquipment,
		Training:             in.Training,
		CarRubber:            in.CarRubber,
		CarModel:             in.CarModel,
		CarFuelCardNumber:    in.CarFuelCardNumber,
		CarGovernmentNumber:  in.CarGovernmentNumber,
		CarStsNumber:         in.CarStsNumber,
		CarContractNumber:    in.CarContractNumber,
		WorkClothes:          in.WorkClothes,
		Instruments:          in.Instruments,
		Equipments:           in.Equipments,
		AvatarPath:           strings.TrimSpace(in.AvatarPath),
		Roles:                in.Roles,
		Active:               true,
	}
	id, err := s.repo.CreateUser(ctx, u)
	if err != nil {
		return nil, err
	}
	return s.repo.GetUserByID(ctx, id)
}

type UpdateUserInput struct {
	Email                *string  `json:"email"`
	FirstName            string   `json:"first_name"`
	SecondName           string   `json:"second_name"`
	LastName             string   `json:"last_name"`
	SubdivisionID        *int64   `json:"subdivision_id"`
	IsEngineer           bool     `json:"is_engineer"`
	IsDispatcher         bool     `json:"is_dispatcher"`
	Color                string   `json:"color"`
	CorporatePhoneNumber string   `json:"corporate_phone_number"`
	OfficeEquipment      string   `json:"office_equipment"`
	Training             string   `json:"training"`
	CarRubber            string   `json:"car_rubber"`
	CarModel             string   `json:"car_model"`
	CarFuelCardNumber    string   `json:"car_fuel_card_number"`
	CarGovernmentNumber  string   `json:"car_government_number"`
	CarStsNumber         string   `json:"car_sts_number"`
	CarContractNumber    string   `json:"car_contract_number"`
	WorkClothes          []string `json:"work_clothes"`
	Instruments          []string `json:"instruments"`
	Equipments           []string `json:"equipments"`
	AvatarPath           *string  `json:"avatar_path"`
	Roles                []string `json:"roles"`
	Active               *bool    `json:"active"`
}

func (s *Service) UpdateUser(ctx context.Context, userID int64, in UpdateUserInput) (*User, error) {
	u, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	wasSuper := slices.Contains(u.Roles, SuperRoleName)
	nextRoles := u.Roles
	if in.Roles != nil {
		nextRoles = in.Roles
	}
	nextActive := u.Active
	if in.Active != nil {
		nextActive = *in.Active
	}
	willBeSuper := slices.Contains(nextRoles, SuperRoleName) && nextActive
	// Защита последнего активного суперадминистратора от отключения или снятия роли.
	if wasSuper && u.Active && !willBeSuper {
		otherSupers, err := s.repo.CountActiveSuperAdmins(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		if otherSupers == 0 {
			return nil, platform.NewConflict("Нельзя отключить или разжаловать последнего суперадминистратора")
		}
	}

	if in.CorporatePhoneNumber != "" {
		parsed, err := platform.ParsePhone10(in.CorporatePhoneNumber)
		if err != nil {
			return nil, err
		}
		u.CorporatePhoneNumber = parsed
	} else {
		u.CorporatePhoneNumber = ""
	}

	u.Email = in.Email
	u.FirstName = strings.TrimSpace(in.FirstName)
	u.SecondName = strings.TrimSpace(in.SecondName)
	u.LastName = strings.TrimSpace(in.LastName)
	if u.FirstName != "" && utf8.RuneCountInString(u.FirstName) < 2 {
		return nil, platform.NewValidation("Ошибка валидации пользователя", map[string]string{"first_name": "Имя должно содержать не менее 2 символов"})
	}
	if u.LastName != "" && utf8.RuneCountInString(u.LastName) < 2 {
		return nil, platform.NewValidation("Ошибка валидации пользователя", map[string]string{"last_name": "Фамилия должна содержать не менее 2 символов"})
	}
	u.FullName = BuildFullName(u.LastName, u.FirstName, u.SecondName)
	u.SubdivisionID = in.SubdivisionID
	u.IsEngineer = in.IsEngineer
	u.IsDispatcher = in.IsDispatcher
	if in.Color != "" {
		u.Color = in.Color
	}
	u.OfficeEquipment = in.OfficeEquipment
	u.Training = in.Training
	u.CarRubber = in.CarRubber
	u.CarModel = in.CarModel
	u.CarFuelCardNumber = in.CarFuelCardNumber
	u.CarGovernmentNumber = in.CarGovernmentNumber
	u.CarStsNumber = in.CarStsNumber
	u.CarContractNumber = in.CarContractNumber
	if in.WorkClothes != nil {
		u.WorkClothes = in.WorkClothes
	}
	if in.Instruments != nil {
		u.Instruments = in.Instruments
	}
	if in.Equipments != nil {
		u.Equipments = in.Equipments
	}
	if in.AvatarPath != nil {
		u.AvatarPath = strings.TrimSpace(*in.AvatarPath)
	}
	u.Roles = nextRoles
	u.Active = nextActive

	if err := s.repo.UpdateUser(ctx, u); err != nil {
		return nil, err
	}
	if !u.Active {
		_ = s.repo.DeleteUserSessions(ctx, u.ID)
		if s.bus != nil {
			s.bus.Publish(ctx, platform.Event{
				Topic:   "users.user.deactivated",
				Payload: map[string]any{"user_id": u.ID},
			})
		}
	}
	return s.repo.GetUserByID(ctx, u.ID)
}

func (s *Service) ChangePassword(ctx context.Context, userID int64, newPassword string) error {
	if len(newPassword) < 6 {
		return platform.NewValidation("Пароль слишком короткий", map[string]string{
			"password": "Минимум 6 символов",
		})
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return s.repo.UpdatePasswordHash(ctx, userID, hash)
}

func (s *Service) GetUser(ctx context.Context, id int64) (*User, error) {
	return s.repo.GetUserByID(ctx, id)
}

func (s *Service) ListUsers(ctx context.Context, activeOnly bool) ([]User, error) {
	return s.repo.ListUsers(ctx, activeOnly)
}

func hasAllSubdivisionsAccess(auth *platform.AuthContext) bool {
	return auth == nil || auth.IsSuperAdmin || auth.HasPermission("admin.user.without_subdivision")
}

// ListUsersForCaller применяет правило RBAC admin.user.without_subdivision (видимость сотрудников своего филиала).
func (s *Service) ListUsersForCaller(ctx context.Context, auth *platform.AuthContext, activeOnly bool) ([]User, error) {
	all, err := s.repo.ListUsers(ctx, activeOnly)
	if err != nil {
		return nil, err
	}
	if hasAllSubdivisionsAccess(auth) {
		return all, nil
	}
	out := make([]User, 0, len(all))
	for _, u := range all {
		if auth.SubdivisionID == nil {
			if u.SubdivisionID == nil {
				out = append(out, u)
			}
			continue
		}
		if u.SubdivisionID != nil && *u.SubdivisionID == *auth.SubdivisionID {
			out = append(out, u)
		}
	}
	return out, nil
}

// CheckSubdivisionAccess запрещает просмотр/изменение пользователя чужого филиала без права admin.user.without_subdivision.
func (s *Service) CheckSubdivisionAccess(auth *platform.AuthContext, targetSubdivisionID *int64) error {
	if hasAllSubdivisionsAccess(auth) {
		return nil
	}
	if auth.SubdivisionID == nil && targetSubdivisionID == nil {
		return nil
	}
	if auth.SubdivisionID != nil && targetSubdivisionID != nil && *auth.SubdivisionID == *targetSubdivisionID {
		return nil
	}
	return platform.NewForbidden("Нет прав на управление сотрудниками другого подразделения")
}

func (s *Service) RefreshAPITokenByRaw(ctx context.Context, raw string) (*APIToken, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", platform.NewUnauthorized("Отсутствует API-токен")
	}
	newExp := s.nowFn().Add(30 * 24 * time.Hour)
	tok, err := s.repo.RefreshAPITokenByHash(ctx, HashToken(raw), newExp)
	if err != nil {
		return nil, "", err
	}
	return tok, raw, nil
}

func (s *Service) RevokeAPITokenByRaw(ctx context.Context, raw, ip, userAgent string, auth *platform.AuthContext) error {
	if strings.TrimSpace(raw) != "" {
		_ = s.repo.DeleteAPITokenByHash(ctx, HashToken(strings.TrimSpace(raw)))
	}
	if auth != nil {
		_ = s.repo.LogLoginEvent(ctx, &LoginEvent{
			Event: "logout", Username: auth.Username, UserID: &auth.UserID, IP: ip, UserAgent: userAgent,
		})
	}
	return nil
}

func (s *Service) ListInventoryItems(ctx context.Context, category string) ([]InventoryItem, error) {
	return s.repo.ListInventoryItems(ctx, category)
}

func (s *Service) CreateInventoryItem(ctx context.Context, category, name string) (*InventoryItem, error) {
	category = strings.TrimSpace(category)
	name = strings.TrimSpace(name)
	switch category {
	case InventoryWorkClothes, InventoryInstrument, InventoryEquipment:
	default:
		return nil, platform.NewValidation("Некорректная категория инвентаря", map[string]string{"category": "work_clothes | instrument | engineer_equipment"})
	}
	if name == "" {
		return nil, platform.NewValidation("Укажите наименование позиции инвентаря", map[string]string{"name": "Обязательное поле"})
	}
	item := &InventoryItem{Category: category, Name: name}
	if err := s.repo.CreateInventoryItem(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) DeleteInventoryItem(ctx context.Context, id int64) error {
	return s.repo.DeleteInventoryItem(ctx, id)
}

func (s *Service) SaveRole(ctx context.Context, name, description string, perms []string) (*Role, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, platform.NewValidation("Укажите имя роли", map[string]string{"name": "обязательно"})
	}
	if name == SuperRoleName {
		return nil, platform.NewForbidden("Нельзя изменять или удалять операции привилегированной роли superadmin")
	}
	r := &Role{Name: name, Description: strings.TrimSpace(description), Permissions: perms}
	if err := s.repo.SaveRole(ctx, r); err != nil {
		return nil, err
	}
	return s.repo.GetRole(ctx, name)
}

func (s *Service) DeleteRole(ctx context.Context, name string) error {
	if name == SuperRoleName {
		return platform.NewForbidden("Нельзя удалить привилегированную роль superadmin")
	}
	return s.repo.DeleteRole(ctx, name)
}

func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	return s.repo.ListRoles(ctx)
}

func (s *Service) ListPermissions(ctx context.Context) ([]platform.PermissionDef, error) {
	return s.repo.ListPermissions(ctx)
}

type CreateTokenInput struct {
	Name          string   `json:"name"`
	CanRead       bool     `json:"can_read"`
	CanWrite      bool     `json:"can_write"`
	AskOnly       bool     `json:"ask_only"`
	AllowedSpaces []string `json:"allowed_spaces"`
	TTLDays       int      `json:"ttl_days"`
}

func normalizeSpaces(spaces []string) []string {
	if len(spaces) == 0 {
		return []string{}
	}
	seen := make(map[string]bool, len(spaces))
	out := make([]string, 0, len(spaces))
	for _, s := range spaces {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, trimmed)
	}
	return out
}

func (s *Service) CreateAPIToken(ctx context.Context, userID int64, in CreateTokenInput) (*APIToken, string, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, "", platform.NewValidation("Укажите название токена", map[string]string{"name": "обязательно"})
	}
	raw, hash, err := GenerateRandomToken()
	if err != nil {
		return nil, "", err
	}
	var exp *time.Time
	if in.TTLDays > 0 {
		t := s.nowFn().Add(time.Duration(in.TTLDays) * 24 * time.Hour)
		exp = &t
	}
	canRead := in.CanRead || in.CanWrite
	canWrite := in.CanWrite
	if in.AskOnly {
		canRead = false
		canWrite = false
	}
	tok := &APIToken{
		UserID:        userID,
		Name:          strings.TrimSpace(in.Name),
		TokenHash:     hash,
		CanRead:       canRead,
		CanWrite:      canWrite,
		AskOnly:       in.AskOnly,
		AllowedSpaces: normalizeSpaces(in.AllowedSpaces),
		ExpiresAt:     exp,
	}
	id, err := s.repo.CreateAPIToken(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	tok.ID = id
	return tok, raw, nil
}

func (s *Service) ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	return s.repo.ListAPITokens(ctx, userID)
}

func (s *Service) DeleteAPIToken(ctx context.Context, tokenID, userID int64) error {
	return s.repo.DeleteAPIToken(ctx, tokenID, userID)
}

func (s *Service) ListLoginEvents(ctx context.Context, userID *int64, event string, limit int) ([]LoginEvent, error) {
	return s.repo.ListLoginEvents(ctx, userID, event, limit)
}

// CanAccessModel реализует правила KB по доступу к LLM-модели:
// isSuperAdmin всегда может; если open_to_all == true — всем; false — только user_ids; nil — всем, если модель бесплатна (isFree).
func (s *Service) CanAccessModel(ctx context.Context, auth *platform.AuthContext, modelID int64, isFree bool) (bool, error) {
	if auth == nil {
		return false, nil
	}
	if auth.IsSuperAdmin {
		return true, nil
	}
	pol, err := s.repo.GetModelAccess(ctx, modelID)
	if err != nil {
		return false, err
	}
	if pol.OpenToAll == nil {
		return isFree, nil
	}
	if *pol.OpenToAll {
		return true, nil
	}
	return slices.Contains(pol.UserIDs, auth.UserID), nil
}

func (s *Service) SetModelAccess(ctx context.Context, pol ModelAccessPolicy) (*ModelAccessPolicy, error) {
	if err := s.repo.SetModelAccess(ctx, pol); err != nil {
		return nil, err
	}
	return s.repo.GetModelAccess(ctx, pol.ModelID)
}

func (s *Service) ListModelAccess(ctx context.Context) ([]ModelAccessPolicy, error) {
	return s.repo.ListModelAccess(ctx)
}

func (s *Service) EnsureInitialAdmin(ctx context.Context, username, password string) error {
	if _, err := s.repo.GetUserByUsername(ctx, username); err == nil {
		return nil
	}
	_, err := s.CreateUser(ctx, CreateUserInput{
		Username:  username,
		Password:  password,
		FirstName: "Администратор",
		LastName:  "Системы",
		Roles:     []string{SuperRoleName},
	})
	return err
}
