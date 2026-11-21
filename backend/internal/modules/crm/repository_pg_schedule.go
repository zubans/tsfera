package crm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

const timesheetColumns = `id, user_id, subdivision_id, kind, starts_at, ends_at, comment, COALESCE(created_by, 0), created_at, updated_at`

func scanTimesheetEntry(row pgx.Row) (*TimesheetEntry, error) {
	var e TimesheetEntry
	var kind string
	if err := row.Scan(&e.ID, &e.UserID, &e.SubdivisionID, &kind, &e.StartsAt, &e.EndsAt, &e.Comment, &e.CreatedBy, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	e.Kind = TimesheetKind(kind)
	return &e, nil
}

func (r *PgRepository) ListTimesheetEntries(ctx context.Context, f TimesheetFilter) ([]TimesheetEntry, error) {
	where := []string{"deleted = FALSE", "starts_at < $2", "ends_at > $1"}
	args := []any{f.From, f.To}
	if len(f.UserIDs) > 0 {
		where = append(where, "user_id = ANY($3)")
		args = append(args, f.UserIDs)
	}
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT %s FROM crm_timesheet_entries
		WHERE %s
		ORDER BY user_id, starts_at
	`, timesheetColumns, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TimesheetEntry{}
	for rows.Next() {
		e, err := scanTimesheetEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetTimesheetEntry(ctx context.Context, id int64) (*TimesheetEntry, error) {
	e, err := scanTimesheetEntry(r.db.QueryRow(ctx, `SELECT `+timesheetColumns+` FROM crm_timesheet_entries WHERE id = $1 AND deleted = FALSE`, id))
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return e, err
}

func (r *PgRepository) CreateTimesheetEntry(ctx context.Context, e *TimesheetEntry) error {
	var createdBy *int64
	if e.CreatedBy > 0 {
		createdBy = &e.CreatedBy
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_timesheet_entries (user_id, subdivision_id, kind, starts_at, ends_at, comment, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`, e.UserID, e.SubdivisionID, string(e.Kind), e.StartsAt, e.EndsAt, e.Comment, createdBy).Scan(&e.ID, &e.CreatedAt, &e.UpdatedAt)
}

func (r *PgRepository) UpdateTimesheetEntry(ctx context.Context, e *TimesheetEntry) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_timesheet_entries
		SET user_id = $2, subdivision_id = $3, kind = $4, starts_at = $5, ends_at = $6, comment = $7, updated_at = now()
		WHERE id = $1 AND deleted = FALSE
	`, "Запись табеля не найдена", e.ID, e.UserID, e.SubdivisionID, string(e.Kind), e.StartsAt, e.EndsAt, e.Comment)
}

func (r *PgRepository) DeleteTimesheetEntry(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_timesheet_entries SET deleted = TRUE, updated_at = now() WHERE id = $1 AND deleted = FALSE
	`, "Запись табеля не найдена", id)
}

// DeleteTimesheetShifts снимает смены инженера, начинающиеся в периоде; отсутствия не трогает.
func (r *PgRepository) DeleteTimesheetShifts(ctx context.Context, userID int64, from, to time.Time) error {
	_, err := r.db.Exec(ctx, `
		UPDATE crm_timesheet_entries SET deleted = TRUE, updated_at = now()
		WHERE user_id = $1 AND deleted = FALSE AND kind IN ('work', 'duty')
		  AND starts_at >= $2 AND starts_at < $3
	`, userID, from, to)
	return err
}

// ListBusyIntervals возвращает заявки инженеров (основных и соисполнителей), пересекающие период.
// Проваленные и отменённые заявки время не занимают.
func (r *PgRepository) ListBusyIntervals(ctx context.Context, userIDs []int64, from, to time.Time) ([]BusyInterval, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.user_id, a.id, a.plan_datetime_from, a.plan_datetime_to, a.is_complete, a.planned_sum, u.user_id = a.engineer_id
		FROM crm_applications a
		CROSS JOIN LATERAL (
			SELECT a.engineer_id AS user_id
			UNION
			SELECT (x)::bigint FROM jsonb_array_elements_text(a.executor_ids) AS x
		) u
		WHERE a.deleted = FALSE
		  AND a.status NOT IN (3, 4)
		  AND a.plan_datetime_from < $3
		  AND a.plan_datetime_to > $2
		  AND u.user_id = ANY($1)
		ORDER BY u.user_id, a.plan_datetime_from
	`, userIDs, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BusyInterval
	for rows.Next() {
		var b BusyInterval
		if err := rows.Scan(&b.UserID, &b.ApplicationID, &b.From, &b.To, &b.IsComplete, &b.PlannedSum, &b.IsMain); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *PgRepository) ListDailyPlans(ctx context.Context) ([]DailyPlan, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, subdivision_id, amount, effective_from, COALESCE(created_by, 0), created_at
		FROM crm_daily_plans
		ORDER BY subdivision_id, effective_from
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DailyPlan{}
	for rows.Next() {
		var p DailyPlan
		if err := rows.Scan(&p.ID, &p.SubdivisionID, &p.Amount, &p.EffectiveFrom, &p.CreatedBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SaveDailyPlan создаёт план с даты или заменяет сумму плана, уже заданного с этой даты.
func (r *PgRepository) SaveDailyPlan(ctx context.Context, p *DailyPlan) error {
	var createdBy *int64
	if p.CreatedBy > 0 {
		createdBy = &p.CreatedBy
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_daily_plans (subdivision_id, amount, effective_from, created_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (subdivision_id, effective_from)
		DO UPDATE SET amount = EXCLUDED.amount, created_by = EXCLUDED.created_by, created_at = now()
		RETURNING id, created_at
	`, p.SubdivisionID, p.Amount, p.EffectiveFrom, createdBy).Scan(&p.ID, &p.CreatedAt)
}

func (r *PgRepository) DeleteDailyPlan(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `DELETE FROM crm_daily_plans WHERE id = $1`, "План не найден", id)
}
