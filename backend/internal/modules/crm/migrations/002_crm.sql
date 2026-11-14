CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS crm_subdivisions (
    id         BIGSERIAL PRIMARY KEY,
    name       VARCHAR(200) NOT NULL,
    address    TEXT NOT NULL DEFAULT '',
    deleted    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS crm_sip_accounts (
    id             BIGSERIAL PRIMARY KEY,
    name           VARCHAR(120) NOT NULL,
    domain         VARCHAR(255) NOT NULL,
    login          VARCHAR(120) NOT NULL,
    password       TEXT NOT NULL,
    protocol       VARCHAR(16) NOT NULL DEFAULT 'wss',
    server         VARCHAR(255) NOT NULL,
    port           INT NOT NULL DEFAULT 7443,
    subdivision_id BIGINT REFERENCES crm_subdivisions(id) ON DELETE SET NULL,
    user_id        BIGINT,
    active         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS crm_contractors (
    id         BIGSERIAL PRIMARY KEY,
    name       VARCHAR(255) NOT NULL,
    type       SMALLINT NOT NULL DEFAULT 1,
    note       TEXT NOT NULL DEFAULT '',
    created_by BIGINT NOT NULL DEFAULT 0,
    deleted    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_crm_contractors_name_trgm
    ON crm_contractors USING gin (name gin_trgm_ops) WHERE deleted = FALSE;

CREATE TABLE IF NOT EXISTS crm_contractor_phones (
    id            BIGSERIAL PRIMARY KEY,
    contractor_id BIGINT NOT NULL REFERENCES crm_contractors(id) ON DELETE CASCADE,
    phone         VARCHAR(10) NOT NULL,
    note          VARCHAR(200) NOT NULL DEFAULT '',
    deleted       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_crm_contractor_phones_active
    ON crm_contractor_phones (contractor_id, phone) WHERE deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_crm_contractor_phones_phone
    ON crm_contractor_phones (phone) WHERE deleted = FALSE;

CREATE TABLE IF NOT EXISTS crm_contractor_emails (
    id            BIGSERIAL PRIMARY KEY,
    contractor_id BIGINT NOT NULL REFERENCES crm_contractors(id) ON DELETE CASCADE,
    email         VARCHAR(255) NOT NULL,
    deleted       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_crm_contractor_emails_active
    ON crm_contractor_emails (contractor_id, lower(email)) WHERE deleted = FALSE;

CREATE TABLE IF NOT EXISTS crm_contractor_addresses (
    id            BIGSERIAL PRIMARY KEY,
    contractor_id BIGINT NOT NULL REFERENCES crm_contractors(id) ON DELETE CASCADE,
    address       TEXT NOT NULL,
    region_code   VARCHAR(16) NOT NULL DEFAULT '',
    city          VARCHAR(120) NOT NULL DEFAULT '',
    street        VARCHAR(200) NOT NULL DEFAULT '',
    house         VARCHAR(64) NOT NULL DEFAULT '',
    apartment     VARCHAR(64) NOT NULL DEFAULT '',
    floor         VARCHAR(32) NOT NULL DEFAULT '',
    entrance      VARCHAR(32) NOT NULL DEFAULT '',
    comment       TEXT NOT NULL DEFAULT '',
    deleted       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_crm_addresses_trgm
    ON crm_contractor_addresses USING gin (address gin_trgm_ops) WHERE deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_crm_addresses_contractor
    ON crm_contractor_addresses (contractor_id) WHERE deleted = FALSE;

CREATE TABLE IF NOT EXISTS crm_equipments (
    id           BIGSERIAL PRIMARY KEY,
    type         VARCHAR(120) NOT NULL,
    model        VARCHAR(120) NOT NULL,
    manufacturer VARCHAR(120) NOT NULL DEFAULT '',
    name         VARCHAR(400) NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_crm_equipments_name_trgm
    ON crm_equipments USING gin (name gin_trgm_ops);

CREATE TABLE IF NOT EXISTS crm_address_equipments (
    id            BIGSERIAL PRIMARY KEY,
    address_id    BIGINT NOT NULL REFERENCES crm_contractor_addresses(id) ON DELETE CASCADE,
    equipment_id  BIGINT NOT NULL REFERENCES crm_equipments(id) ON DELETE RESTRICT,
    serial_number VARCHAR(120) NOT NULL DEFAULT '',
    note          TEXT NOT NULL DEFAULT '',
    deleted       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_crm_address_equipments_addr
    ON crm_address_equipments (address_id) WHERE deleted = FALSE;

CREATE TABLE IF NOT EXISTS crm_maintenance_contracts (
    id            BIGSERIAL PRIMARY KEY,
    contractor_id BIGINT NOT NULL REFERENCES crm_contractors(id) ON DELETE RESTRICT,
    address_id    BIGINT NOT NULL REFERENCES crm_contractor_addresses(id) ON DELETE RESTRICT,
    number        VARCHAR(120) NOT NULL,
    price         NUMERIC(12,2) NOT NULL DEFAULT 0,
    signed_at     DATE NOT NULL,
    valid_from    DATE NOT NULL,
    valid_to      DATE NOT NULL,
    status        SMALLINT NOT NULL DEFAULT 1,
    note          TEXT NOT NULL DEFAULT '',
    created_by    BIGINT NOT NULL DEFAULT 0,
    deleted       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_crm_contracts_valid_to
    ON crm_maintenance_contracts (valid_to) WHERE deleted = FALSE;

CREATE TABLE IF NOT EXISTS crm_applications (
    id                 BIGSERIAL PRIMARY KEY,
    contractor_id      BIGINT NOT NULL REFERENCES crm_contractors(id) ON DELETE RESTRICT,
    address_id         BIGINT NOT NULL REFERENCES crm_contractor_addresses(id) ON DELETE RESTRICT,
    engineer_id        BIGINT NOT NULL,
    subdivision_id     BIGINT REFERENCES crm_subdivisions(id) ON DELETE SET NULL,
    created_by         BIGINT NOT NULL DEFAULT 0,
    plan_datetime_from TIMESTAMPTZ NOT NULL,
    plan_datetime_to   TIMESTAMPTZ NOT NULL,
    status             SMALLINT NOT NULL DEFAULT 1,
    is_complete        BOOLEAN NOT NULL DEFAULT FALSE,
    is_duty            BOOLEAN NOT NULL DEFAULT FALSE,
    work_types         JSONB NOT NULL DEFAULT '[]'::jsonb,
    price_note         TEXT NOT NULL DEFAULT '',
    comment            TEXT NOT NULL DEFAULT '',
    special_notes      TEXT NOT NULL DEFAULT '',
    notes_viewed       BOOLEAN NOT NULL DEFAULT TRUE,
    rating             SMALLINT,
    rating_comment     TEXT NOT NULL DEFAULT '',
    equipment_ids      JSONB NOT NULL DEFAULT '[]'::jsonb,
    serial_numbers     JSONB NOT NULL DEFAULT '[]'::jsonb,
    deleted            BOOLEAN NOT NULL DEFAULT FALSE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_crm_apps_engineer_plan
    ON crm_applications (engineer_id, plan_datetime_from, plan_datetime_to) WHERE deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_crm_apps_contractor
    ON crm_applications (contractor_id) WHERE deleted = FALSE;

CREATE TABLE IF NOT EXISTS crm_certificates (
    id             BIGSERIAL PRIMARY KEY,
    application_id BIGINT REFERENCES crm_applications(id) ON DELETE SET NULL,
    contractor_id  BIGINT NOT NULL REFERENCES crm_contractors(id) ON DELETE RESTRICT,
    address_id     BIGINT NOT NULL REFERENCES crm_contractor_addresses(id) ON DELETE RESTRICT,
    engineer_id    BIGINT NOT NULL,
    created_by     BIGINT NOT NULL DEFAULT 0,
    work_date      DATE NOT NULL,
    total_amount   NUMERIC(12,2) NOT NULL DEFAULT 0,
    paid_amount    NUMERIC(12,2) NOT NULL DEFAULT 0,
    payment_method VARCHAR(32) NOT NULL DEFAULT 'cash',
    work_types     JSONB NOT NULL DEFAULT '[]'::jsonb,
    spare_parts    JSONB NOT NULL DEFAULT '[]'::jsonb,
    results        JSONB NOT NULL DEFAULT '{}'::jsonb,
    special_notes  TEXT NOT NULL DEFAULT '',
    other_note     TEXT NOT NULL DEFAULT '',
    recommendation TEXT NOT NULL DEFAULT '',
    comment        TEXT NOT NULL DEFAULT '',
    status         SMALLINT NOT NULL DEFAULT 2,
    deleted        BOOLEAN NOT NULL DEFAULT FALSE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_crm_certs_application
    ON crm_certificates (application_id) WHERE deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_crm_certs_work_date
    ON crm_certificates (work_date DESC) WHERE deleted = FALSE;

CREATE TABLE IF NOT EXISTS crm_tasks (
    id                BIGSERIAL PRIMARY KEY,
    contractor_id     BIGINT REFERENCES crm_contractors(id) ON DELETE SET NULL,
    address_id        BIGINT REFERENCES crm_contractor_addresses(id) ON DELETE SET NULL,
    application_id    BIGINT REFERENCES crm_applications(id) ON DELETE SET NULL,
    contract_id       BIGINT REFERENCES crm_maintenance_contracts(id) ON DELETE SET NULL,
    task_type         VARCHAR(32) NOT NULL DEFAULT 'call',
    title             VARCHAR(255) NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    status            SMALLINT NOT NULL DEFAULT 1,
    priority          INT NOT NULL DEFAULT 10,
    assigned_user_id  BIGINT,
    subdivision_id    BIGINT REFERENCES crm_subdivisions(id) ON DELETE SET NULL,
    plan_datetime_to  TIMESTAMPTZ NOT NULL,
    decision_time_sec INT NOT NULL DEFAULT 3600,
    is_urgent         BOOLEAN NOT NULL DEFAULT FALSE,
    note              TEXT NOT NULL DEFAULT '',
    created_by        BIGINT NOT NULL DEFAULT 0,
    deleted           BOOLEAN NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_crm_tasks_status_assigned
    ON crm_tasks (status, assigned_user_id, plan_datetime_to) WHERE deleted = FALSE;

CREATE TABLE IF NOT EXISTS crm_task_pool_settings (
    id                          BIGSERIAL PRIMARY KEY,
    name                        VARCHAR(120) NOT NULL,
    task_types                  JSONB NOT NULL DEFAULT '[]'::jsonb,
    priority                    INT NOT NULL DEFAULT 10,
    time_from                   VARCHAR(5) NOT NULL DEFAULT '08:00',
    time_to                     VARCHAR(5) NOT NULL DEFAULT '20:00',
    is_online                   BOOLEAN NOT NULL DEFAULT TRUE,
    with_subdivision            BOOLEAN NOT NULL DEFAULT FALSE,
    for_dispatcher              BOOLEAN NOT NULL DEFAULT TRUE,
    for_engineer                BOOLEAN NOT NULL DEFAULT FALSE,
    tasks_max_count             INT NOT NULL DEFAULT 5,
    transferred_tasks_max_count INT NOT NULL DEFAULT 3,
    active                      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS crm_queue_users (
    user_id          BIGINT PRIMARY KEY,
    position         INT NOT NULL DEFAULT 0,
    last_assigned_at TIMESTAMPTZ,
    active           BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS crm_calls (
    id               BIGSERIAL PRIMARY KEY,
    external_call_id VARCHAR(120) NOT NULL UNIQUE,
    direction        VARCHAR(16) NOT NULL DEFAULT 'inbound',
    status           VARCHAR(32) NOT NULL DEFAULT 'ringing',
    caller_phone     VARCHAR(32) NOT NULL,
    callee_phone     VARCHAR(32) NOT NULL,
    sip_login        VARCHAR(120) NOT NULL DEFAULT '',
    user_id          BIGINT,
    contractor_id    BIGINT REFERENCES crm_contractors(id) ON DELETE SET NULL,
    record_url       TEXT NOT NULL DEFAULT '',
    duration_sec     INT NOT NULL DEFAULT 0,
    started_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at         TIMESTAMPTZ,
    raw_payload      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_crm_calls_started
    ON crm_calls (started_at DESC);

CREATE TABLE IF NOT EXISTS crm_change_logs (
    id          BIGSERIAL PRIMARY KEY,
    entity_type VARCHAR(64) NOT NULL,
    entity_id   BIGINT NOT NULL,
    action      VARCHAR(32) NOT NULL,
    user_id     BIGINT NOT NULL DEFAULT 0,
    changes     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_crm_change_logs_entity
    ON crm_change_logs (entity_type, entity_id, created_at DESC);

CREATE TABLE IF NOT EXISTS crm_notifications (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    kind        VARCHAR(32) NOT NULL DEFAULT 'info',
    title       VARCHAR(200) NOT NULL,
    message     TEXT NOT NULL,
    entity_type VARCHAR(64) NOT NULL DEFAULT '',
    entity_id   BIGINT NOT NULL DEFAULT 0,
    is_read     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_crm_notifications_user_unread
    ON crm_notifications (user_id, is_read, created_at DESC);

CREATE TABLE IF NOT EXISTS crm_user_settings (
    user_id    BIGINT NOT NULL,
    key        VARCHAR(100) NOT NULL,
    value_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key)
);

INSERT INTO crm_subdivisions (name, address)
SELECT 'Основное подразделение', 'Центральный офис'
WHERE NOT EXISTS (SELECT 1 FROM crm_subdivisions);

INSERT INTO crm_equipments (type, model, manufacturer, name)
SELECT 'Газовый котёл', 'Eco Four 24F', 'Baxi', 'Газовый котёл Eco Four 24F Baxi'
WHERE NOT EXISTS (SELECT 1 FROM crm_equipments);

INSERT INTO crm_task_pool_settings (name, task_types, priority, time_from, time_to, is_online, with_subdivision, for_dispatcher, for_engineer, tasks_max_count, transferred_tasks_max_count, active)
SELECT 'Диспетчерский пул (рабочее время)', '["call","quarterly_call","prolongation","no_answer","service","other"]'::jsonb, 10, '08:00', '20:00', TRUE, FALSE, TRUE, FALSE, 5, 3, TRUE
WHERE NOT EXISTS (SELECT 1 FROM crm_task_pool_settings);
