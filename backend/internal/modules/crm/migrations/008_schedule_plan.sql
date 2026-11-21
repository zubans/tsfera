-- Табель инженеров: смены (рабочие и дежурные) и отсутствия (отпуск, больничный, б/с, резерв).
-- subdivision_id — город, где инженер работает в эту смену; пусто — подразделение сотрудника.
CREATE TABLE IF NOT EXISTS crm_timesheet_entries (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    subdivision_id BIGINT,
    kind VARCHAR(16) NOT NULL,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    comment TEXT NOT NULL DEFAULT '',
    created_by BIGINT,
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT crm_timesheet_entries_interval CHECK (ends_at > starts_at),
    CONSTRAINT crm_timesheet_entries_kind CHECK (kind IN ('work', 'duty', 'vacation', 'sick', 'unpaid', 'reserve'))
);

CREATE INDEX IF NOT EXISTS idx_crm_timesheet_user_time ON crm_timesheet_entries(user_id, starts_at) WHERE deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_crm_timesheet_time ON crm_timesheet_entries(starts_at, ends_at) WHERE deleted = FALSE;

-- Дневной финансовый план инженера по подразделению (городу), действует с указанной даты.
CREATE TABLE IF NOT EXISTS crm_daily_plans (
    id BIGSERIAL PRIMARY KEY,
    subdivision_id BIGINT NOT NULL,
    amount NUMERIC(12,2) NOT NULL CHECK (amount >= 0),
    effective_from DATE NOT NULL,
    created_by BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (subdivision_id, effective_from)
);
