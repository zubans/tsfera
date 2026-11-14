package users

import (
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const SuperRoleName = "superadmin"

// User — учётная запись сотрудника Теплосферы и модулей платформы.
type User struct {
	ID                   int64      `json:"id"`
	Username             string     `json:"username"`
	PasswordHash         string     `json:"-"`
	Email                *string    `json:"email"`
	FirstName            string     `json:"first_name"`
	SecondName           string     `json:"second_name"`
	LastName             string     `json:"last_name"`
	FullName             string     `json:"full_name"`
	SubdivisionID        *int64     `json:"subdivision_id"`
	IsEngineer           bool       `json:"is_engineer"`
	IsDispatcher         bool       `json:"is_dispatcher"`
	Color                string     `json:"color"`
	CorporatePhoneNumber string     `json:"corporate_phone_number"`
	OfficeEquipment      string     `json:"office_equipment"`
	Training             string     `json:"training"`
	CarRubber            string     `json:"car_rubber"`
	CarModel             string     `json:"car_model"`
	CarFuelCardNumber    string     `json:"car_fuel_card_number"`
	CarGovernmentNumber  string     `json:"car_government_number"`
	CarStsNumber         string     `json:"car_sts_number"`
	CarContractNumber    string     `json:"car_contract_number"`
	WorkClothes          []string   `json:"work_clothes"`
	Instruments          []string   `json:"instruments"`
	Equipments           []string   `json:"equipments"`
	AvatarPath           string     `json:"avatar_path"`
	Active               bool       `json:"active"`
	Roles                []string   `json:"roles"`
	LastLoginAt          *time.Time `json:"last_login_at"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func BuildFullName(last, first, second string) string {
	var parts []string
	for _, p := range []string{last, first, second} {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

// Role представляет роль RBAC с набором ключей разрешений.
type Role struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	IsSystem    bool     `json:"is_system"`
	Permissions []string `json:"permissions"`
}

// APIToken используется мобильными клиентами (X-API-key) и ИИ-агентами (Bearer).
type APIToken struct {
	ID         int64      `json:"id"`
	UserID     int64      `json:"user_id"`
	Name       string     `json:"name"`
	TokenHash  string     `json:"-"`
	CanRead    bool       `json:"can_read"`
	CanWrite   bool       `json:"can_write"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// LoginEvent фиксирует историю входов, неудачных попыток и выходов пользователей.
type LoginEvent struct {
	ID        int64     `json:"id"`
	Event     string    `json:"event"`
	Username  string    `json:"username"`
	UserID    *int64    `json:"user_id"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ModelAccessPolicy задаёт доступ пользователей к LLM-модели по образцу KB.
type ModelAccessPolicy struct {
	ModelID   int64   `json:"model_id"`
	OpenToAll *bool   `json:"open_to_all"`
	UserIDs   []int64 `json:"user_ids"`
}

const (
	InventoryWorkClothes = "work_clothes"
	InventoryInstrument  = "instrument"
	InventoryEquipment   = "engineer_equipment"
)

// InventoryItem представляет справочник спецодежды, инструмента и оборудования инженеров.
type InventoryItem struct {
	ID        int64     `json:"id"`
	Category  string    `json:"category"`
	Name      string    `json:"name"`
	Deleted   bool      `json:"deleted"`
	CreatedAt time.Time `json:"created_at"`
}

// HashPassword генерирует стойкий bcrypt-хеш (замена устаревшему md5(md5)).
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// VerifyPassword проверяет пароль против bcrypt или legacy double-MD5 (32-символьный hex).
func VerifyPassword(storedHash, plain string) (ok bool, needsUpgrade bool) {
	if strings.HasPrefix(storedHash, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(plain)) == nil, false
	}
	clean := strings.TrimPrefix(storedHash, "md52:")
	if len(clean) == 32 && isHex(clean) {
		first := md5.Sum([]byte(plain))
		firstHex := hex.EncodeToString(first[:])
		second := md5.Sum([]byte(firstHex))
		if strings.EqualFold(hex.EncodeToString(second[:]), clean) {
			return true, true
		}
	}
	return false, false
}

func isHex(s string) bool {
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func GenerateRandomToken() (raw string, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(buf)
	return raw, HashToken(raw), nil
}

func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
