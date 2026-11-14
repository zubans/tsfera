package crm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type memRepo struct {
	mu                sync.Mutex
	nextID            int64
	subdivisions      map[int64]*Subdivision
	sips              map[int64]*SipAccount
	contractors       map[int64]*Contractor
	legalEntities     map[int64]*LegalEntity
	addresses         map[int64]*ContractorAddress
	equipments        map[int64]*Equipment
	addrEqs           map[int64]*AddressEquipment
	refItems          map[int64]*RefItem
	trafficSources    map[int64]*TrafficSource
	contracts         map[int64]*MaintenanceContract
	apps              map[int64]*Application
	certs             map[int64]*Certificate
	tasks             map[int64]*Task
	poolSettings      []TaskPoolSetting
	queueUsers        map[int64]*QueueUser
	calls             map[string]*CallRecord
	missCalls         map[int64]*MissCallPoolItem
	analyticQuestions map[int64]*AnalyticQuestion
	analytics         map[int64]*Analytic
	banners           map[int64]*Banner
	appSettings       map[string]*AppSetting
	changeLogs        []ChangeLog
	notifs            []Notification
	settings          map[string]json.RawMessage
}

func newMemRepo() *memRepo {
	return &memRepo{
		subdivisions:      map[int64]*Subdivision{},
		sips:              map[int64]*SipAccount{},
		contractors:       map[int64]*Contractor{},
		legalEntities:     map[int64]*LegalEntity{},
		addresses:         map[int64]*ContractorAddress{},
		equipments:        map[int64]*Equipment{},
		addrEqs:           map[int64]*AddressEquipment{},
		refItems:          map[int64]*RefItem{},
		trafficSources:    map[int64]*TrafficSource{},
		contracts:         map[int64]*MaintenanceContract{},
		apps:              map[int64]*Application{},
		certs:             map[int64]*Certificate{},
		tasks:             map[int64]*Task{},
		queueUsers:        map[int64]*QueueUser{},
		calls:             map[string]*CallRecord{},
		missCalls:         map[int64]*MissCallPoolItem{},
		analyticQuestions: map[int64]*AnalyticQuestion{},
		analytics:         map[int64]*Analytic{},
		banners:           map[int64]*Banner{},
		appSettings:       map[string]*AppSetting{},
		settings:          map[string]json.RawMessage{},
	}
}

func (m *memRepo) id() int64 {
	m.nextID++
	return m.nextID
}

func (m *memRepo) ListSubdivisions(context.Context) ([]Subdivision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Subdivision
	for _, s := range m.subdivisions {
		if !s.Deleted {
			out = append(out, *s)
		}
	}
	return out, nil
}
func (m *memRepo) CreateSubdivision(_ context.Context, s *Subdivision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.ID = m.id()
	cp := *s
	m.subdivisions[s.ID] = &cp
	return nil
}
func (m *memRepo) UpdateSubdivision(_ context.Context, s *Subdivision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.subdivisions[s.ID] = &cp
	return nil
}
func (m *memRepo) ListSipAccounts(context.Context) ([]SipAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []SipAccount
	for _, s := range m.sips {
		out = append(out, *s)
	}
	return out, nil
}
func (m *memRepo) GetSipAccountByUser(_ context.Context, userID int64) (*SipAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sips {
		if s.UserID != nil && *s.UserID == userID && s.Active {
			cp := *s
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) GetSipAccountByLogin(_ context.Context, login string) (*SipAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sips {
		if s.Login == login && s.Active {
			cp := *s
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) GetSipAccountByAbonentID(_ context.Context, abonentID string) (*SipAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sips {
		if s.AbonentID != "" && s.AbonentID == abonentID && s.Active {
			cp := *s
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) SaveSipAccount(_ context.Context, sip *SipAccount) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sip.ID == 0 {
		sip.ID = m.id()
	}
	cp := *sip
	m.sips[sip.ID] = &cp
	return nil
}

func appHasEngineer(a *Application, engID int64) bool {
	if a.EngineerID == engID {
		return true
	}
	return slices.Contains(a.ExecutorIDs, engID)
}

func (m *memRepo) SearchContractors(_ context.Context, f ContractorFilter) ([]Contractor, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Contractor
	for _, c := range m.contractors {
		if c.Deleted {
			continue
		}
		if f.OnlyLegal && !c.IsLegalEntity {
			continue
		}
		if f.OnlyIndividuals && c.IsLegalEntity {
			continue
		}
		if f.SubdivisionID != nil && (c.SubdivisionID == nil || *c.SubdivisionID != *f.SubdivisionID) {
			continue
		}
		if f.EngineerID != nil {
			visible := false
			from := f.Now.Add(-72 * time.Hour)
			to := f.Now.Add(72 * time.Hour)
			for _, a := range m.apps {
				if !a.Deleted && appHasEngineer(a, *f.EngineerID) && a.ContractorID == c.ID &&
					(a.Status == StatusActive || a.Status == StatusProcessed) &&
					!a.PlanDatetimeFrom.Before(from) && !a.PlanDatetimeFrom.After(to) {
					visible = true
					break
				}
			}
			if !visible {
				continue
			}
		}
		le := m.legalEntities[c.ID]
		if f.Query != "" && !strings.Contains(strings.ToLower(c.Name), strings.ToLower(f.Query)) {
			matched := false
			for _, p := range c.Phones {
				if strings.Contains(p.Phone, f.Query) {
					matched = true
				}
			}
			if le != nil && (strings.Contains(strings.ToLower(le.INN), strings.ToLower(f.Query)) ||
				strings.Contains(strings.ToLower(le.Name), strings.ToLower(f.Query))) {
				matched = true
			}
			if !matched {
				continue
			}
		}
		cp := *c
		if le != nil {
			leCp := *le
			cp.LegalEntity = &leCp
		}
		out = append(out, cp)
	}
	return out, len(out), nil
}

func (m *memRepo) GetContractor(_ context.Context, id int64) (*Contractor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.contractors[id]
	if !ok || c.Deleted {
		return nil, nil
	}
	cp := *c
	if le := m.legalEntities[id]; le != nil {
		leCp := *le
		cp.LegalEntity = &leCp
	}
	for _, addr := range m.addresses {
		if !addr.Deleted && addr.ContractorID == id {
			ac := *addr
			for _, ae := range m.addrEqs {
				if !ae.Deleted && ae.AddressID == addr.ID {
					aec := *ae
					if eq, ok := m.equipments[ae.EquipmentID]; ok {
						aec.EquipmentName = eq.Name
					}
					ac.Equipments = append(ac.Equipments, aec)
				}
			}
			cp.Addresses = append(cp.Addresses, ac)
		}
	}
	return &cp, nil
}

func (m *memRepo) CreateContractor(_ context.Context, c *Contractor) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.ID = m.id()
	cp := *c
	m.contractors[c.ID] = &cp
	if c.LegalEntity != nil {
		le := *c.LegalEntity
		le.ContractorID = c.ID
		m.legalEntities[c.ID] = &le
	}
	return nil
}

func (m *memRepo) UpdateContractor(_ context.Context, c *Contractor) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *c
	m.contractors[c.ID] = &cp
	if c.LegalEntity != nil {
		le := *c.LegalEntity
		le.ContractorID = c.ID
		m.legalEntities[c.ID] = &le
	}
	return nil
}

func (m *memRepo) SoftDeleteContractor(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.contractors[id]; ok {
		c.Deleted = true
	}
	return nil
}

func (m *memRepo) FindContractorByPhone(_ context.Context, phone10 string) (*Contractor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.contractors {
		if c.Deleted {
			continue
		}
		for _, p := range c.Phones {
			if p.Phone == phone10 {
				cp := *c
				return &cp, nil
			}
		}
	}
	return nil, nil
}

func (m *memRepo) FindUniqueContractorByPhone(_ context.Context, phone10 string) (*Contractor, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var matched *Contractor
	count := 0
	for _, c := range m.contractors {
		if c.Deleted {
			continue
		}
		for _, p := range c.Phones {
			if !p.Deleted && p.Phone == phone10 {
				count++
				cp := *c
				matched = &cp
				break
			}
		}
	}
	if count != 1 {
		return nil, nil
	}
	return matched, nil
}

func (m *memRepo) CheckSimilarContractors(_ context.Context, excludeID *int64, phones10 []string, emails []string) ([]SimilarContractorMatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []SimilarContractorMatch
	for _, c := range m.contractors {
		if c.Deleted || (excludeID != nil && c.ID == *excludeID) {
			continue
		}
		for _, p := range c.Phones {
			if !p.Deleted && slices.Contains(phones10, p.Phone) {
				out = append(out, SimilarContractorMatch{ContractorID: c.ID, ContractorName: c.Name, MatchedOn: "phone", ContactValue: p.Phone})
			}
		}
		for _, e := range c.Emails {
			if !e.Deleted {
				for _, target := range emails {
					if strings.EqualFold(e.Email, target) {
						out = append(out, SimilarContractorMatch{ContractorID: c.ID, ContractorName: c.Name, MatchedOn: "email", ContactValue: e.Email})
					}
				}
			}
		}
	}
	return out, nil
}

func (m *memRepo) DetachPhoneFromOtherContractors(_ context.Context, phone10 string, keepContractorID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, c := range m.contractors {
		if id == keepContractorID {
			continue
		}
		kept := make([]ContractorPhone, 0, len(c.Phones))
		for _, p := range c.Phones {
			if p.Phone != phone10 {
				kept = append(kept, p)
			}
		}
		c.Phones = kept
	}
	return nil
}

func (m *memRepo) ReplaceContractorPhones(_ context.Context, contractorID int64, phones []ContractorPhone) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.contractors[contractorID]; ok {
		c.Phones = append([]ContractorPhone(nil), phones...)
	}
	return nil
}

func (m *memRepo) ReplaceContractorEmails(_ context.Context, contractorID int64, emails []ContractorEmail) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.contractors[contractorID]; ok {
		c.Emails = append([]ContractorEmail(nil), emails...)
	}
	return nil
}

func (m *memRepo) GetAddress(_ context.Context, id int64) (*ContractorAddress, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.addresses[id]
	if !ok || a.Deleted {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *memRepo) CreateAddress(_ context.Context, addr *ContractorAddress) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	addr.ID = m.id()
	cp := *addr
	m.addresses[addr.ID] = &cp
	return nil
}

func (m *memRepo) UpdateAddress(_ context.Context, addr *ContractorAddress) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *addr
	m.addresses[addr.ID] = &cp
	return nil
}

func (m *memRepo) SoftDeleteAddress(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.addresses[id]; ok {
		a.Deleted = true
	}
	return nil
}

func (m *memRepo) CountAddressLinks(_ context.Context, addressID int64) (int, int, int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var eqs, contracts, apps, certs int
	for _, ae := range m.addrEqs {
		if !ae.Deleted && ae.AddressID == addressID {
			eqs++
		}
	}
	for _, c := range m.contracts {
		if !c.Deleted && c.AddressID == addressID {
			contracts++
		}
	}
	for _, a := range m.apps {
		if !a.Deleted && a.AddressID == addressID {
			apps++
		}
	}
	for _, c := range m.certs {
		if !c.Deleted && c.AddressID == addressID {
			certs++
		}
	}
	return eqs, contracts, apps, certs, nil
}

func (m *memRepo) SearchAddresses(context.Context, *int64, string, int) ([]ContractorAddress, error) {
	return nil, nil
}
func (m *memRepo) ListEquipments(context.Context, string, int) ([]Equipment, error) {
	return nil, nil
}
func (m *memRepo) CreateEquipment(_ context.Context, eq *Equipment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	eq.ID = m.id()
	cp := *eq
	m.equipments[eq.ID] = &cp
	return nil
}
func (m *memRepo) UpdateEquipment(context.Context, *Equipment) error { return nil }
func (m *memRepo) AttachAddressEquipment(_ context.Context, ae *AddressEquipment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ae.ID = m.id()
	cp := *ae
	m.addrEqs[ae.ID] = &cp
	return nil
}
func (m *memRepo) SoftDeleteAddressEquipment(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ae, ok := m.addrEqs[id]; ok {
		ae.Deleted = true
	}
	return nil
}

func (m *memRepo) ListRefItems(_ context.Context, kind string) ([]RefItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []RefItem
	for _, r := range m.refItems {
		if !r.Deleted && (kind == "" || r.Kind == kind) {
			out = append(out, *r)
		}
	}
	return out, nil
}
func (m *memRepo) CreateRefItem(_ context.Context, item *RefItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item.ID = m.id()
	cp := *item
	m.refItems[item.ID] = &cp
	return nil
}
func (m *memRepo) SoftDeleteRefItem(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if item, ok := m.refItems[id]; ok {
		item.Deleted = true
	}
	return nil
}

func (m *memRepo) ListContracts(_ context.Context, f ContractFilter) ([]MaintenanceContract, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []MaintenanceContract
	for _, c := range m.contracts {
		if c.Deleted {
			continue
		}
		if f.ContractorID != nil && c.ContractorID != *f.ContractorID {
			continue
		}
		if f.AddressID != nil && c.AddressID != *f.AddressID {
			continue
		}
		if f.Status != nil && c.Status != *f.Status {
			continue
		}
		if f.From != nil && c.ValidTo.Before(*f.From) {
			continue
		}
		if f.To != nil && c.ValidTo.After(*f.To) {
			continue
		}
		cp := *c
		for _, other := range m.contracts {
			if !other.Deleted && other.ID != c.ID && other.AddressID == c.AddressID && other.ValidTo.After(c.ValidTo) {
				cp.HasNewerContract = true
				break
			}
		}
		cp.NeedsProlongation = cp.Status == StatusActive && !cp.HasNewerContract
		if f.OnlyNeedingProlongation && !cp.NeedsProlongation {
			continue
		}
		out = append(out, cp)
	}
	return out, nil
}
func (m *memRepo) GetContract(_ context.Context, id int64) (*MaintenanceContract, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.contracts[id]
	if !ok || c.Deleted {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}
func (m *memRepo) CreateContract(_ context.Context, c *MaintenanceContract) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c.ID = m.id()
	cp := *c
	m.contracts[c.ID] = &cp
	return nil
}
func (m *memRepo) UpdateContract(_ context.Context, c *MaintenanceContract) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *c
	m.contracts[c.ID] = &cp
	return nil
}
func (m *memRepo) SoftDeleteContract(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.contracts[id]; ok {
		c.Deleted = true
	}
	return nil
}
func (m *memRepo) ListContractsNeedingQuarterlyTask(_ context.Context, now time.Time) ([]MaintenanceContract, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	var out []MaintenanceContract
	for _, c := range m.contracts {
		if c.Deleted || c.Status != StatusActive {
			continue
		}
		vf := time.Date(c.ValidFrom.Year(), c.ValidFrom.Month(), c.ValidFrom.Day(), 0, 0, 0, 0, time.UTC)
		vt := time.Date(c.ValidTo.Year(), c.ValidTo.Month(), c.ValidTo.Day(), 0, 0, 0, 0, time.UTC)
		daysSince := int(today.Sub(vf).Hours() / 24)
		daysLeft := int(vt.Sub(today).Hours() / 24)
		if daysSince > 0 && daysSince%90 == 0 && daysLeft > 89 {
			out = append(out, *c)
		}
	}
	return out, nil
}
func (m *memRepo) ListContractsNeedingProlongationTask(_ context.Context, now time.Time) ([]MaintenanceContract, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	limit := today.AddDate(0, 0, 21)
	var out []MaintenanceContract
	for _, c := range m.contracts {
		if c.Deleted || c.Status != StatusActive {
			continue
		}
		vt := time.Date(c.ValidTo.Year(), c.ValidTo.Month(), c.ValidTo.Day(), 0, 0, 0, 0, time.UTC)
		if !vt.Before(today) && !vt.After(limit) {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (m *memRepo) HasEngineerVisibleApplicationInWindow(_ context.Context, engineerID, contractorID int64, from, to time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.apps {
		if !a.Deleted && appHasEngineer(a, engineerID) && a.ContractorID == contractorID &&
			(a.Status == StatusActive || a.Status == StatusProcessed) &&
			!a.PlanDatetimeFrom.Before(from) && !a.PlanDatetimeFrom.After(to) {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRepo) HasEngineerTimeOverlap(_ context.Context, engineerID int64, from, to time.Time, excludeAppID *int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.apps {
		if a.Deleted || a.IsDuty || !appHasEngineer(a, engineerID) {
			continue
		}
		if excludeAppID != nil && a.ID == *excludeAppID {
			continue
		}
		if a.Status == StatusFailed || a.Status == StatusCancelled {
			continue
		}
		if a.PlanDatetimeFrom.Before(to) && a.PlanDatetimeTo.After(from) {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRepo) ListApplications(_ context.Context, f ApplicationFilter) ([]Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Application
	for _, a := range m.apps {
		if a.Deleted {
			continue
		}
		if f.EngineerID != nil && !appHasEngineer(a, *f.EngineerID) {
			continue
		}
		if f.SubdivisionID != nil && (a.SubdivisionID == nil || *a.SubdivisionID != *f.SubdivisionID) {
			continue
		}
		if f.ContractorID != nil && a.ContractorID != *f.ContractorID {
			continue
		}
		if f.From != nil && a.PlanDatetimeTo.Before(*f.From) {
			continue
		}
		if f.To != nil && a.PlanDatetimeFrom.After(*f.To) {
			continue
		}
		out = append(out, *a)
	}
	return out, nil
}
func (m *memRepo) ListCalendarEvents(_ context.Context, from, to time.Time, engineerID *int64, subdivisionID *int64) ([]CalendarEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []CalendarEvent
	for _, a := range m.apps {
		if a.Deleted || a.PlanDatetimeTo.Before(from) || a.PlanDatetimeFrom.After(to) {
			continue
		}
		if engineerID != nil && !appHasEngineer(a, *engineerID) {
			continue
		}
		if subdivisionID != nil && (a.SubdivisionID == nil || *a.SubdivisionID != *subdivisionID) {
			continue
		}
		out = append(out, CalendarEvent{
			ID:             a.ID,
			Start:          a.PlanDatetimeFrom,
			End:            a.PlanDatetimeTo,
			ContractorID:   a.ContractorID,
			AddressID:      a.AddressID,
			EngineerID:     a.EngineerID,
			ExecutorIDs:    append([]int64(nil), a.ExecutorIDs...),
			Status:         a.Status,
			IsComplete:     a.IsComplete,
			IsDuty:         a.IsDuty,
			WorkTypes:      a.WorkTypes,
			SpecialNotes:   a.SpecialNotes,
			NotesViewed:    a.NotesViewed,
			Rating:         a.Rating,
			WorkWindowFrom: a.WorkWindowFrom,
			WorkWindowTo:   a.WorkWindowTo,
			PlannedSum:     a.PlannedSum,
		})
	}
	return out, nil
}
func (m *memRepo) GetApplication(_ context.Context, id int64) (*Application, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.apps[id]
	if !ok || a.Deleted {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}
func (m *memRepo) CreateApplication(_ context.Context, app *Application) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	app.ID = m.id()
	cp := *app
	m.apps[app.ID] = &cp
	return nil
}
func (m *memRepo) UpdateApplication(_ context.Context, app *Application) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *app
	m.apps[app.ID] = &cp
	return nil
}
func (m *memRepo) UpdateApplicationRating(_ context.Context, id int64, rating int, comment string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.apps[id]; ok {
		a.Rating = &rating
		a.RatingComment = comment
	}
	return nil
}
func (m *memRepo) MarkApplicationNotesViewed(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.apps[id]; ok {
		a.NotesViewed = true
	}
	return nil
}
func (m *memRepo) SoftDeleteApplication(context.Context, int64) error { return nil }

func (m *memRepo) ListCertificates(_ context.Context, f CertificateFilter) ([]Certificate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Certificate
	for _, c := range m.certs {
		if c.Deleted {
			continue
		}
		if f.EngineerID != nil {
			matched := c.EngineerID == *f.EngineerID
			if !matched {
				for _, ex := range c.Executors {
					if ex.UserID == *f.EngineerID {
						matched = true
						break
					}
				}
			}
			if !matched {
				continue
			}
		}
		if f.ContractorID != nil && c.ContractorID != *f.ContractorID {
			continue
		}
		if f.ApplicationID != nil && (c.ApplicationID == nil || *c.ApplicationID != *f.ApplicationID) {
			continue
		}
		if f.From != nil && c.WorkDate.Before(*f.From) {
			continue
		}
		if f.To != nil && c.WorkDate.After(*f.To) {
			continue
		}
		out = append(out, *c)
	}
	return out, nil
}
func (m *memRepo) GetCertificate(_ context.Context, id int64) (*Certificate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.certs[id]
	if !ok || c.Deleted {
		return nil, nil
	}
	cp := *c
	return &cp, nil
}
func (m *memRepo) CreateCertificate(_ context.Context, cert *Certificate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cert.ID = m.id()
	cp := *cert
	m.certs[cert.ID] = &cp
	return nil
}
func (m *memRepo) UpdateCertificate(_ context.Context, cert *Certificate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *cert
	m.certs[cert.ID] = &cp
	return nil
}
func (m *memRepo) SoftDeleteCertificate(context.Context, int64) error { return nil }

func (m *memRepo) ListTasks(_ context.Context, f TaskFilter) ([]Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Task
	for _, t := range m.tasks {
		if t.Deleted {
			continue
		}
		if f.AssignedUserID != nil && (t.AssignedUserID == nil || *t.AssignedUserID != *f.AssignedUserID) {
			continue
		}
		if f.SubdivisionID != nil && (t.SubdivisionID == nil || *t.SubdivisionID != *f.SubdivisionID) {
			continue
		}
		out = append(out, *t)
	}
	return out, nil
}
func (m *memRepo) GetTask(_ context.Context, id int64) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok || t.Deleted {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}
func (m *memRepo) CreateTask(_ context.Context, task *Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	task.ID = m.id()
	cp := *task
	m.tasks[task.ID] = &cp
	return nil
}
func (m *memRepo) UpdateTask(_ context.Context, task *Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *task
	m.tasks[task.ID] = &cp
	return nil
}
func (m *memRepo) SoftDeleteTask(context.Context, int64) error { return nil }
func (m *memRepo) ListUnassignedActiveTasks(context.Context, int) ([]Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Task
	for _, t := range m.tasks {
		if !t.Deleted && t.AssignedUserID == nil && (t.Status == TaskStatusNew || t.Status == TaskStatusInProgress) {
			out = append(out, *t)
		}
	}
	slices.SortFunc(out, func(a, b Task) int { return b.Priority - a.Priority })
	return out, nil
}
func (m *memRepo) CountUserActivePoolTasks(_ context.Context, userID int64, transferred bool) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cnt := 0
	for _, t := range m.tasks {
		if !t.Deleted && t.AssignedUserID != nil && *t.AssignedUserID == userID && (t.Status == TaskStatusNew || t.Status == TaskStatusInProgress) {
			if transferred && t.PrevTaskID == nil {
				continue
			}
			cnt++
		}
	}
	return cnt, nil
}
func (m *memRepo) ListOverdueActiveTasks(_ context.Context, now time.Time) ([]Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Task
	for _, t := range m.tasks {
		if !t.Deleted && (t.Status == TaskStatusNew || t.Status == TaskStatusInProgress) {
			if !t.PlanDatetimeTo.Add(-time.Duration(t.DecisionTimeSec) * time.Second).After(now) {
				out = append(out, *t)
			}
		}
	}
	return out, nil
}
func (m *memRepo) ListTaskPoolSettings(context.Context) ([]TaskPoolSetting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]TaskPoolSetting(nil), m.poolSettings...), nil
}
func (m *memRepo) SaveTaskPoolSetting(_ context.Context, s *TaskPoolSetting) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.ID == 0 {
		s.ID = m.id()
	}
	m.poolSettings = append(m.poolSettings, *s)
	return nil
}
func (m *memRepo) ListQueueUsers(context.Context) ([]QueueUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []QueueUser
	for _, q := range m.queueUsers {
		if q.Active {
			out = append(out, *q)
		}
	}
	slices.SortFunc(out, func(a, b QueueUser) int { return a.Position - b.Position })
	return out, nil
}
func (m *memRepo) TouchQueueUser(_ context.Context, userID int64, assignedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queueUsers[userID] = &QueueUser{UserID: userID, LastAssignedAt: &assignedAt, Active: true}
	return nil
}
func (m *memRepo) ListCalls(_ context.Context, f CallFilter) ([]CallRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []CallRecord
	for _, c := range m.calls {
		if f.UserID != nil && (c.UserID == nil || *c.UserID != *f.UserID) {
			continue
		}
		if f.ContractorID != nil && (c.ContractorID == nil || *c.ContractorID != *f.ContractorID) {
			continue
		}
		if f.SubdivisionID != nil {
			var subID *int64
			if c.ContractorID != nil {
				if ctr, ok := m.contractors[*c.ContractorID]; ok {
					subID = ctr.SubdivisionID
				}
			}
			if subID == nil || *subID != *f.SubdivisionID {
				continue
			}
		}
		if len(f.Directions) > 0 && !slices.Contains(f.Directions, c.Direction) {
			continue
		}
		if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, c.Status) {
			continue
		}
		if len(f.TypesOfAppeal) > 0 && !slices.Contains(f.TypesOfAppeal, c.TypeOfAppeal) {
			continue
		}
		if len(f.CallerTypes) > 0 && !slices.Contains(f.CallerTypes, c.CallerType) {
			continue
		}
		if len(f.CallResults) > 0 && !slices.Contains(f.CallResults, c.CallResult) {
			continue
		}
		if len(f.TrafficSourceIDs) > 0 && (c.TrafficSourceID == nil || !slices.Contains(f.TrafficSourceIDs, *c.TrafficSourceID)) {
			continue
		}
		out = append(out, *c)
	}
	return out, nil
}
func (m *memRepo) GetCall(_ context.Context, id int64) (*CallRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.calls {
		if c.ID == id {
			cp := *c
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memRepo) GetCallByExternalID(_ context.Context, externalCallID string) (*CallRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.calls[externalCallID]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, nil
}
func (m *memRepo) UpsertCallByExternalID(_ context.Context, call *CallRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev, ok := m.calls[call.ExternalCallID]; ok {
		call.ID = prev.ID
		if call.UserID == nil {
			call.UserID = prev.UserID
		}
		if call.ContractorID == nil {
			call.ContractorID = prev.ContractorID
		}
		if call.ContractorName == "" {
			call.ContractorName = prev.ContractorName
		}
		if call.TypeOfAppeal == "" {
			call.TypeOfAppeal = prev.TypeOfAppeal
		}
		if call.Comment == "" {
			call.Comment = prev.Comment
		}
		if call.AbonentID == "" {
			call.AbonentID = prev.AbonentID
		}
		if call.ExtTrackingID == "" {
			call.ExtTrackingID = prev.ExtTrackingID
		}
	} else if call.ID == 0 {
		call.ID = m.id()
	}
	cp := *call
	m.calls[call.ExternalCallID] = &cp
	return nil
}
func (m *memRepo) UpdateCallCard(_ context.Context, call *CallRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if prev, ok := m.calls[call.ExternalCallID]; ok {
		prev.ContractorID = call.ContractorID
		prev.TrafficSourceID = call.TrafficSourceID
		prev.CallerType = call.CallerType
		prev.CallResult = call.CallResult
		prev.TypeOfAppeal = call.TypeOfAppeal
		prev.Comment = call.Comment
	}
	return nil
}
func (m *memRepo) AmountActsReport(_ context.Context, from, to time.Time, engineerID *int64) ([]AmountActsReportRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	byEng := map[int64]*AmountActsReportRow{}
	for _, c := range m.certs {
		if c.Deleted || c.WorkDate.Before(from) || c.WorkDate.After(to) {
			continue
		}
		if engineerID != nil && c.EngineerID != *engineerID {
			continue
		}
		row, ok := byEng[c.EngineerID]
		if !ok {
			row = &AmountActsReportRow{EngineerID: c.EngineerID}
			byEng[c.EngineerID] = row
		}
		row.CertificatesCount++
		row.TotalAmount += c.TotalAmount
		row.PaidAmount += c.PaidAmount
		row.DebtAmount = row.TotalAmount - row.PaidAmount
	}
	var out []AmountActsReportRow
	for _, r := range byEng {
		out = append(out, *r)
	}
	return out, nil
}
func (m *memRepo) ExpiredTasksReport(context.Context, time.Time, *int64) ([]ExpiredTaskReportRow, error) {
	return nil, nil
}
func (m *memRepo) AddChangeLog(_ context.Context, entry ChangeLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.changeLogs = append(m.changeLogs, entry)
	return nil
}
func (m *memRepo) ListChangeLogs(context.Context, string, int64, int) ([]ChangeLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ChangeLog(nil), m.changeLogs...), nil
}
func (m *memRepo) CreateNotification(_ context.Context, n *Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	n.ID = m.id()
	m.notifs = append(m.notifs, *n)
	return nil
}
func (m *memRepo) ListUserNotifications(_ context.Context, userID int64, unreadOnly bool, _ int) ([]Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Notification
	for _, n := range m.notifs {
		if n.UserID != userID {
			continue
		}
		if unreadOnly && n.IsRead {
			continue
		}
		out = append(out, n)
	}
	return out, nil
}
func (m *memRepo) MarkNotificationRead(context.Context, int64, int64) error { return nil }
func (m *memRepo) MarkAllNotificationsRead(context.Context, int64) error    { return nil }
func (m *memRepo) DeleteOldReadNotifications(_ context.Context, olderThan time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept []Notification
	var deleted int64
	for _, n := range m.notifs {
		if n.IsRead && n.CreatedAt.Before(olderThan) {
			deleted++
			continue
		}
		kept = append(kept, n)
	}
	m.notifs = kept
	return deleted, nil
}
func (m *memRepo) GetUserSetting(context.Context, int64, string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (m *memRepo) SaveUserSetting(context.Context, int64, string, json.RawMessage) error {
	return nil
}

type fakeUsers struct {
	users map[int64]UserBrief
}

func (f *fakeUsers) GetUserBrief(_ context.Context, id int64) (*UserBrief, error) {
	if u, ok := f.users[id]; ok {
		cp := u
		return &cp, nil
	}
	return nil, nil
}
func (f *fakeUsers) ListActiveWorkers(_ context.Context, isEng, isDisp bool, subID *int64) ([]UserBrief, error) {
	var out []UserBrief
	for _, u := range f.users {
		if !u.Active {
			continue
		}
		if isEng && !u.IsEngineer {
			continue
		}
		if isDisp && !u.IsDispatcher {
			continue
		}
		if subID != nil && (u.SubdivisionID == nil || *u.SubdivisionID != *subID) {
			continue
		}
		out = append(out, u)
	}
	slices.SortFunc(out, func(a, b UserBrief) int { return int(a.ID - b.ID) })
	return out, nil
}
func (f *fakeUsers) ListUserIDsByPermission(context.Context, string) ([]int64, error) {
	return nil, nil
}

type staticAuthorizer struct {
	ac *platform.AuthContext
}

func (a staticAuthorizer) ResolveRequest(*http.Request) (*platform.AuthContext, error) {
	if a.ac == nil {
		return nil, platform.NewUnauthorized("unauth")
	}
	return a.ac, nil
}

func TestPhoneDeduplicationAndNormalization(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo, nil, nil, nil, Config{}, nil)
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}

	c, err := svc.CreateContractor(ctx, admin, SaveContractorInput{
		Name: "Иванов И.И.",
		Phones: []ContractorPhone{
			{Phone: "+7 (999) 111-22-33", Note: "Основной"},
			{Phone: "89991112233", Note: "Дубль"},
			{Phone: "9165554433", Note: "Второй"},
		},
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if len(c.Phones) != 2 {
		t.Fatalf("expected 2 deduplicated phones, got %d", len(c.Phones))
	}
	if c.Phones[0].Phone != "9991112233" || c.Phones[1].Phone != "9165554433" {
		t.Fatalf("unexpected normalized phones: %+v", c.Phones)
	}
}

func TestEngineerViewContractorThreeDaysRule(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	svc := NewService(repo, nil, nil, nil, Config{Clock: func() time.Time { return now }}, nil)
	ctx := context.Background()

	cNear := &Contractor{Name: "Рядом Клиент", CreatedBy: 1}
	cFar := &Contractor{Name: "Старый Клиент", CreatedBy: 1}
	_ = repo.CreateContractor(ctx, cNear)
	_ = repo.CreateContractor(ctx, cFar)

	_ = repo.CreateApplication(ctx, &Application{
		ContractorID:     cNear.ID,
		AddressID:        1,
		EngineerID:       10,
		Status:           StatusProcessed,
		PlanDatetimeFrom: now.Add(-24 * time.Hour),
		PlanDatetimeTo:   now.Add(-22 * time.Hour),
	})
	_ = repo.CreateApplication(ctx, &Application{
		ContractorID:     cFar.ID,
		AddressID:        2,
		EngineerID:       10,
		Status:           StatusProcessed,
		PlanDatetimeFrom: now.Add(-10 * 24 * time.Hour),
		PlanDatetimeTo:   now.Add(-10*24*time.Hour + 2*time.Hour),
	})

	engineerAuth := &platform.AuthContext{
		UserID:      10,
		Permissions: map[string]bool{"crm.contractor.view": true},
	}

	if _, err := svc.GetContractor(ctx, engineerAuth, cNear.ID); err != nil {
		t.Fatalf("expected near contractor to be visible within ±3 days: %v", err)
	}
	if _, err := svc.GetContractor(ctx, engineerAuth, cFar.ID); err == nil {
		t.Fatal("expected contractor older than 3 days to be denied")
	}

	// С полным правом crm.contractor.view_all видны оба.
	dispAuth := &platform.AuthContext{
		UserID:      2,
		Permissions: map[string]bool{"crm.contractor.view": true, "crm.contractor.view_all": true},
	}
	if _, err := svc.GetContractor(ctx, dispAuth, cFar.ID); err != nil {
		t.Fatalf("dispatcher should see old contractor: %v", err)
	}
}

func TestAddressDeletionBlockedWhenLinked(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo, nil, nil, nil, Config{}, nil)
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}

	c, _ := svc.CreateContractor(ctx, admin, SaveContractorInput{Name: "Клиент"})
	addr, _ := svc.CreateAddress(ctx, admin, ContractorAddress{ContractorID: c.ID, Address: "ул. Ленина 1"})
	eq, _ := svc.CreateEquipment(ctx, admin, "Котёл", "Eco", "Baxi")
	if eq.Name != "Котёл Eco Baxi" {
		t.Fatalf("unexpected equipment generated name: %q", eq.Name)
	}
	ae, _ := svc.AttachAddressEquipment(ctx, admin, AddressEquipment{AddressID: addr.ID, EquipmentID: eq.ID, SerialNumber: "SN1"})

	if err := svc.DeleteAddress(ctx, admin, addr.ID); err == nil {
		t.Fatal("expected delete address to be blocked while equipment linked")
	}
	_ = svc.DetachAddressEquipment(ctx, admin, ae.ID)
	if err := svc.DeleteAddress(ctx, admin, addr.ID); err != nil {
		t.Fatalf("expected delete address to succeed after detach: %v", err)
	}
}

func TestApplicationOverlapAndDutyBypass(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	svc := NewService(repo, nil, nil, nil, Config{Clock: func() time.Time { return now }}, nil)
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}

	_, err := svc.CreateApplication(ctx, admin, Application{
		ContractorID:     1,
		AddressID:        1,
		EngineerID:       5,
		PlanDatetimeFrom: now,
		PlanDatetimeTo:   now.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("first app failed: %v", err)
	}

	// Пересечение по времени запрещено для обычной заявки.
	if _, err := svc.CreateApplication(ctx, admin, Application{
		ContractorID:     2,
		AddressID:        2,
		EngineerID:       5,
		PlanDatetimeFrom: now.Add(1 * time.Hour),
		PlanDatetimeTo:   now.Add(3 * time.Hour),
	}); err == nil {
		t.Fatal("expected overlap validation error")
	}

	// Дежурный слот (is_duty=true) допускается даже поверх существующей заявки.
	if _, err := svc.CreateApplication(ctx, admin, Application{
		ContractorID:     2,
		AddressID:        2,
		EngineerID:       5,
		IsDuty:           true,
		PlanDatetimeFrom: now.Add(1 * time.Hour),
		PlanDatetimeTo:   now.Add(3 * time.Hour),
	}); err != nil {
		t.Fatalf("duty app should bypass overlap check: %v", err)
	}
}

func TestCertificateClosesApplicationAndResetsNotesViewed(t *testing.T) {
	now := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	svc := NewService(repo, nil, nil, nil, Config{Clock: func() time.Time { return now }}, nil)
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}

	app, err := svc.CreateApplication(ctx, admin, Application{
		ContractorID:     1,
		AddressID:        1,
		EngineerID:       5,
		PlanDatetimeFrom: now,
		PlanDatetimeTo:   now.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}
	if !app.NotesViewed {
		t.Fatal("expected initial NotesViewed=true when no special notes")
	}

	_, err = svc.SaveCertificate(ctx, admin, 0, Certificate{
		ApplicationID: &app.ID,
		ContractorID:  1,
		AddressID:     1,
		EngineerID:    5,
		TotalAmount:   6500,
		PaidAmount:    6500,
		Results: CertificateTelemetry{
			BurnerGasPressure: "12 мбар",
			Voltage:           "228 В",
		},
		SpecialNotes: "Требуется замена датчика NTC",
	})
	if err != nil {
		t.Fatalf("save cert: %v", err)
	}

	updatedApp, _ := svc.GetApplication(ctx, admin, app.ID)
	if !updatedApp.IsComplete || updatedApp.Status != StatusProcessed {
		t.Fatalf("expected application to be closed/processed, got complete=%v status=%d", updatedApp.IsComplete, updatedApp.Status)
	}
	if updatedApp.NotesViewed {
		t.Fatal("expected NotesViewed to reset to false when certificate brings special notes")
	}
}

func TestQuarterlyAndProlongationTaskGeneration(t *testing.T) {
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	svc := NewService(repo, nil, nil, nil, Config{Clock: func() time.Time { return now }}, nil)
	ctx := context.Background()

	// Договор 1: ровно 90 дней с valid_from, осталось > 89 дней -> должен породить quarterly_call.
	_ = repo.CreateContract(ctx, &MaintenanceContract{
		ContractorID: 1, AddressID: 1, Number: "D-90",
		ValidFrom: now.AddDate(0, 0, -90),
		ValidTo:   now.AddDate(0, 0, 275),
		Status:    StatusActive,
	})
	// Договор 2: истекает через 10 дней -> должен породить prolongation.
	_ = repo.CreateContract(ctx, &MaintenanceContract{
		ContractorID: 2, AddressID: 2, Number: "D-PROLONG",
		ValidFrom: now.AddDate(-1, 0, 10),
		ValidTo:   now.AddDate(0, 0, 10),
		Status:    StatusActive,
	})

	qCnt, err := svc.CreateQuarterlyCallTasks(ctx)
	if err != nil || qCnt != 1 {
		t.Fatalf("expected 1 quarterly task, got %d (err=%v)", qCnt, err)
	}
	pCnt, err := svc.CreateProlongationTasks(ctx)
	if err != nil || pCnt != 1 {
		t.Fatalf("expected 1 prolongation task, got %d (err=%v)", pCnt, err)
	}
}

func TestTaskPoolRoundRobinAndCapsAndNoAnswerFollowup(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) // 15:00 MSK
	repo := newMemRepo()
	users := &fakeUsers{
		users: map[int64]UserBrief{
			101: {ID: 101, FullName: "Оператор 1", IsDispatcher: true, Active: true},
			102: {ID: 102, FullName: "Оператор 2", IsDispatcher: true, Active: true},
		},
	}
	svc := NewService(repo, users, nil, nil, Config{Clock: func() time.Time { return now }}, nil)
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}

	_, _ = svc.SaveTaskPoolSetting(ctx, admin, TaskPoolSetting{
		Name:                     "Дневной пул",
		TaskTypes:                []TaskType{TaskTypeCall, TaskTypeQuarterlyCall},
		Priority:                 10,
		TimeFrom:                 "08:00",
		TimeTo:                   "20:00",
		IsOnline:                 false,
		ForDispatcher:            true,
		TasksMaxCount:            2,
		TransferredTasksMaxCount: 2,
		Active:                   true,
	})

	t1, _ := svc.CreateTask(ctx, admin, Task{Title: "Обзвон 1", TaskType: TaskTypeCall, Priority: 20})
	t2, _ := svc.CreateTask(ctx, admin, Task{Title: "Обзвон 2", TaskType: TaskTypeCall, Priority: 10})
	t3, _ := svc.CreateTask(ctx, admin, Task{Title: "Обзвон 3", TaskType: TaskTypeCall, Priority: 5})

	assigned, err := svc.DistributeTaskPool(ctx)
	if err != nil {
		t.Fatalf("distribute pool: %v", err)
	}
	if assigned != 3 {
		t.Fatalf("expected 3 tasks distributed, got %d", assigned)
	}

	got1, _ := svc.GetTask(ctx, t1.ID)
	got2, _ := svc.GetTask(ctx, t2.ID)
	got3, _ := svc.GetTask(ctx, t3.ID)
	if got1.AssignedUserID == nil || got2.AssignedUserID == nil || *got1.AssignedUserID == *got2.AssignedUserID {
		t.Fatalf("expected round-robin across 2 dispatchers, got t1=%v t2=%v", got1.AssignedUserID, got2.AssignedUserID)
	}
	if got3.AssignedUserID == nil {
		t.Fatal("expected t3 assigned")
	}

	// Недозвон автоматически создаёт повторную задачу типа no_answer.
	_, err = svc.UpdateTask(ctx, admin, t1.ID, Task{Status: TaskStatusNoAnswer})
	if err != nil {
		t.Fatalf("update task no answer: %v", err)
	}
	allTasks, _ := svc.ListTasks(ctx, admin, TaskFilter{Limit: 20})
	foundFollowup := false
	for _, tk := range allTasks {
		if tk.TaskType == TaskTypeNoAnswer && strings.Contains(tk.Title, t1.Title) {
			foundFollowup = true
		}
	}
	if !foundFollowup {
		t.Fatal("expected automatic follow-up task after TaskStatusNoAnswer")
	}
}

func TestMTSWebhookResolvesContractorAndEmitsEvent(t *testing.T) {
	now := time.Date(2026, 11, 13, 14, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	bus := platform.NewInMemoryEventBus()
	hub := NewWSHub(nil)
	svc := NewService(repo, nil, bus, hub, Config{
		MTSWebhookSecret: "secret-mts",
		Clock:            func() time.Time { return now },
	}, nil)
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}

	uid := int64(42)
	_, _ = svc.SaveSipAccount(ctx, admin, SipAccount{
		Name: "Disp 42", Domain: "sip.tsfera.local", Login: "sip42", Server: "sip.tsfera.local", UserID: &uid, Active: true,
	})
	_, _ = svc.CreateContractor(ctx, admin, SaveContractorInput{
		Name:   "Петров Сергей",
		Phones: []ContractorPhone{{Phone: "+7 (903) 777-88-99"}},
	})

	var busFired bool
	bus.Subscribe("crm.call.incoming", func(_ context.Context, ev platform.Event) {
		busFired = true
		if len(ev.UserIDs) != 1 || ev.UserIDs[0] != 42 {
			t.Errorf("unexpected target user IDs: %v", ev.UserIDs)
		}
	})

	payload, _ := json.Marshal(MTSWebhookInput{
		ExternalCallID: "call-1001",
		Direction:      "inbound",
		State:          "ringing",
		From:           "+79037778899",
		To:             "74951234567",
		SipLogin:       "sip42",
	})

	if _, err := svc.HandleMTSWebhook(ctx, "wrong-token", payload); err == nil {
		t.Fatal("expected invalid webhook token to fail")
	}

	rec, err := svc.HandleMTSWebhook(ctx, "secret-mts", payload)
	if err != nil {
		t.Fatalf("webhook failed: %v", err)
	}
	if rec.ContractorID == nil || rec.ContractorName != "Петров Сергей" || rec.UserID == nil || *rec.UserID != 42 {
		t.Fatalf("unexpected resolved call record: %+v", rec)
	}
	if !busFired {
		t.Fatal("expected EventBus crm.call.incoming to fire")
	}
}

func TestLegacyMobileEnvelopeAndReadOnlyToken(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo, nil, nil, nil, Config{}, nil)

	// Токен только для чтения: GET должен работать, POST — 403 Forbidden.
	roAuth := staticAuthorizer{
		ac: &platform.AuthContext{
			UserID:        1,
			IsSuperAdmin:  true,
			IsToken:       true,
			TokenCanRead:  true,
			TokenCanWrite: false,
		},
	}
	h := NewHandler(svc, roAuth, nil)
	r := chi.NewRouter()
	h.Mount(r)

	reqGet := httptest.NewRequest(http.MethodGet, "/api/doc/certificate-of-competition", nil)
	recGet := httptest.NewRecorder()
	r.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("expected 200 on legacy GET alias, got %d: %s", recGet.Code, recGet.Body.String())
	}
	var env platform.LegacyAPIEnvelope
	if err := json.Unmarshal(recGet.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}

	reqPost := httptest.NewRequest(http.MethodPost, "/api/doc/application-for-technical-work", bytes.NewBufferString(`{"contractor_id":1}`))
	recPost := httptest.NewRecorder()
	r.ServeHTTP(recPost, reqPost)
	if recPost.Code != http.StatusForbidden {
		t.Fatalf("expected 403 on write endpoint with read-only token, got %d", recPost.Code)
	}
}

func TestAIToolsEnforceContractorVisibility(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	mod := NewModule(repo, nil, staticAuthorizer{}, platform.NewInMemoryEventBus(), Config{
		Clock: func() time.Time { return now },
	}, nil)
	ctx := context.Background()

	cSecret := &Contractor{Name: "Секретный Клиент"}
	_ = repo.CreateContractor(ctx, cSecret)

	engAuth := &platform.AuthContext{
		UserID:      99,
		Permissions: map[string]bool{"crm.contractor.view": true},
	}

	var findTool *platform.ToolDefinition
	for i := range mod.Tools() {
		if mod.Tools()[i].Name == "crm_get_contractor_card" {
			tDef := mod.Tools()[i]
			findTool = &tDef
			break
		}
	}
	if findTool == nil {
		t.Fatal("crm_get_contractor_card tool not registered")
	}
	args, _ := json.Marshal(map[string]any{"contractor_id": cSecret.ID})
	if _, err := findTool.Execute(ctx, engAuth, args); err == nil {
		t.Fatal("expected AI tool to deny contractor without ±3 days active/processed application")
	}
}

func TestCheckSimilarContractorsAndSubdivisionScoping(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	sub1 := int64(10)
	sub2 := int64(20)
	users := &fakeUsers{
		users: map[int64]UserBrief{
			1: {ID: 1, FullName: "Менеджер 1", SubdivisionID: &sub1, Active: true},
			2: {ID: 2, FullName: "Инженер 2", SubdivisionID: &sub2, IsEngineer: true, Active: true},
		},
	}
	svc := NewService(repo, users, nil, nil, Config{Clock: func() time.Time { return now }}, nil)
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 99, IsSuperAdmin: true}

	c1, err := svc.CreateContractor(ctx, admin, SaveContractorInput{
		LastName:      "Петров",
		FirstName:     "Пётр",
		SecondName:    "Петрович",
		SubdivisionID: &sub1,
		Phones:        []ContractorPhone{{Phone: "+7 (901) 555-66-77"}},
		Emails:        []ContractorEmail{{Email: "Petrov@Example.ru"}},
	})
	if err != nil {
		t.Fatalf("create contractor c1: %v", err)
	}
	if c1.Name != "Петров Пётр Петрович" {
		t.Fatalf("expected composed FIO, got %q", c1.Name)
	}
	c2, err := svc.CreateContractor(ctx, admin, SaveContractorInput{
		Name:          "ООО Тепло",
		SubdivisionID: &sub2,
		Phones:        []ContractorPhone{{Phone: "89021112233"}},
	})
	if err != nil {
		t.Fatalf("create contractor c2: %v", err)
	}

	matches, err := svc.CheckSimilarContractors(ctx, nil, []string{"8 (901) 555-66-77"}, []string{"petrov@example.ru"})
	if err != nil || len(matches) != 2 {
		t.Fatalf("expected 2 matches (phone+email), got %d (err=%v)", len(matches), err)
	}
	matchesExcluded, err := svc.CheckSimilarContractors(ctx, &c1.ID, []string{"9015556677"}, nil)
	if err != nil || len(matchesExcluded) != 0 {
		t.Fatalf("expected 0 matches when excluding own ID, got %d", len(matchesExcluded))
	}

	addr1, _ := svc.CreateAddress(ctx, admin, ContractorAddress{ContractorID: c1.ID, Address: "ул. Садовая, 1"})
	addr2, _ := svc.CreateAddress(ctx, admin, ContractorAddress{ContractorID: c2.ID, Address: "пр. Ленина, 2"})

	if _, err := svc.CreateApplication(ctx, admin, Application{
		ContractorID:     c1.ID,
		AddressID:        addr1.ID,
		EngineerID:       1,
		SubdivisionID:    &sub1,
		PlanDatetimeFrom: now,
		PlanDatetimeTo:   now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("create app1: %v", err)
	}
	// Не передаём SubdivisionID — сервис должен подтянуть подразделение инженера 2 (sub2).
	app2, err := svc.CreateApplication(ctx, admin, Application{
		ContractorID:     c2.ID,
		AddressID:        addr2.ID,
		EngineerID:       2,
		PlanDatetimeFrom: now,
		PlanDatetimeTo:   now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create app2: %v", err)
	}
	if app2.SubdivisionID == nil || *app2.SubdivisionID != sub2 {
		t.Fatalf("expected auto subdivision_id=%d from engineer, got %v", sub2, app2.SubdivisionID)
	}

	// Пользователь только своего подразделения sub1 не должен видеть c2 и app2.
	sub1Mgr := &platform.AuthContext{
		UserID:        1,
		SubdivisionID: &sub1,
		Permissions: map[string]bool{
			"crm.contractor.view":     true,
			"crm.contractor.view_all": true,
			"crm.application.view":    true,
		},
	}
	found, total, err := svc.SearchContractors(ctx, sub1Mgr, "", 50, 0)
	if err != nil || total != 1 || found[0].ID != c1.ID {
		t.Fatalf("expected sub1 scope only c1, got total=%d err=%v", total, err)
	}
	if _, err := svc.GetContractor(ctx, sub1Mgr, c2.ID); err == nil {
		t.Fatal("expected forbidden on cross-subdivision contractor")
	}
	events, err := svc.ListCalendarEvents(ctx, sub1Mgr, now.Add(-time.Hour), now.Add(2*time.Hour), nil, &sub2)
	if err != nil || len(events) != 1 || events[0].ContractorID != c1.ID {
		t.Fatalf("expected calendar restricted to sub1, got %+v (err=%v)", events, err)
	}

	// Пользователь с admin.user.without_subdivision видит оба подразделения.
	globalMgr := &platform.AuthContext{
		UserID:        3,
		SubdivisionID: &sub1,
		Permissions: map[string]bool{
			"crm.contractor.view":            true,
			"crm.contractor.view_all":        true,
			"crm.application.view":           true,
			"admin.user.without_subdivision": true,
		},
	}
	_, globalTotal, _ := svc.SearchContractors(ctx, globalMgr, "", 50, 0)
	globalEvents, _ := svc.ListCalendarEvents(ctx, globalMgr, now.Add(-time.Hour), now.Add(2*time.Hour), nil, nil)
	if globalTotal != 2 || len(globalEvents) != 2 {
		t.Fatalf("expected global view 2/2, got contractors=%d events=%d", globalTotal, len(globalEvents))
	}
}

func TestExecuteTaskContractsAndAmountActsReport(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	sub1 := int64(1)
	users := &fakeUsers{
		users: map[int64]UserBrief{
			10: {ID: 10, FullName: "Иванов И.И.", SubdivisionID: &sub1, IsEngineer: true, Active: true},
		},
	}
	svc := NewService(repo, users, nil, nil, Config{Clock: func() time.Time { return now }}, nil)
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}

	_, _ = svc.SaveSubdivision(ctx, admin, 0, "Главный офис", "Москва", []SubdivisionPhone{{Phone: "89990001122", IsDuty: false}})
	c, _ := svc.CreateContractor(ctx, admin, SaveContractorInput{Name: "Клиент Договор"})
	addr, _ := svc.CreateAddress(ctx, admin, ContractorAddress{ContractorID: c.ID, Address: "ул. Мира, 5"})

	if _, err := svc.CreateContract(ctx, admin, MaintenanceContract{
		ContractorID: c.ID, AddressID: addr.ID, Number: "BAD-1",
		Category: "unknown", ValidFrom: now, ValidTo: now.AddDate(1, 0, 0),
	}); err == nil {
		t.Fatal("expected validation error for unknown contract category")
	}

	contractApp, err := svc.CreateApplication(ctx, admin, Application{
		ContractorID:     c.ID,
		AddressID:        addr.ID,
		EngineerID:       10,
		PlanDatetimeFrom: now,
		PlanDatetimeTo:   now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create contract app: %v", err)
	}
	contract, err := svc.CreateContract(ctx, admin, MaintenanceContract{
		ContractorID:  c.ID,
		AddressID:     addr.ID,
		ApplicationID: &contractApp.ID,
		Number:        "TO-2026-1",
		Category:      ContractCategoryPlus,
		Price:         12000,
		ValidFrom:     now.AddDate(0, 0, -5),
		ValidTo:       now.AddDate(1, 0, 0),
		Equipments:    []ContractEquipment{{EquipmentID: 1, EquipmentName: "Baxi Luna", SerialNumber: "SN-99"}},
	})
	if err != nil {
		t.Fatalf("create contract: %v", err)
	}
	if _, err := svc.SaveCertificate(ctx, admin, 0, Certificate{
		ContractorID:          c.ID,
		AddressID:             addr.ID,
		EngineerID:            10,
		MaintenanceContractID: &contract.ID,
		WorkDate:              now,
		TotalAmount:           5000,
		PaidAmount:            4000,
		CostOfParts:           1500,
		ReceiptNumber:         "KVT-77",
		SpecialNotes:          "Проверить клапан",
	}); err != nil {
		t.Fatalf("save certificate: %v", err)
	}

	report, err := svc.AmountActsReport(ctx, admin, now.AddDate(0, -1, 0), now.Add(time.Hour), []int64{10}, []int64{sub1})
	if err != nil || len(report) != 1 {
		t.Fatalf("expected 1 report row, got %+v err=%v", report, err)
	}
	if report[0].ContractsAmount != 12000 || report[0].DebtAmount != 1000 {
		t.Fatalf("unexpected report metrics: %+v", report[0])
	}

	task, err := svc.CreateTask(ctx, admin, Task{Title: "Обзвон договора", AssignedUserID: &admin.UserID})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	customNext := now.Add(24 * time.Hour)
	doneTask, err := svc.ExecuteTask(ctx, admin, task.ID, ExecuteTaskInput{
		TaskResult:        TaskResultNoAnswer,
		Note:              "Абонент не ответил",
		NewPlanDatetimeTo: &customNext,
	})
	if err != nil || doneTask.Status != TaskStatusNoAnswer || doneTask.TaskResult != TaskResultNoAnswer || doneTask.ExecutionDatetime == nil {
		t.Fatalf("expected executed no_answer task, got %+v err=%v", doneTask, err)
	}
	transferredCnt, err := repo.CountUserActivePoolTasks(ctx, admin.UserID, true)
	if err != nil || transferredCnt != 1 {
		t.Fatalf("expected 1 transferred followup task with prev_task_id, got %d err=%v", transferredCnt, err)
	}
}

func TestMTSVPBXNestedWebhookCardTransferMP3AndCurrentCall(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	var gotTransferMethod, gotTransferPath, gotAuthHeader string
	mtsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("X-AUTH-TOKEN")
		switch {
		case strings.Contains(r.URL.Path, "/BlindTransfer"):
			gotTransferMethod = r.Method
			gotTransferPath = r.URL.RequestURI()
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/callRecording/mp3/"):
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("MP3DATA"))
		case strings.HasSuffix(r.URL.Path, "/calls"):
			_ = json.NewEncoder(w).Encode([]MTSVPBXPayload{{
				CallID:             "vpbx-1",
				State:              "Active",
				StartTime:          now.UnixMilli(),
				RemotePartyAddress: "tel:+79991234567",
				ExtTrackingID:      "track-42",
			}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mtsSrv.Close()

	svc := NewService(repo, nil, platform.NewInMemoryEventBus(), nil, Config{
		MTSWebhookSecret: "secret-hdr",
		MTSBaseURL:       mtsSrv.URL,
		MTSAPIToken:      "api-secret",
		Clock:            func() time.Time { return now },
	}, nil)
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 7, IsSuperAdmin: true}

	_, _ = svc.SaveSipAccount(ctx, admin, SipAccount{
		Name: "Оператор", Login: "101", Server: "sip.local", AbonentID: "abon-101", UserID: &admin.UserID, Active: true,
	})
	c1, _ := svc.CreateContractor(ctx, admin, SaveContractorInput{
		Name: "Клиент А", Phones: []ContractorPhone{{Phone: "9991234567"}},
	})
	c2, _ := svc.CreateContractor(ctx, admin, SaveContractorInput{
		Name: "Клиент Б", Phones: []ContractorPhone{{Phone: "9991234567"}},
	})

	// CHECK_ALIVE
	if rec, err := svc.HandleMTSWebhook(ctx, "secret-hdr", []byte(`{"eventType":"CHECK_ALIVE"}`)); err != nil || rec != nil {
		t.Fatalf("expected CHECK_ALIVE nil rec, got rec=%v err=%v", rec, err)
	}

	// Неоднозначный номер (принадлежит обоим контрагентам): подрядчик не должен выбираться автоматически.
	rawIn := []byte(`{"eventType":"CALL_RECEIVED","abonentId":"abon-101","payload":{"callId":"vpbx-1","state":"Alerting","startTime":1763035200000,"callDirection":"Terminator","remotePartyAddress":"tel:+79991234567","extTrackingId":"track-42"}}`)
	rec, err := svc.HandleMTSWebhook(ctx, "secret-hdr", rawIn)
	if err != nil {
		t.Fatalf("CALL_RECEIVED error: %v", err)
	}
	if rec.ContractorID != nil || rec.UserID == nil || *rec.UserID != 7 {
		t.Fatalf("expected user 7 & nil ambiguous contractor, got user=%v contractor=%v", rec.UserID, rec.ContractorID)
	}

	// Карточка звонка закрепляет c1 и отвязывает телефон от c2.
	updatedCall, err := svc.UpdateCallCard(ctx, admin, rec.ID, UpdateCallCardInput{
		ContractorID:     &c1.ID,
		TypeOfAppeal:     CallAppealServiceRequest,
		Comment:          "Вызвать мастера",
		DetachFromOthers: true,
	})
	if err != nil || updatedCall.ContractorID == nil || *updatedCall.ContractorID != c1.ID {
		t.Fatalf("update call card failed: %+v err=%v", updatedCall, err)
	}
	c2After, _ := repo.GetContractor(ctx, c2.ID)
	if len(c2After.Phones) != 0 {
		t.Fatalf("expected phone detached from c2, remaining: %+v", c2After.Phones)
	}

	if err := svc.BlindTransfer(ctx, admin, "vpbx-1", "8 (903) 000-11-22"); err != nil {
		t.Fatalf("blind transfer: %v", err)
	}
	if gotTransferMethod != http.MethodPut || !strings.Contains(gotTransferPath, "/user/abon-101/calls/vpbx-1/BlindTransfer?address=89030001122") || gotAuthHeader != "api-secret" {
		t.Fatalf("unexpected VPBX transfer call: %s %s hdr=%s", gotTransferMethod, gotTransferPath, gotAuthHeader)
	}

	ct, mp3, err := svc.GetCallRecordMP3(ctx, rec.ID)
	if err != nil || ct != "audio/mpeg" || string(mp3) != "MP3DATA" {
		t.Fatalf("expected MP3DATA audio/mpeg, got %q %q err=%v", ct, string(mp3), err)
	}
	current, err := svc.GetCurrentCall(ctx, admin)
	if err != nil || len(current) != 1 || current[0].ExternalCallID != "vpbx-1" {
		t.Fatalf("expected 1 active call from MTS, got %+v err=%v", current, err)
	}
}

func TestLegacyMobileRoutesAndCleanupOldNotifications(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	svc := NewService(repo, nil, nil, nil, Config{Clock: func() time.Time { return now }}, nil)
	ctx := context.Background()

	// Проверка CleanupOldNotifications (> 30 дней прочитанные).
	repo.notifs = []Notification{
		{ID: 1, UserID: 1, IsRead: true, CreatedAt: now.AddDate(0, 0, -35)},
		{ID: 2, UserID: 1, IsRead: true, CreatedAt: now.AddDate(0, 0, -5)},
		{ID: 3, UserID: 1, IsRead: false, CreatedAt: now.AddDate(0, 0, -60)},
	}
	deleted, err := svc.CleanupOldNotifications(ctx)
	if err != nil || deleted != 1 || len(repo.notifs) != 2 {
		t.Fatalf("expected 1 old read notif deleted, got %d remaining=%d err=%v", deleted, len(repo.notifs), err)
	}

	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}
	h := NewHandler(svc, staticAuthorizer{ac: admin}, nil)
	r := chi.NewRouter()
	h.Mount(r)

	for _, path := range []string{
		"/api/doc/maintenance-contract",
		"/api/ref/manufacturer",
		"/api/enum/maintenance-contract-category",
		"/api/enum/payment-option",
		"/api/v1/crm/enums",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s expected 200, got %d: %s", path, rec.Code, rec.Body.String())
		}
	}

	c, _ := svc.CreateContractor(ctx, admin, SaveContractorInput{Name: "Мобильный Клиент"})
	addr, _ := svc.CreateAddress(ctx, admin, ContractorAddress{ContractorID: c.ID, Address: "ул. Мобильная, 1"})
	eq, _ := svc.CreateEquipment(ctx, admin, "Газовый котёл", "Luna", "Baxi")
	_, _ = svc.CreateApplication(ctx, admin, Application{
		ContractorID: c.ID, AddressID: addr.ID, EngineerID: 55,
		PlanDatetimeFrom: now, PlanDatetimeTo: now.Add(time.Hour),
	})

	attachBody := fmt.Sprintf(`{"equipment_id":%d,"serial_number":"SN-M1"}`, eq.ID)
	attachReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/ref/equipment/update-by-address/%d", addr.ID), bytes.NewBufferString(attachBody))
	attachRec := httptest.NewRecorder()
	r.ServeHTTP(attachRec, attachReq)
	if attachRec.Code != http.StatusOK {
		t.Fatalf("attach equipment by address failed: %d %s", attachRec.Code, attachRec.Body.String())
	}

	// Инженер 55 имеет заявку в пределах ±3 дней — selects должен вернуть 200; инженер 77 без заявки — 403!
	hEng55 := NewHandler(svc, staticAuthorizer{ac: &platform.AuthContext{
		UserID: 55,
		Permissions: map[string]bool{
			"crm.contractor.view":  true,
			"crm.equipment.view":   true,
			"crm.application.view": true,
		},
	}}, nil)
	r55 := chi.NewRouter()
	hEng55.Mount(r55)
	for _, path := range []string{
		fmt.Sprintf("/api/ref/contractor/selects/%d", c.ID),
		fmt.Sprintf("/api/ref/equipment/select-by-address-id/%d", addr.ID),
		"/api/doc/application-for-technical-work?date=2026-11-13&mode=offline",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		r55.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("eng55 GET %s expected 200, got %d: %s", path, rec.Code, rec.Body.String())
		}
	}

	hEng77 := NewHandler(svc, staticAuthorizer{ac: &platform.AuthContext{
		UserID:      77,
		Permissions: map[string]bool{"crm.contractor.view": true},
	}}, nil)
	r77 := chi.NewRouter()
	hEng77.Mount(r77)
	reqDenied := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/ref/contractor/selects/%d", c.ID), nil)
	recDenied := httptest.NewRecorder()
	r77.ServeHTTP(recDenied, reqDenied)
	if recDenied.Code != http.StatusForbidden {
		t.Fatalf("eng77 expected 403 on contractor selects outside ±3d, got %d", recDenied.Code)
	}
}
