package users

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

// PgRepository реализует Repository поверх PostgreSQL (`pgx`).
type PgRepository struct {
	db platform.Querier
}

func NewPgRepository(db platform.Querier) *PgRepository {
	return &PgRepository{db: db}
}

func (r *PgRepository) SyncPermissions(ctx context.Context, defs []platform.PermissionDef) error {
	for _, d := range defs {
		_, err := r.db.Exec(ctx, `
			INSERT INTO users_permissions (key, grp, resource, action, description)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (key) DO UPDATE SET
				grp = EXCLUDED.grp,
				resource = EXCLUDED.resource,
				action = EXCLUDED.action,
				description = EXCLUDED.description
		`, d.Key, d.Group, d.Resource, d.Action, d.Description)
		if err != nil {
			return err
		}
		// Как и в legacy DbManager::addItem: каждое новое право автоматически привязывается к superadmin.
		_, err = r.db.Exec(ctx, `
			INSERT INTO users_role_permissions (role_name, permission_key)
			VALUES ($1, $2) ON CONFLICT DO NOTHING
		`, SuperRoleName, d.Key)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *PgRepository) CreateUser(ctx context.Context, u *User) (int64, error) {
	wc, _ := json.Marshal(nonNilStrings(u.WorkClothes))
	ins, _ := json.Marshal(nonNilStrings(u.Instruments))
	eq, _ := json.Marshal(nonNilStrings(u.Equipments))

	var id int64
	err := r.db.QueryRow(ctx, `
		INSERT INTO users_accounts (
			username, password_hash, email, first_name, second_name, last_name, full_name,
			subdivision_id, is_engineer, is_dispatcher, color, corporate_phone_number,
			office_equipment, training, car_rubber, car_model, car_fuel_card_number,
			car_government_number, car_sts_number, car_contract_number,
			work_clothes_json, instruments_json, equipments_json, avatar_path, active
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25
		) RETURNING id
	`,
		u.Username, u.PasswordHash, u.Email, u.FirstName, u.SecondName, u.LastName, u.FullName,
		u.SubdivisionID, u.IsEngineer, u.IsDispatcher, u.Color, u.CorporatePhoneNumber,
		u.OfficeEquipment, u.Training, u.CarRubber, u.CarModel, u.CarFuelCardNumber,
		u.CarGovernmentNumber, u.CarStsNumber, u.CarContractNumber,
		wc, ins, eq, u.AvatarPath, u.Active,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return 0, platform.NewConflict("Пользователь с таким логином уже существует")
		}
		return 0, err
	}
	if err := r.replaceUserRoles(ctx, id, u.Roles); err != nil {
		return 0, err
	}
	return id, nil
}

func (r *PgRepository) UpdateUser(ctx context.Context, u *User) error {
	wc, _ := json.Marshal(nonNilStrings(u.WorkClothes))
	ins, _ := json.Marshal(nonNilStrings(u.Instruments))
	eq, _ := json.Marshal(nonNilStrings(u.Equipments))

	err := platform.ExecExpectingOne(ctx, r.db, `
		UPDATE users_accounts SET
			email=$2, first_name=$3, second_name=$4, last_name=$5, full_name=$6,
			subdivision_id=$7, is_engineer=$8, is_dispatcher=$9, color=$10,
			corporate_phone_number=$11, office_equipment=$12, training=$13,
			car_rubber=$14, car_model=$15, car_fuel_card_number=$16,
			car_government_number=$17, car_sts_number=$18, car_contract_number=$19,
			work_clothes_json=$20, instruments_json=$21, equipments_json=$22,
			avatar_path=$23, active=$24, updated_at=now()
		WHERE id=$1
	`, "Пользователь не найден",
		u.ID, u.Email, u.FirstName, u.SecondName, u.LastName, u.FullName,
		u.SubdivisionID, u.IsEngineer, u.IsDispatcher, u.Color,
		u.CorporatePhoneNumber, u.OfficeEquipment, u.Training,
		u.CarRubber, u.CarModel, u.CarFuelCardNumber,
		u.CarGovernmentNumber, u.CarStsNumber, u.CarContractNumber,
		wc, ins, eq, u.AvatarPath, u.Active,
	)
	if err != nil {
		return err
	}
	return r.replaceUserRoles(ctx, u.ID, u.Roles)
}

func (r *PgRepository) replaceUserRoles(ctx context.Context, userID int64, roles []string) error {
	if _, err := r.db.Exec(ctx, `DELETE FROM users_user_roles WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, role := range roles {
		if _, err := r.db.Exec(ctx,
			`INSERT INTO users_user_roles (user_id, role_name) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			userID, role,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *PgRepository) UpdatePasswordHash(ctx context.Context, userID int64, hash string) error {
	return platform.ExecExpectingOne(ctx, r.db,
		`UPDATE users_accounts SET password_hash=$2, updated_at=now() WHERE id=$1`,
		"Пользователь не найден", userID, hash,
	)
}

func (r *PgRepository) GetUserByID(ctx context.Context, id int64) (*User, error) {
	return r.getUserOne(ctx, `WHERE id = $1`, id)
}

func (r *PgRepository) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return r.getUserOne(ctx, `WHERE lower(username) = lower($1)`, username)
}

func (r *PgRepository) getUserOne(ctx context.Context, where string, arg any) (*User, error) {
	row := r.db.QueryRow(ctx, `
		SELECT id, username, password_hash, email, first_name, second_name, last_name, full_name,
			subdivision_id, is_engineer, is_dispatcher, color, corporate_phone_number,
			office_equipment, training, car_rubber, car_model, car_fuel_card_number,
			car_government_number, car_sts_number, car_contract_number,
			work_clothes_json, instruments_json, equipments_json, avatar_path,
			active, last_login_at, created_at, updated_at
		FROM users_accounts `+where, arg)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, platform.NewNotFound("Пользователь не найден")
		}
		return nil, err
	}
	roles, err := r.getUserRoles(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	u.Roles = roles
	return u, nil
}

func (r *PgRepository) ListUsers(ctx context.Context, activeOnly bool) ([]User, error) {
	query := `
		SELECT id, username, password_hash, email, first_name, second_name, last_name, full_name,
			subdivision_id, is_engineer, is_dispatcher, color, corporate_phone_number,
			office_equipment, training, car_rubber, car_model, car_fuel_card_number,
			car_government_number, car_sts_number, car_contract_number,
			work_clothes_json, instruments_json, equipments_json, avatar_path,
			active, last_login_at, created_at, updated_at
		FROM users_accounts`
	if activeOnly {
		query += ` WHERE active = TRUE`
	}
	query += ` ORDER BY full_name, username`
	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range users {
		roles, err := r.getUserRoles(ctx, users[i].ID)
		if err != nil {
			return nil, err
		}
		users[i].Roles = roles
	}
	return users, nil
}

func (r *PgRepository) getUserRoles(ctx context.Context, userID int64) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT role_name FROM users_user_roles WHERE user_id=$1 ORDER BY role_name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		roles = append(roles, name)
	}
	return roles, rows.Err()
}

func (r *PgRepository) CountActiveSuperAdmins(ctx context.Context, excludeUserID int64) (int, error) {
	var cnt int
	err := r.db.QueryRow(ctx, `
		SELECT count(DISTINCT u.id)
		FROM users_accounts u
		JOIN users_user_roles ur ON ur.user_id = u.id
		WHERE u.active = TRUE AND ur.role_name = $1 AND u.id <> $2
	`, SuperRoleName, excludeUserID).Scan(&cnt)
	return cnt, err
}

func (r *PgRepository) GetUserPermissions(ctx context.Context, userID int64) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT rp.permission_key
		FROM users_user_roles ur
		JOIN users_role_permissions rp ON rp.role_name = ur.role_name
		WHERE ur.user_id = $1
		ORDER BY rp.permission_key
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var perms []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		perms = append(perms, k)
	}
	return perms, rows.Err()
}

func (r *PgRepository) ListRoles(ctx context.Context) ([]Role, error) {
	rows, err := r.db.Query(ctx, `SELECT name, description, is_system FROM users_roles ORDER BY is_system DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []Role
	for rows.Next() {
		var role Role
		if err := rows.Scan(&role.Name, &role.Description, &role.IsSystem); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range roles {
		perms, err := r.getRolePermissions(ctx, roles[i].Name)
		if err != nil {
			return nil, err
		}
		roles[i].Permissions = perms
	}
	return roles, nil
}

func (r *PgRepository) GetRole(ctx context.Context, name string) (*Role, error) {
	var role Role
	err := r.db.QueryRow(ctx, `SELECT name, description, is_system FROM users_roles WHERE name=$1`, name).
		Scan(&role.Name, &role.Description, &role.IsSystem)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, platform.NewNotFound("Роль не найдена")
		}
		return nil, err
	}
	perms, err := r.getRolePermissions(ctx, role.Name)
	if err != nil {
		return nil, err
	}
	role.Permissions = perms
	return &role, nil
}

func (r *PgRepository) getRolePermissions(ctx context.Context, roleName string) ([]string, error) {
	rows, err := r.db.Query(ctx, `SELECT permission_key FROM users_role_permissions WHERE role_name=$1 ORDER BY permission_key`, roleName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var perms []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		perms = append(perms, k)
	}
	return perms, rows.Err()
}

func (r *PgRepository) SaveRole(ctx context.Context, role *Role) error {
	if _, err := r.db.Exec(ctx, `
		INSERT INTO users_roles (name, description, is_system)
		VALUES ($1, $2, $3)
		ON CONFLICT (name) DO UPDATE SET description = EXCLUDED.description
	`, role.Name, role.Description, role.IsSystem); err != nil {
		return err
	}
	if _, err := r.db.Exec(ctx, `DELETE FROM users_role_permissions WHERE role_name=$1`, role.Name); err != nil {
		return err
	}
	for _, perm := range role.Permissions {
		if _, err := r.db.Exec(ctx,
			`INSERT INTO users_role_permissions (role_name, permission_key) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
			role.Name, perm,
		); err != nil {
			return err
		}
	}
	return nil
}

func (r *PgRepository) DeleteRole(ctx context.Context, name string) error {
	return platform.ExecExpectingOne(ctx, r.db, `DELETE FROM users_roles WHERE name=$1 AND is_system=FALSE`,
		"Системную роль удалить нельзя или роль не найдена", name)
}

func (r *PgRepository) ListPermissions(ctx context.Context) ([]platform.PermissionDef, error) {
	rows, err := r.db.Query(ctx, `SELECT key, grp, resource, action, description FROM users_permissions ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []platform.PermissionDef
	for rows.Next() {
		var p platform.PermissionDef
		if err := rows.Scan(&p.Key, &p.Group, &p.Resource, &p.Action, &p.Description); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PgRepository) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	_, err := r.db.Exec(ctx, `INSERT INTO users_sessions (token_hash, user_id, expires_at) VALUES ($1,$2,$3)`, tokenHash, userID, expiresAt)
	return err
}

func (r *PgRepository) GetSessionUserID(ctx context.Context, tokenHash string, now time.Time) (int64, error) {
	var uid int64
	err := r.db.QueryRow(ctx, `SELECT user_id FROM users_sessions WHERE token_hash=$1 AND expires_at > $2`, tokenHash, now).Scan(&uid)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, platform.NewUnauthorized("Сессия недействительна")
		}
		return 0, err
	}
	return uid, nil
}

func (r *PgRepository) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM users_sessions WHERE token_hash=$1`, tokenHash)
	return err
}

func (r *PgRepository) DeleteUserSessions(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx, `DELETE FROM users_sessions WHERE user_id=$1`, userID)
	return err
}

func (r *PgRepository) UpdateLastLogin(ctx context.Context, userID int64, at time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE users_accounts SET last_login_at=$2 WHERE id=$1`, userID, at)
	return err
}

func (r *PgRepository) CreateAPIToken(ctx context.Context, tok *APIToken) (int64, error) {
	spacesJSON, _ := json.Marshal(nonNilStrings(tok.AllowedSpaces))
	var id int64
	err := r.db.QueryRow(ctx, `
		INSERT INTO users_api_tokens (user_id, name, token_hash, can_read, can_write, ask_only, allowed_spaces, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id
	`, tok.UserID, tok.Name, tok.TokenHash, tok.CanRead, tok.CanWrite, tok.AskOnly, spacesJSON, tok.ExpiresAt).Scan(&id)
	return id, err
}

func (r *PgRepository) ListAPITokens(ctx context.Context, userID int64) ([]APIToken, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, name, can_read, can_write, ask_only, allowed_spaces, expires_at, last_used_at, created_at
		FROM users_api_tokens WHERE user_id=$1 ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []APIToken
	for rows.Next() {
		var t APIToken
		var rawSpaces []byte
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.CanRead, &t.CanWrite, &t.AskOnly, &rawSpaces, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rawSpaces, &t.AllowedSpaces)
		if t.AllowedSpaces == nil {
			t.AllowedSpaces = []string{}
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

func (r *PgRepository) GetAPITokenByHash(ctx context.Context, tokenHash string, now time.Time) (*APIToken, error) {
	var t APIToken
	var rawSpaces []byte
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, name, token_hash, can_read, can_write, ask_only, allowed_spaces, expires_at, last_used_at, created_at
		FROM users_api_tokens
		WHERE token_hash=$1 AND (expires_at IS NULL OR expires_at > $2)
	`, tokenHash, now).Scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &t.CanRead, &t.CanWrite, &t.AskOnly, &rawSpaces, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, platform.NewUnauthorized("Недействительный API-токен")
		}
		return nil, err
	}
	_ = json.Unmarshal(rawSpaces, &t.AllowedSpaces)
	if t.AllowedSpaces == nil {
		t.AllowedSpaces = []string{}
	}
	return &t, nil
}

func (r *PgRepository) TouchAPIToken(ctx context.Context, tokenID int64, at time.Time) error {
	_, err := r.db.Exec(ctx, `UPDATE users_api_tokens SET last_used_at=$2 WHERE id=$1`, tokenID, at)
	return err
}

func (r *PgRepository) DeleteAPIToken(ctx context.Context, tokenID int64, userID int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `DELETE FROM users_api_tokens WHERE id=$1 AND user_id=$2`,
		"Токен не найден", tokenID, userID)
}

func (r *PgRepository) RefreshAPITokenByHash(ctx context.Context, tokenHash string, newExpiresAt time.Time) (*APIToken, error) {
	var t APIToken
	var rawSpaces []byte
	err := r.db.QueryRow(ctx, `
		UPDATE users_api_tokens
		SET expires_at = $2, last_used_at = now()
		WHERE token_hash = $1
		RETURNING id, user_id, name, token_hash, can_read, can_write, ask_only, allowed_spaces, expires_at, last_used_at, created_at
	`, tokenHash, newExpiresAt).Scan(&t.ID, &t.UserID, &t.Name, &t.TokenHash, &t.CanRead, &t.CanWrite, &t.AskOnly, &rawSpaces, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, platform.NewUnauthorized("Недействительный API-токен")
		}
		return nil, err
	}
	_ = json.Unmarshal(rawSpaces, &t.AllowedSpaces)
	if t.AllowedSpaces == nil {
		t.AllowedSpaces = []string{}
	}
	return &t, nil
}

func (r *PgRepository) DeleteAPITokenByHash(ctx context.Context, tokenHash string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM users_api_tokens WHERE token_hash = $1`, tokenHash)
	return err
}

func (r *PgRepository) ListInventoryItems(ctx context.Context, category string) ([]InventoryItem, error) {
	q := `SELECT id, category, name, deleted, created_at FROM users_inventory_items WHERE deleted = FALSE`
	var args []any
	if strings.TrimSpace(category) != "" {
		args = append(args, strings.TrimSpace(category))
		q += ` AND category = $1`
	}
	q += ` ORDER BY category, name`
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InventoryItem{}
	for rows.Next() {
		var it InventoryItem
		if err := rows.Scan(&it.ID, &it.Category, &it.Name, &it.Deleted, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *PgRepository) CreateInventoryItem(ctx context.Context, item *InventoryItem) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO users_inventory_items (category, name)
		VALUES ($1, $2)
		RETURNING id, deleted, created_at
	`, item.Category, item.Name).Scan(&item.ID, &item.Deleted, &item.CreatedAt)
}

func (r *PgRepository) DeleteInventoryItem(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `UPDATE users_inventory_items SET deleted = TRUE WHERE id = $1 AND deleted = FALSE`,
		"Позиция справочника инвентаря не найдена", id)
}

func (r *PgRepository) LogLoginEvent(ctx context.Context, ev *LoginEvent) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO users_login_events (event, username, user_id, ip, user_agent, reason)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, ev.Event, ev.Username, ev.UserID, ev.IP, ev.UserAgent, ev.Reason)
	return err
}

func (r *PgRepository) ListLoginEvents(ctx context.Context, userID *int64, event string, limit int) ([]LoginEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 300
	}
	var conds []string
	var args []any
	if userID != nil {
		args = append(args, *userID)
		conds = append(conds, fmt.Sprintf("user_id = $%d", len(args)))
	}
	if event != "" {
		args = append(args, event)
		conds = append(conds, fmt.Sprintf("event = $%d", len(args)))
	}
	query := `SELECT id, event, username, user_id, ip, user_agent, reason, created_at FROM users_login_events`
	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	args = append(args, limit)
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", len(args))

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []LoginEvent
	for rows.Next() {
		var e LoginEvent
		if err := rows.Scan(&e.ID, &e.Event, &e.Username, &e.UserID, &e.IP, &e.UserAgent, &e.Reason, &e.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, e)
	}
	return list, rows.Err()
}

func (r *PgRepository) GetModelAccess(ctx context.Context, modelID int64) (*ModelAccessPolicy, error) {
	var p ModelAccessPolicy
	var rawIDs []byte
	err := r.db.QueryRow(ctx, `SELECT model_id, open_to_all, user_ids FROM users_ai_model_access WHERE model_id=$1`, modelID).
		Scan(&p.ModelID, &p.OpenToAll, &rawIDs)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &ModelAccessPolicy{ModelID: modelID, OpenToAll: nil, UserIDs: []int64{}}, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(rawIDs, &p.UserIDs)
	return &p, nil
}

func (r *PgRepository) SetModelAccess(ctx context.Context, policy ModelAccessPolicy) error {
	ids := policy.UserIDs
	if ids == nil {
		ids = []int64{}
	}
	raw, _ := json.Marshal(ids)
	_, err := r.db.Exec(ctx, `
		INSERT INTO users_ai_model_access (model_id, open_to_all, user_ids, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (model_id) DO UPDATE SET
			open_to_all = EXCLUDED.open_to_all,
			user_ids = EXCLUDED.user_ids,
			updated_at = now()
	`, policy.ModelID, policy.OpenToAll, raw)
	return err
}

func (r *PgRepository) ListModelAccess(ctx context.Context) ([]ModelAccessPolicy, error) {
	rows, err := r.db.Query(ctx, `SELECT model_id, open_to_all, user_ids FROM users_ai_model_access ORDER BY model_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []ModelAccessPolicy
	for rows.Next() {
		var p ModelAccessPolicy
		var raw []byte
		if err := rows.Scan(&p.ModelID, &p.OpenToAll, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &p.UserIDs)
		list = append(list, p)
	}
	return list, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (*User, error) {
	var u User
	var wcRaw, insRaw, eqRaw []byte
	err := row.Scan(
		&u.ID, &u.Username, &u.PasswordHash, &u.Email,
		&u.FirstName, &u.SecondName, &u.LastName, &u.FullName,
		&u.SubdivisionID, &u.IsEngineer, &u.IsDispatcher, &u.Color, &u.CorporatePhoneNumber,
		&u.OfficeEquipment, &u.Training, &u.CarRubber, &u.CarModel, &u.CarFuelCardNumber,
		&u.CarGovernmentNumber, &u.CarStsNumber, &u.CarContractNumber,
		&wcRaw, &insRaw, &eqRaw, &u.AvatarPath,
		&u.Active, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(wcRaw, &u.WorkClothes)
	_ = json.Unmarshal(insRaw, &u.Instruments)
	_ = json.Unmarshal(eqRaw, &u.Equipments)
	return &u, nil
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *PgRepository) ListUserIDsByPermission(ctx context.Context, perm string) ([]int64, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT u.id
		FROM users_accounts u
		JOIN users_user_roles ur ON ur.user_id = u.id
		LEFT JOIN users_role_permissions rp ON rp.role_name = ur.role_name
		WHERE u.active = TRUE
		  AND (ur.role_name = $1 OR rp.permission_key = $2)
		ORDER BY u.id
	`, SuperRoleName, perm)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *PgRepository) CreateFile(ctx context.Context, f *StoredFile) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO users_files (original_name, mime_type, size_bytes, storage_path, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at
	`, f.OriginalName, f.MimeType, f.SizeBytes, f.StoragePath, f.CreatedBy).Scan(&f.ID, &f.CreatedAt)
}

func (r *PgRepository) GetFile(ctx context.Context, id int64) (*StoredFile, error) {
	var f StoredFile
	err := r.db.QueryRow(ctx, `
		SELECT id, original_name, mime_type, size_bytes, storage_path, created_by, created_at
		FROM users_files
		WHERE id = $1
	`, id).Scan(&f.ID, &f.OriginalName, &f.MimeType, &f.SizeBytes, &f.StoragePath, &f.CreatedBy, &f.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, platform.NewNotFound("Файл не найден")
	}
	if err != nil {
		return nil, err
	}
	return &f, nil
}
