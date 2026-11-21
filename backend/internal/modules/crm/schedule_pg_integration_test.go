package crm

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Проверка SQL табеля, занятости и дневного плана на пустой схеме (make migrate-schema).
// Тест пишет данные, поэтому запускается только на отдельной тестовой базе.
func TestSchedulePgRepository(t *testing.T) {
	dsn := os.Getenv("SCHEDULE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set SCHEDULE_TEST_DATABASE_URL to an empty migrated test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := NewPgRepository(pool)
	base := time.Date(2031, 3, 10, 0, 0, 0, 0, time.UTC)

	e := &TimesheetEntry{UserID: 900001, Kind: TimesheetWork, StartsAt: base.Add(8 * time.Hour), EndsAt: base.Add(17 * time.Hour), CreatedBy: 1}
	if err := repo.CreateTimesheetEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	e.Kind = TimesheetReserve
	e.Comment = "врач"
	if err := repo.UpdateTimesheetEntry(ctx, e); err != nil {
		t.Fatal(err)
	}
	got, err := repo.ListTimesheetEntries(ctx, TimesheetFilter{From: base, To: base.Add(24 * time.Hour), UserIDs: []int64{900001}})
	if err != nil || len(got) != 1 || got[0].Kind != TimesheetReserve || got[0].Comment != "врач" {
		t.Fatalf("list: %+v %v", got, err)
	}
	shift := &TimesheetEntry{UserID: 900001, Kind: TimesheetWork, StartsAt: base.Add(32 * time.Hour), EndsAt: base.Add(41 * time.Hour)}
	if err := repo.CreateTimesheetEntry(ctx, shift); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteTimesheetShifts(ctx, 900001, base, base.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if left, _ := repo.ListTimesheetEntries(ctx, TimesheetFilter{From: base, To: base.Add(48 * time.Hour), UserIDs: []int64{900001}}); len(left) != 1 {
		t.Fatalf("смены сняты, резерв остаётся: %+v", left)
	}
	if err := repo.DeleteTimesheetEntry(ctx, e.ID); err != nil {
		t.Fatal(err)
	}

	var appID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO crm_applications (engineer_id, executor_ids, plan_datetime_from, plan_datetime_to, planned_sum)
		VALUES (900001, '[900002]'::jsonb, $1, $2, 4500) RETURNING id`, base.Add(9*time.Hour), base.Add(11*time.Hour)).Scan(&appID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `DELETE FROM crm_applications WHERE id = $1`, appID)
	busy, err := repo.ListBusyIntervals(ctx, []int64{900001, 900002}, base, base.Add(24*time.Hour))
	if err != nil || len(busy) != 2 {
		t.Fatalf("busy: %+v %v", busy, err)
	}
	for _, b := range busy {
		if b.IsMain != (b.UserID == 900001) || b.PlannedSum != 4500 {
			t.Fatalf("busy row: %+v", b)
		}
	}

	p := &DailyPlan{SubdivisionID: 900001, Amount: 10000, EffectiveFrom: base, CreatedBy: 1}
	if err := repo.SaveDailyPlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	p.Amount = 12000
	if err := repo.SaveDailyPlan(ctx, p); err != nil {
		t.Fatal(err)
	}
	plans, err := repo.ListDailyPlans(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, pl := range plans {
		if pl.SubdivisionID == 900001 {
			found++
			if pl.Amount != 12000 {
				t.Fatalf("plan: %+v", pl)
			}
		}
	}
	if found != 1 {
		t.Fatalf("повторное сохранение с той же даты должно заменять сумму, найдено %d", found)
	}
	if err := repo.DeleteDailyPlan(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
}
