package crm

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

// --- Расширения memRepo для новых методов Repository ---

func (m *memRepo) GetSubdivision(_ context.Context, id int64) (*Subdivision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.subdivisions[id]; ok {
		cp := *s
		return &cp, nil
	}
	return nil, nil
}

func (m *memRepo) SoftDeleteSubdivision(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.subdivisions, id)
	return nil
}

func (m *memRepo) ListSipUsersSelect(context.Context) ([]SipUserSelectOption, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []SipUserSelectOption
	for _, sip := range m.sips {
		if sip.UserID != nil && sip.Active {
			out = append(out, SipUserSelectOption{
				ID:        sip.ID,
				AbonentID: sip.AbonentID,
				Name:      sip.Name,
				UserID:    sip.UserID,
			})
		}
	}
	return out, nil
}

func (m *memRepo) UpsertLegalEntity(_ context.Context, le *LegalEntity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *le
	cp.UpdatedAt = time.Now()
	m.legalEntities[le.ContractorID] = &cp
	if c, ok := m.contractors[le.ContractorID]; ok {
		c.IsLegalEntity = true
	}
	return nil
}

func (m *memRepo) FindAddressByText(_ context.Context, contractorID int64, address string) (*ContractorAddress, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	norm := strings.ToLower(strings.TrimSpace(address))
	for _, a := range m.addresses {
		if !a.Deleted && a.ContractorID == contractorID && strings.ToLower(strings.TrimSpace(a.Address)) == norm {
			cp := *a
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memRepo) ListTrafficSources(_ context.Context, activeOnly bool) ([]TrafficSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []TrafficSource
	for _, ts := range m.trafficSources {
		if activeOnly && !ts.Active {
			continue
		}
		out = append(out, *ts)
	}
	slices.SortFunc(out, func(a, b TrafficSource) int { return a.Sort - b.Sort })
	return out, nil
}

func (m *memRepo) GetTrafficSource(_ context.Context, id int64) (*TrafficSource, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ts, ok := m.trafficSources[id]; ok {
		cp := *ts
		return &cp, nil
	}
	return nil, nil
}

func (m *memRepo) CreateTrafficSource(_ context.Context, ts *TrafficSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	ts.ID = m.id()
	cp := *ts
	m.trafficSources[ts.ID] = &cp
	return nil
}

func (m *memRepo) UpdateTrafficSource(_ context.Context, ts *TrafficSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *ts
	m.trafficSources[ts.ID] = &cp
	return nil
}

func (m *memRepo) DeleteTrafficSource(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.trafficSources, id)
	return nil
}

func (m *memRepo) CountRecentContractorApplications(_ context.Context, contractorID int64, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cnt := 0
	for _, a := range m.apps {
		if !a.Deleted && a.ContractorID == contractorID && a.Status != StatusCancelled && a.Status != StatusFailed && !a.PlanDatetimeFrom.Before(since) {
			cnt++
		}
	}
	return cnt, nil
}

func (m *memRepo) HasRecentAppLimitTask(_ context.Context, contractorID int64, since time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, tk := range m.tasks {
		if !tk.Deleted && tk.TaskType == TaskTypeAppLimit && tk.ContractorID != nil && *tk.ContractorID == contractorID && !tk.CreatedAt.Before(since) {
			return true, nil
		}
	}
	return false, nil
}

func (m *memRepo) FindMatchingContractForCertificate(_ context.Context, contractorID, addressID int64, workDate time.Time) (*int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.contracts {
		if c.Deleted || c.ContractorID != contractorID || c.AddressID != addressID || c.Status != StatusActive {
			continue
		}
		if !workDate.Before(c.ValidFrom.AddDate(0, 0, -45)) && !workDate.After(c.ValidTo) {
			id := c.ID
			return &id, nil
		}
	}
	return nil, nil
}

func (m *memRepo) ListMissedCallsToSync(_ context.Context, since time.Time) ([]CallRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []CallRecord
	for _, c := range m.calls {
		if (c.Direction == "in" || c.Direction == "inbound") &&
			(c.Status == "missed" || c.Status == "failed") &&
			!c.StartedAt.Before(since) {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (m *memRepo) AddToMissCallPool(_ context.Context, callID, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.missCalls {
		if item.CallID == callID && item.UserID == userID {
			return nil
		}
	}
	id := m.id()
	var caller, callee string
	var started time.Time
	var cid *int64
	for _, c := range m.calls {
		if c.ID == callID {
			caller = c.CallerPhone
			callee = c.CalleePhone
			started = c.StartedAt
			cid = c.ContractorID
			break
		}
	}
	m.missCalls[id] = &MissCallPoolItem{
		ID:           id,
		CallID:       callID,
		UserID:       userID,
		CallerPhone:  caller,
		CalleePhone:  callee,
		StartedAt:    started,
		ContractorID: cid,
		CreatedAt:    time.Now(),
	}
	return nil
}

func (m *memRepo) ListMissCallPool(_ context.Context, userID *int64, onlyUnviewed bool, _ int) ([]MissCallPoolItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []MissCallPoolItem
	for _, item := range m.missCalls {
		if userID != nil && item.UserID != *userID {
			continue
		}
		if onlyUnviewed && item.IsViewed {
			continue
		}
		cp := *item
		if cp.ContractorID != nil {
			if c, ok := m.contractors[*cp.ContractorID]; ok {
				cp.ContractorName = c.Name
			}
		}
		out = append(out, cp)
	}
	return out, nil
}

func (m *memRepo) MarkMissCallViewed(_ context.Context, callID int64, userID *int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.missCalls {
		if item.CallID == callID && (userID == nil || item.UserID == *userID || item.UserID == 0) {
			item.IsViewed = true
		}
	}
	return nil
}

func (m *memRepo) MarkMissCallsViewedByPhone(_ context.Context, phone10 string, userID *int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range m.missCalls {
		p10, _ := platform.ParsePhone10(item.CallerPhone)
		if p10 == phone10 && (userID == nil || item.UserID == *userID || item.UserID == 0) {
			item.IsViewed = true
		}
	}
	return nil
}

func (m *memRepo) ListAnalyticQuestions(_ context.Context, activeOnly bool) ([]AnalyticQuestion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []AnalyticQuestion
	for _, q := range m.analyticQuestions {
		if activeOnly && !q.Active {
			continue
		}
		out = append(out, *q)
	}
	return out, nil
}

func (m *memRepo) GetAnalyticQuestion(_ context.Context, id int64) (*AnalyticQuestion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if q, ok := m.analyticQuestions[id]; ok {
		cp := *q
		return &cp, nil
	}
	return nil, nil
}

func (m *memRepo) CreateAnalyticQuestion(_ context.Context, q *AnalyticQuestion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	q.ID = m.id()
	cp := *q
	m.analyticQuestions[q.ID] = &cp
	return nil
}

func (m *memRepo) UpdateAnalyticQuestion(_ context.Context, q *AnalyticQuestion) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *q
	m.analyticQuestions[q.ID] = &cp
	return nil
}

func (m *memRepo) IsAnalyticOptionUsed(_ context.Context, questionID, optionID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.analytics {
		if a.Deleted {
			continue
		}
		for _, ans := range a.Answers {
			if ans.QuestionID == questionID && slices.Contains(ans.OptionIDs, optionID) {
				return true, nil
			}
		}
	}
	return false, nil
}

func (m *memRepo) ListAnalytics(_ context.Context, f AnalyticFilter) ([]Analytic, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Analytic
	for _, a := range m.analytics {
		if a.Deleted {
			continue
		}
		if f.CertificateID != nil && a.CertificateID != *f.CertificateID {
			continue
		}
		if f.Rating != nil && a.Rating != *f.Rating {
			continue
		}
		if cert, ok := m.certs[a.CertificateID]; ok {
			if f.ContractorID != nil && cert.ContractorID != *f.ContractorID {
				continue
			}
			if f.EngineerID != nil && cert.EngineerID != *f.EngineerID {
				continue
			}
		}
		out = append(out, *a)
	}
	return out, nil
}

func (m *memRepo) GetAnalytic(_ context.Context, id int64) (*Analytic, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.analytics[id]; ok && !a.Deleted {
		cp := *a
		return &cp, nil
	}
	return nil, nil
}

func (m *memRepo) GetAnalyticByCertificate(_ context.Context, certID int64) (*Analytic, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.analytics {
		if !a.Deleted && a.CertificateID == certID {
			cp := *a
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memRepo) UpsertAnalytic(_ context.Context, a *Analytic) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.analytics {
		if !existing.Deleted && existing.CertificateID == a.CertificateID {
			a.ID = existing.ID
			break
		}
	}
	if a.ID == 0 {
		a.ID = m.id()
		a.CreatedAt = time.Now()
	}
	a.UpdatedAt = time.Now()
	cp := *a
	m.analytics[a.ID] = &cp
	if cert, ok := m.certs[a.CertificateID]; ok {
		cert.AnalyticID = &a.ID
		cert.AnalyticRating = &a.Rating
	}
	return nil
}

func (m *memRepo) UpdateAnalyticRating(_ context.Context, id int64, rating int, comment string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.analytics[id]; ok {
		a.Rating = rating
		if comment != "" {
			a.Comment = comment
		}
		if cert, ok := m.certs[a.CertificateID]; ok {
			cert.AnalyticRating = &rating
		}
	}
	return nil
}

func (m *memRepo) ListBanners(_ context.Context, activeOnly bool) ([]Banner, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Banner
	for _, b := range m.banners {
		if activeOnly && !b.Active {
			continue
		}
		out = append(out, *b)
	}
	return out, nil
}

func (m *memRepo) GetBanner(_ context.Context, id int64) (*Banner, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.banners[id]; ok {
		cp := *b
		return &cp, nil
	}
	return nil, nil
}

func (m *memRepo) CreateBanner(_ context.Context, b *Banner) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b.ID = m.id()
	cp := *b
	m.banners[b.ID] = &cp
	return nil
}

func (m *memRepo) UpdateBanner(_ context.Context, b *Banner) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *b
	m.banners[b.ID] = &cp
	return nil
}

func (m *memRepo) DeleteBanner(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.banners, id)
	return nil
}

func (m *memRepo) ListAppSettings(context.Context) ([]AppSetting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []AppSetting
	for _, s := range m.appSettings {
		out = append(out, *s)
	}
	return out, nil
}

func (m *memRepo) GetAppSetting(_ context.Context, key string) (*AppSetting, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.appSettings[key]; ok {
		cp := *s
		return &cp, nil
	}
	return nil, nil
}

func (m *memRepo) SaveAppSetting(_ context.Context, s *AppSetting) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	cp.UpdatedAt = time.Now()
	m.appSettings[s.Key] = &cp
	return nil
}

func (m *memRepo) AmountActsReportFiltered(_ context.Context, f ReportFilter) ([]AmountActsReportRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := map[int64]*AmountActsReportRow{}
	for _, c := range m.certs {
		if c.Deleted || c.WorkDate.Before(f.From) || c.WorkDate.After(f.To) {
			continue
		}
		if f.EngineerID != nil && c.EngineerID != *f.EngineerID {
			continue
		}
		if len(f.UserIDs) > 0 && !slices.Contains(f.UserIDs, c.EngineerID) {
			continue
		}
		if len(f.PaymentMethods) > 0 && !slices.Contains(f.PaymentMethods, c.PaymentMethod) {
			continue
		}
		r := rows[c.EngineerID]
		if r == nil {
			r = &AmountActsReportRow{EngineerID: c.EngineerID, ByPaymentOption: map[string]float64{}}
			rows[c.EngineerID] = r
		}
		r.CertificatesCount++
		r.TotalAmount += c.TotalAmount
		r.SumTotal += c.TotalAmount
		r.PaidAmount += c.PaidAmount
		r.ByPaymentOption[c.PaymentMethod] += c.TotalAmount
	}
	var out []AmountActsReportRow
	for _, r := range rows {
		out = append(out, *r)
	}
	return out, nil
}

func (m *memRepo) ExpiredTasksReportFiltered(ctx context.Context, now time.Time, _, _ *time.Time, userID *int64) ([]ExpiredTaskReportRow, error) {
	return m.ExpiredTasksReport(ctx, now, userID)
}

func (m *memRepo) SubdivisionSumsReport(_ context.Context, _ ReportFilter) ([]SubdivisionSumReportRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sums := map[int64]*SubdivisionSumReportRow{}
	for _, s := range m.subdivisions {
		sums[s.ID] = &SubdivisionSumReportRow{
			SubdivisionID:   s.ID,
			SubdivisionName: s.Name,
			ByPaymentOption: map[string]float64{},
		}
	}
	for _, a := range m.apps {
		if !a.Deleted && a.SubdivisionID != nil {
			if row, ok := sums[*a.SubdivisionID]; ok {
				row.PlannedSumTotal += a.PlannedSum
			}
		}
	}
	for _, c := range m.certs {
		if !c.Deleted {
			var subID *int64
			if ctr, ok := m.contractors[c.ContractorID]; ok {
				subID = ctr.SubdivisionID
			}
			if subID != nil {
				if row, ok := sums[*subID]; ok {
					row.SumTotal += c.TotalAmount
					row.ByPaymentOption[c.PaymentMethod] += c.TotalAmount
				}
			}
		}
	}
	var out []SubdivisionSumReportRow
	for _, r := range sums {
		out = append(out, *r)
	}
	return out, nil
}

func (m *memRepo) AmountSparePartsReport(_ context.Context, f ReportFilter) (*AmountSparePartsReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rep := &AmountSparePartsReport{}
	for _, c := range m.certs {
		if c.Deleted || c.WorkDate.Before(f.From) || c.WorkDate.After(f.To) {
			continue
		}
		rep.TotalCostOfParts += c.CostOfParts
		rep.CertificatesCount++
	}
	return rep, nil
}

func (m *memRepo) CallConversionReport(_ context.Context, f ReportFilter) (*CallConversionReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	counts := map[string]map[int]int{}
	for _, c := range m.calls {
		if c.StartedAt.Before(f.From) || c.StartedAt.After(f.To) || c.TypeOfAppeal == "" {
			continue
		}
		if counts[c.TypeOfAppeal] == nil {
			counts[c.TypeOfAppeal] = map[int]int{}
		}
		counts[c.TypeOfAppeal][c.CallResult]++
	}
	return BuildCallConversionMatrix(counts), nil
}

func (m *memRepo) ContractorsReport(_ context.Context, _ ReportFilter) ([]ContractorReportRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ContractorReportRow
	for _, c := range m.contractors {
		if c.Deleted {
			continue
		}
		row := ContractorReportRow{ContractorID: c.ID, Name: c.Name}
		for _, cert := range m.certs {
			if !cert.Deleted && cert.ContractorID == c.ID {
				row.CertificatesCount++
				row.FactSum += cert.TotalAmount
				row.CostOfParts += cert.CostOfParts
			}
		}
		out = append(out, row)
	}
	return out, nil
}

func (m *memRepo) EngineersReport(_ context.Context, f ReportFilter) ([]EngineerReportRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	byEng := map[int64]*EngineerReportRow{}
	ensure := func(uid int64) *EngineerReportRow {
		if byEng[uid] == nil {
			byEng[uid] = &EngineerReportRow{EngineerID: uid}
		}
		return byEng[uid]
	}
	for _, a := range m.apps {
		if !a.Deleted && !a.PlanDatetimeFrom.Before(f.From) && !a.PlanDatetimeFrom.After(f.To) {
			ensure(a.EngineerID).PlannedSum += a.PlannedSum
		}
	}
	for _, c := range m.certs {
		if c.Deleted || c.WorkDate.Before(f.From) || c.WorkDate.After(f.To) {
			continue
		}
		r := ensure(c.EngineerID)
		r.CertificatesCount++
		r.FactSum += c.TotalAmount
		if c.TotalAmount == 0 {
			r.ZeroSumCount++
		}
	}
	var out []EngineerReportRow
	for _, r := range byEng {
		if r.CertificatesCount > 0 {
			r.ZeroPercent = float64(r.ZeroSumCount) * 100 / float64(r.CertificatesCount)
		}
		out = append(out, *r)
	}
	return out, nil
}

func (m *memRepo) TrafficSourcesReport(_ context.Context, f ReportFilter) (*TrafficSourceReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rep := &TrafficSourceReport{}
	for _, ts := range m.trafficSources {
		id := ts.ID
		row := TrafficSourceReportRow{TrafficSourceID: &id, Name: ts.Name}
		for _, c := range m.calls {
			if c.TrafficSourceID != nil && *c.TrafficSourceID == ts.ID {
				row.Calls++
			}
		}
		for _, a := range m.apps {
			if !a.Deleted && a.TrafficSourceID != nil && *a.TrafficSourceID == ts.ID {
				row.Applications++
				if a.IsComplete {
					row.ClosedApplications++
				}
			}
		}
		if row.Calls > 0 {
			row.ApplicationConversion = float64(row.Applications) * 100 / float64(row.Calls)
			row.ClosedApplicationConversion = float64(row.ClosedApplications) * 100 / float64(row.Calls)
		}
		rep.Rows = append(rep.Rows, row)
		rep.SummaryRow.Calls += row.Calls
		rep.SummaryRow.Applications += row.Applications
		rep.SummaryRow.ClosedApplications += row.ClosedApplications
	}
	return rep, nil
}

// --- Тесты новых возможностей из CRM-master ---

func TestLegalEntityAddressDuplicateAndExtendedContractRenew(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo, nil, nil, nil, Config{}, nil)
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}

	c, err := svc.CreateContractor(ctx, admin, SaveContractorInput{
		Name:          `ООО "ТеплоДом"`,
		IsLegalEntity: true,
		Phones:        []ContractorPhone{{Phone: "+79991112233"}},
		LegalEntity: &LegalEntity{
			Name: `ООО "ТеплоДом"`,
			INN:  "7701234567",
			KPP:  "770101001",
		},
	})
	if err != nil {
		t.Fatalf("create legal entity contractor failed: %v", err)
	}
	if !c.IsLegalEntity || c.LegalEntity == nil || c.LegalEntity.INN != "7701234567" {
		t.Fatalf("unexpected legal entity: %+v", c.LegalEntity)
	}

	// Поиск с фильтром OnlyLegal и по ИНН.
	found, _, err := svc.SearchContractorsFiltered(ctx, admin, ContractorFilter{
		Query:     "770123",
		OnlyLegal: true,
	})
	if err != nil || len(found) != 1 {
		t.Fatalf("expected 1 legal contractor by INN, got %d err=%v", len(found), err)
	}
	inds, _, _ := svc.SearchContractorsFiltered(ctx, admin, ContractorFilter{OnlyIndividuals: true})
	if len(inds) != 0 {
		t.Fatalf("expected 0 individuals, got %d", len(inds))
	}

	// Проверка защиты от дубликата адреса.
	addr, err := svc.CreateAddress(ctx, admin, ContractorAddress{
		ContractorID: c.ID,
		Address:      "г. Москва, ул. Строителей, 10",
	})
	if err != nil {
		t.Fatalf("create address failed: %v", err)
	}
	if _, err := svc.CreateAddress(ctx, admin, ContractorAddress{
		ContractorID: c.ID,
		Address:      "г. Москва, ул. Строителей, 10",
	}); err == nil {
		t.Fatalf("expected duplicate address error")
	}

	// Договор с новой категорией (5..10) + продление + HTML-печать.
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	ct, err := svc.CreateContract(ctx, admin, MaintenanceContract{
		ContractorID: c.ID,
		AddressID:    addr.ID,
		Number:       "ТО-2026-01",
		Category:     ContractCategoryOptimal,
		Price:        18500,
		Status:       StatusActive,
		ValidFrom:    now,
		ValidTo:      now.AddDate(1, 0, 0),
	})
	if err != nil {
		t.Fatalf("create extended contract failed: %v", err)
	}
	renewed, err := svc.RenewContract(ctx, admin, ct.ID, RenewContractInput{})
	if err != nil {
		t.Fatalf("renew contract failed: %v", err)
	}
	if renewed.ValidFrom.Year() != 2027 || renewed.Category != ContractCategoryOptimal {
		t.Fatalf("unexpected renewed contract: %+v", renewed)
	}
	html, err := svc.RenderContractPrintHTML(ctx, admin, ct.ID)
	if err != nil || !strings.Contains(html, "ДОГОВОР") || !strings.Contains(html, "7701234567") {
		t.Fatalf("expected legal entity contract print HTML, got len=%d err=%v", len(html), err)
	}
}

func TestApplicationCoExecutorAppLimitAndEngineerCertPolicy(t *testing.T) {
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	users := &fakeUsers{users: map[int64]UserBrief{
		1:  {ID: 1, FullName: "Админ", Active: true},
		10: {ID: 10, FullName: "Инженер Основной", IsEngineer: true, Active: true},
		20: {ID: 20, FullName: "Инженер Напарник", IsEngineer: true, Active: true},
	}}
	svc := NewService(repo, users, nil, nil, Config{Clock: func() time.Time { return now }}, nil)
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}
	eng := &platform.AuthContext{
		UserID: 10,
		Permissions: map[string]bool{
			"crm.contractor.view":    true,
			"crm.application.view":   true,
			"crm.certificate.view":   true,
			"crm.certificate.create": true,
			"crm.certificate.update": true,
		},
	}

	c, _ := svc.CreateContractor(ctx, admin, SaveContractorInput{
		Name:   "Петров П.П.",
		Phones: []ContractorPhone{{Phone: "+79990001122"}},
	})
	addr, _ := svc.CreateAddress(ctx, admin, ContractorAddress{ContractorID: c.ID, Address: "ул. Садовая, 1"})
	// Активный договор ТО в окне -45 дней для проверки автопривязки к акту.
	_, err := svc.CreateContract(ctx, admin, MaintenanceContract{
		ContractorID: c.ID,
		AddressID:    addr.ID,
		Number:       "ТО-2026-02",
		Category:     ContractCategoryMaximum,
		Status:       StatusActive,
		ValidFrom:    now.AddDate(0, 0, 10),
		ValidTo:      now.AddDate(1, 0, 10),
	})
	if err != nil {
		t.Fatalf("create contract failed: %v", err)
	}

	// Первая заявка с соисполнителем 20 и плановой суммой 7500.
	winFrom := now.Add(30 * time.Minute)
	winTo := now.Add(2 * time.Hour)
	app1, err := svc.CreateApplication(ctx, admin, Application{
		ContractorID:     c.ID,
		AddressID:        addr.ID,
		EngineerID:       10,
		ExecutorIDs:      []int64{20},
		PlanDatetimeFrom: now.Add(time.Hour),
		PlanDatetimeTo:   now.Add(2 * time.Hour),
		WorkWindowFrom:   &winFrom,
		WorkWindowTo:     &winTo,
		PlannedSum:       7500,
	})
	if err != nil {
		t.Fatalf("create app1 failed: %v", err)
	}

	// Напарник 20 не может получить пересекающуюся заявку.
	if _, err := svc.CreateApplication(ctx, admin, Application{
		ContractorID:     c.ID,
		AddressID:        addr.ID,
		EngineerID:       20,
		PlanDatetimeFrom: now.Add(90 * time.Minute),
		PlanDatetimeTo:   now.Add(3 * time.Hour),
	}); err == nil {
		t.Fatalf("expected co-executor time overlap error")
	}

	// Создаём ещё 2 заявки контрагенту за 30 дней => должна появиться задача TaskTypeAppLimit!
	for i := 0; i < 2; i++ {
		_, err := svc.CreateApplication(ctx, admin, Application{
			ContractorID:     c.ID,
			AddressID:        addr.ID,
			EngineerID:       10,
			PlanDatetimeFrom: now.AddDate(0, 0, i+1),
			PlanDatetimeTo:   now.AddDate(0, 0, i+1).Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("create app #%d failed: %v", i+2, err)
		}
	}
	limitTasks, _ := svc.ListTasks(ctx, admin, TaskFilter{})
	hasAppLimit := false
	for _, tk := range limitTasks {
		if tk.TaskType == TaskTypeAppLimit && tk.ContractorID != nil && *tk.ContractorID == c.ID {
			hasAppLimit = true
		}
	}
	if !hasAppLimit {
		t.Fatalf("expected auto app_limit task when contractor reached 3 applications in 30 days")
	}

	// Инженер 10 не может создать акт без заявки.
	if _, err := svc.SaveCertificate(ctx, eng, 0, Certificate{
		ContractorID: c.ID,
		AddressID:    addr.ID,
		EngineerID:   10,
		WorkDate:     now,
	}); err == nil {
		t.Fatalf("expected engineer standalone certificate forbidden error")
	}

	// Инженер 10 не может создать акт на другой день от даты заявки.
	if _, err := svc.SaveCertificate(ctx, eng, 0, Certificate{
		ApplicationID: &app1.ID,
		ContractorID:  c.ID,
		AddressID:     addr.ID,
		EngineerID:    10,
		WorkDate:      now.AddDate(0, 0, 2),
	}); err == nil {
		t.Fatalf("expected engineer different-date certificate forbidden error")
	}

	// Инженер создаёт акт на день заявки с распределением суммы между исполнителями.
	cert, err := svc.SaveCertificate(ctx, eng, 0, Certificate{
		ApplicationID: &app1.ID,
		ContractorID:  c.ID,
		AddressID:     addr.ID,
		EngineerID:    10,
		WorkDate:      now,
		TotalAmount:   7500,
		WorkTypes:     []string{"ТО котла"},
		Results:       CertificateTelemetry{WorkingPressure: "1.8"},
		Executors: []CertificateExecutor{
			{UserID: 10, Sum: 5000},
			{UserID: 20, Sum: 2500},
		},
	})
	if err != nil {
		t.Fatalf("engineer create cert failed: %v", err)
	}
	if cert.MaintenanceContractID == nil {
		t.Fatalf("expected auto-linked MaintenanceContractID within -45d window")
	}
	// Несовпадение суммы соисполнителей должно отклоняться.
	if _, err := svc.SaveCertificate(ctx, admin, cert.ID, Certificate{
		ApplicationID: &app1.ID,
		ContractorID:  c.ID,
		AddressID:     addr.ID,
		EngineerID:    10,
		WorkDate:      now,
		TotalAmount:   7500,
		Executors:     []CertificateExecutor{{UserID: 10, Sum: 1000}},
	}); err == nil {
		t.Fatalf("expected executor sum mismatch validation error")
	}

	// Печать акта HTML.
	certHTML, err := svc.RenderCertificatePrintHTML(ctx, admin, cert.ID)
	if err != nil || !strings.Contains(certHTML, "АКТ ВЫПОЛНЕННЫХ РАБОТ") {
		t.Fatalf("expected certificate print HTML, got len=%d err=%v", len(certHTML), err)
	}
}

func TestCallsMissPoolAnalyticsBannersCloudPaymentsDaDataAndXLSX(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	dadataSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token test-dadata-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"suggestions": []map[string]any{
				{
					"value":              "г Москва, ул Тверская, д 1",
					"unrestricted_value": "125009, г Москва, ул Тверская, д 1",
					"data": map[string]any{
						"postal_code":      "125009",
						"city_with_type":   "Москва",
						"street_with_type": "Тверская",
						"house":            "1",
						"geo_lat":          "55.757",
						"geo_lon":          "37.615",
					},
				},
			},
		})
	}))
	defer dadataSrv.Close()

	svc := NewService(repo, nil, nil, nil, Config{
		DaDataAPIKey:           "test-dadata-key",
		DaDataBaseURL:          dadataSrv.URL,
		CloudPaymentsAPISecret: "cp-secret-key",
		Clock:                  func() time.Time { return now },
	}, nil)
	ctx := context.Background()
	admin := &platform.AuthContext{UserID: 1, IsSuperAdmin: true}

	// 1. Источник трафика, пропущенный звонок и условная валидация CallCard.
	ts, err := svc.CreateTrafficSource(ctx, admin, TrafficSource{
		Name: "Авито Москва", TrafficSourceTypeID: TrafficSourceTypeClassifieds, Active: true,
	})
	if err != nil {
		t.Fatalf("create traffic source failed: %v", err)
	}
	c, _ := svc.CreateContractor(ctx, admin, SaveContractorInput{
		Name: "Сидоров С.С.", Phones: []ContractorPhone{{Phone: "+79162223344"}},
	})
	addr, _ := svc.CreateAddress(ctx, admin, ContractorAddress{ContractorID: c.ID, Address: "г Москва, ул Тверская, д 1"})
	missed := &CallRecord{
		ExternalCallID: "mts-miss-1",
		Direction:      "in",
		Status:         "missed",
		CallerPhone:    "+79162223344",
		CalleePhone:    "+74951110000",
		ContractorID:   &c.ID,
		StartedAt:      now.Add(-5 * time.Minute),
	}
	_ = repo.UpsertCallByExternalID(ctx, missed)
	if added, err := svc.SyncMissedCallPool(ctx); err != nil || added == 0 {
		t.Fatalf("expected SyncMissedCallPool to add items, added=%d err=%v", added, err)
	}
	pool, err := svc.ListMissCallPool(ctx, admin, true, 50)
	if err != nil || len(pool) == 0 {
		t.Fatalf("expected unviewed miss call pool items, len=%d err=%v", len(pool), err)
	}

	// Для CallerTypeClient без traffic_source_id/call_result должен быть отказ.
	if _, err := svc.UpdateCallCard(ctx, admin, missed.ID, UpdateCallCardInput{
		CallerType:   CallerTypeClient,
		TypeOfAppeal: CallAppealServiceWork,
	}); err == nil {
		t.Fatalf("expected validation error for incomplete client call card")
	}
	// Полная карточка звонка проходит и помечает пропущенный просмотренным.
	updatedCall, err := svc.UpdateCallCard(ctx, admin, missed.ID, UpdateCallCardInput{
		CallerType:      CallerTypeClient,
		TypeOfAppeal:    CallAppealServiceWork,
		ContractorID:    &c.ID,
		TrafficSourceID: &ts.ID,
		CallResult:      CallResultCreateApplication,
		Comment:         "Оформлена заявка",
	})
	if err != nil || updatedCall.CallResult != CallResultCreateApplication {
		t.Fatalf("update call card failed: %v", err)
	}
	poolAfter, _ := svc.ListMissCallPool(ctx, admin, true, 50)
	if len(poolAfter) != 0 {
		t.Fatalf("expected miss call marked viewed after call card update, left=%d", len(poolAfter))
	}

	// 2. Аналитика контроля качества, защита варианта ответа и авто-задача call_feedback.
	q, err := svc.CreateAnalyticQuestion(ctx, admin, AnalyticQuestion{
		Name:   "Оцените качество визита",
		Active: true,
		Options: []AnalyticQuestionOption{
			{ID: 1, Name: "Отлично", Sort: 1},
			{ID: 2, Name: "Есть замечания", Sort: 2},
		},
	})
	if err != nil {
		t.Fatalf("create analytic question failed: %v", err)
	}
	cert, _ := svc.SaveCertificate(ctx, admin, 0, Certificate{
		ContractorID: c.ID, AddressID: addr.ID, EngineerID: 10, WorkDate: now, TotalAmount: 4200, CostOfParts: 700,
	})
	analytic, err := svc.SaveAnalyticByCertificate(ctx, admin, cert.ID, SaveAnalyticInput{
		Rating:     3,
		Comment:    "Мастер задержался",
		Answers:    []AnalyticAnswer{{QuestionID: q.ID, OptionIDs: []int64{2}}},
		CreateTask: true,
	})
	if err != nil || analytic.Rating != 3 {
		t.Fatalf("save analytic failed: %v", err)
	}
	// Вариант 2 используется в анкете — удалять его нельзя.
	if _, err := svc.UpdateAnalyticQuestion(ctx, admin, q.ID, AnalyticQuestion{
		Name:    q.Name,
		Active:  true,
		Options: []AnalyticQuestionOption{{ID: 1, Name: "Отлично"}},
	}); err == nil {
		t.Fatalf("expected error deleting used analytic question option")
	}
	// Рейтинг 3 (< 4) должен создать задачу call_feedback.
	tasks, _ := svc.ListTasks(ctx, admin, TaskFilter{})
	hasFeedbackTask := false
	for _, tk := range tasks {
		if tk.TaskType == TaskTypeCallFeedback {
			hasFeedbackTask = true
		}
	}
	if !hasFeedbackTask {
		t.Fatalf("expected call_feedback task for low rating")
	}

	// 3. Баннеры и app.banners_for_engineer.
	b, err := svc.CreateBanner(ctx, admin, Banner{Name: "Фото до работы", Text: "Сделайте фото котла до ТО", Active: true})
	if err != nil {
		t.Fatalf("create banner failed: %v", err)
	}
	cfgJSON, _ := json.Marshal(BannersForEngineerConfig{BeforeCreate: []int64{b.ID}, AfterCreate: []int64{b.ID}})
	if _, err := svc.SaveAppSetting(ctx, admin, "app.banners_for_engineer", "Баннеры инженера", cfgJSON); err != nil {
		t.Fatalf("save app setting failed: %v", err)
	}
	resolved, err := svc.GetBannersForEngineerResolved(ctx)
	if err != nil || len(resolved["before_create"]) != 1 || len(resolved["after_create"]) != 1 {
		t.Fatalf("unexpected resolved engineer banners: %+v err=%v", resolved, err)
	}

	// 4. DaData и CloudPayments HMAC.
	sug, err := svc.SuggestAddressesDaData(ctx, "Москва Тверская", 5)
	if err != nil || len(sug) != 1 || sug[0].City != "Москва" {
		t.Fatalf("DaData suggest failed: %+v err=%v", sug, err)
	}
	cpBody := []byte("TransactionId=9991&Amount=18500&AccountId=12&Status=Completed")
	mac := hmac.New(sha256.New, []byte("cp-secret-key"))
	mac.Write(cpBody)
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if err := svc.HandleCloudPaymentsWebhook(ctx, "pay", cpBody, sig); err != nil {
		t.Fatalf("valid CloudPayments webhook rejected: %v", err)
	}
	if err := svc.HandleCloudPaymentsWebhook(ctx, "pay", cpBody, "bad-hmac"); err == nil {
		t.Fatalf("expected invalid CloudPayments HMAC error")
	}

	// 5. Отчёты + XLSX экспорт через HTTP handler.
	conv, err := svc.CallConversionReport(ctx, ReportFilter{From: now.AddDate(0, 0, -1), To: now.AddDate(0, 0, 1)})
	if err != nil || conv.SummaryRow.Total == 0 {
		t.Fatalf("expected non-empty call conversion report: %+v err=%v", conv, err)
	}
	h := NewHandler(svc, staticAuthorizer{ac: admin}, nil)
	r := chi.NewRouter()
	h.Mount(r)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/crm/calls/export?format=xlsx", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "spreadsheetml.sheet") {
		t.Fatalf("expected xlsx download 200, got %d ct=%s", rec.Code, rec.Header().Get("Content-Type"))
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil || len(zr.File) < 5 {
		t.Fatalf("expected valid OOXML xlsx zip archive, err=%v files=%d", err, len(zr.File))
	}
}
