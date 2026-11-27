package crm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

func TestResolveEntityApplicationStructuredContext(t *testing.T) {
	now := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	repo := newMemRepo()
	users := &fakeUsers{
		users: map[int64]UserBrief{
			88: {ID: 88, FullName: "Лунгу Андрей Николаевич", IsEngineer: true, Active: true},
			99: {ID: 99, FullName: "Борисов Максим Алексеевич", IsEngineer: true, Active: true},
		},
	}
	svc := NewService(repo, users, nil, nil, Config{}, nil)

	admin := &platform.AuthContext{
		UserID:       1,
		Username:     "admin",
		IsSuperAdmin: true,
		Permissions:  map[string]bool{"crm.application.view": true, "crm.contractor.view": true},
	}

	contractor := &Contractor{
		ID:            13266,
		Name:          "АО КЭМЗ гостиница",
		IsLegalEntity: true,
		Note:          "Пропускной режим на КПП",
		Phones: []ContractorPhone{
			{ID: 41560, ContractorID: 13266, Phone: "4923293535", Note: "ресепшн"},
			{ID: 41561, ContractorID: 13266, Phone: "9807500149", Note: "Марина"},
		},
		Addresses: []ContractorAddress{
			{
				ID:           13229,
				ContractorID: 13266,
				Address:      "601903, Владимирская обл, Ковровский р-н, село Любец, д 46",
				Equipments: []AddressEquipment{
					{ID: 16302, AddressID: 13229, EquipmentID: 1749, EquipmentName: "Котел 28 LX. A THERMONA", SerialNumber: "левый (гвс)"},
					{ID: 16303, AddressID: 13229, EquipmentID: 1750, EquipmentName: "Котел Therm duo 50 A THERMONA", SerialNumber: "правый (отопление)"},
					{ID: 18211, AddressID: 13229, EquipmentID: 1201, EquipmentName: "Котел бойлер Proterm FE 200 BM БОЙЛЕР", SerialNumber: "21143500100159650002005570N8"},
					{ID: 18212, AddressID: 13229, EquipmentID: 1254, EquipmentName: "Котел бойлер FE 200 BM БОЙЛЕР", SerialNumber: "21143700100159650002005755N8"},
					{ID: 19000, AddressID: 13229, EquipmentID: 9999, EquipmentName: "Резервный насос Grundfos", SerialNumber: "GF-001"},
				},
			},
		},
	}
	repo.contractors[contractor.ID] = contractor
	repo.addresses[13229] = &contractor.Addresses[0]
	// Оборудование из справочника с ID > 200, не привязанное к адресу (проверка GetEquipmentsByIDs).
	repo.equipments[7500] = &Equipment{ID: 7500, Name: "Стабилизатор Штиль R600"}

	winFrom := now
	winTo := now.Add(time.Hour)
	rating := 5
	analyticRating := 4
	app := &Application{
		ID:               120162,
		ContractorID:     13266,
		ContractorName:   "АО КЭМЗ гостиница",
		AddressID:        13229,
		AddressText:      "601903, Владимирская обл, Ковровский р-н, село Любец, д 46",
		EngineerID:       88,
		PhoneIDs:         []int64{41560},
		SourceRelations:  json.RawMessage(`{"application_phones":[{"phone":"9150001122"}]}`),
		PlannedSum:       6400,
		ActsTotalAmount:  6400,
		PlanDatetimeFrom: now,
		PlanDatetimeTo:   now.Add(2 * time.Hour),
		WorkWindowFrom:   &winFrom,
		WorkWindowTo:     &winTo,
		Status:           StatusProcessed,
		WorkTypes:        []string{"ТО по договору"},
		Comment:          "очередной осмотр 6400",
		SpecialNotes:     "взять прокладку",
		Rating:           &rating,
		RatingComment:    "отлично",
		AnalyticRating:   &analyticRating,
		EquipmentIDs:     []int64{1749, 1750, 1201, 1254, 7500},
		SerialNumbers:    []string{"", "", "", "", "ST-7500"},
		CreatedAt:        now.Add(-24 * time.Hour),
		UpdatedAt:        now,
	}
	repo.apps[app.ID] = app

	// В прошлой заявке EngineerName намеренно пуст (как из PgRepository.ListApplications без JOIN users),
	// чтобы проверить enrichApplicationUsers.
	prevApp := &Application{
		ID:               115000,
		ContractorID:     13266,
		ContractorName:   "АО КЭМЗ гостиница",
		AddressID:        13229,
		AddressText:      "601903, Владимирская обл, Ковровский р-н, село Любец, д 46",
		EngineerID:       88,
		PlannedSum:       5800,
		ActsTotalAmount:  5800,
		PlanDatetimeFrom: now.AddDate(0, -3, 0),
		PlanDatetimeTo:   now.AddDate(0, -3, 0).Add(time.Hour),
		Status:           StatusProcessed,
		IsComplete:       true,
		WorkTypes:        []string{"Ремонт"},
		Comment:          "ошибка розжига на правом котле",
		CreatedAt:        now.AddDate(0, -3, 0),
	}
	repo.apps[prevApp.ID] = prevApp

	// Чужая заявка другого инженера (#99) вчера — должна быть видна админу, но скрыта от инженера #88 вне окна ±3 дней.
	otherEngApp := &Application{
		ID:               119999,
		ContractorID:     13266,
		ContractorName:   "АО КЭМЗ гостиница",
		AddressID:        13229,
		EngineerID:       99,
		PlannedSum:       3500,
		PlanDatetimeFrom: now.Add(-24 * time.Hour),
		PlanDatetimeTo:   now.Add(-23 * time.Hour),
		Status:           StatusActive,
		WorkTypes:        []string{"Аварийный выезд"},
		Comment:          "чужая свежая заявка",
		CreatedAt:        now.Add(-24 * time.Hour),
	}
	repo.apps[otherEngApp.ID] = otherEngApp

	prevAppID := prevApp.ID
	repo.certs[501] = &Certificate{
		ID:             501,
		ApplicationID:  &prevAppID,
		ContractorID:   13266,
		EngineerID:     88,
		WorkDate:       now.AddDate(0, -3, 0),
		TotalAmount:    5800,
		PaidAmount:     5800,
		CostOfParts:    1200,
		PaymentMethod:  "безнал",
		WorkTypes:      []string{"Чистка электрода"},
		SpareParts:     []SparePart{{Name: "Электрод розжига", Quantity: 1, Price: 1200}},
		Results:        CertificateTelemetry{GasPressureInPipeline: "20 мбар", HeatingExpansionTankPressure: "1.0 бар", Voltage: "228 В"},
		Comment:        "котел запущен штатно",
		Recommendation: "Запланировать замену датчика NTC на следующем ТО",
	}

	repo.contracts[701] = &MaintenanceContract{
		ID:           701,
		Number:       "ТО-2026/45",
		ContractorID: 13266,
		AddressID:    13229,
		Status:       StatusProcessed,
		ValidFrom:    now.AddDate(0, -6, 0),
		ValidTo:      now.AddDate(0, 6, 0),
		Price:        25600,
		Note:         "4 визита в год",
	}

	t.Run("full_context_for_admin", func(t *testing.T) {
		summary, err := svc.ResolveEntity(context.Background(), admin, "application", 120162)
		if err != nil {
			t.Fatalf("ResolveEntity(application) error: %v", err)
		}
		md := summary.Markdown

		requiredSnippets := []string{
			"## 1. Общие сведения",
			"## 2. Контрагент и объект",
			"Пропускной режим на КПП",
			"[указан в заявке]",
			"+7 (915) 000-11-22",
			"## 3. Оборудование в заявке",
			"Всего в заявке позиций: **5**",
			"Котел 28 LX. A THERMONA",
			"Котел Therm duo 50 A THERMONA",
			"Котел бойлер Proterm FE 200 BM БОЙЛЕР",
			"Котел бойлер FE 200 BM БОЙЛЕР",
			"Стабилизатор Штиль R600",
			"Прочее оборудование на адресе",
			"Резервный насос Grundfos",
			"## 4. Работы, финансы и комментарии",
			"Оценка контроля качества (ОКК):** 4/5",
			"## 6. Договоры ТО контрагента",
			"ТО-2026/45",
			"## 7. История предыдущих заявок контрагента",
			"#119999",
			"инж. Борисов Максим Алексеевич",
			"#115000",
			"инж. Лунгу Андрей Николаевич",
			"газ вх.: 20 мбар",
			"РБ отопл.: 1.0 бар",
			"котел запущен штатно",
			"Запланировать замену датчика NTC",
		}
		for _, want := range requiredSnippets {
			if !strings.Contains(md, want) {
				t.Errorf("application context missing %q\n--- FULL MARKDOWN ---\n%s", want, md)
			}
		}
	})

	t.Run("rbac_3day_boundary_for_archived_engineer_app", func(t *testing.T) {
		// У инженера #88 переведём заявку #120162 в далёкое прошлое, чтобы окно ±3 дня к реальному времени не выполнялось.
		app.PlanDatetimeFrom = time.Now().AddDate(-1, 0, 0)
		app.PlanDatetimeTo = time.Now().AddDate(-1, 0, 0).Add(time.Hour)
		prevApp.PlanDatetimeFrom = time.Now().AddDate(-1, -3, 0)
		prevApp.PlanDatetimeTo = time.Now().AddDate(-1, -3, 0).Add(time.Hour)

		engAuth := &platform.AuthContext{
			UserID:      88,
			Username:    "lungu",
			Permissions: map[string]bool{"crm.application.view": true, "crm.contractor.view": true},
		}
		summary, err := svc.ResolveEntity(context.Background(), engAuth, "application", 120162)
		if err != nil {
			t.Fatalf("ResolveEntity(application) for engineer error: %v", err)
		}
		md := summary.Markdown

		// Своё оборудование заявки и своя прошлая заявка #115000 доступны.
		if !strings.Contains(md, "Котел 28 LX. A THERMONA") || !strings.Contains(md, "#115000") {
			t.Fatalf("engineer should still see own application equipment and own history:\n%s", md)
		}
		// Актуальные телефоны контрагента, договоры ТО и чужая заявка #119999 вне окна ±3 дней скрыты.
		forbidden := []string{"980-750-01-49", "Пропускной режим на КПП", "ТО-2026/45", "#119999", "чужая свежая заявка", "Резервный насос Grundfos"}
		for _, bad := range forbidden {
			if strings.Contains(md, bad) {
				t.Errorf("RBAC ±3d leak: engineer saw %q outside ±3d window:\n%s", bad, md)
			}
		}
	})
}
