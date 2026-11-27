package crm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/zubans/tsfera/backend/pkg/platform"
)

type PgRepository struct {
	db platform.Querier
}

func NewPgRepository(db platform.Querier) *PgRepository {
	return &PgRepository{db: db}
}

func normalizeLimit(limit, def, maxVal int) int {
	if limit <= 0 {
		return def
	}
	if limit > maxVal {
		return maxVal
	}
	return limit
}

// --- Subdivisions & SIP ---

func (r *PgRepository) ListSubdivisions(ctx context.Context) ([]Subdivision, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, address, phones, supervisor_user_id, payment_options, deleted, created_at, updated_at
		FROM crm_subdivisions
		WHERE deleted = FALSE
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Subdivision
	for rows.Next() {
		var s Subdivision
		var phonesRaw, payRaw []byte
		if err := rows.Scan(&s.ID, &s.Name, &s.Address, &phonesRaw, &s.SupervisorUserID, &payRaw, &s.Deleted, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(phonesRaw, &s.Phones)
		_ = json.Unmarshal(payRaw, &s.PaymentOptions)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *PgRepository) CreateSubdivision(ctx context.Context, sub *Subdivision) error {
	phonesRaw, _ := json.Marshal(sub.Phones)
	if len(phonesRaw) == 0 {
		phonesRaw = []byte("[]")
	}
	payRaw, _ := json.Marshal(sub.PaymentOptions)
	if len(payRaw) == 0 {
		payRaw = []byte("[]")
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_subdivisions (name, address, phones, supervisor_user_id, payment_options)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at
	`, sub.Name, sub.Address, phonesRaw, sub.SupervisorUserID, payRaw).Scan(&sub.ID, &sub.CreatedAt, &sub.UpdatedAt)
}

func (r *PgRepository) UpdateSubdivision(ctx context.Context, sub *Subdivision) error {
	phonesRaw, _ := json.Marshal(sub.Phones)
	if len(phonesRaw) == 0 {
		phonesRaw = []byte("[]")
	}
	payRaw, _ := json.Marshal(sub.PaymentOptions)
	if len(payRaw) == 0 {
		payRaw = []byte("[]")
	}
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_subdivisions
		SET name = $2, address = $3, phones = $4, supervisor_user_id = $5, payment_options = $6, updated_at = now()
		WHERE id = $1 AND deleted = FALSE
	`, "Подразделение не найдено", sub.ID, sub.Name, sub.Address, phonesRaw, sub.SupervisorUserID, payRaw)
}

const selectSipAccountCols = `id, name, domain, login, password, protocol, server, port, abonent_id, subdivision_id, user_id, active, created_at, updated_at`

func scanSipAccount(row pgx.Row) (*SipAccount, error) {
	var s SipAccount
	err := row.Scan(
		&s.ID, &s.Name, &s.Domain, &s.Login, &s.Password, &s.Protocol,
		&s.Server, &s.Port, &s.AbonentID, &s.SubdivisionID, &s.UserID, &s.Active, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *PgRepository) ListSipAccounts(ctx context.Context) ([]SipAccount, error) {
	rows, err := r.db.Query(ctx, `
		SELECT `+selectSipAccountCols+`
		FROM crm_sip_accounts
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SipAccount
	for rows.Next() {
		s, err := scanSipAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetSipAccountByUser(ctx context.Context, userID int64) (*SipAccount, error) {
	return scanSipAccount(r.db.QueryRow(ctx, `
		SELECT `+selectSipAccountCols+`
		FROM crm_sip_accounts
		WHERE user_id = $1 AND active = TRUE
		ORDER BY id ASC
		LIMIT 1
	`, userID))
}

func (r *PgRepository) GetSipAccountByLogin(ctx context.Context, login string) (*SipAccount, error) {
	return scanSipAccount(r.db.QueryRow(ctx, `
		SELECT `+selectSipAccountCols+`
		FROM crm_sip_accounts
		WHERE login = $1 AND active = TRUE
		LIMIT 1
	`, login))
}

func (r *PgRepository) GetSipAccountByAbonentID(ctx context.Context, abonentID string) (*SipAccount, error) {
	return scanSipAccount(r.db.QueryRow(ctx, `
		SELECT `+selectSipAccountCols+`
		FROM crm_sip_accounts
		WHERE abonent_id = $1 AND abonent_id <> '' AND active = TRUE
		ORDER BY id ASC
		LIMIT 1
	`, abonentID))
}

func (r *PgRepository) SaveSipAccount(ctx context.Context, sip *SipAccount) error {
	if sip.ID == 0 {
		return r.db.QueryRow(ctx, `
			INSERT INTO crm_sip_accounts (name, domain, login, password, protocol, server, port, abonent_id, subdivision_id, user_id, active)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id, created_at, updated_at
		`, sip.Name, sip.Domain, sip.Login, sip.Password, sip.Protocol, sip.Server, sip.Port, sip.AbonentID, sip.SubdivisionID, sip.UserID, sip.Active).
			Scan(&sip.ID, &sip.CreatedAt, &sip.UpdatedAt)
	}
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_sip_accounts
		SET name=$2, domain=$3, login=$4, password=CASE WHEN $5='' THEN password ELSE $5 END,
		    protocol=$6, server=$7, port=$8, abonent_id=$9, subdivision_id=$10, user_id=$11, active=$12, updated_at=now()
		WHERE id = $1
	`, "SIP-аккаунт не найден", sip.ID, sip.Name, sip.Domain, sip.Login, sip.Password, sip.Protocol, sip.Server, sip.Port, sip.AbonentID, sip.SubdivisionID, sip.UserID, sip.Active)
}

// --- Contractors ---

func (r *PgRepository) SearchContractors(ctx context.Context, f ContractorFilter) ([]Contractor, int, error) {
	limit := normalizeLimit(f.Limit, 50, 200)
	offset := max(0, f.Offset)
	words, nums4to9, phones10 := platform.TokenizeSearchQuery(f.Query)

	where := []string{"c.deleted = FALSE"}
	args := []any{}
	argIdx := 1

	if f.SubdivisionID != nil {
		where = append(where, fmt.Sprintf("c.subdivision_id = $%d", argIdx))
		args = append(args, *f.SubdivisionID)
		argIdx++
	}
	if f.OnlyLegal {
		where = append(where, "(c.is_legal_entity = TRUE OR c.type = 2)")
	} else if f.OnlyIndividuals {
		where = append(where, "(c.is_legal_entity = FALSE AND c.type <> 2)")
	}

	// Для инженера без полного доступа действует правило видимости ±3 дня по проведённым/активным заявкам.
	if f.EngineerID != nil {
		from := f.Now.Add(-72 * time.Hour)
		to := f.Now.Add(72 * time.Hour)
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM crm_applications a
			WHERE a.contractor_id = c.id AND a.deleted = FALSE
			  AND a.engineer_id = $%d AND a.status IN (1, 2)
			  AND a.plan_datetime_from BETWEEN $%d AND $%d
		)`, argIdx, argIdx+1, argIdx+2))
		args = append(args, *f.EngineerID, from, to)
		argIdx += 3
	}

	scoreExpr := "0.0::float8"
	if strings.TrimSpace(f.Query) != "" {
		rawArg := argIdx
		args = append(args, strings.TrimSpace(f.Query))
		argIdx++

		scoreParts := []string{
			fmt.Sprintf("(CASE WHEN c.name ILIKE '%%' || $%d || '%%' THEN 10 ELSE 0 END)", rawArg),
		}
		matchParts := []string{
			fmt.Sprintf("c.name ILIKE '%%' || $%d || '%%'", rawArg),
			fmt.Sprintf("EXISTS (SELECT 1 FROM crm_contractor_addresses ca WHERE ca.contractor_id=c.id AND ca.deleted=FALSE AND ca.address ILIKE '%%' || $%d || '%%')", rawArg),
			fmt.Sprintf("EXISTS (SELECT 1 FROM crm_contractor_emails ce WHERE ce.contractor_id=c.id AND ce.deleted=FALSE AND ce.email ILIKE '%%' || $%d || '%%')", rawArg),
			fmt.Sprintf("EXISTS (SELECT 1 FROM crm_legal_entities le WHERE le.contractor_id=c.id AND (le.inn ILIKE '%%' || $%d || '%%' OR le.name ILIKE '%%' || $%d || '%%'))", rawArg, rawArg),
		}

		for _, w := range words {
			p := argIdx
			args = append(args, w)
			argIdx++
			scoreParts = append(scoreParts, fmt.Sprintf("(CASE WHEN lower(c.name) LIKE '%%' || $%d || '%%' THEN 5 ELSE 0 END)", p))
			scoreParts = append(scoreParts, fmt.Sprintf("(CASE WHEN EXISTS (SELECT 1 FROM crm_contractor_addresses ca WHERE ca.contractor_id=c.id AND ca.deleted=FALSE AND lower(ca.address) LIKE '%%' || $%d || '%%') THEN 4 ELSE 0 END)", p))
			matchParts = append(matchParts,
				fmt.Sprintf("lower(c.name) LIKE '%%' || $%d || '%%'", p),
				fmt.Sprintf("EXISTS (SELECT 1 FROM crm_contractor_addresses ca WHERE ca.contractor_id=c.id AND ca.deleted=FALSE AND lower(ca.address) LIKE '%%' || $%d || '%%')", p),
			)
		}
		for _, n := range nums4to9 {
			p := argIdx
			args = append(args, n)
			argIdx++
			scoreParts = append(scoreParts, fmt.Sprintf("(CASE WHEN EXISTS (SELECT 1 FROM crm_contractor_phones cp WHERE cp.contractor_id=c.id AND cp.deleted=FALSE AND cp.phone LIKE '%%' || $%d || '%%') THEN 8 ELSE 0 END)", p))
			matchParts = append(matchParts, fmt.Sprintf("EXISTS (SELECT 1 FROM crm_contractor_phones cp WHERE cp.contractor_id=c.id AND cp.deleted=FALSE AND cp.phone LIKE '%%' || $%d || '%%')", p))
		}
		for _, ph := range phones10 {
			p := argIdx
			args = append(args, ph)
			argIdx++
			scoreParts = append(scoreParts, fmt.Sprintf("(CASE WHEN EXISTS (SELECT 1 FROM crm_contractor_phones cp WHERE cp.contractor_id=c.id AND cp.deleted=FALSE AND cp.phone = $%d) THEN 20 ELSE 0 END)", p))
			matchParts = append(matchParts, fmt.Sprintf("EXISTS (SELECT 1 FROM crm_contractor_phones cp WHERE cp.contractor_id=c.id AND cp.deleted=FALSE AND cp.phone = $%d)", p))
		}
		where = append(where, "("+strings.Join(matchParts, " OR ")+")")
		scoreExpr = "(" + strings.Join(scoreParts, " + ") + ")::float8"
	}

	whereSQL := strings.Join(where, " AND ")
	var total int
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM crm_contractors c WHERE %s", whereSQL)
	if err := r.db.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	argsWithPage := append(args, limit, offset)
	querySQL := fmt.Sprintf(`
		SELECT c.id, c.name, c.first_name, c.second_name, c.last_name, c.subdivision_id, c.type, c.is_legal_entity, c.traffic_source_id, c.note, c.created_by, c.deleted, %s AS score, c.created_at, c.updated_at
		FROM crm_contractors c
		WHERE %s
		ORDER BY score DESC, c.id DESC
		LIMIT $%d OFFSET $%d
	`, scoreExpr, whereSQL, argIdx, argIdx+1)

	rows, err := r.db.Query(ctx, querySQL, argsWithPage...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []Contractor
	for rows.Next() {
		var c Contractor
		if err := rows.Scan(&c.ID, &c.Name, &c.FirstName, &c.SecondName, &c.LastName, &c.SubdivisionID, &c.Type, &c.IsLegalEntity, &c.TrafficSourceID, &c.Note, &c.CreatedBy, &c.Deleted, &c.Score, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	for i := range out {
		_ = r.hydrateContractorRelations(ctx, &out[i])
	}
	return out, total, nil
}

func (r *PgRepository) hydrateContractorRelations(ctx context.Context, c *Contractor) error {
	if err := r.db.QueryRow(ctx, "SELECT client_category_id FROM crm_contractors WHERE id=$1", c.ID).Scan(&c.ClientCategoryID); err != nil {
		return err
	}
	pRows, err := r.db.Query(ctx, `
		SELECT id, contractor_id, phone, note, deleted, created_at
		FROM crm_contractor_phones
		WHERE contractor_id = $1 AND deleted = FALSE
		ORDER BY id ASC
	`, c.ID)
	if err != nil {
		return err
	}
	for pRows.Next() {
		var p ContractorPhone
		if err := pRows.Scan(&p.ID, &p.ContractorID, &p.Phone, &p.Note, &p.Deleted, &p.CreatedAt); err != nil {
			pRows.Close()
			return err
		}
		p.Formatted = platform.FormatPhoneRU(p.Phone)
		c.Phones = append(c.Phones, p)
	}
	pRows.Close()

	eRows, err := r.db.Query(ctx, `
		SELECT id, contractor_id, email, deleted, created_at
		FROM crm_contractor_emails
		WHERE contractor_id = $1 AND deleted = FALSE
		ORDER BY id ASC
	`, c.ID)
	if err != nil {
		return err
	}
	for eRows.Next() {
		var e ContractorEmail
		if err := eRows.Scan(&e.ID, &e.ContractorID, &e.Email, &e.Deleted, &e.CreatedAt); err != nil {
			eRows.Close()
			return err
		}
		c.Emails = append(c.Emails, e)
	}
	eRows.Close()

	aRows, err := r.db.Query(ctx, `
		SELECT id, contractor_id, address, region_code, city, street, house, apartment, floor, entrance, comment, deleted, created_at, updated_at
		FROM crm_contractor_addresses
		WHERE contractor_id = $1 AND deleted = FALSE
		ORDER BY id ASC
	`, c.ID)
	if err != nil {
		return err
	}
	for aRows.Next() {
		var a ContractorAddress
		if err := aRows.Scan(&a.ID, &a.ContractorID, &a.Address, &a.RegionCode, &a.City, &a.Street, &a.House, &a.Apartment, &a.Floor, &a.Entrance, &a.Comment, &a.Deleted, &a.CreatedAt, &a.UpdatedAt); err != nil {
			aRows.Close()
			return err
		}
		c.Addresses = append(c.Addresses, a)
	}
	aRows.Close()

	for i := range c.Addresses {
		aeRows, err := r.db.Query(ctx, `
			SELECT ae.id, ae.address_id, ae.equipment_id, e.name, ae.serial_number, ae.note, ae.deleted, ae.created_at
			FROM crm_address_equipments ae
			JOIN crm_equipments e ON e.id = ae.equipment_id
			WHERE ae.address_id = $1 AND ae.deleted = FALSE
			ORDER BY ae.id ASC
		`, c.Addresses[i].ID)
		if err != nil {
			return err
		}
		for aeRows.Next() {
			var ae AddressEquipment
			if err := aeRows.Scan(&ae.ID, &ae.AddressID, &ae.EquipmentID, &ae.EquipmentName, &ae.SerialNumber, &ae.Note, &ae.Deleted, &ae.CreatedAt); err != nil {
				aeRows.Close()
				return err
			}
			c.Addresses[i].Equipments = append(c.Addresses[i].Equipments, ae)
		}
		aeRows.Close()
	}

	var le LegalEntity
	leErr := r.db.QueryRow(ctx, `
		SELECT contractor_id, name, director_name, actual_address, legal_address, bank_name,
		       inn, kpp, bik, checking_account, corresponding_account, okpo, ogrn, updated_at
		FROM crm_legal_entities
		WHERE contractor_id = $1
	`, c.ID).Scan(
		&le.ContractorID, &le.Name, &le.DirectorName, &le.ActualAddress, &le.LegalAddress, &le.BankName,
		&le.INN, &le.KPP, &le.BIK, &le.CheckingAccount, &le.CorrespondingAccount, &le.OKPO, &le.OGRN, &le.UpdatedAt,
	)
	if leErr == nil {
		c.LegalEntity = &le
	} else if !errors.Is(leErr, pgx.ErrNoRows) {
		return leErr
	}
	return nil
}

func (r *PgRepository) GetContractor(ctx context.Context, id int64) (*Contractor, error) {
	var c Contractor
	err := r.db.QueryRow(ctx, `
		SELECT id, name, first_name, second_name, last_name, subdivision_id, type, is_legal_entity, traffic_source_id, note, created_by, deleted, created_at, updated_at
		FROM crm_contractors
		WHERE id = $1 AND deleted = FALSE
	`, id).Scan(&c.ID, &c.Name, &c.FirstName, &c.SecondName, &c.LastName, &c.SubdivisionID, &c.Type, &c.IsLegalEntity, &c.TrafficSourceID, &c.Note, &c.CreatedBy, &c.Deleted, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := r.hydrateContractorRelations(ctx, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *PgRepository) CreateContractor(ctx context.Context, c *Contractor) error {
	err := r.db.QueryRow(ctx, `
		INSERT INTO crm_contractors (name, first_name, second_name, last_name, subdivision_id, type, is_legal_entity, traffic_source_id, note, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at
	`, c.Name, c.FirstName, c.SecondName, c.LastName, c.SubdivisionID, c.Type, c.IsLegalEntity, c.TrafficSourceID, c.Note, c.CreatedBy).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return err
	}
	if c.LegalEntity != nil {
		c.LegalEntity.ContractorID = c.ID
		return r.UpsertLegalEntity(ctx, c.LegalEntity)
	}
	return nil
}

func (r *PgRepository) UpdateContractor(ctx context.Context, c *Contractor) error {
	err := platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_contractors
		SET name = $2, first_name = $3, second_name = $4, last_name = $5, subdivision_id = $6, type = $7, is_legal_entity = $8, traffic_source_id = $9, note = $10, updated_at = now()
		WHERE id = $1 AND deleted = FALSE
	`, "Контрагент не найден", c.ID, c.Name, c.FirstName, c.SecondName, c.LastName, c.SubdivisionID, c.Type, c.IsLegalEntity, c.TrafficSourceID, c.Note)
	if err != nil {
		return err
	}
	if c.LegalEntity != nil {
		c.LegalEntity.ContractorID = c.ID
		return r.UpsertLegalEntity(ctx, c.LegalEntity)
	}
	return nil
}

func (r *PgRepository) SoftDeleteContractor(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_contractors SET deleted = TRUE, updated_at = now()
		WHERE id = $1 AND deleted = FALSE
	`, "Контрагент не найден", id)
}

func (r *PgRepository) FindContractorByPhone(ctx context.Context, phone10 string) (*Contractor, error) {
	var id int64
	err := r.db.QueryRow(ctx, `
		SELECT cp.contractor_id
		FROM crm_contractor_phones cp
		JOIN crm_contractors c ON c.id = cp.contractor_id
		WHERE cp.phone = $1 AND cp.deleted = FALSE AND c.deleted = FALSE
		LIMIT 1
	`, phone10).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.GetContractor(ctx, id)
}

// FindUniqueContractorByPhone возвращает контрагента только при однозначном совпадении телефона (как MtsEventService Теплосферы).
func (r *PgRepository) FindUniqueContractorByPhone(ctx context.Context, phone10 string) (*Contractor, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT cp.contractor_id
		FROM crm_contractor_phones cp
		JOIN crm_contractors c ON c.id = cp.contractor_id
		WHERE cp.phone = $1 AND cp.deleted = FALSE AND c.deleted = FALSE
		LIMIT 2
	`, phone10)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) != 1 {
		return nil, nil
	}
	return r.GetContractor(ctx, ids[0])
}

func (r *PgRepository) CheckSimilarContractors(ctx context.Context, excludeID *int64, phones10 []string, emails []string) ([]SimilarContractorMatch, error) {
	var ex int64
	if excludeID != nil {
		ex = *excludeID
	}
	var out []SimilarContractorMatch
	if len(phones10) > 0 {
		rows, err := r.db.Query(ctx, `
			SELECT c.id, c.name, cp.phone
			FROM crm_contractor_phones cp
			JOIN crm_contractors c ON c.id = cp.contractor_id
			WHERE cp.deleted = FALSE AND c.deleted = FALSE
			  AND cp.phone = ANY($1)
			  AND ($2::bigint = 0 OR c.id <> $2)
		`, phones10, ex)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var m SimilarContractorMatch
			m.MatchedOn = "phone"
			if err := rows.Scan(&m.ContractorID, &m.ContractorName, &m.ContactValue); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, m)
		}
		rows.Close()
	}
	if len(emails) > 0 {
		lowerEmails := make([]string, 0, len(emails))
		for _, e := range emails {
			lowerEmails = append(lowerEmails, strings.ToLower(strings.TrimSpace(e)))
		}
		rows, err := r.db.Query(ctx, `
			SELECT c.id, c.name, ce.email
			FROM crm_contractor_emails ce
			JOIN crm_contractors c ON c.id = ce.contractor_id
			WHERE ce.deleted = FALSE AND c.deleted = FALSE
			  AND lower(ce.email) = ANY($1)
			  AND ($2::bigint = 0 OR c.id <> $2)
		`, lowerEmails, ex)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var m SimilarContractorMatch
			m.MatchedOn = "email"
			if err := rows.Scan(&m.ContractorID, &m.ContractorName, &m.ContactValue); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, m)
		}
		rows.Close()
	}
	return out, nil
}

func (r *PgRepository) DetachPhoneFromOtherContractors(ctx context.Context, phone10 string, keepContractorID int64) error {
	_, err := r.db.Exec(ctx, `
		UPDATE crm_contractor_phones
		SET deleted = TRUE
		WHERE phone = $1 AND contractor_id <> $2 AND deleted = FALSE
	`, phone10, keepContractorID)
	return err
}

func (r *PgRepository) ReplaceContractorPhones(ctx context.Context, contractorID int64, phones []ContractorPhone) error {
	if _, err := r.db.Exec(ctx, `UPDATE crm_contractor_phones SET deleted = TRUE WHERE contractor_id = $1 AND deleted = FALSE`, contractorID); err != nil {
		return err
	}
	for _, p := range phones {
		if _, err := r.db.Exec(ctx, `
			INSERT INTO crm_contractor_phones (contractor_id, phone, note)
			VALUES ($1, $2, $3)
		`, contractorID, p.Phone, p.Note); err != nil {
			return err
		}
	}
	return nil
}

func (r *PgRepository) ReplaceContractorEmails(ctx context.Context, contractorID int64, emails []ContractorEmail) error {
	if _, err := r.db.Exec(ctx, `UPDATE crm_contractor_emails SET deleted = TRUE WHERE contractor_id = $1 AND deleted = FALSE`, contractorID); err != nil {
		return err
	}
	for _, e := range emails {
		if _, err := r.db.Exec(ctx, `
			INSERT INTO crm_contractor_emails (contractor_id, email)
			VALUES ($1, $2)
		`, contractorID, e.Email); err != nil {
			return err
		}
	}
	return nil
}

func (r *PgRepository) GetAddress(ctx context.Context, id int64) (*ContractorAddress, error) {
	var a ContractorAddress
	err := r.db.QueryRow(ctx, `
		SELECT id, contractor_id, address, region_code, city, street, house, apartment, floor, entrance, comment, deleted, created_at, updated_at
		FROM crm_contractor_addresses
		WHERE id = $1 AND deleted = FALSE
	`, id).Scan(&a.ID, &a.ContractorID, &a.Address, &a.RegionCode, &a.City, &a.Street, &a.House, &a.Apartment, &a.Floor, &a.Entrance, &a.Comment, &a.Deleted, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *PgRepository) CreateAddress(ctx context.Context, addr *ContractorAddress) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_contractor_addresses (contractor_id, address, region_code, city, street, house, apartment, floor, entrance, comment)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at, updated_at
	`, addr.ContractorID, addr.Address, addr.RegionCode, addr.City, addr.Street, addr.House, addr.Apartment, addr.Floor, addr.Entrance, addr.Comment).
		Scan(&addr.ID, &addr.CreatedAt, &addr.UpdatedAt)
}

func (r *PgRepository) UpdateAddress(ctx context.Context, addr *ContractorAddress) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_contractor_addresses
		SET address=$2, region_code=$3, city=$4, street=$5, house=$6, apartment=$7, floor=$8, entrance=$9, comment=$10, updated_at=now()
		WHERE id = $1 AND deleted = FALSE
	`, "Адрес не найден", addr.ID, addr.Address, addr.RegionCode, addr.City, addr.Street, addr.House, addr.Apartment, addr.Floor, addr.Entrance, addr.Comment)
}

func (r *PgRepository) SoftDeleteAddress(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_contractor_addresses SET deleted = TRUE, updated_at = now()
		WHERE id = $1 AND deleted = FALSE
	`, "Адрес не найден", id)
}

func (r *PgRepository) CountAddressLinks(ctx context.Context, addressID int64) (int, int, int, int, error) {
	var eqs, contracts, apps, certs int
	err := r.db.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM crm_address_equipments WHERE address_id = $1 AND deleted = FALSE),
			(SELECT COUNT(*) FROM crm_maintenance_contracts WHERE address_id = $1 AND deleted = FALSE),
			(SELECT COUNT(*) FROM crm_applications WHERE address_id = $1 AND deleted = FALSE),
			(SELECT COUNT(*) FROM crm_certificates WHERE address_id = $1 AND deleted = FALSE)
	`, addressID).Scan(&eqs, &contracts, &apps, &certs)
	return eqs, contracts, apps, certs, err
}

func (r *PgRepository) SearchAddresses(ctx context.Context, contractorID *int64, query string, limit int) ([]ContractorAddress, error) {
	limit = normalizeLimit(limit, 50, 200)
	where := []string{"deleted = FALSE"}
	args := []any{}
	idx := 1
	if contractorID != nil {
		where = append(where, fmt.Sprintf("contractor_id = $%d", idx))
		args = append(args, *contractorID)
		idx++
	}
	if strings.TrimSpace(query) != "" {
		where = append(where, fmt.Sprintf("(address ILIKE '%%' || $%d || '%%' OR strict_word_similarity($%d, address) > 0.25)", idx, idx))
		args = append(args, strings.TrimSpace(query))
		idx++
	}
	args = append(args, limit)
	sql := fmt.Sprintf(`
		SELECT id, contractor_id, address, region_code, city, street, house, apartment, floor, entrance, comment, deleted, created_at, updated_at
		FROM crm_contractor_addresses
		WHERE %s
		ORDER BY id DESC
		LIMIT $%d
	`, strings.Join(where, " AND "), idx)
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContractorAddress
	for rows.Next() {
		var a ContractorAddress
		if err := rows.Scan(&a.ID, &a.ContractorID, &a.Address, &a.RegionCode, &a.City, &a.Street, &a.House, &a.Apartment, &a.Floor, &a.Entrance, &a.Comment, &a.Deleted, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- Equipment ---

func (r *PgRepository) ListEquipments(ctx context.Context, query string, limit int) ([]Equipment, error) {
	limit = normalizeLimit(limit, 50, 200)
	var rows pgx.Rows
	var err error
	if q := strings.TrimSpace(query); q != "" {
		rows, err = r.db.Query(ctx, `
			SELECT id, type, model, manufacturer, name, created_at, updated_at
			FROM crm_equipments
			WHERE name ILIKE '%' || $1 || '%' OR strict_word_similarity($1, name) > 0.25
			ORDER BY strict_word_similarity($1, name) DESC, id ASC
			LIMIT $2
		`, q, limit)
	} else {
		rows, err = r.db.Query(ctx, `
			SELECT id, type, model, manufacturer, name, created_at, updated_at
			FROM crm_equipments
			ORDER BY id ASC
			LIMIT $1
		`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Equipment
	for rows.Next() {
		var e Equipment
		if err := rows.Scan(&e.ID, &e.Type, &e.Model, &e.Manufacturer, &e.Name, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetEquipmentsByIDs(ctx context.Context, ids []int64) ([]Equipment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx, `
		SELECT id, type, model, manufacturer, name, created_at, updated_at
		FROM crm_equipments
		WHERE id = ANY($1)
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Equipment
	for rows.Next() {
		var e Equipment
		if err := rows.Scan(&e.ID, &e.Type, &e.Model, &e.Manufacturer, &e.Name, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (r *PgRepository) CreateEquipment(ctx context.Context, eq *Equipment) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_equipments (type, model, manufacturer, name)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`, eq.Type, eq.Model, eq.Manufacturer, eq.Name).Scan(&eq.ID, &eq.CreatedAt, &eq.UpdatedAt)
}

func (r *PgRepository) UpdateEquipment(ctx context.Context, eq *Equipment) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_equipments SET type=$2, model=$3, manufacturer=$4, name=$5, updated_at=now()
		WHERE id = $1
	`, "Оборудование не найдено", eq.ID, eq.Type, eq.Model, eq.Manufacturer, eq.Name)
}

func (r *PgRepository) AttachAddressEquipment(ctx context.Context, ae *AddressEquipment) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_address_equipments (address_id, equipment_id, serial_number, note)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`, ae.AddressID, ae.EquipmentID, ae.SerialNumber, ae.Note).Scan(&ae.ID, &ae.CreatedAt)
}

func (r *PgRepository) SoftDeleteAddressEquipment(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_address_equipments SET deleted = TRUE WHERE id = $1 AND deleted = FALSE
	`, "Привязка оборудования не найдена", id)
}

func (r *PgRepository) ListRefItems(ctx context.Context, kind string) ([]RefItem, error) {
	sql := `SELECT id, kind, name, deleted, created_at FROM crm_ref_items WHERE deleted = FALSE`
	args := []any{}
	if strings.TrimSpace(kind) != "" {
		sql += ` AND kind = $1`
		args = append(args, strings.TrimSpace(kind))
	}
	sql += ` ORDER BY id ASC`
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RefItem
	for rows.Next() {
		var item RefItem
		if err := rows.Scan(&item.ID, &item.Kind, &item.Name, &item.Deleted, &item.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *PgRepository) CreateRefItem(ctx context.Context, item *RefItem) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_ref_items (kind, name)
		VALUES ($1, $2)
		RETURNING id, deleted, created_at
	`, item.Kind, item.Name).Scan(&item.ID, &item.Deleted, &item.CreatedAt)
}

func (r *PgRepository) UpdateRefItem(ctx context.Context, id int64, kind, name string) (*RefItem, error) {
	var item RefItem
	err := r.db.QueryRow(ctx, `
		UPDATE crm_ref_items SET name = $1
		WHERE id = $2 AND kind = $3 AND deleted = FALSE
		RETURNING id, kind, name, deleted, created_at
	`, name, id, kind).Scan(&item.ID, &item.Kind, &item.Name, &item.Deleted, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *PgRepository) SoftDeleteRefItem(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_ref_items SET deleted = TRUE
		WHERE id = $1 AND deleted = FALSE
	`, "Запись справочника не найдена", id)
}

// --- Contracts ---

const selectContractSQL = `
	SELECT mc.id, mc.contractor_id, c.name, mc.address_id, ca.address,
	       mc.application_id, mc.category, mc.number, mc.price, mc.signed_at, mc.valid_from, mc.valid_to,
	       mc.status, mc.equipments, mc.note, mc.created_by, mc.deleted,
	       EXISTS (
	           SELECT 1 FROM crm_maintenance_contracts newer
	           WHERE newer.deleted = FALSE AND newer.contractor_id = mc.contractor_id
	             AND newer.address_id = mc.address_id AND newer.valid_from > mc.valid_from
	       ) AS has_newer_contract,
	       (mc.status = 1 AND mc.valid_to <= (CURRENT_DATE + INTERVAL '21 days')::date AND NOT EXISTS (
	           SELECT 1 FROM crm_maintenance_contracts newer
	           WHERE newer.deleted = FALSE AND newer.contractor_id = mc.contractor_id
	             AND newer.address_id = mc.address_id AND newer.valid_from > mc.valid_from
	       )) AS needs_prolongation,
	       mc.created_at, mc.updated_at
	FROM crm_maintenance_contracts mc
	JOIN crm_contractors c ON c.id = mc.contractor_id
	JOIN crm_contractor_addresses ca ON ca.id = mc.address_id
`

func scanContract(row pgx.Row) (*MaintenanceContract, error) {
	var c MaintenanceContract
	var eqRaw []byte
	err := row.Scan(
		&c.ID, &c.ContractorID, &c.ContractorName, &c.AddressID, &c.AddressText,
		&c.ApplicationID, &c.Category, &c.Number, &c.Price, &c.SignedAt, &c.ValidFrom, &c.ValidTo,
		&c.Status, &eqRaw, &c.Note, &c.CreatedBy, &c.Deleted,
		&c.HasNewerContract, &c.NeedsProlongation,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(eqRaw, &c.Equipments)
	return &c, nil
}

func (r *PgRepository) ListContracts(ctx context.Context, f ContractFilter) ([]MaintenanceContract, error) {
	limit := normalizeLimit(f.Limit, 50, 500)
	offset := max(0, f.Offset)
	where := []string{"mc.deleted = FALSE"}
	args := []any{}
	idx := 1
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, fmt.Sprintf("(mc.number ILIKE '%%' || $%d || '%%' OR c.name ILIKE '%%' || $%d || '%%' OR ca.address ILIKE '%%' || $%d || '%%')", idx, idx, idx))
		args = append(args, q)
		idx++
	}
	if f.ContractorID != nil {
		where = append(where, fmt.Sprintf("mc.contractor_id = $%d", idx))
		args = append(args, *f.ContractorID)
		idx++
	}
	if f.AddressID != nil {
		where = append(where, fmt.Sprintf("mc.address_id = $%d", idx))
		args = append(args, *f.AddressID)
		idx++
	}
	if f.Status != nil {
		where = append(where, fmt.Sprintf("mc.status = $%d", idx))
		args = append(args, *f.Status)
		idx++
	}
	if f.From != nil {
		where = append(where, fmt.Sprintf("mc.valid_to >= $%d", idx))
		args = append(args, *f.From)
		idx++
	}
	if f.To != nil {
		where = append(where, fmt.Sprintf("mc.valid_to <= $%d", idx))
		args = append(args, *f.To)
		idx++
	}
	if f.OnlyNeedingProlongation {
		where = append(where, `mc.status = 1 AND mc.valid_to <= (CURRENT_DATE + INTERVAL '21 days')::date AND NOT EXISTS (
			SELECT 1 FROM crm_maintenance_contracts newer
			WHERE newer.deleted = FALSE AND newer.contractor_id = mc.contractor_id
			  AND newer.address_id = mc.address_id AND newer.valid_from > mc.valid_from
		)`)
	}
	args = append(args, limit, offset)
	// Аномально далёкие даты окончания (опечатки года вроде 3024/4203) уводим в конец списка.
	sql := fmt.Sprintf("%s WHERE %s ORDER BY (mc.valid_to > CURRENT_DATE + INTERVAL '10 years') ASC, mc.valid_to DESC, mc.id DESC LIMIT $%d OFFSET $%d",
		selectContractSQL, strings.Join(where, " AND "), idx, idx+1)
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MaintenanceContract
	for rows.Next() {
		c, err := scanContract(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetContract(ctx context.Context, id int64) (*MaintenanceContract, error) {
	return scanContract(r.db.QueryRow(ctx, selectContractSQL+" WHERE mc.id = $1 AND mc.deleted = FALSE", id))
}

func (r *PgRepository) CreateContract(ctx context.Context, c *MaintenanceContract) error {
	eqRaw, _ := json.Marshal(c.Equipments)
	if len(eqRaw) == 0 {
		eqRaw = []byte("[]")
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_maintenance_contracts (contractor_id, address_id, application_id, category, number, price, signed_at, valid_from, valid_to, status, equipments, note, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, created_at, updated_at
	`, c.ContractorID, c.AddressID, c.ApplicationID, c.Category, c.Number, c.Price, c.SignedAt, c.ValidFrom, c.ValidTo, c.Status, eqRaw, c.Note, c.CreatedBy).
		Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt)
}

func (r *PgRepository) UpdateContract(ctx context.Context, c *MaintenanceContract) error {
	eqRaw, _ := json.Marshal(c.Equipments)
	if len(eqRaw) == 0 {
		eqRaw = []byte("[]")
	}
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_maintenance_contracts
		SET contractor_id=$2, address_id=$3, application_id=$4, category=$5, number=$6, price=$7, signed_at=$8, valid_from=$9, valid_to=$10, status=$11, equipments=$12, note=$13, updated_at=now()
		WHERE id = $1 AND deleted = FALSE
	`, "Договор не найден", c.ID, c.ContractorID, c.AddressID, c.ApplicationID, c.Category, c.Number, c.Price, c.SignedAt, c.ValidFrom, c.ValidTo, c.Status, eqRaw, c.Note)
}

func (r *PgRepository) SoftDeleteContract(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_maintenance_contracts SET deleted = TRUE, updated_at = now()
		WHERE id = $1 AND deleted = FALSE
	`, "Договор не найден", id)
}

func (r *PgRepository) ListContractsNeedingQuarterlyTask(ctx context.Context, now time.Time) ([]MaintenanceContract, error) {
	today := now.UTC().Format("2006-01-02")
	rows, err := r.db.Query(ctx, selectContractSQL+`
		WHERE mc.deleted = FALSE AND mc.status = 1
		  AND ($1::date - mc.valid_from) > 0
		  AND (($1::date - mc.valid_from) % 90) = 0
		  AND (mc.valid_to - $1::date) > 89
		  AND NOT EXISTS (
			  SELECT 1 FROM crm_tasks t
			  WHERE t.contract_id = mc.id AND t.task_type = 'quarterly_call' AND t.deleted = FALSE
			    AND t.created_at::date = $1::date
		  )
	`, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MaintenanceContract
	for rows.Next() {
		c, err := scanContract(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (r *PgRepository) ListContractsNeedingProlongationTask(ctx context.Context, now time.Time) ([]MaintenanceContract, error) {
	today := now.UTC().Format("2006-01-02")
	limitDate := now.UTC().AddDate(0, 0, 21).Format("2006-01-02")
	cutoff := now.UTC().AddDate(0, 0, -30)
	rows, err := r.db.Query(ctx, selectContractSQL+`
		WHERE mc.deleted = FALSE AND mc.status = 1
		  AND mc.valid_to BETWEEN $1::date AND $2::date
		  AND NOT EXISTS (
			  SELECT 1 FROM crm_maintenance_contracts newer
			  WHERE newer.address_id = mc.address_id AND newer.deleted = FALSE AND newer.valid_from > mc.valid_from
		  )
		  AND NOT EXISTS (
			  SELECT 1 FROM crm_applications a
			  WHERE a.address_id = mc.address_id AND a.deleted = FALSE AND a.plan_datetime_from > mc.valid_to::timestamptz
		  )
		  AND NOT EXISTS (
			  SELECT 1 FROM crm_tasks t
			  WHERE t.contract_id = mc.id AND t.task_type = 'prolongation' AND t.deleted = FALSE AND t.created_at >= $3
		  )
	`, today, limitDate, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MaintenanceContract
	for rows.Next() {
		c, err := scanContract(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// --- Applications & Calendar ---

func (r *PgRepository) HasEngineerVisibleApplicationInWindow(ctx context.Context, engineerID, contractorID int64, from, to time.Time) (bool, error) {
	var ok bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM crm_applications
			WHERE deleted = FALSE
			  AND (engineer_id = $1 OR executor_ids @> to_jsonb(ARRAY[$1::bigint]))
			  AND contractor_id = $2
			  AND status IN (1, 2)
			  AND plan_datetime_from BETWEEN $3 AND $4
		)
	`, engineerID, contractorID, from, to).Scan(&ok)
	return ok, err
}

func (r *PgRepository) HasEngineerTimeOverlap(ctx context.Context, engineerID int64, from, to time.Time, excludeAppID *int64) (bool, error) {
	var ex int64
	if excludeAppID != nil {
		ex = *excludeAppID
	}
	var overlap bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM crm_applications
			WHERE deleted = FALSE
			  AND is_duty = FALSE
			  AND (engineer_id = $1 OR executor_ids @> to_jsonb(ARRAY[$1::bigint]))
			  AND id <> $2
			  AND status NOT IN (3, 4)
			  AND plan_datetime_from < $4
			  AND plan_datetime_to > $3
		)
	`, engineerID, ex, from, to).Scan(&overlap)
	return overlap, err
}

func scanApplication(row pgx.Row) (*Application, error) {
	var a Application
	var wtRaw, eqRaw, snRaw, exRaw, phonesRaw []byte
	var contractorID, addressID *int64
	var planFrom, planTo *time.Time
	err := row.Scan(
		&a.ID, &contractorID, &a.ContractorName, &addressID, &a.AddressText,
		&a.EngineerID, &a.SubdivisionID, &a.TrafficSourceID, &a.PlannedSum,
		&a.WorkWindowFrom, &a.WorkWindowTo, &exRaw,
		&a.CreatedBy, &planFrom, &planTo,
		&a.Status, &a.IsComplete, &a.IsDuty, &wtRaw, &a.PriceNote, &a.Comment,
		&a.SpecialNotes, &a.NotesViewed, &a.Rating, &a.RatingComment, &eqRaw, &snRaw,
		&a.AnalyticID, &a.AnalyticRating,
		&a.Deleted, &a.CreatedAt, &a.UpdatedAt, &a.Number, &a.WorkflowStatus, &a.SourceData, &a.SourceRelations, &phonesRaw,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Incomplete = contractorID == nil || addressID == nil || planFrom == nil || planTo == nil
	if contractorID != nil {
		a.ContractorID = *contractorID
	}
	if addressID != nil {
		a.AddressID = *addressID
	}
	if planFrom != nil {
		a.PlanDatetimeFrom = *planFrom
	}
	if planTo != nil {
		a.PlanDatetimeTo = *planTo
	}
	_ = json.Unmarshal(phonesRaw, &a.PhoneIDs)
	_ = json.Unmarshal(wtRaw, &a.WorkTypes)
	_ = json.Unmarshal(eqRaw, &a.EquipmentIDs)
	_ = json.Unmarshal(snRaw, &a.SerialNumbers)
	_ = json.Unmarshal(exRaw, &a.ExecutorIDs)
	return &a, nil
}

func (r *PgRepository) ListApplications(ctx context.Context, f ApplicationFilter) ([]Application, error) {
	limit := normalizeLimit(f.Limit, 50, 200)
	offset := max(0, f.Offset)
	where := []string{"a.deleted = FALSE"}
	args := []any{}
	idx := 1
	if f.From != nil {
		where = append(where, fmt.Sprintf("a.plan_datetime_to >= $%d", idx))
		args = append(args, *f.From)
		idx++
	}
	if f.To != nil {
		where = append(where, fmt.Sprintf("a.plan_datetime_from <= $%d", idx))
		args = append(args, *f.To)
		idx++
	}
	if f.EngineerID != nil {
		where = append(where, fmt.Sprintf("(a.engineer_id = $%d OR a.executor_ids @> to_jsonb(ARRAY[$%d::bigint]))", idx, idx))
		args = append(args, *f.EngineerID)
		idx++
	}
	if f.SubdivisionID != nil {
		where = append(where, fmt.Sprintf("a.subdivision_id = $%d", idx))
		args = append(args, *f.SubdivisionID)
		idx++
	}
	if f.ContractorID != nil {
		where = append(where, fmt.Sprintf("a.contractor_id = $%d", idx))
		args = append(args, *f.ContractorID)
		idx++
	}
	if f.Status != nil {
		where = append(where, fmt.Sprintf("a.status = $%d", idx))
		args = append(args, *f.Status)
		idx++
	}
	if f.IsComplete != nil {
		where = append(where, fmt.Sprintf("a.is_complete = $%d", idx))
		args = append(args, *f.IsComplete)
		idx++
	}
	orderSQL := "a.plan_datetime_from DESC"
	switch f.OrderBy {
	case "plan_asc":
		orderSQL = "a.plan_datetime_from ASC"
	case "planned_sum_desc":
		orderSQL = "a.planned_sum DESC, a.plan_datetime_from DESC"
	}
	args = append(args, limit, offset)
	sql := fmt.Sprintf(`
		SELECT a.id, a.contractor_id, COALESCE(c.name,''), a.address_id, COALESCE(ca.address,''),
		       a.engineer_id, a.subdivision_id, a.traffic_source_id, a.planned_sum,
		       a.work_window_from, a.work_window_to, a.executor_ids,
		       a.created_by, a.plan_datetime_from, a.plan_datetime_to,
		       a.status, a.is_complete, a.is_duty, a.work_types, a.price_note, a.comment,
		       a.special_notes, a.notes_viewed, a.rating, a.rating_comment, a.equipment_ids, a.serial_numbers,
		       an.id, an.rating,
		       a.deleted, a.created_at, a.updated_at, a.number, a.workflow_status, a.source_data, a.source_relations || jsonb_build_object('application_phones',
                 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',ap.id,'phone_id',ap.phone_id,'phone',cp.phone,'unresolved_phone_id',ap.unresolved_phone_id) ORDER BY ap.id) FROM crm_application_phones ap LEFT JOIN crm_contractor_phones cp ON cp.id=ap.phone_id WHERE ap.application_id=a.id),'[]'::jsonb)),
               COALESCE((SELECT jsonb_agg(ap.phone_id ORDER BY ap.id) FROM crm_application_phones ap WHERE ap.application_id=a.id AND ap.phone_id IS NOT NULL),'[]'::jsonb)
		FROM crm_applications a
		LEFT JOIN crm_contractors c ON c.id = a.contractor_id
		LEFT JOIN crm_contractor_addresses ca ON ca.id = a.address_id
		LEFT JOIN LATERAL (
			SELECT an2.id, an2.rating
			FROM crm_certificates cert2
			JOIN crm_analytics an2 ON an2.certificate_id = cert2.id AND an2.deleted = FALSE
			WHERE cert2.application_id = a.id AND cert2.deleted = FALSE
			ORDER BY an2.id DESC LIMIT 1
		) an ON TRUE
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, strings.Join(where, " AND "), orderSQL, idx, idx+1)

	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Application
	for rows.Next() {
		a, err := scanApplication(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

func (r *PgRepository) ListCalendarEvents(ctx context.Context, from, to time.Time, engineerID *int64, subdivisionID *int64) ([]CalendarEvent, error) {
	where := []string{
		"a.deleted = FALSE",
		// Проваленные и отменённые заявки не занимают время в расписании.
		"a.status NOT IN (3, 4)",
		"a.plan_datetime_to >= $1",
		"a.plan_datetime_from <= $2",
	}
	args := []any{from, to}
	idx := 3
	if engineerID != nil {
		where = append(where, fmt.Sprintf("(a.engineer_id = $%d OR a.executor_ids @> to_jsonb(ARRAY[$%d::bigint]))", idx, idx))
		args = append(args, *engineerID)
		idx++
	}
	if subdivisionID != nil {
		where = append(where, fmt.Sprintf("a.subdivision_id = $%d", idx))
		args = append(args, *subdivisionID)
		idx++
	}
	sql := fmt.Sprintf(`
		SELECT
			a.id,
			COALESCE(c.name,''),
			a.plan_datetime_from,
			a.plan_datetime_to,
			COALESCE(a.contractor_id,0),
			COALESCE(a.address_id,0),
			COALESCE(ca.address,''),
			a.engineer_id,
			a.status,
			a.is_complete,
			a.is_duty,
			a.planned_sum,
			a.work_window_from,
			a.work_window_to,
			a.executor_ids,
			a.traffic_source_id,
			an.id,
			an.rating,
			a.work_types,
			a.serial_numbers,
			a.special_notes,
			a.notes_viewed,
			a.rating,
			COALESCE((
				SELECT json_agg(cp.phone)
				FROM crm_contractor_phones cp
				WHERE cp.deleted = FALSE AND (EXISTS(SELECT 1 FROM crm_application_phones ap WHERE ap.application_id=a.id AND ap.phone_id=cp.id) OR (NOT EXISTS(SELECT 1 FROM crm_application_phones ap WHERE ap.application_id=a.id) AND cp.contractor_id=a.contractor_id))
			), '[]'::json) AS phones,
			COALESCE((
				SELECT json_agg(e.name)
				FROM crm_address_equipments ae
				JOIN crm_equipments e ON e.id = ae.equipment_id
				WHERE ae.address_id = a.address_id AND ae.deleted = FALSE
			), '[]'::json) AS equipments,
			COALESCE((
				SELECT SUM(cert.total_amount)
				FROM crm_certificates cert
				WHERE cert.application_id = a.id AND cert.deleted = FALSE
			), 0) AS acts_sum
		FROM crm_applications a
		LEFT JOIN crm_contractors c ON c.id = a.contractor_id
		LEFT JOIN crm_contractor_addresses ca ON ca.id = a.address_id
		LEFT JOIN LATERAL (
			SELECT an2.id, an2.rating
			FROM crm_certificates cert2
			JOIN crm_analytics an2 ON an2.certificate_id = cert2.id AND an2.deleted = FALSE
			WHERE cert2.application_id = a.id AND cert2.deleted = FALSE
			ORDER BY an2.id DESC LIMIT 1
		) an ON TRUE
		WHERE %s
		ORDER BY a.plan_datetime_from ASC
	`, strings.Join(where, " AND "))

	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CalendarEvent
	for rows.Next() {
		var ev CalendarEvent
		var wtRaw, snRaw, phRaw, eqRaw, exRaw []byte
		if err := rows.Scan(
			&ev.ID, &ev.ContractorName, &ev.Start, &ev.End,
			&ev.ContractorID, &ev.AddressID, &ev.AddressText, &ev.EngineerID,
			&ev.Status, &ev.IsComplete, &ev.IsDuty,
			&ev.PlannedSum, &ev.WorkWindowFrom, &ev.WorkWindowTo, &exRaw, &ev.TrafficSourceID,
			&ev.AnalyticID, &ev.AnalyticRating,
			&wtRaw, &snRaw, &ev.SpecialNotes, &ev.NotesViewed, &ev.Rating,
			&phRaw, &eqRaw, &ev.ActsTotalAmount,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(exRaw, &ev.ExecutorIDs)
		_ = json.Unmarshal(wtRaw, &ev.WorkTypes)
		_ = json.Unmarshal(snRaw, &ev.SerialNumbers)
		_ = json.Unmarshal(phRaw, &ev.Phones)
		_ = json.Unmarshal(eqRaw, &ev.Equipments)
		for i, ph := range ev.Phones {
			ev.Phones[i] = platform.FormatPhoneRU(ph)
		}
		ev.Title = ev.ContractorName + " — " + ev.AddressText
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetApplication(ctx context.Context, id int64) (*Application, error) {
	return scanApplication(r.db.QueryRow(ctx, `
		SELECT a.id, a.contractor_id, COALESCE(c.name,''), a.address_id, COALESCE(ca.address,''),
		       a.engineer_id, a.subdivision_id, a.traffic_source_id, a.planned_sum,
		       a.work_window_from, a.work_window_to, a.executor_ids,
		       a.created_by, a.plan_datetime_from, a.plan_datetime_to,
		       a.status, a.is_complete, a.is_duty, a.work_types, a.price_note, a.comment,
		       a.special_notes, a.notes_viewed, a.rating, a.rating_comment, a.equipment_ids, a.serial_numbers,
		       an.id, an.rating,
		       a.deleted, a.created_at, a.updated_at, a.number, a.workflow_status, a.source_data, a.source_relations || jsonb_build_object('application_phones',
                 COALESCE((SELECT jsonb_agg(jsonb_build_object('id',ap.id,'phone_id',ap.phone_id,'phone',cp.phone,'unresolved_phone_id',ap.unresolved_phone_id) ORDER BY ap.id) FROM crm_application_phones ap LEFT JOIN crm_contractor_phones cp ON cp.id=ap.phone_id WHERE ap.application_id=a.id),'[]'::jsonb)),
               COALESCE((SELECT jsonb_agg(ap.phone_id ORDER BY ap.id) FROM crm_application_phones ap WHERE ap.application_id=a.id AND ap.phone_id IS NOT NULL),'[]'::jsonb)
		FROM crm_applications a
		LEFT JOIN crm_contractors c ON c.id = a.contractor_id
		LEFT JOIN crm_contractor_addresses ca ON ca.id = a.address_id
		LEFT JOIN LATERAL (
			SELECT an2.id, an2.rating
			FROM crm_certificates cert2
			JOIN crm_analytics an2 ON an2.certificate_id = cert2.id AND an2.deleted = FALSE
			WHERE cert2.application_id = a.id AND cert2.deleted = FALSE
			ORDER BY an2.id DESC LIMIT 1
		) an ON TRUE
		WHERE a.id = $1 AND a.deleted = FALSE
	`, id))
}

func (r *PgRepository) CreateApplication(ctx context.Context, app *Application) error {
	wt, _ := json.Marshal(app.WorkTypes)
	eq, _ := json.Marshal(app.EquipmentIDs)
	sn, _ := json.Marshal(app.SerialNumbers)
	ex, _ := json.Marshal(app.ExecutorIDs)
	if len(ex) == 0 {
		ex = []byte("[]")
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_applications (
			contractor_id, address_id, engineer_id, subdivision_id, traffic_source_id,
			planned_sum, work_window_from, work_window_to, executor_ids, created_by,
			plan_datetime_from, plan_datetime_to, status, is_complete, is_duty,
			work_types, price_note, comment, special_notes, notes_viewed, equipment_ids, serial_numbers
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
		RETURNING id, created_at, updated_at
	`, app.ContractorID, app.AddressID, app.EngineerID, app.SubdivisionID, app.TrafficSourceID,
		app.PlannedSum, app.WorkWindowFrom, app.WorkWindowTo, ex, app.CreatedBy,
		app.PlanDatetimeFrom, app.PlanDatetimeTo, app.Status, app.IsComplete, app.IsDuty,
		wt, app.PriceNote, app.Comment, app.SpecialNotes, app.NotesViewed, eq, sn,
	).Scan(&app.ID, &app.CreatedAt, &app.UpdatedAt)
}

func (r *PgRepository) UpdateApplication(ctx context.Context, app *Application) error {
	wt, _ := json.Marshal(app.WorkTypes)
	eq, _ := json.Marshal(app.EquipmentIDs)
	sn, _ := json.Marshal(app.SerialNumbers)
	ex, _ := json.Marshal(app.ExecutorIDs)
	if len(ex) == 0 {
		ex = []byte("[]")
	}
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_applications
		SET contractor_id=$2, address_id=$3, engineer_id=$4, subdivision_id=$5,
		    traffic_source_id=$6, planned_sum=$7, work_window_from=$8, work_window_to=$9, executor_ids=$10,
		    plan_datetime_from=$11, plan_datetime_to=$12, status=$13, is_complete=$14, is_duty=$15,
		    work_types=$16, price_note=$17, comment=$18, special_notes=$19, notes_viewed=$20,
		    equipment_ids=$21, serial_numbers=$22, updated_at=now()
		WHERE id = $1 AND deleted = FALSE
	`, "Заявка не найдена", app.ID, app.ContractorID, app.AddressID, app.EngineerID, app.SubdivisionID,
		app.TrafficSourceID, app.PlannedSum, app.WorkWindowFrom, app.WorkWindowTo, ex,
		app.PlanDatetimeFrom, app.PlanDatetimeTo, app.Status, app.IsComplete, app.IsDuty,
		wt, app.PriceNote, app.Comment, app.SpecialNotes, app.NotesViewed, eq, sn)
}

func (r *PgRepository) UpdateApplicationRating(ctx context.Context, id int64, rating int, comment string) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_applications SET rating=$2, rating_comment=$3, updated_at=now()
		WHERE id = $1 AND deleted = FALSE
	`, "Заявка не найдена", id, rating, comment)
}

func (r *PgRepository) MarkApplicationNotesViewed(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_applications SET notes_viewed = TRUE, updated_at = now()
		WHERE id = $1 AND deleted = FALSE
	`, "Заявка не найдена", id)
}

func (r *PgRepository) SoftDeleteApplication(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_applications SET deleted = TRUE, updated_at = now()
		WHERE id = $1 AND deleted = FALSE
	`, "Заявка не найдена", id)
}

// --- Certificates ---

const selectCertificateSQL = `
	SELECT cert.id, cert.application_id, cert.maintenance_contract_id, cert.without_maintenance_contract,
	       cert.contractor_id, c.name, cert.address_id, ca.address,
	       cert.engineer_id, cert.executors, cert.created_by, cert.work_date, cert.total_amount, cert.paid_amount,
	       cert.cost_of_parts, cert.receipt_number, cert.payment_method,
	       cert.work_types, cert.spare_parts, cert.results, cert.special_notes, cert.other_note,
	       cert.notes_viewed, cert.recommendation, cert.comment, cert.status,
	       an.id, an.rating,
	       cert.deleted, cert.created_at, cert.updated_at, cert.number, cert.source_data, cert.source_relations,
           COALESCE((SELECT jsonb_agg(jsonb_build_object('id',ce.id,'equipment_id',ce.equipment_id,'equipment_name',(SELECT name FROM crm_equipments WHERE id=ce.equipment_id),'serial_number',ce.serial_number) ORDER BY ce.id) FROM crm_certificate_equipments ce WHERE ce.certificate_id=cert.id),'[]'::jsonb)
	FROM crm_certificates cert
	JOIN crm_contractors c ON c.id = cert.contractor_id
	JOIN crm_contractor_addresses ca ON ca.id = cert.address_id
	LEFT JOIN LATERAL (
		SELECT an2.id, an2.rating
		FROM crm_analytics an2
		WHERE an2.certificate_id = cert.id AND an2.deleted = FALSE
		ORDER BY an2.id DESC LIMIT 1
	) an ON TRUE
`

func scanCertificate(row pgx.Row) (*Certificate, error) {
	var c Certificate
	var wtRaw, spRaw, resRaw, exRaw []byte
	err := row.Scan(
		&c.ID, &c.ApplicationID, &c.MaintenanceContractID, &c.WithoutMaintenanceContract,
		&c.ContractorID, &c.ContractorName, &c.AddressID, &c.AddressText,
		&c.EngineerID, &exRaw, &c.CreatedBy, &c.WorkDate, &c.TotalAmount, &c.PaidAmount,
		&c.CostOfParts, &c.ReceiptNumber, &c.PaymentMethod,
		&wtRaw, &spRaw, &resRaw, &c.SpecialNotes, &c.OtherNote,
		&c.NotesViewed, &c.Recommendation, &c.Comment, &c.Status,
		&c.AnalyticID, &c.AnalyticRating,
		&c.Deleted, &c.CreatedAt, &c.UpdatedAt, &c.Number, &c.SourceData, &c.SourceRelations, &c.Equipments,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(exRaw, &c.Executors)
	_ = json.Unmarshal(wtRaw, &c.WorkTypes)
	_ = json.Unmarshal(spRaw, &c.SpareParts)
	_ = json.Unmarshal(resRaw, &c.Results)
	return &c, nil
}

func (r *PgRepository) ListCertificates(ctx context.Context, f CertificateFilter) ([]Certificate, error) {
	limit := normalizeLimit(f.Limit, 50, 200)
	offset := max(0, f.Offset)
	where := []string{"cert.deleted = FALSE"}
	args := []any{}
	idx := 1
	if f.From != nil {
		where = append(where, fmt.Sprintf("cert.work_date >= $%d", idx))
		args = append(args, *f.From)
		idx++
	}
	if f.To != nil {
		where = append(where, fmt.Sprintf("cert.work_date <= $%d", idx))
		args = append(args, *f.To)
		idx++
	}
	if f.EngineerID != nil {
		where = append(where, fmt.Sprintf(`(cert.engineer_id = $%d OR EXISTS (
			SELECT 1 FROM jsonb_array_elements(cert.executors) ex WHERE (ex->>'user_id')::bigint = $%d
		))`, idx, idx))
		args = append(args, *f.EngineerID)
		idx++
	}
	if f.ContractorID != nil {
		where = append(where, fmt.Sprintf("cert.contractor_id = $%d", idx))
		args = append(args, *f.ContractorID)
		idx++
	}
	if f.ApplicationID != nil {
		where = append(where, fmt.Sprintf("cert.application_id = $%d", idx))
		args = append(args, *f.ApplicationID)
		idx++
	}
	args = append(args, limit, offset)
	sql := fmt.Sprintf("%s WHERE %s ORDER BY cert.work_date DESC, cert.id DESC LIMIT $%d OFFSET $%d",
		selectCertificateSQL, strings.Join(where, " AND "), idx, idx+1)
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Certificate
	for rows.Next() {
		cert, err := scanCertificate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *cert)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetCertificate(ctx context.Context, id int64) (*Certificate, error) {
	return scanCertificate(r.db.QueryRow(ctx, selectCertificateSQL+" WHERE cert.id = $1 AND cert.deleted = FALSE", id))
}

func (r *PgRepository) CreateCertificate(ctx context.Context, cert *Certificate) error {
	wt, _ := json.Marshal(cert.WorkTypes)
	sp, _ := json.Marshal(cert.SpareParts)
	res, _ := json.Marshal(cert.Results)
	ex, _ := json.Marshal(cert.Executors)
	if len(ex) == 0 {
		ex = []byte("[]")
	}
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_certificates (
			application_id, maintenance_contract_id, without_maintenance_contract,
			contractor_id, address_id, engineer_id, executors, created_by, work_date,
			total_amount, paid_amount, cost_of_parts, receipt_number, payment_method,
			work_types, spare_parts, results, special_notes, other_note, notes_viewed,
			recommendation, comment, status
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		RETURNING id, created_at, updated_at
	`, cert.ApplicationID, cert.MaintenanceContractID, cert.WithoutMaintenanceContract,
		cert.ContractorID, cert.AddressID, cert.EngineerID, ex, cert.CreatedBy, cert.WorkDate,
		cert.TotalAmount, cert.PaidAmount, cert.CostOfParts, cert.ReceiptNumber, cert.PaymentMethod,
		wt, sp, res, cert.SpecialNotes, cert.OtherNote, cert.NotesViewed,
		cert.Recommendation, cert.Comment, cert.Status,
	).Scan(&cert.ID, &cert.CreatedAt, &cert.UpdatedAt)
}

func (r *PgRepository) UpdateCertificate(ctx context.Context, cert *Certificate) error {
	wt, _ := json.Marshal(cert.WorkTypes)
	sp, _ := json.Marshal(cert.SpareParts)
	res, _ := json.Marshal(cert.Results)
	ex, _ := json.Marshal(cert.Executors)
	if len(ex) == 0 {
		ex = []byte("[]")
	}
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_certificates
		SET application_id=$2, maintenance_contract_id=$3, without_maintenance_contract=$4,
		    contractor_id=$5, address_id=$6, engineer_id=$7, executors=$8, work_date=$9,
		    total_amount=$10, paid_amount=$11, cost_of_parts=$12, receipt_number=$13, payment_method=$14,
		    work_types=$15, spare_parts=$16, results=$17, special_notes=$18, other_note=$19,
		    notes_viewed=$20, recommendation=$21, comment=$22, status=$23, updated_at=now()
		WHERE id = $1 AND deleted = FALSE
	`, "Акт не найден", cert.ID, cert.ApplicationID, cert.MaintenanceContractID, cert.WithoutMaintenanceContract,
		cert.ContractorID, cert.AddressID, cert.EngineerID, ex, cert.WorkDate,
		cert.TotalAmount, cert.PaidAmount, cert.CostOfParts, cert.ReceiptNumber, cert.PaymentMethod,
		wt, sp, res, cert.SpecialNotes, cert.OtherNote, cert.NotesViewed,
		cert.Recommendation, cert.Comment, cert.Status)
}

func (r *PgRepository) SoftDeleteCertificate(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_certificates SET deleted = TRUE, updated_at = now()
		WHERE id = $1 AND deleted = FALSE
	`, "Акт не найден", id)
}

// --- Tasks & Pool ---

func scanTask(row pgx.Row) (*Task, error) {
	var t Task
	var deadline *time.Time
	var cName, aText *string
	err := row.Scan(
		&t.ID, &t.ContractorID, &cName, &t.AddressID, &aText,
		&t.ApplicationID, &t.ContractID, &t.PrevTaskID, &t.TaskType, &t.TaskResult, &t.Title, &t.Description,
		&t.Status, &t.Priority, &t.AssignedUserID, &t.SubdivisionID,
		&deadline, &t.ExecutionDatetime, &t.DecisionTimeSec, &t.IsUrgent, &t.Note,
		&t.CreatedBy, &t.Deleted, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if cName != nil {
		t.ContractorName = *cName
	}
	if aText != nil {
		t.AddressText = *aText
	}
	t.MissingDeadline = deadline == nil
	if deadline != nil {
		t.PlanDatetimeTo = *deadline
	}
	return &t, nil
}

const selectTaskSQL = `
	SELECT t.id, t.contractor_id, c.name, t.address_id, ca.address,
	       t.application_id, t.contract_id, t.prev_task_id, t.task_type, t.task_result, t.title, t.description,
	       t.status, t.priority, t.assigned_user_id, t.subdivision_id,
	       t.plan_datetime_to, t.execution_datetime, t.decision_time_sec, t.is_urgent, t.note,
	       t.created_by, t.deleted, t.created_at, t.updated_at
	FROM crm_tasks t
	LEFT JOIN crm_contractors c ON c.id = t.contractor_id
	LEFT JOIN crm_contractor_addresses ca ON ca.id = t.address_id
`

func (r *PgRepository) ListTasks(ctx context.Context, f TaskFilter) ([]Task, error) {
	limit := normalizeLimit(f.Limit, 50, 500)
	offset := max(0, f.Offset)
	where := []string{"t.deleted = FALSE"}
	args := []any{}
	idx := 1
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, fmt.Sprintf("(t.title ILIKE '%%' || $%d || '%%' OR t.description ILIKE '%%' || $%d || '%%' OR COALESCE(c.name, '') ILIKE '%%' || $%d || '%%' OR CAST(t.id AS TEXT) ILIKE '%%' || $%d || '%%')", idx, idx, idx, idx))
		args = append(args, q)
		idx++
	}
	if f.AssignedUserID != nil {
		where = append(where, fmt.Sprintf("t.assigned_user_id = $%d", idx))
		args = append(args, *f.AssignedUserID)
		idx++
	}
	if f.SubdivisionID != nil {
		where = append(where, fmt.Sprintf("t.subdivision_id = $%d", idx))
		args = append(args, *f.SubdivisionID)
		idx++
	}
	if f.Status != nil {
		where = append(where, fmt.Sprintf("t.status = $%d", idx))
		args = append(args, *f.Status)
		idx++
	}
	if f.TaskType != nil {
		where = append(where, fmt.Sprintf("t.task_type = $%d", idx))
		args = append(args, *f.TaskType)
		idx++
	}
	if f.ContractorID != nil {
		where = append(where, fmt.Sprintf("t.contractor_id = $%d", idx))
		args = append(args, *f.ContractorID)
		idx++
	}
	if f.OnlyUrgent {
		where = append(where, "t.is_urgent = TRUE")
	}
	args = append(args, limit, offset)
	sql := fmt.Sprintf("%s WHERE %s ORDER BY t.priority DESC, t.plan_datetime_to ASC LIMIT $%d OFFSET $%d",
		selectTaskSQL, strings.Join(where, " AND "), idx, idx+1)
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetTask(ctx context.Context, id int64) (*Task, error) {
	return scanTask(r.db.QueryRow(ctx, selectTaskSQL+" WHERE t.id = $1 AND t.deleted = FALSE", id))
}

func (r *PgRepository) CreateTask(ctx context.Context, task *Task) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_tasks (
			contractor_id, address_id, application_id, contract_id, prev_task_id, task_type, task_result, title, description,
			status, priority, assigned_user_id, subdivision_id, plan_datetime_to, execution_datetime, decision_time_sec,
			is_urgent, note, created_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING id, created_at, updated_at
	`, task.ContractorID, task.AddressID, task.ApplicationID, task.ContractID, task.PrevTaskID, task.TaskType, task.TaskResult, task.Title, task.Description,
		task.Status, task.Priority, task.AssignedUserID, task.SubdivisionID, task.PlanDatetimeTo, task.ExecutionDatetime, task.DecisionTimeSec,
		task.IsUrgent, task.Note, task.CreatedBy,
	).Scan(&task.ID, &task.CreatedAt, &task.UpdatedAt)
}

func (r *PgRepository) UpdateTask(ctx context.Context, task *Task) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_tasks
		SET contractor_id=$2, address_id=$3, application_id=$4, contract_id=$5, prev_task_id=$6, task_type=$7, task_result=$8, title=$9,
		    description=$10, status=$11, priority=$12, assigned_user_id=$13, subdivision_id=$14,
		    plan_datetime_to=$15, execution_datetime=$16, decision_time_sec=$17, is_urgent=$18, note=$19, updated_at=now()
		WHERE id = $1 AND deleted = FALSE
	`, "Задача не найдена", task.ID, task.ContractorID, task.AddressID, task.ApplicationID, task.ContractID, task.PrevTaskID,
		task.TaskType, task.TaskResult, task.Title, task.Description, task.Status, task.Priority, task.AssignedUserID, task.SubdivisionID,
		task.PlanDatetimeTo, task.ExecutionDatetime, task.DecisionTimeSec, task.IsUrgent, task.Note)
}

func (r *PgRepository) SoftDeleteTask(ctx context.Context, id int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_tasks SET deleted = TRUE, updated_at = now()
		WHERE id = $1 AND deleted = FALSE
	`, "Задача не найдена", id)
}

func (r *PgRepository) ListUnassignedActiveTasks(ctx context.Context, limit int) ([]Task, error) {
	limit = normalizeLimit(limit, 100, 500)
	rows, err := r.db.Query(ctx, selectTaskSQL+`
		WHERE t.deleted = FALSE AND t.assigned_user_id IS NULL AND t.status IN (1, 2)
		ORDER BY t.priority DESC, t.plan_datetime_to ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *PgRepository) CountUserActivePoolTasks(ctx context.Context, userID int64, transferred bool) (int, error) {
	var cnt int
	query := `SELECT COUNT(*) FROM crm_tasks WHERE deleted = FALSE AND assigned_user_id = $1 AND status IN (1, 2)`
	if transferred {
		query += ` AND prev_task_id IS NOT NULL`
	}
	err := r.db.QueryRow(ctx, query, userID).Scan(&cnt)
	return cnt, err
}

func (r *PgRepository) ListOverdueActiveTasks(ctx context.Context, now time.Time) ([]Task, error) {
	rows, err := r.db.Query(ctx, selectTaskSQL+`
		WHERE t.deleted = FALSE AND t.status IN (1, 2)
		  AND (t.plan_datetime_to - make_interval(secs => t.decision_time_sec)) <= $1
		ORDER BY t.plan_datetime_to ASC
	`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *PgRepository) ListTaskPoolSettings(ctx context.Context) ([]TaskPoolSetting, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, name, task_types, priority, time_from, time_to, is_online, with_subdivision,
		       for_dispatcher, for_engineer, tasks_max_count, transferred_tasks_max_count, active, created_at, updated_at
		FROM crm_task_pool_settings
		ORDER BY priority DESC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskPoolSetting
	for rows.Next() {
		var s TaskPoolSetting
		var ttRaw []byte
		if err := rows.Scan(
			&s.ID, &s.Name, &ttRaw, &s.Priority, &s.TimeFrom, &s.TimeTo, &s.IsOnline, &s.WithSubdivision,
			&s.ForDispatcher, &s.ForEngineer, &s.TasksMaxCount, &s.TransferredTasksMaxCount, &s.Active, &s.CreatedAt, &s.UpdatedAt,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(ttRaw, &s.TaskTypes)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *PgRepository) SaveTaskPoolSetting(ctx context.Context, s *TaskPoolSetting) error {
	tt, _ := json.Marshal(s.TaskTypes)
	if s.ID == 0 {
		return r.db.QueryRow(ctx, `
			INSERT INTO crm_task_pool_settings (
				name, task_types, priority, time_from, time_to, is_online, with_subdivision,
				for_dispatcher, for_engineer, tasks_max_count, transferred_tasks_max_count, active
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			RETURNING id, created_at, updated_at
		`, s.Name, tt, s.Priority, s.TimeFrom, s.TimeTo, s.IsOnline, s.WithSubdivision,
			s.ForDispatcher, s.ForEngineer, s.TasksMaxCount, s.TransferredTasksMaxCount, s.Active,
		).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	}
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_task_pool_settings
		SET name=$2, task_types=$3, priority=$4, time_from=$5, time_to=$6, is_online=$7, with_subdivision=$8,
		    for_dispatcher=$9, for_engineer=$10, tasks_max_count=$11, transferred_tasks_max_count=$12, active=$13, updated_at=now()
		WHERE id = $1
	`, "Правило пула не найдено", s.ID, s.Name, tt, s.Priority, s.TimeFrom, s.TimeTo, s.IsOnline, s.WithSubdivision,
		s.ForDispatcher, s.ForEngineer, s.TasksMaxCount, s.TransferredTasksMaxCount, s.Active)
}

func (r *PgRepository) ListQueueUsers(ctx context.Context) ([]QueueUser, error) {
	rows, err := r.db.Query(ctx, `
		SELECT user_id, position, last_assigned_at, active
		FROM crm_queue_users
		WHERE active = TRUE
		ORDER BY COALESCE(last_assigned_at, '1970-01-01'::timestamptz) ASC, position ASC, user_id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []QueueUser
	for rows.Next() {
		var q QueueUser
		if err := rows.Scan(&q.UserID, &q.Position, &q.LastAssignedAt, &q.Active); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func (r *PgRepository) TouchQueueUser(ctx context.Context, userID int64, assignedAt time.Time) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO crm_queue_users (user_id, position, last_assigned_at, active)
		VALUES ($1, 0, $2, TRUE)
		ON CONFLICT (user_id) DO UPDATE SET last_assigned_at = EXCLUDED.last_assigned_at
	`, userID, assignedAt)
	return err
}

// --- Calls ---

const selectCallSQL = `
	SELECT c.id, c.external_call_id, c.abonent_id, c.ext_tracking_id, c.direction, c.status,
	       c.caller_phone, c.callee_phone, c.sip_login, c.user_id, c.contractor_id, COALESCE(ct.name, ''),
	       c.traffic_source_id, c.caller_type, c.call_result,
	       c.type_of_appeal, c.comment, c.record_url, c.transcript_text, c.transcript_status, c.transcript_error, c.transcribed_at,
	       c.duration_sec, c.started_at, c.answer_time, c.ended_at,
	       c.raw_payload, c.created_at, c.updated_at
	FROM crm_calls c
	LEFT JOIN crm_contractors ct ON ct.id = c.contractor_id
`

func scanCall(row pgx.Row) (*CallRecord, error) {
	var rec CallRecord
	err := row.Scan(
		&rec.ID, &rec.ExternalCallID, &rec.AbonentID, &rec.ExtTrackingID, &rec.Direction, &rec.Status,
		&rec.CallerPhone, &rec.CalleePhone, &rec.SipLogin, &rec.UserID, &rec.ContractorID, &rec.ContractorName,
		&rec.TrafficSourceID, &rec.CallerType, &rec.CallResult,
		&rec.TypeOfAppeal, &rec.Comment, &rec.RecordURL, &rec.TranscriptText, &rec.TranscriptStatus, &rec.TranscriptError, &rec.TranscribedAt,
		&rec.DurationSec, &rec.StartedAt, &rec.AnswerTime, &rec.EndedAt,
		&rec.RawPayload, &rec.CreatedAt, &rec.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *PgRepository) ListCalls(ctx context.Context, f CallFilter) ([]CallRecord, error) {
	limit := normalizeLimit(f.Limit, 50, 500)
	offset := max(0, f.Offset)
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if f.Phone != "" {
		where = append(where, fmt.Sprintf("(c.caller_phone ILIKE '%%' || $%d || '%%' OR c.callee_phone ILIKE '%%' || $%d || '%%')", idx, idx))
		args = append(args, f.Phone)
		idx++
	}
	if f.ContractorID != nil {
		where = append(where, fmt.Sprintf("c.contractor_id = $%d", idx))
		args = append(args, *f.ContractorID)
		idx++
	}
	if f.UserID != nil {
		where = append(where, fmt.Sprintf("c.user_id = $%d", idx))
		args = append(args, *f.UserID)
		idx++
	}
	if f.SubdivisionID != nil {
		where = append(where, fmt.Sprintf("(ct.subdivision_id = $%d OR EXISTS(SELECT 1 FROM crm_sip_accounts sa WHERE sa.abonent_id = c.abonent_id AND sa.subdivision_id = $%d))", idx, idx))
		args = append(args, *f.SubdivisionID)
		idx++
	}
	if len(f.Directions) > 0 {
		where = append(where, fmt.Sprintf("c.direction = ANY($%d)", idx))
		args = append(args, f.Directions)
		idx++
	}
	if len(f.Statuses) > 0 {
		where = append(where, fmt.Sprintf("c.status = ANY($%d)", idx))
		args = append(args, f.Statuses)
		idx++
	}
	if len(f.TypesOfAppeal) > 0 {
		where = append(where, fmt.Sprintf("c.type_of_appeal = ANY($%d)", idx))
		args = append(args, f.TypesOfAppeal)
		idx++
	}
	if len(f.CallerTypes) > 0 {
		where = append(where, fmt.Sprintf("c.caller_type = ANY($%d)", idx))
		args = append(args, f.CallerTypes)
		idx++
	}
	if len(f.CallResults) > 0 {
		where = append(where, fmt.Sprintf("c.call_result = ANY($%d)", idx))
		args = append(args, f.CallResults)
		idx++
	}
	if len(f.TrafficSourceIDs) > 0 {
		where = append(where, fmt.Sprintf("c.traffic_source_id = ANY($%d)", idx))
		args = append(args, f.TrafficSourceIDs)
		idx++
	}
	if f.From != nil {
		where = append(where, fmt.Sprintf("c.started_at >= $%d", idx))
		args = append(args, *f.From)
		idx++
	}
	if f.To != nil {
		where = append(where, fmt.Sprintf("c.started_at <= $%d", idx))
		args = append(args, *f.To)
		idx++
	}
	args = append(args, limit, offset)
	sql := fmt.Sprintf("%s WHERE %s ORDER BY c.started_at DESC LIMIT $%d OFFSET $%d",
		selectCallSQL, strings.Join(where, " AND "), idx, idx+1)
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CallRecord
	for rows.Next() {
		rec, err := scanCall(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

func (r *PgRepository) GetCall(ctx context.Context, id int64) (*CallRecord, error) {
	return scanCall(r.db.QueryRow(ctx, selectCallSQL+" WHERE c.id = $1", id))
}

func (r *PgRepository) UpdateCallTranscript(ctx context.Context, id int64, status, text, errMsg string, transcribedAt *time.Time) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_calls
		SET transcript_status = $2,
		    transcript_text = $3,
		    transcript_error = $4,
		    transcribed_at = $5,
		    updated_at = now()
		WHERE id = $1
	`, "Звонок не найден", id, status, text, errMsg, transcribedAt)
}

func (r *PgRepository) UpdateCallTranscriptError(ctx context.Context, id int64, errMsg string) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_calls
		SET transcript_status = CASE
		        WHEN transcript_status = 'done' AND COALESCE(transcript_text, '') <> '' THEN 'done'
		        ELSE 'failed'
		    END,
		    transcript_error = $2,
		    updated_at = now()
		WHERE id = $1
	`, "Звонок не найден", id, errMsg)
}

func (r *PgRepository) GetCallByExternalID(ctx context.Context, externalCallID string) (*CallRecord, error) {
	return scanCall(r.db.QueryRow(ctx, selectCallSQL+" WHERE c.external_call_id = $1", externalCallID))
}

func (r *PgRepository) UpsertCallByExternalID(ctx context.Context, call *CallRecord) error {
	raw := call.RawPayload
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	err := r.db.QueryRow(ctx, `
		INSERT INTO crm_calls (
			external_call_id, abonent_id, ext_tracking_id, direction, status,
			caller_phone, callee_phone, sip_login, user_id, contractor_id,
			traffic_source_id, caller_type, call_result,
			type_of_appeal, comment, record_url, duration_sec, started_at, answer_time, ended_at, raw_payload
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		ON CONFLICT (external_call_id) DO UPDATE SET
			abonent_id = CASE WHEN EXCLUDED.abonent_id <> '' THEN EXCLUDED.abonent_id ELSE crm_calls.abonent_id END,
			ext_tracking_id = CASE WHEN EXCLUDED.ext_tracking_id <> '' THEN EXCLUDED.ext_tracking_id ELSE crm_calls.ext_tracking_id END,
			direction = CASE WHEN EXCLUDED.direction <> '' THEN EXCLUDED.direction ELSE crm_calls.direction END,
			status = EXCLUDED.status,
			caller_phone = CASE WHEN EXCLUDED.caller_phone <> '' THEN EXCLUDED.caller_phone ELSE crm_calls.caller_phone END,
			callee_phone = CASE WHEN EXCLUDED.callee_phone <> '' THEN EXCLUDED.callee_phone ELSE crm_calls.callee_phone END,
			sip_login = CASE WHEN EXCLUDED.sip_login <> '' THEN EXCLUDED.sip_login ELSE crm_calls.sip_login END,
			user_id = COALESCE(EXCLUDED.user_id, crm_calls.user_id),
			contractor_id = COALESCE(EXCLUDED.contractor_id, crm_calls.contractor_id),
			traffic_source_id = COALESCE(EXCLUDED.traffic_source_id, crm_calls.traffic_source_id),
			caller_type = CASE WHEN EXCLUDED.caller_type <> 0 THEN EXCLUDED.caller_type ELSE crm_calls.caller_type END,
			call_result = CASE WHEN EXCLUDED.call_result <> 0 THEN EXCLUDED.call_result ELSE crm_calls.call_result END,
			type_of_appeal = CASE WHEN EXCLUDED.type_of_appeal <> '' THEN EXCLUDED.type_of_appeal ELSE crm_calls.type_of_appeal END,
			comment = CASE WHEN EXCLUDED.comment <> '' THEN EXCLUDED.comment ELSE crm_calls.comment END,
			record_url = CASE WHEN EXCLUDED.record_url <> '' THEN EXCLUDED.record_url ELSE crm_calls.record_url END,
			duration_sec = GREATEST(EXCLUDED.duration_sec, crm_calls.duration_sec),
			answer_time = COALESCE(EXCLUDED.answer_time, crm_calls.answer_time),
			ended_at = COALESCE(EXCLUDED.ended_at, crm_calls.ended_at),
			raw_payload = EXCLUDED.raw_payload,
			updated_at = now()
        WHERE EXCLUDED.external_call_id NOT LIKE 'mango:%'
           OR (COALESCE((EXCLUDED.raw_payload->>'mango_seq')::bigint,0) > COALESCE((crm_calls.raw_payload->>'mango_seq')::bigint,0)
               AND (crm_calls.ended_at IS NULL OR EXCLUDED.ended_at IS NOT NULL))
		RETURNING id, user_id, contractor_id, created_at, updated_at
	`, call.ExternalCallID, call.AbonentID, call.ExtTrackingID, call.Direction, call.Status,
		call.CallerPhone, call.CalleePhone, call.SipLogin, call.UserID, call.ContractorID,
		call.TrafficSourceID, call.CallerType, call.CallResult,
		call.TypeOfAppeal, call.Comment, call.RecordURL, call.DurationSec, call.StartedAt, call.AnswerTime, call.EndedAt, raw,
	).Scan(&call.ID, &call.UserID, &call.ContractorID, &call.CreatedAt, &call.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return errStaleCallEvent
	}
	return err
}

func (r *PgRepository) UpdateCallCard(ctx context.Context, call *CallRecord) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_calls
		SET contractor_id = $2, traffic_source_id = $3, caller_type = $4, call_result = $5,
		    type_of_appeal = $6, comment = $7, updated_at = now()
		WHERE id = $1
	`, "Звонок не найден", call.ID, call.ContractorID, call.TrafficSourceID, call.CallerType, call.CallResult, call.TypeOfAppeal, call.Comment)
}

// --- Reports, Audit, Notifications, Settings ---

func (r *PgRepository) AmountActsReport(ctx context.Context, from, to time.Time, engineerID *int64) ([]AmountActsReportRow, error) {
	where := []string{"deleted = FALSE", "work_date BETWEEN $1 AND $2"}
	args := []any{from, to}
	if engineerID != nil {
		where = append(where, "engineer_id = $3")
		args = append(args, *engineerID)
	}
	sql := fmt.Sprintf(`
		SELECT engineer_id, COUNT(*), COALESCE(SUM(total_amount), 0), COALESCE(SUM(paid_amount), 0)
		FROM crm_certificates
		WHERE %s
		GROUP BY engineer_id
		ORDER BY SUM(total_amount) DESC
	`, strings.Join(where, " AND "))
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AmountActsReportRow
	for rows.Next() {
		var row AmountActsReportRow
		if err := rows.Scan(&row.EngineerID, &row.CertificatesCount, &row.TotalAmount, &row.PaidAmount); err != nil {
			return nil, err
		}
		row.DebtAmount = row.TotalAmount - row.PaidAmount
		row.EngineerName = "Инженер #" + strconv.FormatInt(row.EngineerID, 10)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *PgRepository) ExpiredTasksReport(ctx context.Context, now time.Time, userID *int64) ([]ExpiredTaskReportRow, error) {
	where := []string{"t.deleted = FALSE", "t.status IN (1, 2)", "t.plan_datetime_to < $1"}
	args := []any{now}
	if userID != nil {
		where = append(where, "t.assigned_user_id = $2")
		args = append(args, *userID)
	}
	sql := fmt.Sprintf(`
		SELECT t.id, t.title, t.task_type, t.assigned_user_id, COALESCE(c.name, ''), t.plan_datetime_to,
		       EXTRACT(EPOCH FROM ($1 - t.plan_datetime_to))::bigint / 60 AS overdue_minutes
		FROM crm_tasks t
		LEFT JOIN crm_contractors c ON c.id = t.contractor_id
		WHERE %s
		ORDER BY overdue_minutes DESC
	`, strings.Join(where, " AND "))
	rows, err := r.db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExpiredTaskReportRow
	for rows.Next() {
		var row ExpiredTaskReportRow
		if err := rows.Scan(&row.TaskID, &row.Title, &row.TaskType, &row.AssignedUserID, &row.ContractorName, &row.PlanDatetimeTo, &row.OverdueMinutes); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (r *PgRepository) AddChangeLog(ctx context.Context, entry ChangeLog) error {
	ch := entry.Changes
	if len(ch) == 0 {
		ch = json.RawMessage(`{}`)
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO crm_change_logs (entity_type, entity_id, action, user_id, changes)
		VALUES ($1, $2, $3, $4, $5)
	`, entry.EntityType, entry.EntityID, entry.Action, entry.UserID, ch)
	return err
}

func (r *PgRepository) ListChangeLogs(ctx context.Context, entityType string, entityID int64, limit int) ([]ChangeLog, error) {
	limit = normalizeLimit(limit, 50, 200)
	rows, err := r.db.Query(ctx, `
		SELECT id, entity_type, entity_id, action, user_id, changes, created_at
		FROM crm_change_logs
		WHERE entity_type = $1 AND entity_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT $3
	`, entityType, entityID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChangeLog
	for rows.Next() {
		var entry ChangeLog
		if err := rows.Scan(&entry.ID, &entry.EntityType, &entry.EntityID, &entry.Action, &entry.UserID, &entry.Changes, &entry.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, rows.Err()
}

func (r *PgRepository) CreateNotification(ctx context.Context, n *Notification) error {
	return r.db.QueryRow(ctx, `
		INSERT INTO crm_notifications (user_id, kind, title, message, entity_type, entity_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, is_read, created_at
	`, n.UserID, n.Kind, n.Title, n.Message, n.EntityType, n.EntityID).Scan(&n.ID, &n.IsRead, &n.CreatedAt)
}

func (r *PgRepository) ListUserNotifications(ctx context.Context, userID int64, unreadOnly bool, limit int) ([]Notification, error) {
	limit = normalizeLimit(limit, 30, 100)
	sql := `
		SELECT id, user_id, kind, title, message, entity_type, entity_id, is_read, created_at
		FROM crm_notifications
		WHERE user_id = $1
	`
	if unreadOnly {
		sql += " AND is_read = FALSE"
	}
	sql += " ORDER BY created_at DESC LIMIT $2"
	rows, err := r.db.Query(ctx, sql, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notification
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, &n.Title, &n.Message, &n.EntityType, &n.EntityID, &n.IsRead, &n.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (r *PgRepository) MarkNotificationRead(ctx context.Context, userID, notificationID int64) error {
	return platform.ExecExpectingOne(ctx, r.db, `
		UPDATE crm_notifications SET is_read = TRUE
		WHERE id = $1 AND user_id = $2
	`, "Уведомление не найдено", notificationID, userID)
}

func (r *PgRepository) MarkAllNotificationsRead(ctx context.Context, userID int64) error {
	_, err := r.db.Exec(ctx, `UPDATE crm_notifications SET is_read = TRUE WHERE user_id = $1 AND is_read = FALSE`, userID)
	return err
}

func (r *PgRepository) DeleteOldReadNotifications(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM crm_notifications WHERE is_read = TRUE AND created_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *PgRepository) GetUserSetting(ctx context.Context, userID int64, key string) (json.RawMessage, error) {
	var raw json.RawMessage
	err := r.db.QueryRow(ctx, `SELECT value_json FROM crm_user_settings WHERE user_id = $1 AND key = $2`, userID, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	return raw, err
}

func (r *PgRepository) SaveUserSetting(ctx context.Context, userID int64, key string, val json.RawMessage) error {
	if len(val) == 0 {
		val = json.RawMessage(`{}`)
	}
	_, err := r.db.Exec(ctx, `
		INSERT INTO crm_user_settings (user_id, key, value_json)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, key) DO UPDATE SET value_json = EXCLUDED.value_json, updated_at = now()
	`, userID, key, val)
	return err
}
