package crm

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

type Config struct {
	MTSWebhookSecret       string
	MTSBaseURL             string
	MTSAPIToken            string
	DaDataAPIKey           string
	DaDataBaseURL          string
	CloudPaymentsPublicID  string
	CloudPaymentsAPISecret string
	CloudPaymentsBaseURL   string
	Clock                  func() time.Time
}

type Service struct {
	repo       Repository
	users      UserDirectory
	bus        platform.EventBus
	hub        *WSHub
	httpClient *http.Client
	cfg        Config
	logger     *slog.Logger
}

func NewService(
	repo Repository,
	users UserDirectory,
	bus platform.EventBus,
	hub *WSHub,
	cfg Config,
	logger *slog.Logger,
) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return &Service{
		repo:       repo,
		users:      users,
		bus:        bus,
		hub:        hub,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		cfg:        cfg,
		logger:     logger,
	}
}

func (s *Service) now() time.Time {
	return s.cfg.Clock()
}

func (s *Service) moscowLoc() *time.Location {
	loc, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		return time.FixedZone("MSK", 3*3600)
	}
	return loc
}

func (s *Service) logChange(ctx context.Context, auth *platform.AuthContext, entityType string, entityID int64, action string, payload any) {
	var uid int64
	if auth != nil {
		uid = auth.UserID
	}
	raw, _ := json.Marshal(payload)
	if err := s.repo.AddChangeLog(ctx, ChangeLog{
		EntityType: entityType,
		EntityID:   entityID,
		Action:     action,
		UserID:     uid,
		Changes:    raw,
	}); err != nil {
		s.logger.Warn("change log write failed", "entity", entityType, "id", entityID, "error", err)
	}
}

func (s *Service) pushNotification(ctx context.Context, userID int64, kind, title, message, entityType string, entityID int64) {
	if userID <= 0 {
		return
	}
	n := &Notification{
		UserID:     userID,
		Kind:       kind,
		Title:      title,
		Message:    message,
		EntityType: entityType,
		EntityID:   entityID,
	}
	if err := s.repo.CreateNotification(ctx, n); err != nil {
		s.logger.Warn("create notification failed", "user_id", userID, "error", err)
		return
	}
	if s.hub != nil {
		s.hub.BroadcastToUsers([]int64{userID}, WSEvent{
			Type:    "notification:new",
			Payload: n,
		})
	}
}

func hasFullContractorAccess(auth *platform.AuthContext) bool {
	if auth == nil {
		return false
	}
	return auth.IsSuperAdmin || auth.HasPermission("crm.contractor.view_all")
}

func hasWithoutSubdivisionAccess(auth *platform.AuthContext) bool {
	return auth != nil && (auth.IsSuperAdmin || auth.HasPermission("admin.user.without_subdivision"))
}

// CanViewContractor реализует правило EngineerViewContractor (±3 дня по заявкам инженера) и ограничение подразделения.
func (s *Service) CanViewContractor(ctx context.Context, auth *platform.AuthContext, contractorID int64) (bool, error) {
	if auth == nil {
		return false, nil
	}
	if hasFullContractorAccess(auth) {
		if !hasWithoutSubdivisionAccess(auth) && auth.SubdivisionID != nil {
			c, err := s.repo.GetContractor(ctx, contractorID)
			if err != nil {
				return false, err
			}
			if c != nil && c.SubdivisionID != nil && *c.SubdivisionID != *auth.SubdivisionID {
				return false, nil
			}
		}
		return true, nil
	}
	if !auth.HasPermission("crm.contractor.view") {
		return false, nil
	}
	now := s.now()
	return s.repo.HasEngineerVisibleApplicationInWindow(
		ctx,
		auth.UserID,
		contractorID,
		now.Add(-72*time.Hour),
		now.Add(72*time.Hour),
	)
}

// --- Contractors & Addresses ---

func (s *Service) SearchContractors(ctx context.Context, auth *platform.AuthContext, query string, limit, offset int) ([]Contractor, int, error) {
	return s.SearchContractorsFiltered(ctx, auth, ContractorFilter{
		Query:  query,
		Limit:  limit,
		Offset: offset,
	})
}

func (s *Service) SearchContractorsFiltered(ctx context.Context, auth *platform.AuthContext, f ContractorFilter) ([]Contractor, int, error) {
	if auth == nil || (!hasFullContractorAccess(auth) && !auth.HasPermission("crm.contractor.view")) {
		return nil, 0, platform.NewForbidden("Нет прав на просмотр контрагентов")
	}
	f.Query = strings.TrimSpace(f.Query)
	f.Now = s.now()
	if !hasFullContractorAccess(auth) {
		uid := auth.UserID
		f.EngineerID = &uid
	} else if !hasWithoutSubdivisionAccess(auth) && auth.SubdivisionID != nil {
		subID := *auth.SubdivisionID
		f.SubdivisionID = &subID
	}
	return s.repo.SearchContractors(ctx, f)
}

func (s *Service) GetContractor(ctx context.Context, auth *platform.AuthContext, id int64) (*Contractor, error) {
	ok, err := s.CanViewContractor(ctx, auth, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, platform.NewForbidden("Контрагент недоступен: отсутствует активная заявка в пределах ±3 дней")
	}
	c, err := s.repo.GetContractor(ctx, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, platform.NewNotFound("Контрагент не найден")
	}
	return c, nil
}

type SaveContractorInput struct {
	Name            string            `json:"name"`
	FirstName       string            `json:"first_name"`
	SecondName      string            `json:"second_name"`
	LastName        string            `json:"last_name"`
	SubdivisionID   *int64            `json:"subdivision_id"`
	Type            ContractorType    `json:"type"`
	IsLegalEntity   bool              `json:"is_legal_entity"`
	TrafficSourceID *int64            `json:"traffic_source_id"`
	LegalEntity     *LegalEntity      `json:"legal_entity"`
	Note            string            `json:"note"`
	Phones          []ContractorPhone `json:"phones"`
	Emails          []ContractorEmail `json:"emails"`
}

func resolveContractorName(in SaveContractorInput) string {
	if name := strings.TrimSpace(in.Name); name != "" {
		return name
	}
	parts := make([]string, 0, 3)
	for _, p := range []string{in.LastName, in.FirstName, in.SecondName} {
		if t := strings.TrimSpace(p); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

func normalizeUniquePhones(in []ContractorPhone) ([]ContractorPhone, error) {
	seen := make(map[string]bool, len(in))
	out := make([]ContractorPhone, 0, len(in))
	for _, p := range in {
		if strings.TrimSpace(p.Phone) == "" {
			continue
		}
		clean, err := platform.ParsePhone10(p.Phone)
		if err != nil {
			return nil, err
		}
		if seen[clean] {
			continue
		}
		seen[clean] = true
		out = append(out, ContractorPhone{
			Phone:     clean,
			Formatted: platform.FormatPhoneRU(clean),
			Note:      strings.TrimSpace(p.Note),
		})
	}
	return out, nil
}

func normalizeUniqueEmails(in []ContractorEmail) []ContractorEmail {
	seen := make(map[string]bool, len(in))
	out := make([]ContractorEmail, 0, len(in))
	for _, e := range in {
		trimmed := strings.TrimSpace(e.Email)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ContractorEmail{Email: trimmed})
	}
	return out
}

func (s *Service) CheckSimilarContractors(ctx context.Context, excludeID *int64, phones []string, emails []string) ([]SimilarContractorMatch, error) {
	seenPhones := map[string]bool{}
	phones10 := make([]string, 0, len(phones))
	for _, raw := range phones {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		p10, err := platform.ParsePhone10(raw)
		if err != nil || seenPhones[p10] {
			continue
		}
		seenPhones[p10] = true
		phones10 = append(phones10, p10)
	}
	seenEmails := map[string]bool{}
	cleanEmails := make([]string, 0, len(emails))
	for _, raw := range emails {
		e := strings.ToLower(strings.TrimSpace(raw))
		if e == "" || seenEmails[e] {
			continue
		}
		seenEmails[e] = true
		cleanEmails = append(cleanEmails, e)
	}
	if len(phones10) == 0 && len(cleanEmails) == 0 {
		return nil, nil
	}
	return s.repo.CheckSimilarContractors(ctx, excludeID, phones10, cleanEmails)
}

func sanitizeLegalEntityInput(in *LegalEntity, fallbackName string) *LegalEntity {
	if in == nil {
		return nil
	}
	cp := *in
	cp.Name = strings.TrimSpace(cp.Name)
	if cp.Name == "" {
		cp.Name = fallbackName
	}
	cp.DirectorName = strings.TrimSpace(cp.DirectorName)
	cp.ActualAddress = strings.TrimSpace(cp.ActualAddress)
	cp.LegalAddress = strings.TrimSpace(cp.LegalAddress)
	cp.BankName = strings.TrimSpace(cp.BankName)
	cp.INN = strings.TrimSpace(cp.INN)
	cp.KPP = strings.TrimSpace(cp.KPP)
	cp.BIK = strings.TrimSpace(cp.BIK)
	cp.CheckingAccount = strings.TrimSpace(cp.CheckingAccount)
	cp.CorrespondingAccount = strings.TrimSpace(cp.CorrespondingAccount)
	cp.OKPO = strings.TrimSpace(cp.OKPO)
	cp.OGRN = strings.TrimSpace(cp.OGRN)
	return &cp
}

func (s *Service) CreateContractor(ctx context.Context, auth *platform.AuthContext, in SaveContractorInput) (*Contractor, error) {
	name := resolveContractorName(in)
	if name == "" && in.LegalEntity != nil {
		name = strings.TrimSpace(in.LegalEntity.Name)
	}
	if name == "" {
		return nil, platform.NewValidation("Укажите наименование/ФИО контрагента", map[string]string{"name": "Обязательное поле"})
	}
	cType := in.Type
	isLegal := in.IsLegalEntity || cType == ContractorLegal
	if isLegal {
		cType = ContractorLegal
	} else if cType != ContractorIndividual {
		cType = ContractorIndividual
	}
	phones, err := normalizeUniquePhones(in.Phones)
	if err != nil {
		return nil, err
	}
	emails := normalizeUniqueEmails(in.Emails)

	var uid int64
	if auth != nil {
		uid = auth.UserID
	}
	subID := in.SubdivisionID
	if subID == nil && auth != nil && auth.SubdivisionID != nil {
		sID := *auth.SubdivisionID
		subID = &sID
	}
	var legal *LegalEntity
	if isLegal || in.LegalEntity != nil {
		legal = sanitizeLegalEntityInput(in.LegalEntity, name)
	}
	c := &Contractor{
		Name:            name,
		FirstName:       strings.TrimSpace(in.FirstName),
		SecondName:      strings.TrimSpace(in.SecondName),
		LastName:        strings.TrimSpace(in.LastName),
		SubdivisionID:   subID,
		Type:            cType,
		IsLegalEntity:   isLegal,
		TrafficSourceID: in.TrafficSourceID,
		LegalEntity:     legal,
		Note:            strings.TrimSpace(in.Note),
		CreatedBy:       uid,
	}
	if err := s.repo.CreateContractor(ctx, c); err != nil {
		return nil, err
	}
	if len(phones) > 0 {
		if err := s.repo.ReplaceContractorPhones(ctx, c.ID, phones); err != nil {
			return nil, err
		}
	}
	if len(emails) > 0 {
		if err := s.repo.ReplaceContractorEmails(ctx, c.ID, emails); err != nil {
			return nil, err
		}
	}
	s.logChange(ctx, auth, "contractor", c.ID, "create", in)
	return s.repo.GetContractor(ctx, c.ID)
}

func (s *Service) UpdateContractor(ctx context.Context, auth *platform.AuthContext, id int64, in SaveContractorInput) (*Contractor, error) {
	existing, err := s.repo.GetContractor(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, platform.NewNotFound("Контрагент не найден")
	}
	name := resolveContractorName(in)
	if name == "" && in.LegalEntity != nil {
		name = strings.TrimSpace(in.LegalEntity.Name)
	}
	if name == "" {
		return nil, platform.NewValidation("Укажите наименование/ФИО контрагента", map[string]string{"name": "Обязательное поле"})
	}
	cType := in.Type
	isLegal := in.IsLegalEntity || cType == ContractorLegal
	if isLegal {
		cType = ContractorLegal
	} else if cType != ContractorIndividual {
		if in.Type == 0 && existing.Type == ContractorLegal && !in.IsLegalEntity {
			cType = ContractorIndividual
		} else if cType != ContractorIndividual {
			cType = ContractorIndividual
		}
	}
	phones, err := normalizeUniquePhones(in.Phones)
	if err != nil {
		return nil, err
	}
	emails := normalizeUniqueEmails(in.Emails)

	existing.Name = name
	existing.FirstName = strings.TrimSpace(in.FirstName)
	existing.SecondName = strings.TrimSpace(in.SecondName)
	existing.LastName = strings.TrimSpace(in.LastName)
	if in.SubdivisionID != nil {
		existing.SubdivisionID = in.SubdivisionID
	}
	existing.Type = cType
	existing.IsLegalEntity = isLegal
	if in.TrafficSourceID != nil {
		existing.TrafficSourceID = in.TrafficSourceID
	}
	if in.LegalEntity != nil {
		existing.LegalEntity = sanitizeLegalEntityInput(in.LegalEntity, name)
	}
	existing.Note = strings.TrimSpace(in.Note)
	if err := s.repo.UpdateContractor(ctx, existing); err != nil {
		return nil, err
	}
	if in.Phones != nil {
		if err := s.repo.ReplaceContractorPhones(ctx, id, phones); err != nil {
			return nil, err
		}
	}
	if in.Emails != nil {
		if err := s.repo.ReplaceContractorEmails(ctx, id, emails); err != nil {
			return nil, err
		}
	}
	s.logChange(ctx, auth, "contractor", id, "update", in)
	return s.repo.GetContractor(ctx, id)
}

func (s *Service) DeleteContractor(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.SoftDeleteContractor(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "contractor", id, "delete", nil)
	return nil
}

func (s *Service) CreateAddress(ctx context.Context, auth *platform.AuthContext, addr ContractorAddress) (*ContractorAddress, error) {
	if addr.ContractorID <= 0 || strings.TrimSpace(addr.Address) == "" {
		return nil, platform.NewValidation("Укажите контрагента и адрес", map[string]string{"address": "Обязательное поле"})
	}
	c, err := s.repo.GetContractor(ctx, addr.ContractorID)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, platform.NewNotFound("Контрагент не найден")
	}
	addr.Address = strings.TrimSpace(addr.Address)
	dup, err := s.repo.FindAddressByText(ctx, addr.ContractorID, addr.Address)
	if err != nil {
		return nil, err
	}
	if dup != nil {
		return nil, platform.NewValidation("Такой адрес уже добавлен контрагенту", map[string]string{"address": "Дубликат адреса"})
	}
	if err := s.repo.CreateAddress(ctx, &addr); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "address", addr.ID, "create", addr)
	return &addr, nil
}

func (s *Service) UpdateAddress(ctx context.Context, auth *platform.AuthContext, id int64, in ContractorAddress) (*ContractorAddress, error) {
	existing, err := s.repo.GetAddress(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, platform.NewNotFound("Адрес не найден")
	}
	if strings.TrimSpace(in.Address) == "" {
		return nil, platform.NewValidation("Адрес не может быть пустым", map[string]string{"address": "Обязательное поле"})
	}
	existing.Address = strings.TrimSpace(in.Address)
	existing.RegionCode = strings.TrimSpace(in.RegionCode)
	existing.City = strings.TrimSpace(in.City)
	existing.Street = strings.TrimSpace(in.Street)
	existing.House = strings.TrimSpace(in.House)
	existing.Apartment = strings.TrimSpace(in.Apartment)
	existing.Floor = strings.TrimSpace(in.Floor)
	existing.Entrance = strings.TrimSpace(in.Entrance)
	existing.Comment = strings.TrimSpace(in.Comment)
	if err := s.repo.UpdateAddress(ctx, existing); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "address", id, "update", existing)
	return existing, nil
}

// DeleteAddress запрещает удаление адреса, если к нему привязаны оборудование, договоры, заявки или акты (как в legacy CRM).
func (s *Service) DeleteAddress(ctx context.Context, auth *platform.AuthContext, id int64) error {
	eqs, contracts, apps, certs, err := s.repo.CountAddressLinks(ctx, id)
	if err != nil {
		return err
	}
	if eqs+contracts+apps+certs > 0 {
		return platform.NewRuleError(fmt.Sprintf(
			"Нельзя удалить адрес: имеются привязанные объекты (оборудование: %d, договоры: %d, заявки: %d, акты: %d)",
			eqs, contracts, apps, certs,
		))
	}
	if err := s.repo.SoftDeleteAddress(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "address", id, "delete", nil)
	return nil
}

func (s *Service) SearchAddresses(ctx context.Context, contractorID *int64, query string, limit int) ([]ContractorAddress, error) {
	return s.repo.SearchAddresses(ctx, contractorID, query, limit)
}

func (s *Service) GetAddressEquipments(ctx context.Context, auth *platform.AuthContext, addressID int64) ([]AddressEquipment, error) {
	addr, err := s.repo.GetAddress(ctx, addressID)
	if err != nil {
		return nil, err
	}
	if addr == nil {
		return nil, platform.NewNotFound("Адрес не найден")
	}
	c, err := s.GetContractor(ctx, auth, addr.ContractorID)
	if err != nil {
		return nil, err
	}
	for _, a := range c.Addresses {
		if a.ID == addressID {
			return a.Equipments, nil
		}
	}
	return nil, nil
}

// --- Equipments ---

func (s *Service) ListEquipments(ctx context.Context, query string, limit int) ([]Equipment, error) {
	return s.repo.ListEquipments(ctx, query, limit)
}

func (s *Service) CreateEquipment(ctx context.Context, auth *platform.AuthContext, eqType, model, manufacturer string) (*Equipment, error) {
	name := BuildEquipmentName(eqType, model, manufacturer)
	if name == "" {
		return nil, platform.NewValidation("Укажите тип или модель оборудования", nil)
	}
	eq := &Equipment{
		Type:         strings.TrimSpace(eqType),
		Model:        strings.TrimSpace(model),
		Manufacturer: strings.TrimSpace(manufacturer),
		Name:         name,
	}
	if err := s.repo.CreateEquipment(ctx, eq); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "equipment", eq.ID, "create", eq)
	return eq, nil
}

func (s *Service) AttachAddressEquipment(ctx context.Context, auth *platform.AuthContext, ae AddressEquipment) (*AddressEquipment, error) {
	if ae.AddressID <= 0 || ae.EquipmentID <= 0 {
		return nil, platform.NewValidation("Укажите адрес и модель оборудования", nil)
	}
	ae.SerialNumber = strings.TrimSpace(ae.SerialNumber)
	ae.Note = strings.TrimSpace(ae.Note)
	if err := s.repo.AttachAddressEquipment(ctx, &ae); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "address_equipment", ae.ID, "create", ae)
	return &ae, nil
}

func (s *Service) DetachAddressEquipment(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.SoftDeleteAddressEquipment(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "address_equipment", id, "delete", nil)
	return nil
}

// --- Reference Catalogs (Manufacturers, Equipment Types, Work Types, Special Notes) ---

func isValidRefKind(kind string) bool {
	switch kind {
	case RefKindManufacturer, RefKindEquipmentType, RefKindWorkType, RefKindSpecialNote:
		return true
	default:
		return false
	}
}

func (s *Service) ListRefItems(ctx context.Context, kind string) ([]RefItem, error) {
	kind = strings.TrimSpace(kind)
	if kind != "" && !isValidRefKind(kind) {
		return nil, platform.NewValidation("Неизвестный тип справочника", map[string]string{"kind": "Неверное значение"})
	}
	return s.repo.ListRefItems(ctx, kind)
}

func (s *Service) CreateRefItem(ctx context.Context, auth *platform.AuthContext, kind, name string) (*RefItem, error) {
	kind = strings.TrimSpace(kind)
	name = strings.TrimSpace(name)
	if !isValidRefKind(kind) {
		return nil, platform.NewValidation("Неизвестный тип справочника", map[string]string{"kind": "Неверное значение"})
	}
	if name == "" {
		return nil, platform.NewValidation("Название записи справочника обязательно", map[string]string{"name": "Обязательное поле"})
	}
	item := &RefItem{Kind: kind, Name: name}
	if err := s.repo.CreateRefItem(ctx, item); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "ref_"+kind, item.ID, "create", item)
	return item, nil
}

func (s *Service) DeleteRefItem(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.SoftDeleteRefItem(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "ref_item", id, "delete", nil)
	return nil
}

// --- Maintenance Contracts ---

func (s *Service) ListContracts(ctx context.Context, f ContractFilter) ([]MaintenanceContract, error) {
	return s.repo.ListContracts(ctx, f)
}

func (s *Service) GetContract(ctx context.Context, id int64) (*MaintenanceContract, error) {
	c, err := s.repo.GetContract(ctx, id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, platform.NewNotFound("Договор ТО не найден")
	}
	return c, nil
}

func (s *Service) CreateContract(ctx context.Context, auth *platform.AuthContext, in MaintenanceContract) (*MaintenanceContract, error) {
	if err := validateContractInput(&in); err != nil {
		return nil, err
	}
	if auth != nil {
		in.CreatedBy = auth.UserID
	}
	if in.Status == 0 {
		in.Status = StatusActive
	}
	if err := s.repo.CreateContract(ctx, &in); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "contract", in.ID, "create", in)
	return s.repo.GetContract(ctx, in.ID)
}

func (s *Service) UpdateContract(ctx context.Context, auth *platform.AuthContext, id int64, in MaintenanceContract) (*MaintenanceContract, error) {
	existing, err := s.repo.GetContract(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, platform.NewNotFound("Договор ТО не найден")
	}
	in.ID = id
	if err := validateContractInput(&in); err != nil {
		return nil, err
	}
	if in.Status == 0 {
		in.Status = existing.Status
	}
	if err := s.repo.UpdateContract(ctx, &in); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "contract", id, "update", in)
	return s.repo.GetContract(ctx, id)
}

func (s *Service) DeleteContract(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.SoftDeleteContract(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "contract", id, "delete", nil)
	return nil
}

func validateContractInput(c *MaintenanceContract) error {
	c.Number = strings.TrimSpace(c.Number)
	if c.ContractorID <= 0 || c.AddressID <= 0 || c.Number == "" {
		return platform.NewValidation("Укажите контрагента, адрес и номер договора", nil)
	}
	if c.Price < 0 {
		return platform.NewValidation("Сумма договора не может быть отрицательной", map[string]string{"price": "Неверное значение"})
	}
	if c.ValidTo.Before(c.ValidFrom) {
		return platform.NewValidation("Дата окончания договора раньше даты начала", map[string]string{"valid_to": "Должна быть >= даты начала"})
	}
	if c.SignedAt.IsZero() {
		c.SignedAt = c.ValidFrom
	}
	c.Category = strings.TrimSpace(c.Category)
	if c.Category == "" {
		c.Category = ContractCategoryStandard
	}
	if !IsValidExtendedContractCategory(c.Category) {
		return platform.NewValidation("Неизвестная категория договора ТО", map[string]string{"category": "Неверное значение"})
	}
	for i := range c.Equipments {
		c.Equipments[i].SerialNumber = strings.TrimSpace(c.Equipments[i].SerialNumber)
		c.Equipments[i].EquipmentName = strings.TrimSpace(c.Equipments[i].EquipmentName)
	}
	return nil
}

type RenewContractInput struct {
	Number    string     `json:"number"`
	Price     *float64   `json:"price"`
	Category  string     `json:"category"`
	ValidFrom *time.Time `json:"valid_from"`
	ValidTo   *time.Time `json:"valid_to"`
	Note      string     `json:"note"`
}

// RenewContract продлевает договор ТО на следующий год с переносом адреса, оборудования и тарифа.
func (s *Service) RenewContract(ctx context.Context, auth *platform.AuthContext, id int64, in RenewContractInput) (*MaintenanceContract, error) {
	old, err := s.repo.GetContract(ctx, id)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, platform.NewNotFound("Договор ТО не найден")
	}
	from := old.ValidTo.AddDate(0, 0, 1)
	if in.ValidFrom != nil && !in.ValidFrom.IsZero() {
		from = *in.ValidFrom
	}
	to := from.AddDate(1, 0, -1)
	if in.ValidTo != nil && !in.ValidTo.IsZero() {
		to = *in.ValidTo
	}
	number := strings.TrimSpace(in.Number)
	if number == "" {
		number = strings.TrimSpace(old.Number) + "-П"
	}
	price := old.Price
	if in.Price != nil {
		price = *in.Price
	}
	category := strings.TrimSpace(in.Category)
	if category == "" {
		category = old.Category
	}
	note := strings.TrimSpace(in.Note)
	if note == "" {
		note = fmt.Sprintf("Пролонгация договора №%s", old.Number)
	}
	var uid int64
	if auth != nil {
		uid = auth.UserID
	}
	next := &MaintenanceContract{
		ContractorID: old.ContractorID,
		AddressID:    old.AddressID,
		Category:     category,
		Number:       number,
		Price:        price,
		SignedAt:     s.now(),
		ValidFrom:    from,
		ValidTo:      to,
		Status:       StatusActive,
		Equipments:   slices.Clone(old.Equipments),
		Note:         note,
		CreatedBy:    uid,
	}
	if err := validateContractInput(next); err != nil {
		return nil, err
	}
	if err := s.repo.CreateContract(ctx, next); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "contract", next.ID, "renew", map[string]any{"source_contract_id": old.ID, "new_contract_id": next.ID})
	return s.repo.GetContract(ctx, next.ID)
}

// --- Applications & Calendar ---

func (s *Service) ListApplications(ctx context.Context, auth *platform.AuthContext, f ApplicationFilter) ([]Application, error) {
	if auth != nil {
		if !hasFullContractorAccess(auth) {
			uid := auth.UserID
			f.EngineerID = &uid
		} else if !hasWithoutSubdivisionAccess(auth) && auth.SubdivisionID != nil {
			subID := *auth.SubdivisionID
			f.SubdivisionID = &subID
		}
	}
	items, err := s.repo.ListApplications(ctx, f)
	if err != nil {
		return nil, err
	}
	s.enrichApplicationUsers(ctx, items)
	return items, nil
}

func (s *Service) ListCalendarEvents(ctx context.Context, auth *platform.AuthContext, from, to time.Time, engineerID *int64, subdivisionID *int64) ([]CalendarEvent, error) {
	if auth != nil {
		if !hasFullContractorAccess(auth) && !auth.HasPermission("crm.calendar.select_user") {
			uid := auth.UserID
			engineerID = &uid
		}
		if !hasWithoutSubdivisionAccess(auth) && auth.SubdivisionID != nil {
			subID := *auth.SubdivisionID
			subdivisionID = &subID
		}
	}
	events, err := s.repo.ListCalendarEvents(ctx, from, to, engineerID, subdivisionID)
	if err != nil {
		return nil, err
	}
	cache := map[int64]*UserBrief{}
	for i := range events {
		u := s.resolveUserCached(ctx, events[i].EngineerID, cache)
		if u != nil {
			events[i].EngineerName = u.FullName
			if u.Color != "" {
				events[i].Color = u.Color
			}
		}
		if events[i].IsDuty {
			events[i].Color = "#94a3b8"
		} else if events[i].Color == "" {
			events[i].Color = "#3b82f6"
		}
	}
	return events, nil
}

func (s *Service) GetApplication(ctx context.Context, auth *platform.AuthContext, id int64) (*Application, error) {
	app, err := s.repo.GetApplication(ctx, id)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, platform.NewNotFound("Заявка не найдена")
	}
	if auth != nil && !hasFullContractorAccess(auth) && app.EngineerID != auth.UserID && !slices.Contains(app.ExecutorIDs, auth.UserID) {
		return nil, platform.NewForbidden("Нет доступа к чужой заявке")
	}
	if s.users != nil {
		if u, _ := s.users.GetUserBrief(ctx, app.EngineerID); u != nil {
			app.EngineerName = u.FullName
			app.EngineerColor = u.Color
		}
		for _, eid := range app.ExecutorIDs {
			if u, _ := s.users.GetUserBrief(ctx, eid); u != nil {
				app.ExecutorNames = append(app.ExecutorNames, u.FullName)
			}
		}
	}
	return app, nil
}

func (s *Service) CreateApplication(ctx context.Context, auth *platform.AuthContext, in Application) (*Application, error) {
	if err := s.validateApplicationSlot(ctx, &in, nil); err != nil {
		return nil, err
	}
	if auth != nil {
		in.CreatedBy = auth.UserID
	}
	// Как в ApplicationForTechnicalWork::beforeSave(): подразделение берётся у назначенного инженера.
	if in.SubdivisionID == nil && s.users != nil {
		if eng, err := s.users.GetUserBrief(ctx, in.EngineerID); err == nil && eng != nil && eng.SubdivisionID != nil {
			subID := *eng.SubdivisionID
			in.SubdivisionID = &subID
		}
	}
	if in.SubdivisionID == nil && auth != nil && auth.SubdivisionID != nil {
		subID := *auth.SubdivisionID
		in.SubdivisionID = &subID
	}
	if in.Status == 0 {
		in.Status = StatusActive
	}
	if strings.TrimSpace(in.SpecialNotes) != "" {
		in.NotesViewed = false
	} else {
		in.NotesViewed = true
	}
	if err := s.repo.CreateApplication(ctx, &in); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "application", in.ID, "create", in)
	timeStr := in.PlanDatetimeFrom.In(s.moscowLoc()).Format("02.01 15:04")
	s.pushNotification(ctx, in.EngineerID, "application", "Новая заявка #"+strconv.FormatInt(in.ID, 10),
		"Назначена заявка на "+timeStr, "application", in.ID)
	for _, eid := range in.ExecutorIDs {
		s.pushNotification(ctx, eid, "application", "Новая заявка #"+strconv.FormatInt(in.ID, 10),
			"Вы назначены соисполнителем на "+timeStr, "application", in.ID)
	}
	s.markContractorMissedCallsViewed(ctx, in.ContractorID, auth)
	s.maybeCreateAppLimitTask(ctx, &in)
	s.broadcastCalendarUpdate(in.ID)
	return s.GetApplication(ctx, auth, in.ID)
}

func (s *Service) markContractorMissedCallsViewed(ctx context.Context, contractorID int64, auth *platform.AuthContext) {
	c, err := s.repo.GetContractor(ctx, contractorID)
	if err != nil || c == nil {
		return
	}
	var uid *int64
	if auth != nil {
		idCopy := auth.UserID
		uid = &idCopy
	}
	for _, ph := range c.Phones {
		_ = s.repo.MarkMissCallsViewedByPhone(ctx, ph.Phone, uid)
	}
}

func (s *Service) maybeCreateAppLimitTask(ctx context.Context, app *Application) {
	now := s.now()
	since := now.AddDate(0, 0, -30)
	cnt, err := s.repo.CountRecentContractorApplications(ctx, app.ContractorID, since)
	if err != nil || cnt < 3 {
		return
	}
	hasTask, err := s.repo.HasRecentAppLimitTask(ctx, app.ContractorID, since)
	if err != nil || hasTask {
		return
	}
	var supervisorID *int64
	if app.SubdivisionID != nil {
		if sub, err := s.repo.GetSubdivision(ctx, *app.SubdivisionID); err == nil && sub != nil && sub.SupervisorUserID != nil {
			sup := *sub.SupervisorUserID
			supervisorID = &sup
		}
	}
	cid := app.ContractorID
	aid := app.AddressID
	appID := app.ID
	task := &Task{
		ContractorID:    &cid,
		AddressID:       &aid,
		ApplicationID:   &appID,
		TaskType:        TaskTypeAppLimit,
		Title:           fmt.Sprintf("Лимит заявок по контрагенту #%d (%d за 30 дней)", app.ContractorID, cnt),
		Description:     fmt.Sprintf("Создана заявка #%d. Количество активных/закрытых заявок за последние 30 дней: %d", app.ID, cnt),
		Status:          TaskStatusNew,
		Priority:        70,
		AssignedUserID:  supervisorID,
		SubdivisionID:   app.SubdivisionID,
		PlanDatetimeTo:  now.Add(24 * time.Hour),
		DecisionTimeSec: 3600 * 8,
		IsUrgent:        true,
	}
	if err := s.repo.CreateTask(ctx, task); err == nil && supervisorID != nil {
		s.notifyTaskAssigned(ctx, task)
	}
}

func (s *Service) UpdateApplication(ctx context.Context, auth *platform.AuthContext, id int64, in Application) (*Application, error) {
	existing, err := s.repo.GetApplication(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, platform.NewNotFound("Заявка не найдена")
	}
	if err := s.validateApplicationSlot(ctx, &in, &id); err != nil {
		return nil, err
	}
	existing.ContractorID = in.ContractorID
	existing.AddressID = in.AddressID
	existing.EngineerID = in.EngineerID
	existing.ExecutorIDs = in.ExecutorIDs
	if in.TrafficSourceID != nil {
		existing.TrafficSourceID = in.TrafficSourceID
	}
	existing.PlannedSum = in.PlannedSum
	existing.WorkWindowFrom = in.WorkWindowFrom
	existing.WorkWindowTo = in.WorkWindowTo
	if in.SubdivisionID != nil {
		existing.SubdivisionID = in.SubdivisionID
	} else if existing.SubdivisionID == nil && s.users != nil {
		if eng, err := s.users.GetUserBrief(ctx, in.EngineerID); err == nil && eng != nil && eng.SubdivisionID != nil {
			subID := *eng.SubdivisionID
			existing.SubdivisionID = &subID
		}
	}
	existing.PlanDatetimeFrom = in.PlanDatetimeFrom
	existing.PlanDatetimeTo = in.PlanDatetimeTo
	if in.Status != 0 {
		existing.Status = in.Status
	}
	existing.IsComplete = in.IsComplete
	existing.IsDuty = in.IsDuty
	existing.WorkTypes = in.WorkTypes
	existing.PriceNote = in.PriceNote
	existing.Comment = in.Comment
	if strings.TrimSpace(in.SpecialNotes) != "" && in.SpecialNotes != existing.SpecialNotes {
		existing.NotesViewed = false
	} else {
		existing.NotesViewed = in.NotesViewed
	}
	existing.SpecialNotes = in.SpecialNotes
	existing.EquipmentIDs = in.EquipmentIDs
	existing.SerialNumbers = in.SerialNumbers

	if err := s.repo.UpdateApplication(ctx, existing); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "application", id, "update", existing)
	s.broadcastCalendarUpdate(id)
	return s.GetApplication(ctx, auth, id)
}

func (s *Service) CompleteApplication(ctx context.Context, auth *platform.AuthContext, id int64, isComplete bool, comment string) (*Application, error) {
	existing, err := s.repo.GetApplication(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, platform.NewNotFound("Заявка не найдена")
	}
	if auth != nil && !hasFullContractorAccess(auth) && existing.EngineerID != auth.UserID && !slices.Contains(existing.ExecutorIDs, auth.UserID) {
		return nil, platform.NewForbidden("Нет прав на изменение чужой заявки")
	}
	existing.IsComplete = isComplete
	if isComplete {
		existing.Status = StatusProcessed
	} else if existing.Status == StatusProcessed {
		existing.Status = StatusActive
	}
	if trimmed := strings.TrimSpace(comment); trimmed != "" {
		existing.Comment = trimmed
	}
	if err := s.repo.UpdateApplication(ctx, existing); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "application", id, "complete", map[string]any{"is_complete": isComplete, "comment": existing.Comment})
	s.broadcastCalendarUpdate(id)
	return s.GetApplication(ctx, auth, id)
}

func (s *Service) validateApplicationSlot(ctx context.Context, app *Application, excludeID *int64) error {
	if app.ContractorID <= 0 || app.AddressID <= 0 || app.EngineerID <= 0 {
		return platform.NewValidation("Укажите контрагента, адрес и инженера", nil)
	}
	if app.PlannedSum < 0 {
		return platform.NewValidation("Плановая сумма не может быть отрицательной", map[string]string{"planned_sum": "Неверное значение"})
	}
	if app.WorkWindowFrom != nil && app.WorkWindowTo != nil && app.WorkWindowTo.Before(*app.WorkWindowFrom) {
		return platform.NewValidation("Конец окна выполнения раньше начала", map[string]string{"work_window_to": "Неверный интервал"})
	}
	if !app.PlanDatetimeTo.After(app.PlanDatetimeFrom) {
		return platform.NewValidation("Время окончания должно быть позже начала", map[string]string{"plan_datetime_to": "Неверный интервал"})
	}
	seenExec := map[int64]bool{app.EngineerID: true}
	cleanExec := make([]int64, 0, len(app.ExecutorIDs))
	for _, eid := range app.ExecutorIDs {
		if eid > 0 && !seenExec[eid] {
			seenExec[eid] = true
			cleanExec = append(cleanExec, eid)
		}
	}
	app.ExecutorIDs = cleanExec

	// Дежурные и отменённые/проваленные слоты не блокируют сетку инженеров.
	if app.IsDuty || app.Status == StatusFailed || app.Status == StatusCancelled {
		return nil
	}
	allEngineers := append([]int64{app.EngineerID}, app.ExecutorIDs...)
	for _, engID := range allEngineers {
		overlap, err := s.repo.HasEngineerTimeOverlap(ctx, engID, app.PlanDatetimeFrom, app.PlanDatetimeTo, excludeID)
		if err != nil {
			return err
		}
		if overlap {
			return platform.NewRuleError("У выбранного инженера или соисполнителя уже есть заявка в этот интервал времени")
		}
	}
	return nil
}

func (s *Service) RateApplication(ctx context.Context, auth *platform.AuthContext, id int64, rating int, comment string) error {
	if rating < 1 || rating > 5 {
		return platform.NewValidation("Оценка должна быть от 1 до 5", map[string]string{"rating": "От 1 до 5"})
	}
	if err := s.repo.UpdateApplicationRating(ctx, id, rating, strings.TrimSpace(comment)); err != nil {
		return err
	}
	s.logChange(ctx, auth, "application", id, "rate", map[string]any{"rating": rating, "comment": comment})
	s.broadcastCalendarUpdate(id)
	return nil
}

func (s *Service) MarkApplicationNotesViewed(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.MarkApplicationNotesViewed(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "application", id, "notes_viewed", nil)
	s.broadcastCalendarUpdate(id)
	return nil
}

func (s *Service) DeleteApplication(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.SoftDeleteApplication(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "application", id, "delete", nil)
	s.broadcastCalendarUpdate(id)
	return nil
}

func (s *Service) broadcastCalendarUpdate(appID int64) {
	if s.hub != nil {
		s.hub.BroadcastAll(WSEvent{
			Type:    "calendar:updated",
			Payload: map[string]any{"application_id": appID},
		})
	}
}

// --- Certificates ---

func (s *Service) ListCertificates(ctx context.Context, auth *platform.AuthContext, f CertificateFilter) ([]Certificate, error) {
	if auth != nil && !hasFullContractorAccess(auth) {
		uid := auth.UserID
		f.EngineerID = &uid
	}
	return s.repo.ListCertificates(ctx, f)
}

func hasCertificateExecutor(executors []CertificateExecutor, uid int64) bool {
	for _, ex := range executors {
		if ex.UserID == uid {
			return true
		}
	}
	return false
}

func (s *Service) GetCertificate(ctx context.Context, auth *platform.AuthContext, id int64) (*Certificate, error) {
	cert, err := s.repo.GetCertificate(ctx, id)
	if err != nil {
		return nil, err
	}
	if cert == nil {
		return nil, platform.NewNotFound("Акт выполненных работ не найден")
	}
	if auth != nil && !hasFullContractorAccess(auth) && cert.EngineerID != auth.UserID && !hasCertificateExecutor(cert.Executors, auth.UserID) {
		return nil, platform.NewForbidden("Нет доступа к акту другого инженера")
	}
	s.enrichCertificateUsers(ctx, cert)
	return cert, nil
}

func (s *Service) enrichCertificateUsers(ctx context.Context, cert *Certificate) {
	if cert == nil || s.users == nil {
		return
	}
	cache := map[int64]*UserBrief{}
	if u := s.resolveUserCached(ctx, cert.EngineerID, cache); u != nil {
		cert.EngineerName = u.FullName
	}
	for i := range cert.Executors {
		if u := s.resolveUserCached(ctx, cert.Executors[i].UserID, cache); u != nil {
			cert.Executors[i].FullName = u.FullName
		}
	}
}

func (s *Service) normalizeCertificateExecutors(in []CertificateExecutor, totalAmount float64) ([]CertificateExecutor, error) {
	if len(in) == 0 {
		return nil, nil
	}
	seen := make(map[int64]bool, len(in))
	out := make([]CertificateExecutor, 0, len(in))
	var sum float64
	for _, ex := range in {
		if ex.UserID <= 0 || seen[ex.UserID] {
			continue
		}
		if ex.Sum < 0 {
			return nil, platform.NewValidation("Сумма соисполнителя не может быть отрицательной", map[string]string{"executors": "Отрицательная сумма"})
		}
		seen[ex.UserID] = true
		sum += ex.Sum
		out = append(out, CertificateExecutor{UserID: ex.UserID, Sum: ex.Sum})
	}
	if len(out) > 0 && math.Abs(sum-totalAmount) > 0.01 {
		return nil, platform.NewValidation("Сумма распределений по соисполнителям должна равняться итоговой сумме акта", map[string]string{
			"executors": fmt.Sprintf("Сумма долей (%.2f) не равна сумме акта (%.2f)", sum, totalAmount),
		})
	}
	return out, nil
}

func sameMoscowDate(a, b time.Time, loc *time.Location) bool {
	ay, am, ad := a.In(loc).Date()
	by, bm, bd := b.In(loc).Date()
	return ay == by && am == bm && ad == bd
}

func moscowMidnight(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

func (s *Service) SaveCertificate(ctx context.Context, auth *platform.AuthContext, id int64, in Certificate) (*Certificate, error) {
	if in.ContractorID <= 0 || in.AddressID <= 0 || in.EngineerID <= 0 {
		return nil, platform.NewValidation("Укажите контрагента, адрес и инженера", nil)
	}
	if in.TotalAmount < 0 || in.PaidAmount < 0 || in.CostOfParts < 0 {
		return nil, platform.NewValidation("Суммы акта не могут быть отрицательными", nil)
	}
	if in.WorkDate.IsZero() {
		in.WorkDate = s.now()
	}
	if in.Status == 0 {
		in.Status = StatusProcessed
	}
	executors, err := s.normalizeCertificateExecutors(in.Executors, in.TotalAmount)
	if err != nil {
		return nil, err
	}
	in.Executors = executors
	in.ReceiptNumber = strings.TrimSpace(in.ReceiptNumber)
	if in.WithoutMaintenanceContract {
		in.MaintenanceContractID = nil
	}
	if auth != nil {
		in.CreatedBy = auth.UserID
	}

	loc := s.moscowLoc()
	today := moscowMidnight(s.now(), loc)
	restrictedEngineer := auth != nil && !hasFullContractorAccess(auth) && !auth.HasPermission("crm.certificate.update_any")

	if id == 0 {
		if restrictedEngineer {
			if in.ApplicationID == nil || *in.ApplicationID <= 0 {
				return nil, platform.NewForbidden("Инженер может создать акт только из назначенной заявки")
			}
			app, err := s.repo.GetApplication(ctx, *in.ApplicationID)
			if err != nil {
				return nil, err
			}
			if app == nil {
				return nil, platform.NewNotFound("Связанная заявка не найдена")
			}
			if app.EngineerID != auth.UserID && !slices.Contains(app.ExecutorIDs, auth.UserID) {
				return nil, platform.NewForbidden("Нет прав оформлять акт по чужой заявке")
			}
			if !sameMoscowDate(app.PlanDatetimeFrom, s.now(), loc) || !sameMoscowDate(in.WorkDate, s.now(), loc) {
				return nil, platform.NewForbidden("Инженер может оформить акт только в дату выполнения заявки")
			}
		}
		if !in.WithoutMaintenanceContract && in.MaintenanceContractID == nil {
			if matchedID, err := s.repo.FindMatchingContractForCertificate(ctx, in.ContractorID, in.AddressID, in.WorkDate); err == nil && matchedID != nil {
				in.MaintenanceContractID = matchedID
			}
		}
		in.NotesViewed = strings.TrimSpace(in.SpecialNotes) == "" && strings.TrimSpace(in.OtherNote) == ""
		if err := s.repo.CreateCertificate(ctx, &in); err != nil {
			return nil, err
		}
		s.logChange(ctx, auth, "certificate", in.ID, "create", in)
	} else {
		existing, err := s.repo.GetCertificate(ctx, id)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, platform.NewNotFound("Акт выполненных работ не найден")
		}
		if restrictedEngineer {
			if existing.EngineerID != auth.UserID && !hasCertificateExecutor(existing.Executors, auth.UserID) && existing.CreatedBy != auth.UserID {
				return nil, platform.NewForbidden("Нет прав на редактирование чужого акта")
			}
			workDay := moscowMidnight(existing.WorkDate, loc)
			if today.Before(workDay) || today.After(workDay.AddDate(0, 0, 3)) {
				return nil, platform.NewForbidden("Редактирование акта инженером доступно только в течение 3 дней от даты работ")
			}
		}
		if (strings.TrimSpace(in.SpecialNotes) != "" && in.SpecialNotes != existing.SpecialNotes) ||
			(strings.TrimSpace(in.OtherNote) != "" && in.OtherNote != existing.OtherNote) {
			in.NotesViewed = false
		}
		in.ID = id
		if err := s.repo.UpdateCertificate(ctx, &in); err != nil {
			return nil, err
		}
		s.logChange(ctx, auth, "certificate", in.ID, "update", in)
	}

	// Сохранение акта автоматически завершает связанную заявку и поднимает флаг непросмотренных замечаний.
	if in.ApplicationID != nil && *in.ApplicationID > 0 {
		if app, err := s.repo.GetApplication(ctx, *in.ApplicationID); err == nil && app != nil {
			app.IsComplete = true
			app.Status = StatusProcessed
			if strings.TrimSpace(in.SpecialNotes) != "" || strings.TrimSpace(in.OtherNote) != "" {
				app.NotesViewed = false
				if strings.TrimSpace(in.SpecialNotes) != "" {
					app.SpecialNotes = in.SpecialNotes
				}
			}
			_ = s.repo.UpdateApplication(ctx, app)
			s.broadcastCalendarUpdate(app.ID)
		}
	}

	return s.GetCertificate(ctx, auth, in.ID)
}

func (s *Service) DeleteCertificate(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.SoftDeleteCertificate(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "certificate", id, "delete", nil)
	return nil
}

func (s *Service) ExportCertificatesCSV(ctx context.Context, auth *platform.AuthContext, f CertificateFilter) ([]byte, error) {
	items, err := s.ListCertificates(ctx, auth, f)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF") // BOM для корректного открытия кириллицы в Excel.
	w := csv.NewWriter(&buf)
	w.Comma = ';'
	_ = w.Write([]string{
		"ID", "Дата работ", "Контрагент", "Адрес", "Инженер ID",
		"Сумма", "Оплачено", "Способ оплаты", "Виды работ", "Особые отметки",
	})
	for _, c := range items {
		_ = w.Write([]string{
			strconv.FormatInt(c.ID, 10),
			c.WorkDate.Format("2006-01-02"),
			c.ContractorName,
			c.AddressText,
			strconv.FormatInt(c.EngineerID, 10),
			fmt.Sprintf("%.2f", c.TotalAmount),
			fmt.Sprintf("%.2f", c.PaidAmount),
			c.PaymentMethod,
			strings.Join(c.WorkTypes, ", "),
			strings.TrimSpace(c.SpecialNotes + " " + c.OtherNote),
		})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

// --- Subdivisions, SIP, Reports, Audit, Notifications, Settings ---

func (s *Service) ListSubdivisions(ctx context.Context) ([]Subdivision, error) {
	return s.repo.ListSubdivisions(ctx)
}

func normalizeSubdivisionPhones(in []SubdivisionPhone) ([]SubdivisionPhone, error) {
	seen := make(map[string]bool, len(in))
	out := make([]SubdivisionPhone, 0, len(in))
	for _, p := range in {
		if strings.TrimSpace(p.Phone) == "" {
			continue
		}
		p10, err := platform.ParsePhone10(p.Phone)
		if err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s:%v:%v", p10, p.IsDuty, p.IsMain)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, SubdivisionPhone{
			ID:     strings.TrimSpace(p.ID),
			Name:   strings.TrimSpace(p.Name),
			Phone:  p10,
			IsMain: p.IsMain,
			IsDuty: p.IsDuty,
		})
	}
	return out, nil
}

type SubdivisionInput struct {
	Name             string                     `json:"name"`
	Address          string                     `json:"address"`
	Phones           []SubdivisionPhone         `json:"phones"`
	SupervisorUserID *int64                     `json:"supervisor_user_id"`
	PaymentOptions   []SubdivisionPaymentOption `json:"payment_options"`
}

func normalizeSubdivisionPaymentOptions(in []SubdivisionPaymentOption) []SubdivisionPaymentOption {
	out := make([]SubdivisionPaymentOption, 0, len(in))
	for _, po := range in {
		if po.PaymentOptionID <= 0 {
			continue
		}
		key := po.Key
		if key == "" {
			for _, opt := range PaymentOptions {
				if opt.ID == po.PaymentOptionID {
					key = opt.Key
					break
				}
			}
		}
		fileURL := po.FileURL
		if po.FileID != nil && *po.FileID > 0 {
			fileURL = fmt.Sprintf("/api/v1/files/%d", *po.FileID)
		}
		out = append(out, SubdivisionPaymentOption{
			PaymentOptionID: po.PaymentOptionID,
			Key:             key,
			FileID:          po.FileID,
			FileURL:         fileURL,
			IsMain:          po.IsMain,
		})
	}
	return out
}

func (s *Service) SaveSubdivision(ctx context.Context, auth *platform.AuthContext, id int64, name, address string, phones []SubdivisionPhone) (*Subdivision, error) {
	return s.SaveSubdivisionFull(ctx, auth, id, SubdivisionInput{
		Name:    name,
		Address: address,
		Phones:  phones,
	})
}

func (s *Service) SaveSubdivisionFull(ctx context.Context, auth *platform.AuthContext, id int64, in SubdivisionInput) (*Subdivision, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, platform.NewValidation("Название подразделения обязательно", nil)
	}
	cleanPhones, err := normalizeSubdivisionPhones(in.Phones)
	if err != nil {
		return nil, err
	}
	sub := &Subdivision{
		ID:               id,
		Name:             name,
		Address:          strings.TrimSpace(in.Address),
		Phones:           cleanPhones,
		SupervisorUserID: in.SupervisorUserID,
		PaymentOptions:   normalizeSubdivisionPaymentOptions(in.PaymentOptions),
	}
	if id == 0 {
		if err := s.repo.CreateSubdivision(ctx, sub); err != nil {
			return nil, err
		}
		s.logChange(ctx, auth, "subdivision", sub.ID, "create", sub)
		return sub, nil
	}
	if err := s.repo.UpdateSubdivision(ctx, sub); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "subdivision", sub.ID, "update", sub)
	return sub, nil
}

func (s *Service) DeleteSubdivision(ctx context.Context, auth *platform.AuthContext, id int64) error {
	if err := s.repo.SoftDeleteSubdivision(ctx, id); err != nil {
		return err
	}
	s.logChange(ctx, auth, "subdivision", id, "delete", nil)
	return nil
}

func (s *Service) ListSipAccounts(ctx context.Context) ([]SipAccount, error) {
	items, err := s.repo.ListSipAccounts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Password = ""
	}
	return items, nil
}

func (s *Service) SaveSipAccount(ctx context.Context, auth *platform.AuthContext, in SipAccount) (*SipAccount, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.AbonentID = strings.TrimSpace(in.AbonentID)
	in.Login = strings.TrimSpace(in.Login)
	in.Server = strings.TrimSpace(in.Server)
	if in.Name == "" && in.AbonentID == "" && (in.Login == "" || in.Server == "") {
		return nil, platform.NewValidation("Укажите название / Abonent ID или SIP логин и сервер", nil)
	}
	if in.Protocol == "" {
		in.Protocol = "wss"
	}
	if in.Port == 0 {
		in.Port = 7443
	}
	if err := s.repo.SaveSipAccount(ctx, &in); err != nil {
		return nil, err
	}
	s.logChange(ctx, auth, "sip_account", in.ID, "save", map[string]any{"login": in.Login, "user_id": in.UserID, "abonent_id": in.AbonentID})
	in.Password = ""
	return &in, nil
}

func (s *Service) GetSoftPhoneConfig(ctx context.Context, auth *platform.AuthContext) (*SoftPhoneConfig, error) {
	if auth == nil {
		return nil, platform.NewUnauthorized("Требуется авторизация")
	}
	sip, err := s.repo.GetSipAccountByUser(ctx, auth.UserID)
	if err != nil {
		return nil, err
	}
	if sip == nil {
		return &SoftPhoneConfig{Enabled: false}, nil
	}
	proto := sip.Protocol
	if proto == "" {
		proto = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s:%d/ws", proto, sip.Server, sip.Port)
	return &SoftPhoneConfig{
		Enabled:   true,
		Name:      sip.Name,
		AbonentID: sip.AbonentID,
		Domain:    sip.Domain,
		Login:     sip.Login,
		Password:  sip.Password,
		Protocol:  proto,
		Server:    sip.Server,
		Port:      sip.Port,
		WSURL:     wsURL,
	}, nil
}

func (s *Service) AmountActsReport(ctx context.Context, auth *platform.AuthContext, from, to time.Time, engineerIDs, subdivisionIDs []int64) ([]AmountActsReportRow, error) {
	if auth != nil && !hasWithoutSubdivisionAccess(auth) && auth.SubdivisionID != nil {
		subdivisionIDs = []int64{*auth.SubdivisionID}
	}
	var singleEngID *int64
	if len(engineerIDs) == 1 {
		id := engineerIDs[0]
		singleEngID = &id
	}
	rows, err := s.repo.AmountActsReport(ctx, from, to, singleEngID)
	if err != nil {
		return nil, err
	}

	byEngineer := make(map[int64]*AmountActsReportRow, len(rows))
	for i := range rows {
		r := rows[i]
		byEngineer[r.EngineerID] = &r
	}

	// Учитываем суммы договоров ТО, подписанных в выбранный период (отчёт AmountActsReport).
	if contracts, err := s.repo.ListContracts(ctx, ContractFilter{Limit: 1000}); err == nil {
		for _, c := range contracts {
			if c.Status == StatusFailed || c.Status == StatusCancelled {
				continue
			}
			if c.SignedAt.Before(from) || c.SignedAt.After(to) {
				continue
			}
			ownerID := c.CreatedBy
			if c.ApplicationID != nil && *c.ApplicationID > 0 {
				if app, err := s.repo.GetApplication(ctx, *c.ApplicationID); err == nil && app != nil {
					ownerID = app.EngineerID
				}
			}
			if ownerID <= 0 {
				continue
			}
			row, ok := byEngineer[ownerID]
			if !ok {
				row = &AmountActsReportRow{EngineerID: ownerID}
				byEngineer[ownerID] = row
			}
			row.ContractsAmount += c.Price
		}
	}

	subNames := map[int64]string{}
	if subs, err := s.repo.ListSubdivisions(ctx); err == nil {
		for _, sub := range subs {
			subNames[sub.ID] = sub.Name
		}
	}

	engSet := make(map[int64]bool, len(engineerIDs))
	for _, id := range engineerIDs {
		engSet[id] = true
	}
	subSet := make(map[int64]bool, len(subdivisionIDs))
	for _, id := range subdivisionIDs {
		subSet[id] = true
	}

	cache := map[int64]*UserBrief{}
	out := make([]AmountActsReportRow, 0, len(byEngineer))
	for _, row := range byEngineer {
		if len(engSet) > 0 && !engSet[row.EngineerID] {
			continue
		}
		if u := s.resolveUserCached(ctx, row.EngineerID, cache); u != nil {
			if u.FullName != "" {
				row.EngineerName = u.FullName
			}
			row.SubdivisionID = u.SubdivisionID
			if u.SubdivisionID != nil {
				row.SubdivisionName = subNames[*u.SubdivisionID]
			}
		}
		if row.EngineerName == "" {
			row.EngineerName = "Инженер #" + strconv.FormatInt(row.EngineerID, 10)
		}
		if len(subSet) > 0 && (row.SubdivisionID == nil || !subSet[*row.SubdivisionID]) {
			continue
		}
		out = append(out, *row)
	}
	return out, nil
}

func (s *Service) ExpiredTasksReport(ctx context.Context, userID *int64) ([]ExpiredTaskReportRow, error) {
	rows, err := s.repo.ExpiredTasksReport(ctx, s.now(), userID)
	if err != nil {
		return nil, err
	}
	cache := map[int64]*UserBrief{}
	for i := range rows {
		if rows[i].AssignedUserID != nil {
			if u := s.resolveUserCached(ctx, *rows[i].AssignedUserID, cache); u != nil {
				rows[i].AssignedName = u.FullName
			}
		}
	}
	return rows, nil
}

func (s *Service) ListChangeLogs(ctx context.Context, entityType string, entityID int64, limit int) ([]ChangeLog, error) {
	logs, err := s.repo.ListChangeLogs(ctx, entityType, entityID, limit)
	if err != nil {
		return nil, err
	}
	cache := map[int64]*UserBrief{}
	for i := range logs {
		if u := s.resolveUserCached(ctx, logs[i].UserID, cache); u != nil {
			logs[i].UserName = u.FullName
		}
	}
	return logs, nil
}

func (s *Service) ListNotifications(ctx context.Context, auth *platform.AuthContext, unreadOnly bool, limit int) ([]Notification, error) {
	return s.repo.ListUserNotifications(ctx, auth.UserID, unreadOnly, limit)
}

func (s *Service) MarkNotificationRead(ctx context.Context, auth *platform.AuthContext, id int64) error {
	return s.repo.MarkNotificationRead(ctx, auth.UserID, id)
}

func (s *Service) MarkAllNotificationsRead(ctx context.Context, auth *platform.AuthContext) error {
	return s.repo.MarkAllNotificationsRead(ctx, auth.UserID)
}

func (s *Service) CleanupOldNotifications(ctx context.Context) (int64, error) {
	return s.repo.DeleteOldReadNotifications(ctx, s.now().AddDate(0, 0, -30))
}

func (s *Service) GetUserSetting(ctx context.Context, auth *platform.AuthContext, key string) (json.RawMessage, error) {
	return s.repo.GetUserSetting(ctx, auth.UserID, key)
}

func (s *Service) SaveUserSetting(ctx context.Context, auth *platform.AuthContext, key string, val json.RawMessage) error {
	if strings.TrimSpace(key) == "" {
		return platform.NewValidation("Ключ настройки пуст", nil)
	}
	return s.repo.SaveUserSetting(ctx, auth.UserID, key, val)
}

func (s *Service) resolveUserCached(ctx context.Context, userID int64, cache map[int64]*UserBrief) *UserBrief {
	if userID <= 0 || s.users == nil {
		return nil
	}
	if u, ok := cache[userID]; ok {
		return u
	}
	u, err := s.users.GetUserBrief(ctx, userID)
	if err != nil {
		return nil
	}
	cache[userID] = u
	return u
}

func (s *Service) enrichApplicationUsers(ctx context.Context, apps []Application) {
	cache := map[int64]*UserBrief{}
	for i := range apps {
		if u := s.resolveUserCached(ctx, apps[i].EngineerID, cache); u != nil {
			apps[i].EngineerName = u.FullName
			apps[i].EngineerColor = u.Color
		}
		for _, eid := range apps[i].ExecutorIDs {
			if u := s.resolveUserCached(ctx, eid, cache); u != nil {
				apps[i].ExecutorNames = append(apps[i].ExecutorNames, u.FullName)
			}
		}
	}
}

// ResolveEntity формирует контекст сущности CRM для модулей документации, поиска и чата с ИИ.
func (s *Service) ResolveEntity(ctx context.Context, auth *platform.AuthContext, entityType string, entityID int64) (*platform.EntitySummary, error) {
	switch entityType {
	case "contractor":
		c, err := s.GetContractor(ctx, auth, entityID)
		if err != nil {
			return nil, err
		}
		var b strings.Builder
		fmt.Fprintf(&b, "# Контрагент #%d: %s\n\n", c.ID, c.Name)
		if c.Note != "" {
			fmt.Fprintf(&b, "Примечание: %s\n\n", c.Note)
		}
		if len(c.Phones) > 0 {
			b.WriteString("## Телефоны\n")
			for _, p := range c.Phones {
				fmt.Fprintf(&b, "- %s %s\n", platform.FormatPhoneRU(p.Phone), p.Note)
			}
			b.WriteString("\n")
		}
		if len(c.Addresses) > 0 {
			b.WriteString("## Адреса и оборудование\n")
			for _, a := range c.Addresses {
				fmt.Fprintf(&b, "- **%s**", a.Address)
				for _, eq := range a.Equipments {
					fmt.Fprintf(&b, " | %s (S/N: %s)", eq.EquipmentName, eq.SerialNumber)
				}
				b.WriteString("\n")
			}
		}
		return &platform.EntitySummary{
			EntityType: "contractor",
			EntityID:   c.ID,
			Title:      c.Name,
			Markdown:   b.String(),
			Path:       fmt.Sprintf("/crm/contractors/%d", c.ID),
		}, nil

	case "application":
		app, err := s.GetApplication(ctx, auth, entityID)
		if err != nil {
			return nil, err
		}
		md := fmt.Sprintf(
			"# Заявка #%d (%s)\n- Контрагент: %s (#%d)\n- Адрес: %s\n- Инженер: %s (#%d)\n- План: %s — %s\n- Виды работ: %s\n- Комментарий: %s\n- Особые отметки: %s\n",
			app.ID, app.Status.Label(), app.ContractorName, app.ContractorID, app.AddressText,
			app.EngineerName, app.EngineerID,
			app.PlanDatetimeFrom.Format("02.01.2006 15:04"), app.PlanDatetimeTo.Format("15:04"),
			strings.Join(app.WorkTypes, ", "), app.Comment, app.SpecialNotes,
		)
		return &platform.EntitySummary{
			EntityType: "application",
			EntityID:   app.ID,
			Title:      fmt.Sprintf("Заявка #%d — %s", app.ID, app.ContractorName),
			Markdown:   md,
			Path:       fmt.Sprintf("/crm/applications/%d", app.ID),
		}, nil

	case "certificate":
		cert, err := s.GetCertificate(ctx, auth, entityID)
		if err != nil {
			return nil, err
		}
		md := fmt.Sprintf(
			"# Акт выполненных работ #%d\n- Дата: %s\n- Контрагент: %s\n- Адрес: %s\n- Сумма / Оплачено: %.2f / %.2f (%s)\n- Виды работ: %s\n- Рекомендации: %s\n- Особые отметки: %s\n",
			cert.ID, cert.WorkDate.Format("02.01.2006"), cert.ContractorName, cert.AddressText,
			cert.TotalAmount, cert.PaidAmount, cert.PaymentMethod,
			strings.Join(cert.WorkTypes, ", "), cert.Recommendation, cert.SpecialNotes,
		)
		return &platform.EntitySummary{
			EntityType: "certificate",
			EntityID:   cert.ID,
			Title:      fmt.Sprintf("Акт #%d — %s", cert.ID, cert.ContractorName),
			Markdown:   md,
			Path:       fmt.Sprintf("/crm/certificates/%d", cert.ID),
		}, nil

	case "contract":
		c, err := s.GetContract(ctx, entityID)
		if err != nil {
			return nil, err
		}
		md := fmt.Sprintf(
			"# Договор ТО №%s\n- Контрагент: %s\n- Адрес: %s\n- Период: %s — %s\n- Сумма: %.2f\n- Примечание: %s\n",
			c.Number, c.ContractorName, c.AddressText,
			c.ValidFrom.Format("02.01.2006"), c.ValidTo.Format("02.01.2006"), c.Price, c.Note,
		)
		return &platform.EntitySummary{
			EntityType: "contract",
			EntityID:   c.ID,
			Title:      fmt.Sprintf("Договор №%s", c.Number),
			Markdown:   md,
			Path:       fmt.Sprintf("/crm/contracts/%d", c.ID),
		}, nil

	case "task":
		t, err := s.repo.GetTask(ctx, entityID)
		if err != nil {
			return nil, err
		}
		if t == nil {
			return nil, platform.NewNotFound("Задача не найдена")
		}
		md := fmt.Sprintf(
			"# Задача #%d: %s\n- Тип: %s\n- Статус: %s\n- Контрагент: %s\n- Срок: %s\n- Описание: %s\n- Примечание: %s\n",
			t.ID, t.Title, t.TaskType.Label(), t.Status.Label(), t.ContractorName,
			t.PlanDatetimeTo.Format("02.01.2006 15:04"), t.Description, t.Note,
		)
		return &platform.EntitySummary{
			EntityType: "task",
			EntityID:   t.ID,
			Title:      t.Title,
			Markdown:   md,
			Path:       fmt.Sprintf("/crm/tasks/%d", t.ID),
		}, nil

	default:
		return nil, platform.NewValidation("Неизвестный тип сущности CRM: "+entityType, nil)
	}
}
