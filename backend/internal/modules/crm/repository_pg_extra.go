package crm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

func (r *PgRepository) GetSubdivision(ctx context.Context, id int64) (*Subdivision, error) {
	var s Subdivision
	var phonesRaw, payRaw []byte
	err := r.db.QueryRow(ctx, `
		SELECT id, name, address, phones, supervisor_user_id, payment_options, deleted, created_at, updated_at
		FROM crm_subdivisions
		WHERE id = $1
	`, id).Scan(&s.ID, &s.Name, &s.Address, &phonesRaw, &s.SupervisorUserID, &payRaw, &s.Deleted, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, platform.NewNotFound("Подразделение не найдено")
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(phonesRaw, &s.Phones)
	_ = json.Unmarshal(payRaw, &s.PaymentOptions)
	return &s, nil
}

func (r *PgRepository) SoftDeleteSubdivision(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_subdivisions SET deleted = TRUE, updated_at = now() WHERE id = $1 AND deleted = FALSE
	`, "Подразделение не найдено", id)
}

func (r *PgRepository) ListSipUsersSelect(ctx context.Context) ([]SipUserSelectOption, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, abonent_id, name, user_id
		FROM crm_sip_accounts
		WHERE active = TRUE
		ORDER BY name, id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SipUserSelectOption
	for rows.Next() {
		var opt SipUserSelectOption
		if err := rows.Scan(&opt.ID, &opt.AbonentID, &opt.Name, &opt.UserID); err != nil {
			return nil, err
		}
		out = append(out, opt)
	}
	return out, rows.Err()
}

func (r *PgRepository) UpsertLegalEntity(ctx context.Context, le *LegalEntity) error {
	if le == nil || le.ContractorID <= 0 {
		return nil
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_legal_entities (
			contractor_id, name, director_name, actual_address, legal_address,
			bank_name, inn, kpp, bik, checking_account, corresponding_account, okpo, ogrn, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13, now())
		ON CONFLICT (contractor_id) DO UPDATE SET
			name = EXCLUDED.name,
			director_name = EXCLUDED.director_name,
			actual_address = EXCLUDED.actual_address,
			legal_address = EXCLUDED.legal_address,
			bank_name = EXCLUDED.bank_name,
			inn = EXCLUDED.inn,
			kpp = EXCLUDED.kpp,
			bik = EXCLUDED.bik,
			checking_account = EXCLUDED.checking_account,
			corresponding_account = EXCLUDED.corresponding_account,
			okpo = EXCLUDED.okpo,
			ogrn = EXCLUDED.ogrn,
			updated_at = now()
		RETURNING updated_at
	`,
		le.ContractorID, le.Name, le.DirectorName, le.ActualAddress, le.LegalAddress,
		le.BankName, le.INN, le.KPP, le.BIK, le.CheckingAccount, le.CorrespondingAccount, le.OKPO, le.OGRN,
	).Scan(&le.UpdatedAt)
}

func (r *PgRepository) FindAddressByText(ctx context.Context, contractorID int64, address string) (*ContractorAddress, error) {
	var a ContractorAddress
	err := r.db.QueryRow(ctx, `
		SELECT id, contractor_id, address, region_code, city, street, house, apartment, floor, entrance, comment, deleted, created_at, updated_at
		FROM crm_contractor_addresses
		WHERE contractor_id = $1 AND deleted = FALSE AND lower(trim(address)) = lower(trim($2))
		LIMIT 1
	`, contractorID, address).Scan(
		&a.ID, &a.ContractorID, &a.Address, &a.RegionCode, &a.City, &a.Street, &a.House,
		&a.Apartment, &a.Floor, &a.Entrance, &a.Comment, &a.Deleted, &a.CreatedAt, &a.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// Traffic sources CRUD.

func (r *PgRepository) ListTrafficSources(ctx context.Context, activeOnly bool) ([]TrafficSource, error) {
	where := ""
	if activeOnly {
		where = "WHERE active = TRUE"
	}
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT id, name, traffic_source_type_id, sort, active, created_at, updated_at
		FROM crm_traffic_sources
		%s
		ORDER BY sort ASC, id ASC
	`, where))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrafficSource
	for rows.Next() {
		var ts TrafficSource
		if err := rows.Scan(&ts.ID, &ts.Name, &ts.TrafficSourceTypeID, &ts.Sort, &ts.Active, &ts.CreatedAt, &ts.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, ts)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetTrafficSource(ctx context.Context, id int64) (*TrafficSource, error) {
	var ts TrafficSource
	err := r.db.QueryRow(ctx, `
		SELECT id, name, traffic_source_type_id, sort, active, created_at, updated_at
		FROM crm_traffic_sources
		WHERE id = $1
	`, id).Scan(&ts.ID, &ts.Name, &ts.TrafficSourceTypeID, &ts.Sort, &ts.Active, &ts.CreatedAt, &ts.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, platform.NewNotFound("Источник трафика не найден")
	}
	if err != nil {
		return nil, err
	}
	return &ts, nil
}

func (r *PgRepository) CreateTrafficSource(ctx context.Context, ts *TrafficSource) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_traffic_sources (name, traffic_source_type_id, sort, active)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`, ts.Name, ts.TrafficSourceTypeID, ts.Sort, ts.Active).Scan(&ts.ID, &ts.CreatedAt, &ts.UpdatedAt)
}

func (r *PgRepository) UpdateTrafficSource(ctx context.Context, ts *TrafficSource) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_traffic_sources
		SET name = $2, traffic_source_type_id = $3, sort = $4, active = $5, updated_at = now()
		WHERE id = $1
	`, "Источник трафика не найден", ts.ID, ts.Name, ts.TrafficSourceTypeID, ts.Sort, ts.Active)
}

func (r *PgRepository) DeleteTrafficSource(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_traffic_sources SET active = FALSE, updated_at = now() WHERE id = $1
	`, "Источник трафика не найден", id)
}

// Applications & Certificates extended helpers.

func (r *PgRepository) CountRecentContractorApplications(ctx context.Context, contractorID int64, since time.Time) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM crm_applications
		WHERE contractor_id = $1 AND deleted = FALSE AND status IN (1, 2) AND created_at >= $2
	`, contractorID, since).Scan(&count)
	return count, err
}

func (r *PgRepository) HasRecentAppLimitTask(ctx context.Context, contractorID int64, since time.Time) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM crm_tasks
			WHERE contractor_id = $1 AND task_type = $2 AND deleted = FALSE AND created_at >= $3
		)
	`, contractorID, string(TaskTypeAppLimit), since).Scan(&exists)
	return exists, err
}

func (r *PgRepository) FindMatchingContractForCertificate(ctx context.Context, contractorID, addressID int64, workDate time.Time) (*int64, error) {
	var id int64
	err := r.db.QueryRow(ctx, `
		SELECT id
		FROM crm_maintenance_contracts
		WHERE deleted = FALSE
		  AND status = 1
		  AND contractor_id = $1
		  AND address_id = $2
		  AND $3::date BETWEEN (valid_from - INTERVAL '45 days')::date AND valid_to
		ORDER BY valid_to DESC, id DESC
		LIMIT 1
	`, contractorID, addressID, workDate).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// MissCallPool.

func (r *PgRepository) ListMissedCallsToSync(ctx context.Context, since time.Time) ([]CallRecord, error) {
	rows, err := r.db.Query(ctx, `
		SELECT c.id, c.external_call_id, c.abonent_id, c.ext_tracking_id, c.direction, c.status,
		       c.caller_phone, c.callee_phone, c.sip_login, c.user_id, c.contractor_id,
		       c.traffic_source_id, c.caller_type, c.call_result,
		       c.type_of_appeal, c.comment, c.record_url, c.duration_sec,
		       c.started_at, c.answer_time, c.ended_at, c.raw_payload, c.created_at, c.updated_at
		FROM crm_calls c
		WHERE c.direction IN ('in', 'inbound')
		  AND c.status = 'missed'
		  AND c.started_at >= $1
		  AND NOT EXISTS (SELECT 1 FROM crm_miss_call_pool p WHERE p.call_id = c.id)
		ORDER BY c.started_at DESC
		LIMIT 200
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CallRecord
	for rows.Next() {
		c, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *PgRepository) AddToMissCallPool(ctx context.Context, callID, userID int64) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO crm_miss_call_pool (call_id, user_id, is_viewed)
		VALUES ($1, $2, FALSE)
		ON CONFLICT (call_id, user_id) DO NOTHING
	`, callID, userID)
	return err
}

func (r *PgRepository) ListMissCallPool(ctx context.Context, userID *int64, onlyUnviewed bool, limit int) ([]MissCallPoolItem, error) {
	limit = normalizeLimit(limit, 50, 200)
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if userID != nil {
		where = append(where, fmt.Sprintf("(p.user_id = $%d OR p.user_id = 0)", idx))
		args = append(args, *userID)
		idx++
	}
	if onlyUnviewed {
		where = append(where, "p.is_viewed = FALSE")
	}
	args = append(args, limit)
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT p.id, p.call_id, p.user_id, p.is_viewed, c.caller_phone, c.callee_phone,
		       c.started_at, c.contractor_id, COALESCE(ct.name, ''), p.created_at
		FROM crm_miss_call_pool p
		JOIN crm_calls c ON c.id = p.call_id
		LEFT JOIN crm_contractors ct ON ct.id = c.contractor_id
		WHERE %s
		ORDER BY p.is_viewed ASC, c.started_at DESC
		LIMIT $%d
	`, strings.Join(where, " AND "), idx), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MissCallPoolItem
	for rows.Next() {
		var it MissCallPoolItem
		if err := rows.Scan(
			&it.ID, &it.CallID, &it.UserID, &it.IsViewed, &it.CallerPhone, &it.CalleePhone,
			&it.StartedAt, &it.ContractorID, &it.ContractorName, &it.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (r *PgRepository) MarkMissCallViewed(ctx context.Context, callID int64, userID *int64) error {
	if userID != nil {
		_, err := r.db.Exec(ctx, `
			UPDATE crm_miss_call_pool SET is_viewed = TRUE
			WHERE call_id = $1 AND (user_id = $2 OR user_id = 0) AND is_viewed = FALSE
		`, callID, *userID)
		return err
	}
	_, err := r.db.Exec(ctx, `UPDATE crm_miss_call_pool SET is_viewed = TRUE WHERE call_id = $1 AND is_viewed = FALSE`, callID)
	return err
}

func (r *PgRepository) MarkMissCallsViewedByPhone(ctx context.Context, phone10 string, userID *int64) error {
	if phone10 == "" {
		return nil
	}
	if userID != nil {
		_, err := r.db.Exec(ctx, `
			UPDATE crm_miss_call_pool p
			SET is_viewed = TRUE
			FROM crm_calls c
			WHERE p.call_id = c.id
			  AND p.is_viewed = FALSE
			  AND (p.user_id = $2 OR p.user_id = 0)
			  AND c.caller_phone LIKE '%' || $1
		`, phone10, *userID)
		return err
	}
	_, err := r.db.Exec(ctx, `
		UPDATE crm_miss_call_pool p
		SET is_viewed = TRUE
		FROM crm_calls c
		WHERE p.call_id = c.id AND p.is_viewed = FALSE AND c.caller_phone LIKE '%' || $1
	`, phone10)
	return err
}

// Analytics & AnalyticQuestions.

func (r *PgRepository) ListAnalyticQuestions(ctx context.Context, activeOnly bool) ([]AnalyticQuestion, error) {
	where := ""
	if activeOnly {
		where = "WHERE active = TRUE"
	}
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT id, name, analytic_document_type_id, sort, active, options, created_at, updated_at
		FROM crm_analytic_questions
		%s
		ORDER BY sort ASC, id ASC
	`, where))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AnalyticQuestion
	for rows.Next() {
		var q AnalyticQuestion
		var optsRaw []byte
		if err := rows.Scan(&q.ID, &q.Name, &q.AnalyticDocumentTypeID, &q.Sort, &q.Active, &optsRaw, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(optsRaw, &q.Options)
		out = append(out, q)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetAnalyticQuestion(ctx context.Context, id int64) (*AnalyticQuestion, error) {
	var q AnalyticQuestion
	var optsRaw []byte
	err := r.db.QueryRow(ctx, `
		SELECT id, name, analytic_document_type_id, sort, active, options, created_at, updated_at
		FROM crm_analytic_questions
		WHERE id = $1
	`, id).Scan(&q.ID, &q.Name, &q.AnalyticDocumentTypeID, &q.Sort, &q.Active, &optsRaw, &q.CreatedAt, &q.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, platform.NewNotFound("Вопрос аналитики не найден")
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(optsRaw, &q.Options)
	return &q, nil
}

func (r *PgRepository) CreateAnalyticQuestion(ctx context.Context, q *AnalyticQuestion) error {
	optsRaw, _ := json.Marshal(q.Options)
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_analytic_questions (name, analytic_document_type_id, sort, active, options)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`, q.Name, q.AnalyticDocumentTypeID, q.Sort, q.Active, optsRaw).Scan(&q.ID, &q.CreatedAt, &q.UpdatedAt)
}

func (r *PgRepository) UpdateAnalyticQuestion(ctx context.Context, q *AnalyticQuestion) error {
	optsRaw, _ := json.Marshal(q.Options)
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_analytic_questions
		SET name = $2, analytic_document_type_id = $3, sort = $4, active = $5, options = $6, updated_at = now()
		WHERE id = $1
	`, "Вопрос аналитики не найден", q.ID, q.Name, q.AnalyticDocumentTypeID, q.Sort, q.Active, optsRaw)
}

func (r *PgRepository) IsAnalyticOptionUsed(ctx context.Context, questionID, optionID int64) (bool, error) {
	var used bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM crm_analytics a,
			     jsonb_array_elements(a.answers) AS ans
			WHERE a.deleted = FALSE
			  AND (ans->>'question_id')::bigint = $1
			  AND ans->'option_ids' @> to_jsonb(ARRAY[$2::bigint])
		)
	`, questionID, optionID).Scan(&used)
	return used, err
}

func scanAnalytic(row pgx.Row) (*Analytic, error) {
	var a Analytic
	var ansRaw []byte
	err := row.Scan(
		&a.ID, &a.Number, &a.CertificateID, &a.Rating, &a.Comment, &a.Status, &ansRaw,
		&a.ContractorID, &a.ContractorName, &a.EngineerID,
		&a.CreatedBy, &a.Deleted, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(ansRaw, &a.Answers)
	return &a, nil
}

func (r *PgRepository) ListAnalytics(ctx context.Context, f AnalyticFilter) ([]Analytic, error) {
	where := []string{"a.deleted = FALSE"}
	args := []any{}
	idx := 1
	if f.From != nil {
		where = append(where, fmt.Sprintf("a.created_at >= $%d", idx))
		args = append(args, *f.From)
		idx++
	}
	if f.To != nil {
		where = append(where, fmt.Sprintf("a.created_at <= $%d", idx))
		args = append(args, *f.To)
		idx++
	}
	if f.CertificateID != nil {
		where = append(where, fmt.Sprintf("a.certificate_id = $%d", idx))
		args = append(args, *f.CertificateID)
		idx++
	}
	if f.ContractorID != nil {
		where = append(where, fmt.Sprintf("c.contractor_id = $%d", idx))
		args = append(args, *f.ContractorID)
		idx++
	}
	if f.EngineerID != nil {
		where = append(where, fmt.Sprintf("c.engineer_id = $%d", idx))
		args = append(args, *f.EngineerID)
		idx++
	}
	if f.Rating != nil {
		where = append(where, fmt.Sprintf("a.rating = $%d", idx))
		args = append(args, *f.Rating)
		idx++
	}
	if f.Status != nil {
		where = append(where, fmt.Sprintf("a.status = $%d", idx))
		args = append(args, *f.Status)
		idx++
	}
	limit := normalizeLimit(f.Limit, 50, 500)
	args = append(args, limit, max(0, f.Offset))
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT a.id, a.number, a.certificate_id, a.rating, a.comment, a.status, a.answers,
		       COALESCE(c.contractor_id, 0), COALESCE(ct.name, ''), COALESCE(c.engineer_id, 0),
		       a.created_by, a.deleted, a.created_at, a.updated_at
		FROM crm_analytics a
		LEFT JOIN crm_certificates c ON c.id = a.certificate_id
		LEFT JOIN crm_contractors ct ON ct.id = c.contractor_id
		WHERE %s
		ORDER BY a.created_at DESC, a.id DESC
		LIMIT $%d OFFSET $%d
	`, strings.Join(where, " AND "), idx, idx+1), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Analytic
	for rows.Next() {
		a, err := scanAnalytic(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetAnalytic(ctx context.Context, id int64) (*Analytic, error) {
	a, err := scanAnalytic(r.db.QueryRow(ctx, `
		SELECT a.id, a.number, a.certificate_id, a.rating, a.comment, a.status, a.answers,
		       COALESCE(c.contractor_id, 0), COALESCE(ct.name, ''), COALESCE(c.engineer_id, 0),
		       a.created_by, a.deleted, a.created_at, a.updated_at
		FROM crm_analytics a
		LEFT JOIN crm_certificates c ON c.id = a.certificate_id
		LEFT JOIN crm_contractors ct ON ct.id = c.contractor_id
		WHERE a.id = $1 AND a.deleted = FALSE
	`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, platform.NewNotFound("Анкета аналитики не найдена")
	}
	return a, err
}

func (r *PgRepository) GetAnalyticByCertificate(ctx context.Context, certID int64) (*Analytic, error) {
	a, err := scanAnalytic(r.db.QueryRow(ctx, `
		SELECT a.id, a.number, a.certificate_id, a.rating, a.comment, a.status, a.answers,
		       COALESCE(c.contractor_id, 0), COALESCE(ct.name, ''), COALESCE(c.engineer_id, 0),
		       a.created_by, a.deleted, a.created_at, a.updated_at
		FROM crm_analytics a
		LEFT JOIN crm_certificates c ON c.id = a.certificate_id
		LEFT JOIN crm_contractors ct ON ct.id = c.contractor_id
		WHERE a.certificate_id = $1 AND a.deleted = FALSE
		ORDER BY a.id DESC
		LIMIT 1
	`, certID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (r *PgRepository) UpsertAnalytic(ctx context.Context, a *Analytic) error {
	ansRaw, _ := json.Marshal(a.Answers)
	if a.ID > 0 {
		return r.db.QueryRow(ctx, `
			UPDATE crm_analytics
			SET number = $2, rating = $3, comment = $4, status = $5, answers = $6, updated_at = now()
			WHERE id = $1 AND deleted = FALSE
			RETURNING updated_at
		`, a.ID, a.Number, a.Rating, a.Comment, a.Status, ansRaw).Scan(&a.UpdatedAt)
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_analytics (number, certificate_id, rating, comment, status, answers, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`, a.Number, a.CertificateID, a.Rating, a.Comment, a.Status, ansRaw, a.CreatedBy).Scan(&a.ID, &a.CreatedAt, &a.UpdatedAt)
}

func (r *PgRepository) UpdateAnalyticRating(ctx context.Context, id int64, rating int, comment string) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_analytics SET rating = $2, comment = $3, updated_at = now() WHERE id = $1 AND deleted = FALSE
	`, "Анкета аналитики не найдена", id, rating, comment)
}

// Banners & AppSettings.

func (r *PgRepository) ListBanners(ctx context.Context, activeOnly bool) ([]Banner, error) {
	where := ""
	if activeOnly {
		where = "WHERE active = TRUE"
	}
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT id, name, text, file_id, active, created_at, updated_at
		FROM crm_banners
		%s
		ORDER BY id DESC
	`, where))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Banner
	for rows.Next() {
		var b Banner
		if err := rows.Scan(&b.ID, &b.Name, &b.Text, &b.FileID, &b.Active, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, err
		}
		if b.FileID != nil && *b.FileID > 0 {
			b.FileURL = fmt.Sprintf("/api/v1/files/%d", *b.FileID)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetBanner(ctx context.Context, id int64) (*Banner, error) {
	var b Banner
	err := r.db.QueryRow(ctx, `
		SELECT id, name, text, file_id, active, created_at, updated_at
		FROM crm_banners
		WHERE id = $1
	`, id).Scan(&b.ID, &b.Name, &b.Text, &b.FileID, &b.Active, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, platform.NewNotFound("Баннер не найден")
	}
	if err != nil {
		return nil, err
	}
	if b.FileID != nil && *b.FileID > 0 {
		b.FileURL = fmt.Sprintf("/api/v1/files/%d", *b.FileID)
	}
	return &b, nil
}

func (r *PgRepository) CreateBanner(ctx context.Context, b *Banner) error {
	err := r.db.QueryRow(ctx, `
		INSERT INTO crm_banners (name, text, file_id, active)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`, b.Name, b.Text, b.FileID, b.Active).Scan(&b.ID, &b.CreatedAt, &b.UpdatedAt)
	if err == nil && b.FileID != nil && *b.FileID > 0 {
		b.FileURL = fmt.Sprintf("/api/v1/files/%d", *b.FileID)
	}
	return err
}

func (r *PgRepository) UpdateBanner(ctx context.Context, b *Banner) error {
	if b.FileID != nil && *b.FileID > 0 {
		b.FileURL = fmt.Sprintf("/api/v1/files/%d", *b.FileID)
	} else {
		b.FileURL = ""
	}
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_banners SET name = $2, text = $3, file_id = $4, active = $5, updated_at = now() WHERE id = $1
	`, "Баннер не найден", b.ID, b.Name, b.Text, b.FileID, b.Active)
}

func (r *PgRepository) DeleteBanner(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `DELETE FROM crm_banners WHERE id = $1`, "Баннер не найден", id)
}

func (r *PgRepository) ListAppSettings(ctx context.Context) ([]AppSetting, error) {
	rows, err := r.db.Query(ctx, `SELECT key, name, data, updated_at FROM crm_app_settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppSetting
	for rows.Next() {
		var s AppSetting
		if err := rows.Scan(&s.Key, &s.Name, &s.Data, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetAppSetting(ctx context.Context, key string) (*AppSetting, error) {
	var s AppSetting
	err := r.db.QueryRow(ctx, `SELECT key, name, data, updated_at FROM crm_app_settings WHERE key = $1`, key).
		Scan(&s.Key, &s.Name, &s.Data, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, platform.NewNotFound("Настройка не найдена")
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *PgRepository) SaveAppSetting(ctx context.Context, s *AppSetting) error {
	if len(s.Data) == 0 {
		s.Data = json.RawMessage(`{}`)
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_app_settings (key, name, data, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (key) DO UPDATE SET name = EXCLUDED.name, data = EXCLUDED.data, updated_at = now()
		RETURNING updated_at
	`, s.Key, s.Name, s.Data).Scan(&s.UpdatedAt)
}

// Extended Reports.

func (r *PgRepository) AmountActsReportFiltered(ctx context.Context, f ReportFilter) ([]AmountActsReportRow, error) {
	where := []string{"c.deleted = FALSE", "c.work_date BETWEEN $1 AND $2"}
	args := []any{f.From, f.To}
	idx := 3
	if f.EngineerID != nil {
		where = append(where, fmt.Sprintf("exp.exec_user_id = $%d", idx))
		args = append(args, *f.EngineerID)
		idx++
	}
	if len(f.UserIDs) > 0 {
		where = append(where, fmt.Sprintf("exp.exec_user_id = ANY($%d)", idx))
		args = append(args, f.UserIDs)
		idx++
	}
	if len(f.SubdivisionIDs) > 0 {
		where = append(where, fmt.Sprintf("COALESCE(a.subdivision_id, ct.subdivision_id) = ANY($%d)", idx))
		args = append(args, f.SubdivisionIDs)
		idx++
	}
	if len(f.PaymentMethods) > 0 {
		where = append(where, fmt.Sprintf("c.payment_method = ANY($%d)", idx))
		args = append(args, f.PaymentMethods)
		idx++
	}

	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		WITH expanded AS (
			SELECT c.id AS cert_id, c.application_id, c.contractor_id, c.payment_method,
			       c.paid_amount, c.total_amount,
			       (exe->>'user_id')::bigint AS exec_user_id,
			       COALESCE((exe->>'sum')::numeric, 0) AS share_amount
			FROM crm_certificates c
			CROSS JOIN LATERAL jsonb_array_elements(c.executors) AS exe
			WHERE c.deleted = FALSE AND jsonb_array_length(c.executors) > 0
			UNION ALL
			SELECT c.id, c.application_id, c.contractor_id, c.payment_method,
			       c.paid_amount, c.total_amount,
			       c.engineer_id AS exec_user_id,
			       c.total_amount AS share_amount
			FROM crm_certificates c
			WHERE c.deleted = FALSE AND jsonb_array_length(c.executors) = 0
		)
		SELECT exp.exec_user_id, c.payment_method, COUNT(DISTINCT c.id),
		       COALESCE(SUM(exp.share_amount), 0),
		       COALESCE(SUM(CASE WHEN c.total_amount > 0 THEN c.paid_amount * (exp.share_amount / c.total_amount) ELSE 0 END), 0)
		FROM expanded exp
		JOIN crm_certificates c ON c.id = exp.cert_id
		LEFT JOIN crm_applications a ON a.id = c.application_id
		LEFT JOIN crm_contractors ct ON ct.id = c.contractor_id
		WHERE %s
		GROUP BY exp.exec_user_id, c.payment_method
	`, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byEng := map[int64]*AmountActsReportRow{}
	ensureRow := func(uid int64) *AmountActsReportRow {
		if row, ok := byEng[uid]; ok {
			return row
		}
		row := &AmountActsReportRow{
			EngineerID:      uid,
			ByPaymentOption: map[string]float64{},
		}
		byEng[uid] = row
		return row
	}
	for rows.Next() {
		var uid int64
		var pm string
		var cnt int
		var sumAmt, paidAmt float64
		if err := rows.Scan(&uid, &pm, &cnt, &sumAmt, &paidAmt); err != nil {
			return nil, err
		}
		row := ensureRow(uid)
		row.CertificatesCount += cnt
		row.TotalAmount += sumAmt
		row.SumTotal += sumAmt
		row.PaidAmount += paidAmt
		if pm != "" {
			row.ByPaymentOption[pm] += sumAmt
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Плановая сумма из заявок за период.
	appWhere := []string{"a.deleted = FALSE", "a.plan_datetime_from BETWEEN $1 AND $2"}
	appArgs := []any{f.From, f.To}
	appIdx := 3
	if f.EngineerID != nil {
		appWhere = append(appWhere, fmt.Sprintf("a.engineer_id = $%d", appIdx))
		appArgs = append(appArgs, *f.EngineerID)
		appIdx++
	}
	if len(f.UserIDs) > 0 {
		appWhere = append(appWhere, fmt.Sprintf("a.engineer_id = ANY($%d)", appIdx))
		appArgs = append(appArgs, f.UserIDs)
		appIdx++
	}
	if len(f.SubdivisionIDs) > 0 {
		appWhere = append(appWhere, fmt.Sprintf("a.subdivision_id = ANY($%d)", appIdx))
		appArgs = append(appArgs, f.SubdivisionIDs)
	}
	appRows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT a.engineer_id, COALESCE(SUM(a.planned_sum), 0)
		FROM crm_applications a
		WHERE %s
		GROUP BY a.engineer_id
	`, strings.Join(appWhere, " AND ")), appArgs...)
	if err != nil {
		return nil, err
	}
	defer appRows.Close()
	for appRows.Next() {
		var uid int64
		var planned float64
		if err := appRows.Scan(&uid, &planned); err != nil {
			return nil, err
		}
		if row, ok := byEng[uid]; ok || planned > 0 {
			if !ok {
				row = ensureRow(uid)
			}
			row.PlannedSumTotal += planned
		}
	}
	if err := appRows.Err(); err != nil {
		return nil, err
	}

	out := make([]AmountActsReportRow, 0, len(byEng))
	for _, r := range byEng {
		r.DebtAmount = r.TotalAmount - r.PaidAmount
		out = append(out, *r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalAmount != out[j].TotalAmount {
			return out[i].TotalAmount > out[j].TotalAmount
		}
		return out[i].EngineerID < out[j].EngineerID
	})
	return out, nil
}

func (r *PgRepository) ExpiredTasksReportFiltered(ctx context.Context, now time.Time, from, to *time.Time, userID *int64) ([]ExpiredTaskReportRow, error) {
	where := []string{
		"t.deleted = FALSE",
		"((t.status IN (1, 2) AND t.plan_datetime_to < $1) OR (t.status = 3 AND t.execution_datetime IS NOT NULL AND t.execution_datetime > t.plan_datetime_to))",
	}
	args := []any{now}
	idx := 2
	if from != nil && to != nil {
		where = append(where, fmt.Sprintf("t.plan_datetime_to BETWEEN $%d AND $%d", idx, idx+1))
		args = append(args, *from, *to)
		idx += 2
	}
	if userID != nil {
		where = append(where, fmt.Sprintf("t.assigned_user_id = $%d", idx))
		args = append(args, *userID)
	}
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT t.id, t.title, t.task_type, t.status, t.assigned_user_id,
		       COALESCE(c.name, ''), t.plan_datetime_to, t.execution_datetime
		FROM crm_tasks t
		LEFT JOIN crm_contractors c ON c.id = t.contractor_id
		WHERE %s
		ORDER BY t.plan_datetime_to ASC
		LIMIT 500
	`, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExpiredTaskReportRow
	for rows.Next() {
		var row ExpiredTaskReportRow
		var taskTypeStr string
		if err := rows.Scan(&row.TaskID, &row.Title, &taskTypeStr, &row.Status, &row.AssignedUserID, &row.ContractorName, &row.PlanDatetimeTo, &row.ExecutionDatetime); err != nil {
			return nil, err
		}
		row.TaskType = TaskType(taskTypeStr)
		ref := now
		if row.ExecutionDatetime != nil {
			ref = *row.ExecutionDatetime
		}
		if ref.After(row.PlanDatetimeTo) {
			row.OverdueMinutes = int64(ref.Sub(row.PlanDatetimeTo).Minutes())
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *PgRepository) SubdivisionSumsReport(ctx context.Context, f ReportFilter) ([]SubdivisionSumReportRow, error) {
	subs, err := r.ListSubdivisions(ctx)
	if err != nil {
		return nil, err
	}
	bySub := map[int64]*SubdivisionSumReportRow{}
	allowedSub := map[int64]bool{}
	for _, id := range f.SubdivisionIDs {
		allowedSub[id] = true
	}
	for _, s := range subs {
		if s.Deleted {
			continue
		}
		if len(allowedSub) > 0 && !allowedSub[s.ID] {
			continue
		}
		bySub[s.ID] = &SubdivisionSumReportRow{
			SubdivisionID:   s.ID,
			SubdivisionName: s.Name,
			ByPaymentOption: map[string]float64{},
		}
	}

	certWhere := []string{"c.deleted = FALSE", "c.work_date BETWEEN $1 AND $2"}
	certArgs := []any{f.From, f.To}
	idx := 3
	if len(f.PaymentMethods) > 0 {
		certWhere = append(certWhere, fmt.Sprintf("c.payment_method = ANY($%d)", idx))
		certArgs = append(certArgs, f.PaymentMethods)
		idx++
	}
	if len(f.UserIDs) > 0 {
		certWhere = append(certWhere, fmt.Sprintf("c.engineer_id = ANY($%d)", idx))
		certArgs = append(certArgs, f.UserIDs)
	}

	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT COALESCE(a.subdivision_id, ct.subdivision_id, 0) AS sub_id,
		       c.payment_method,
		       COALESCE(SUM(c.total_amount), 0)
		FROM crm_certificates c
		LEFT JOIN crm_applications a ON a.id = c.application_id
		LEFT JOIN crm_contractors ct ON ct.id = c.contractor_id
		WHERE %s
		GROUP BY 1, 2
	`, strings.Join(certWhere, " AND ")), certArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var subID int64
		var pm string
		var sum float64
		if err := rows.Scan(&subID, &pm, &sum); err != nil {
			return nil, err
		}
		row, ok := bySub[subID]
		if !ok {
			if len(allowedSub) > 0 && !allowedSub[subID] {
				continue
			}
			row = &SubdivisionSumReportRow{
				SubdivisionID:   subID,
				SubdivisionName: "Без подразделения",
				ByPaymentOption: map[string]float64{},
			}
			bySub[subID] = row
		}
		row.SumTotal += sum
		if pm != "" {
			row.ByPaymentOption[pm] += sum
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	appWhere := []string{"a.deleted = FALSE", "a.plan_datetime_from BETWEEN $1 AND $2"}
	appArgs := []any{f.From, f.To}
	if len(f.UserIDs) > 0 {
		appWhere = append(appWhere, "a.engineer_id = ANY($3)")
		appArgs = append(appArgs, f.UserIDs)
	}
	appRows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT COALESCE(a.subdivision_id, ct.subdivision_id, 0), COALESCE(SUM(a.planned_sum), 0)
		FROM crm_applications a
		LEFT JOIN crm_contractors ct ON ct.id = a.contractor_id
		WHERE %s
		GROUP BY 1
	`, strings.Join(appWhere, " AND ")), appArgs...)
	if err != nil {
		return nil, err
	}
	defer appRows.Close()
	for appRows.Next() {
		var subID int64
		var planned float64
		if err := appRows.Scan(&subID, &planned); err != nil {
			return nil, err
		}
		if row, ok := bySub[subID]; ok {
			row.PlannedSumTotal += planned
		}
	}
	if err := appRows.Err(); err != nil {
		return nil, err
	}

	out := make([]SubdivisionSumReportRow, 0, len(bySub))
	for _, row := range bySub {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SubdivisionID < out[j].SubdivisionID
	})
	return out, nil
}

func (r *PgRepository) AmountSparePartsReport(ctx context.Context, f ReportFilter) (*AmountSparePartsReport, error) {
	where := []string{"c.deleted = FALSE", "c.work_date BETWEEN $1 AND $2"}
	args := []any{f.From, f.To}
	idx := 3
	if len(f.SubdivisionIDs) > 0 {
		where = append(where, fmt.Sprintf("COALESCE(a.subdivision_id, ct.subdivision_id) = ANY($%d)", idx))
		args = append(args, f.SubdivisionIDs)
		idx++
	}
	if len(f.UserIDs) > 0 {
		where = append(where, fmt.Sprintf("c.engineer_id = ANY($%d)", idx))
		args = append(args, f.UserIDs)
		idx++
	}
	if len(f.PaymentMethods) > 0 {
		where = append(where, fmt.Sprintf("c.payment_method = ANY($%d)", idx))
		args = append(args, f.PaymentMethods)
	}
	var out AmountSparePartsReport
	err := r.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT COALESCE(SUM(c.cost_of_parts), 0), COUNT(*)
		FROM crm_certificates c
		LEFT JOIN crm_applications a ON a.id = c.application_id
		LEFT JOIN crm_contractors ct ON ct.id = c.contractor_id
		WHERE %s
	`, strings.Join(where, " AND ")), args...).Scan(&out.TotalCostOfParts, &out.CertificatesCount)
	return &out, err
}

func (r *PgRepository) CallConversionReport(ctx context.Context, f ReportFilter) (*CallConversionReport, error) {
	where := []string{"started_at BETWEEN $1 AND $2"}
	args := []any{f.From, f.To}
	if len(f.UserIDs) > 0 {
		where = append(where, "user_id = ANY($3)")
		args = append(args, f.UserIDs)
	}
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT type_of_appeal, call_result, COUNT(*)
		FROM crm_calls
		WHERE %s
		GROUP BY type_of_appeal, call_result
	`, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]map[int]int{}
	for rows.Next() {
		var appeal string
		var res, cnt int
		if err := rows.Scan(&appeal, &res, &cnt); err != nil {
			return nil, err
		}
		if counts[appeal] == nil {
			counts[appeal] = map[int]int{}
		}
		counts[appeal][res] += cnt
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return BuildCallConversionMatrix(counts), nil
}

// BuildCallConversionMatrix строит матрицу конверсии звонков (общая логика для БД и unit-тестов).
func BuildCallConversionMatrix(counts map[string]map[int]int) *CallConversionReport {

	rowsDef := []struct {
		key    string
		label  string
		target bool
	}{
		{CallAppealToContract, "Договор ТО", true},
		{CallAppealServiceWork, "Сервисные / ремонтные работы", true},
		{CallAppealSaleEquipment, "Продажа оборудования", true},
		{CallAppealPartsStore, "Магазин запчастей", true},
		{CallAppealSpecify, "Уточнить информацию", true},
		{CallAppealSpamCall, "Спам", false},
		{CallAppealOther, "Прочее", false},
	}

	for _, option := range ExtendedCallTypesOfAppeal {
		if strings.HasPrefix(option.Key, "import_") {
			rowsDef = append(rowsDef, struct {
				key    string
				label  string
				target bool
			}{option.Key, option.Label, option.ID == 1001 || option.ID == 1002 || option.ID == 1003 || (option.ID >= 1009 && option.ID <= 1013) || option.ID == 1015})
		}
	}
	normalizeAppeal := func(k string) string {
		for _, option := range ExtendedCallTypesOfAppeal {
			if option.Key == k && strings.HasPrefix(k, "import_") {
				return k
			}
		}
		switch k {
		case CallAppealGeneralQuestion:
			return CallAppealSpecify
		case CallAppealServiceRequest:
			return CallAppealServiceWork
		case CallAppealBuyParts:
			return CallAppealPartsStore
		case CallAppealToContract, CallAppealServiceWork, CallAppealSaleEquipment, CallAppealPartsStore, CallAppealSpecify, CallAppealSpamCall, CallAppealOther:
			return k
		default:
			return CallAppealOther
		}
	}

	merged := map[string]map[int]int{}
	for rawAppeal, byRes := range counts {
		key := normalizeAppeal(rawAppeal)
		if merged[key] == nil {
			merged[key] = map[int]int{}
		}
		for res, cnt := range byRes {
			merged[key][res] += cnt
		}
	}

	makeRow := func(key, label string, src map[int]int) CallConversionRow {
		row := CallConversionRow{
			TypeOfAppeal: key,
			Label:        label,
			Results:      map[int]int{},
			Percents:     map[int]float64{},
		}
		isMain := map[int]bool{
			CallResultCreateContract:    true,
			CallResultExtendContract:    true,
			CallResultCreateApplication: true,
			CallResultConsultation:      true,
			CallResultRecall:            true,
		}
		for res, cnt := range src {
			row.Total += cnt
			row.Results[res] += cnt
			if !isMain[res] {
				row.OtherCount += cnt
			}
		}
		for res := range row.Results {
			if row.Total > 0 {
				row.Percents[res] = math.Round(float64(row.Results[res])*1000/float64(row.Total)) / 10
			}
		}
		if row.Total > 0 {
			row.OtherPercent = math.Round(float64(row.OtherCount)*1000/float64(row.Total)) / 10
		}
		return row
	}

	rep := &CallConversionReport{}
	targetSrc := map[int]int{}
	summarySrc := map[int]int{}
	for _, def := range rowsDef {
		row := makeRow(def.key, def.label, merged[def.key])
		rep.Rows = append(rep.Rows, row)
		for res, cnt := range merged[def.key] {
			summarySrc[res] += cnt
			if def.target {
				targetSrc[res] += cnt
			}
		}
	}
	rep.TargetRow = makeRow("target", "Целевые", targetSrc)
	rep.SummaryRow = makeRow("total", "Итого", summarySrc)
	return rep
}

func (r *PgRepository) ContractorsReport(ctx context.Context, f ReportFilter) ([]ContractorReportRow, error) {
	where := []string{"c.deleted = FALSE", "ct.deleted = FALSE", "c.work_date BETWEEN $1 AND $2"}
	args := []any{f.From, f.To}
	idx := 3
	if len(f.SubdivisionIDs) > 0 {
		where = append(where, fmt.Sprintf("ct.subdivision_id = ANY($%d)", idx))
		args = append(args, f.SubdivisionIDs)
		idx++
	}
	if len(f.UserIDs) > 0 {
		where = append(where, fmt.Sprintf("c.engineer_id = ANY($%d)", idx))
		args = append(args, f.UserIDs)
	}
	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT ct.id, ct.name,
		       COALESCE(ARRAY(SELECT p.phone FROM crm_contractor_phones p WHERE p.contractor_id = ct.id AND p.deleted = FALSE ORDER BY p.id), ARRAY[]::text[]),
		       COALESCE(ARRAY(SELECT a.address FROM crm_contractor_addresses a WHERE a.contractor_id = ct.id AND a.deleted = FALSE ORDER BY a.id), ARRAY[]::text[]),
		       COUNT(c.id),
		       COALESCE(SUM(c.total_amount), 0),
		       COALESCE(SUM(c.cost_of_parts), 0)
		FROM crm_certificates c
		JOIN crm_contractors ct ON ct.id = c.contractor_id
		WHERE %s
		GROUP BY ct.id, ct.name
		ORDER BY SUM(c.total_amount) DESC, ct.id ASC
		LIMIT 1000
	`, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContractorReportRow
	for rows.Next() {
		var row ContractorReportRow
		if err := rows.Scan(&row.ContractorID, &row.Name, &row.Phones, &row.Addresses, &row.CertificatesCount, &row.FactSum, &row.CostOfParts); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *PgRepository) EngineersReport(ctx context.Context, f ReportFilter) ([]EngineerReportRow, error) {
	where := []string{"c.deleted = FALSE", "c.work_date BETWEEN $1 AND $2"}
	args := []any{f.From, f.To}
	idx := 3
	if len(f.UserIDs) > 0 {
		where = append(where, fmt.Sprintf("exp.exec_user_id = ANY($%d)", idx))
		args = append(args, f.UserIDs)
		idx++
	}
	if len(f.SubdivisionIDs) > 0 {
		where = append(where, fmt.Sprintf("COALESCE(a.subdivision_id, ct.subdivision_id) = ANY($%d)", idx))
		args = append(args, f.SubdivisionIDs)
	}

	rows, err := r.db.Query(ctx, fmt.Sprintf(`
		WITH expanded AS (
			SELECT c.id AS cert_id, (exe->>'user_id')::bigint AS exec_user_id,
			       COALESCE((exe->>'sum')::numeric, 0) AS share_amount
			FROM crm_certificates c
			CROSS JOIN LATERAL jsonb_array_elements(c.executors) AS exe
			WHERE c.deleted = FALSE AND jsonb_array_length(c.executors) > 0
			UNION ALL
			SELECT c.id, c.engineer_id AS exec_user_id, c.total_amount AS share_amount
			FROM crm_certificates c
			WHERE c.deleted = FALSE AND jsonb_array_length(c.executors) = 0
		)
		SELECT exp.exec_user_id,
		       COUNT(DISTINCT c.id),
		       COUNT(DISTINCT c.id) FILTER (WHERE exp.share_amount = 0),
		       COUNT(DISTINCT c.id) FILTER (WHERE c.work_types @> '["ТО"]'::jsonb OR c.maintenance_contract_id IS NOT NULL),
		       COALESCE(SUM(exp.share_amount) FILTER (WHERE c.work_types @> '["ТО"]'::jsonb OR c.maintenance_contract_id IS NOT NULL), 0),
		       COALESCE(SUM(exp.share_amount), 0)
		FROM expanded exp
		JOIN crm_certificates c ON c.id = exp.cert_id
		LEFT JOIN crm_applications a ON a.id = c.application_id
		LEFT JOIN crm_contractors ct ON ct.id = c.contractor_id
		WHERE %s
		GROUP BY exp.exec_user_id
	`, strings.Join(where, " AND ")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byEng := map[int64]*EngineerReportRow{}
	for rows.Next() {
		var rrow EngineerReportRow
		if err := rows.Scan(
			&rrow.EngineerID, &rrow.CertificatesCount, &rrow.ZeroSumCount,
			&rrow.TypeJobCertCount, &rrow.TypeJobFactSum, &rrow.FactSum,
		); err != nil {
			return nil, err
		}
		if rrow.CertificatesCount > 0 {
			rrow.ZeroPercent = math.Round(float64(rrow.ZeroSumCount)*1000/float64(rrow.CertificatesCount)) / 10
		}
		byEng[rrow.EngineerID] = &rrow
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	appWhere := []string{"a.deleted = FALSE", "a.plan_datetime_from BETWEEN $1 AND $2"}
	appArgs := []any{f.From, f.To}
	appIdx := 3
	if len(f.UserIDs) > 0 {
		appWhere = append(appWhere, fmt.Sprintf("a.engineer_id = ANY($%d)", appIdx))
		appArgs = append(appArgs, f.UserIDs)
		appIdx++
	}
	if len(f.SubdivisionIDs) > 0 {
		appWhere = append(appWhere, fmt.Sprintf("a.subdivision_id = ANY($%d)", appIdx))
		appArgs = append(appArgs, f.SubdivisionIDs)
	}
	appRows, err := r.db.Query(ctx, fmt.Sprintf(`
		SELECT a.engineer_id, COALESCE(SUM(a.planned_sum), 0)
		FROM crm_applications a
		WHERE %s
		GROUP BY a.engineer_id
	`, strings.Join(appWhere, " AND ")), appArgs...)
	if err != nil {
		return nil, err
	}
	defer appRows.Close()
	for appRows.Next() {
		var uid int64
		var planned float64
		if err := appRows.Scan(&uid, &planned); err != nil {
			return nil, err
		}
		if row, ok := byEng[uid]; ok {
			row.PlannedSum += planned
		} else if planned > 0 {
			byEng[uid] = &EngineerReportRow{EngineerID: uid, PlannedSum: planned}
		}
	}
	if err := appRows.Err(); err != nil {
		return nil, err
	}

	out := make([]EngineerReportRow, 0, len(byEng))
	for _, row := range byEng {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FactSum != out[j].FactSum {
			return out[i].FactSum > out[j].FactSum
		}
		return out[i].EngineerID < out[j].EngineerID
	})
	return out, nil
}

func (r *PgRepository) TrafficSourcesReport(ctx context.Context, f ReportFilter) (*TrafficSourceReport, error) {
	sources, err := r.ListTrafficSources(ctx, false)
	if err != nil {
		return nil, err
	}

	callRows, err := r.db.Query(ctx, `
		SELECT COALESCE(traffic_source_id, 0), COUNT(*)
		FROM crm_calls
		WHERE started_at BETWEEN $1 AND $2
		GROUP BY 1
	`, f.From, f.To)
	if err != nil {
		return nil, err
	}
	defer callRows.Close()
	callsMap := map[int64]int{}
	for callRows.Next() {
		var sid int64
		var cnt int
		if err := callRows.Scan(&sid, &cnt); err != nil {
			return nil, err
		}
		callsMap[sid] = cnt
	}
	if err := callRows.Err(); err != nil {
		return nil, err
	}

	appRows, err := r.db.Query(ctx, `
		SELECT COALESCE(a.traffic_source_id, 0),
		       COUNT(DISTINCT a.id),
		       COUNT(DISTINCT a.id) FILTER (WHERE a.status = 2 OR a.is_complete = TRUE),
		       COALESCE(SUM(c.total_amount), 0)
		FROM crm_applications a
		LEFT JOIN crm_certificates c ON c.application_id = a.id AND c.deleted = FALSE
		WHERE a.deleted = FALSE AND a.plan_datetime_from BETWEEN $1 AND $2
		GROUP BY 1
	`, f.From, f.To)
	if err != nil {
		return nil, err
	}
	defer appRows.Close()
	type appStats struct {
		apps, closed int
		sum          float64
	}
	appsMap := map[int64]appStats{}
	for appRows.Next() {
		var sid int64
		var st appStats
		if err := appRows.Scan(&sid, &st.apps, &st.closed, &st.sum); err != nil {
			return nil, err
		}
		appsMap[sid] = st
	}
	if err := appRows.Err(); err != nil {
		return nil, err
	}

	rep := &TrafficSourceReport{SummaryRow: TrafficSourceReportRow{Name: "Итого"}}
	var totalSum float64
	buildRow := func(idPtr *int64, name string, key int64) TrafficSourceReportRow {
		st := appsMap[key]
		calls := callsMap[key]
		row := TrafficSourceReportRow{
			TrafficSourceID:    idPtr,
			Name:               name,
			Calls:              calls,
			Applications:       st.apps,
			ClosedApplications: st.closed,
			Sum:                st.sum,
		}
		if calls > 0 {
			row.ApplicationConversion = math.Round(float64(st.apps)*1000/float64(calls)) / 10
		}
		if st.apps > 0 {
			row.ClosedApplicationConversion = math.Round(float64(st.closed)*1000/float64(st.apps)) / 10
		}
		totalSum += st.sum
		rep.SummaryRow.Calls += calls
		rep.SummaryRow.Applications += st.apps
		rep.SummaryRow.ClosedApplications += st.closed
		rep.SummaryRow.Sum += st.sum
		return row
	}

	for _, ts := range sources {
		if !ts.Active && callsMap[ts.ID] == 0 && appsMap[ts.ID].apps == 0 {
			continue
		}
		idCopy := ts.ID
		rep.Rows = append(rep.Rows, buildRow(&idCopy, ts.Name, ts.ID))
	}
	if callsMap[0] > 0 || appsMap[0].apps > 0 {
		rep.Rows = append(rep.Rows, buildRow(nil, "Не указан", 0))
	}
	if totalSum > 0 {
		for i := range rep.Rows {
			rep.Rows[i].SumConversion = math.Round(rep.Rows[i].Sum*1000/totalSum) / 10
		}
		rep.SummaryRow.SumConversion = 100
	}
	if rep.SummaryRow.Calls > 0 {
		rep.SummaryRow.ApplicationConversion = math.Round(float64(rep.SummaryRow.Applications)*1000/float64(rep.SummaryRow.Calls)) / 10
	}
	if rep.SummaryRow.Applications > 0 {
		rep.SummaryRow.ClosedApplicationConversion = math.Round(float64(rep.SummaryRow.ClosedApplications)*1000/float64(rep.SummaryRow.Applications)) / 10
	}
	return rep, nil
}
