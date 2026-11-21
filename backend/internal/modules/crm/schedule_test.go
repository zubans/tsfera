package crm

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/zubans/tsfera/backend/pkg/platform"
)

// --- memRepo: табель, занятость и дневной план ---

func (m *memRepo) ListTimesheetEntries(_ context.Context, f TimesheetFilter) ([]TimesheetEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []TimesheetEntry{}
	for _, e := range m.timesheet {
		if !e.StartsAt.Before(f.To) || !e.EndsAt.After(f.From) {
			continue
		}
		if len(f.UserIDs) > 0 && !slices.Contains(f.UserIDs, e.UserID) {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (m *memRepo) GetTimesheetEntry(_ context.Context, id int64) (*TimesheetEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.timesheet {
		if e.ID == id {
			cp := e
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memRepo) CreateTimesheetEntry(_ context.Context, e *TimesheetEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	e.ID = m.nextID
	m.timesheet = append(m.timesheet, *e)
	return nil
}

func (m *memRepo) UpdateTimesheetEntry(_ context.Context, e *TimesheetEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.timesheet {
		if m.timesheet[i].ID == e.ID {
			m.timesheet[i] = *e
			return nil
		}
	}
	return platform.NewConflict("Запись табеля не найдена")
}

func (m *memRepo) DeleteTimesheetEntry(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.timesheet {
		if m.timesheet[i].ID == id {
			m.timesheet = slices.Delete(m.timesheet, i, i+1)
			return nil
		}
	}
	return platform.NewConflict("Запись табеля не найдена")
}

func (m *memRepo) DeleteTimesheetShifts(_ context.Context, userID int64, from, to time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.timesheet = slices.DeleteFunc(m.timesheet, func(e TimesheetEntry) bool {
		return e.UserID == userID && e.Kind.IsShift() && !e.StartsAt.Before(from) && e.StartsAt.Before(to)
	})
	return nil
}

func (m *memRepo) ListBusyIntervals(_ context.Context, userIDs []int64, from, to time.Time) ([]BusyInterval, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []BusyInterval
	for _, a := range m.apps {
		if a.Deleted || a.Status == StatusFailed || a.Status == StatusCancelled || !a.PlanDatetimeFrom.Before(to) || !a.PlanDatetimeTo.After(from) {
			continue
		}
		for _, uid := range append([]int64{a.EngineerID}, a.ExecutorIDs...) {
			if slices.Contains(userIDs, uid) {
				out = append(out, BusyInterval{UserID: uid, ApplicationID: a.ID, From: a.PlanDatetimeFrom, To: a.PlanDatetimeTo, IsComplete: a.IsComplete, PlannedSum: a.PlannedSum, IsMain: uid == a.EngineerID})
			}
		}
	}
	return out, nil
}

func (m *memRepo) ListDailyPlans(context.Context) ([]DailyPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]DailyPlan{}, m.dailyPlans...), nil
}

func (m *memRepo) SaveDailyPlan(_ context.Context, p *DailyPlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.dailyPlans {
		if m.dailyPlans[i].SubdivisionID == p.SubdivisionID && m.dailyPlans[i].EffectiveFrom.Equal(p.EffectiveFrom) {
			p.ID = m.dailyPlans[i].ID
			m.dailyPlans[i] = *p
			return nil
		}
	}
	m.nextID++
	p.ID = m.nextID
	m.dailyPlans = append(m.dailyPlans, *p)
	return nil
}

func (m *memRepo) DeleteDailyPlan(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dailyPlans = slices.DeleteFunc(m.dailyPlans, func(p DailyPlan) bool { return p.ID == id })
	return nil
}

// --- Чистая логика ---

var msk = time.FixedZone("MSK", 3*3600)

func at(day, hour, minute int) time.Time {
	return time.Date(2026, 11, day, hour, minute, 0, 0, msk)
}

func TestIntervalOps(t *testing.T) {
	merged := mergeIntervals([]Interval{{at(1, 10, 0), at(1, 12, 0)}, {at(1, 8, 0), at(1, 9, 0)}, {at(1, 11, 0), at(1, 13, 0)}, {at(1, 13, 0), at(1, 14, 0)}})
	if len(merged) != 2 || !merged[1].From.Equal(at(1, 10, 0)) || !merged[1].To.Equal(at(1, 14, 0)) {
		t.Fatalf("merge: %+v", merged)
	}
	sub := subtractIntervals([]Interval{{at(1, 8, 0), at(1, 17, 0)}}, []Interval{{at(1, 9, 0), at(1, 10, 0)}, {at(1, 12, 0), at(1, 13, 0)}, {at(1, 16, 0), at(1, 18, 0)}})
	want := []Interval{{at(1, 8, 0), at(1, 9, 0)}, {at(1, 10, 0), at(1, 12, 0)}, {at(1, 13, 0), at(1, 16, 0)}}
	if len(sub) != len(want) {
		t.Fatalf("subtract: %+v", sub)
	}
	for i := range want {
		if !sub[i].From.Equal(want[i].From) || !sub[i].To.Equal(want[i].To) {
			t.Fatalf("subtract[%d]: %+v", i, sub[i])
		}
	}
	inter := intersectIntervals([]Interval{{at(1, 8, 0), at(1, 12, 0)}}, []Interval{{at(1, 11, 0), at(1, 15, 0)}})
	if len(inter) != 1 || inter[0].Duration() != time.Hour {
		t.Fatalf("intersect: %+v", inter)
	}
}

func TestComputeDayLoad(t *testing.T) {
	day := Interval{From: at(21, 0, 0), To: at(22, 0, 0)}
	base := DayLoadInput{Day: day, MinFree: time.Hour, MinShown: 15 * time.Minute}

	t.Run("смена 8–17, заявки и резерв", func(t *testing.T) {
		in := base
		in.Entries = []TimesheetEntry{
			{Kind: TimesheetWork, StartsAt: at(21, 8, 0), EndsAt: at(21, 17, 0)},
			{Kind: TimesheetReserve, StartsAt: at(21, 8, 0), EndsAt: at(21, 9, 0), Comment: "врач"},
		}
		in.Busy = []Interval{{at(21, 9, 0), at(21, 11, 0)}, {at(21, 11, 20), at(21, 13, 0)}, {at(21, 15, 0), at(21, 17, 0)}}
		got := ComputeDayLoad(in)
		if got.WorkMinutes != 8*60 || got.LoadPercent == nil || *got.LoadPercent != 71 {
			t.Fatalf("work=%d load=%v", got.WorkMinutes, got.LoadPercent)
		}
		if len(got.Free) != 2 || !got.Free[0].Short || got.Free[1].Short || !got.Free[1].From.Equal(at(21, 13, 0)) {
			t.Fatalf("free: %+v", got.Free)
		}
		if len(got.Absences) != 1 || got.Absences[0].Comment != "врач" {
			t.Fatalf("absences: %+v", got.Absences)
		}
	})

	t.Run("суточная смена с 9 до 9 следующего дня", func(t *testing.T) {
		in := base
		in.Entries = []TimesheetEntry{{Kind: TimesheetWork, StartsAt: at(21, 9, 0), EndsAt: at(22, 9, 0)}}
		got := ComputeDayLoad(in)
		if got.WorkMinutes != 15*60 {
			t.Fatalf("expected 15h within the day, got %d", got.WorkMinutes)
		}
	})

	t.Run("дежурство не даёт свободных окон", func(t *testing.T) {
		in := base
		in.Entries = []TimesheetEntry{{Kind: TimesheetDuty, StartsAt: at(21, 8, 0), EndsAt: at(21, 23, 0)}}
		got := ComputeDayLoad(in)
		if !got.OnDuty || len(got.Free) != 0 || got.LoadPercent != nil {
			t.Fatalf("duty: %+v", got)
		}
	})

	t.Run("отпуск весь день", func(t *testing.T) {
		in := base
		in.DefaultWork = []Interval{{at(21, 8, 0), at(21, 17, 0)}}
		in.Entries = []TimesheetEntry{{Kind: TimesheetVacation, StartsAt: at(15, 0, 0), EndsAt: at(29, 0, 0)}}
		got := ComputeDayLoad(in)
		if got.WorkMinutes != 0 || len(got.Free) != 0 || got.ScheduleSource != "timesheet" {
			t.Fatalf("vacation: %+v", got)
		}
	})

	t.Run("без табеля — окно по умолчанию", func(t *testing.T) {
		in := base
		in.DefaultWork = []Interval{{at(21, 8, 0), at(21, 17, 0)}}
		got := ComputeDayLoad(in)
		if got.ScheduleSource != "default" || got.WorkMinutes != 9*60 || len(got.Free) != 1 {
			t.Fatalf("default: %+v", got)
		}
	})
}

func TestCountPlanDays(t *testing.T) {
	sub2 := int64(2)
	entries := []TimesheetEntry{
		{Kind: TimesheetWork, StartsAt: at(2, 9, 0), EndsAt: at(3, 9, 0)},                         // суточная — один день
		{Kind: TimesheetWork, StartsAt: at(4, 8, 0), EndsAt: at(4, 17, 0), SubdivisionID: &sub2}, // в другом городе
		{Kind: TimesheetWork, StartsAt: at(5, 8, 0), EndsAt: at(5, 17, 0)},
		{Kind: TimesheetSick, StartsAt: at(5, 0, 0), EndsAt: at(6, 0, 0)}, // больничный — минус день
		{Kind: TimesheetWork, StartsAt: at(6, 8, 0), EndsAt: at(6, 17, 0)},
		{Kind: TimesheetUnpaid, StartsAt: at(6, 0, 0), EndsAt: at(7, 0, 0)}, // б/с — не вычитается
		{Kind: TimesheetDuty, StartsAt: at(7, 8, 0), EndsAt: at(7, 23, 0)},  // дежурство — не в плане
		{Kind: TimesheetWork, StartsAt: at(30, 9, 0), EndsAt: at(30, 18, 0)},
	}
	days := CountPlanDays(entries, at(1, 0, 0), at(30, 0, 0), msk)
	if len(days) != 3 {
		t.Fatalf("expected 3 plan days, got %+v", days)
	}
	if days[1].SubdivisionID == nil || *days[1].SubdivisionID != 2 {
		t.Fatalf("expected city of shift on day 4, got %+v", days[1])
	}
}

func TestPlanAmountOn(t *testing.T) {
	d := func(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, time.UTC) }
	plans := []DailyPlan{
		{SubdivisionID: 1, Amount: 10000, EffectiveFrom: d(1)},
		{SubdivisionID: 1, Amount: 12000, EffectiveFrom: d(15)},
		{SubdivisionID: 2, Amount: 8000, EffectiveFrom: d(1)},
	}
	cases := []struct {
		sub  int64
		day  int
		want float64
	}{{1, 14, 10000}, {1, 15, 12000}, {2, 20, 8000}, {3, 20, 0}}
	for _, c := range cases {
		if got := PlanAmountOn(plans, c.sub, d(c.day)); got != c.want {
			t.Fatalf("sub=%d day=%d: got %v want %v", c.sub, c.day, got, c.want)
		}
	}
}

func TestGenerateShifts(t *testing.T) {
	// 2026-11-02 — понедельник.
	shifts, ok := GenerateShifts(Pattern52, at(2, 0, 0), at(2, 0, 0), at(9, 0, 0), 8*time.Hour, 17*time.Hour, msk)
	if !ok || len(shifts) != 5 {
		t.Fatalf("5/2: %v %d", ok, len(shifts))
	}
	shifts, _ = GenerateShifts(Pattern22, at(3, 0, 0), at(1, 0, 0), at(9, 0, 0), 9*time.Hour, 9*time.Hour, msk)
	// Цикл начинается 3-го: 1–2 — выходные предыдущего цикла, дальше 3–4 работа, 5–6 отдых, 7–8 работа.
	var days []int
	for _, s := range shifts {
		days = append(days, s.From.Day())
		if s.To.Sub(s.From) != 24*time.Hour {
			t.Fatalf("суточная смена должна длиться 24 часа: %+v", s)
		}
	}
	if !slices.Equal(days, []int{3, 4, 7, 8}) {
		t.Fatalf("2/2 days: %v", days)
	}
	if _, ok := GenerateShifts("4/4", at(1, 0, 0), at(1, 0, 0), at(2, 0, 0), 0, time.Hour, msk); ok {
		t.Fatal("unknown pattern must be rejected")
	}
}

// --- Сервис ---

func scheduleFixture(t *testing.T) (*Service, *memRepo, *platform.AuthContext) {
	t.Helper()
	repo := newMemRepo()
	sub1 := int64(1)
	users := &fakeUsers{users: map[int64]UserBrief{
		10: {ID: 10, FullName: "Петров", SubdivisionID: &sub1, IsEngineer: true, Active: true},
		11: {ID: 11, FullName: "Сидоров", SubdivisionID: &sub1, IsEngineer: true, Active: true},
		12: {ID: 12, FullName: "Офис", SubdivisionID: &sub1, IsEngineer: true, Active: true},
	}}
	now := at(21, 7, 0)
	svc := NewService(repo, users, nil, nil, Config{Clock: func() time.Time { return now }}, nil)
	data, _ := json.Marshal(map[string]any{"service_user_ids": []int64{12}})
	repo.appSettings[CalendarLoadSettingsKey] = &AppSetting{Key: CalendarLoadSettingsKey, Data: data}
	mgr := &platform.AuthContext{UserID: 1, SubdivisionID: &sub1, Permissions: map[string]bool{
		"crm.calendar.view": true, "crm.calendar.select_user": true, "crm.calendar.select_subdivision": true,
		"crm.timesheet.manage": true, "crm.plan.manage": true, "crm.contractor.view_all": true,
	}}
	return svc, repo, mgr
}

func TestCalendarLoadService(t *testing.T) {
	svc, repo, mgr := scheduleFixture(t)
	ctx := context.Background()
	sub1 := int64(1)
	for _, e := range []TimesheetEntry{
		{UserID: 10, Kind: TimesheetWork, StartsAt: at(21, 8, 0), EndsAt: at(21, 17, 0)},
		{UserID: 11, Kind: TimesheetVacation, StartsAt: at(20, 0, 0), EndsAt: at(28, 0, 0)},
	} {
		if _, err := svc.CreateTimesheetEntry(ctx, mgr, e); err != nil {
			t.Fatalf("timesheet: %v", err)
		}
	}
	repo.apps[100] = &Application{ID: 100, EngineerID: 12, ExecutorIDs: []int64{10}, PlanDatetimeFrom: at(21, 9, 0), PlanDatetimeTo: at(21, 12, 0), Status: StatusActive, PlannedSum: 5000}
	repo.apps[101] = &Application{ID: 101, EngineerID: 10, PlanDatetimeFrom: at(21, 13, 0), PlanDatetimeTo: at(21, 14, 0), Status: StatusFailed}

	res, err := svc.CalendarLoad(ctx, mgr, at(21, 0, 0), nil, &sub1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Engineers) != 3 || res.Totals.Engineers != 2 || res.Totals.Applications != 1 {
		t.Fatalf("result: %+v", res)
	}
	byID := map[int64]EngineerDayLoad{}
	for _, e := range res.Engineers {
		byID[e.UserID] = e
	}
	petrov := byID[10]
	if petrov.LoadPercent == nil || *petrov.LoadPercent != 33 || petrov.ApplicationsCount != 1 || petrov.PlannedSum != 0 {
		t.Fatalf("соисполнитель занят, но плановая сумма — у основного: %+v", petrov)
	}
	if sidorov := byID[11]; sidorov.WorkMinutes != 0 || len(sidorov.Absences) != 1 {
		t.Fatalf("отпуск: %+v", sidorov)
	}
	if office := byID[12]; !office.IsService || office.LoadPercent != nil || len(office.Free) != 0 {
		t.Fatalf("служебная учётка: %+v", office)
	}
}

func TestCheckApplicationSlot(t *testing.T) {
	svc, repo, mgr := scheduleFixture(t)
	ctx := context.Background()
	_, _ = svc.CreateTimesheetEntry(ctx, mgr, TimesheetEntry{UserID: 10, Kind: TimesheetWork, StartsAt: at(21, 8, 0), EndsAt: at(21, 17, 0)})
	_, _ = svc.CreateTimesheetEntry(ctx, mgr, TimesheetEntry{UserID: 11, Kind: TimesheetDuty, StartsAt: at(21, 8, 0), EndsAt: at(21, 23, 0)})
	repo.apps[100] = &Application{ID: 100, EngineerID: 10, PlanDatetimeFrom: at(21, 9, 0), PlanDatetimeTo: at(21, 10, 0), Status: StatusActive}

	res, err := svc.CheckApplicationSlot(ctx, SlotCheckInput{EngineerID: 10, From: at(21, 9, 30), To: at(21, 10, 30)})
	if err != nil || res.Conflict == "" {
		t.Fatalf("expected conflict, got %+v %v", res, err)
	}
	res, _ = svc.CheckApplicationSlot(ctx, SlotCheckInput{EngineerID: 10, ExecutorIDs: []int64{11}, From: at(21, 16, 0), To: at(21, 18, 0)})
	kinds := map[string]bool{}
	for _, w := range res.Warnings {
		kinds[w.Kind] = true
	}
	if res.Conflict != "" || !kinds["outside_schedule"] || !kinds["duty"] {
		t.Fatalf("expected outside_schedule and duty warnings, got %+v", res)
	}
}

func TestPlanFactService(t *testing.T) {
	svc, repo, mgr := scheduleFixture(t)
	ctx := context.Background()
	sub1 := int64(1)
	if _, err := svc.SaveDailyPlan(ctx, mgr, 1, 10000, "2026-11-01"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.GenerateTimesheet(ctx, mgr, GenerateTimesheetInput{UserID: 10, Pattern: Pattern52, DateFrom: "2026-11-02", DateTo: "2026-11-08", TimeFrom: "08:00", TimeTo: "17:00"}); err != nil {
		t.Fatal(err)
	}
	_, _ = svc.CreateTimesheetEntry(ctx, mgr, TimesheetEntry{UserID: 10, Kind: TimesheetVacation, StartsAt: at(6, 0, 0), EndsAt: at(7, 0, 0)})
	repo.certs[500] = &Certificate{ID: 500, EngineerID: 10, WorkDate: at(3, 12, 0), TotalAmount: 30000, PaidAmount: 20000}

	from, _ := svc.ParseLocalDate("2026-11-01")
	to, _ := svc.ParseLocalDate("2026-12-01")
	rep, err := svc.PlanFact(ctx, mgr, from, to, &sub1)
	if err != nil {
		t.Fatal(err)
	}
	var petrov *PlanFactRow
	for i := range rep.Rows {
		if rep.Rows[i].UserID == 10 {
			petrov = &rep.Rows[i]
		}
		if rep.Rows[i].UserID == 12 {
			t.Fatal("служебная учётка не должна попадать в план/факт")
		}
	}
	if petrov == nil || petrov.PlanDays != 4 || petrov.PlanAmount != 40000 || petrov.FactAmount != 20000 || petrov.Percent == nil || *petrov.Percent != 50 {
		t.Fatalf("petrov: %+v", petrov)
	}

	engineer := &platform.AuthContext{UserID: 11, Permissions: map[string]bool{"crm.plan.view_own": true}}
	own, err := svc.PlanFact(ctx, engineer, from, to, nil)
	if err != nil || len(own.Rows) != 1 || own.Rows[0].UserID != 11 {
		t.Fatalf("инженер видит только себя: %+v %v", own, err)
	}
}

func TestRescheduleApplication(t *testing.T) {
	svc, repo, mgr := scheduleFixture(t)
	ctx := context.Background()
	repo.apps[100] = &Application{ID: 100, ContractorID: 1, AddressID: 1, EngineerID: 10, ExecutorIDs: []int64{11}, PlanDatetimeFrom: at(21, 9, 0), PlanDatetimeTo: at(21, 10, 0), Status: StatusActive}
	repo.apps[101] = &Application{ID: 101, ContractorID: 1, AddressID: 1, EngineerID: 11, PlanDatetimeFrom: at(21, 14, 0), PlanDatetimeTo: at(21, 15, 0), Status: StatusActive}

	if _, err := svc.RescheduleApplication(ctx, mgr, 100, 0, at(21, 14, 30), at(21, 15, 30)); err == nil {
		t.Fatal("соисполнитель занят — перенос должен быть запрещён")
	}
	if _, err := svc.RescheduleApplication(ctx, mgr, 100, 11, at(21, 11, 0), at(21, 12, 0)); err != nil {
		t.Fatal(err)
	}
	got := repo.apps[100]
	if got.EngineerID != 11 || len(got.ExecutorIDs) != 0 || !got.PlanDatetimeFrom.Equal(at(21, 11, 0)) {
		t.Fatalf("moved: %+v", got)
	}
}
