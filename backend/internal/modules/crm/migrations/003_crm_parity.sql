ALTER TABLE crm_subdivisions
    ADD COLUMN IF NOT EXISTS phones JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE crm_sip_accounts
    ADD COLUMN IF NOT EXISTS abonent_id VARCHAR(120) NOT NULL DEFAULT '';

ALTER TABLE crm_contractors
    ADD COLUMN IF NOT EXISTS first_name     VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS second_name    VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS last_name      VARCHAR(100) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS subdivision_id BIGINT REFERENCES crm_subdivisions(id) ON DELETE SET NULL;

ALTER TABLE crm_maintenance_contracts
    ADD COLUMN IF NOT EXISTS category       VARCHAR(32) NOT NULL DEFAULT 'standard',
    ADD COLUMN IF NOT EXISTS application_id BIGINT REFERENCES crm_applications(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS equipments     JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE crm_certificates
    ADD COLUMN IF NOT EXISTS maintenance_contract_id      BIGINT REFERENCES crm_maintenance_contracts(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS without_maintenance_contract BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS receipt_number               VARCHAR(120) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cost_of_parts                NUMERIC(12,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS notes_viewed                 BOOLEAN NOT NULL DEFAULT TRUE;

ALTER TABLE crm_tasks
    ADD COLUMN IF NOT EXISTS task_result        VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prev_task_id       BIGINT REFERENCES crm_tasks(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS execution_datetime TIMESTAMPTZ;

ALTER TABLE crm_calls
    ADD COLUMN IF NOT EXISTS abonent_id      VARCHAR(120) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ext_tracking_id VARCHAR(160) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS type_of_appeal  VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS comment         TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS answer_time     TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS crm_ref_items (
    id         BIGSERIAL PRIMARY KEY,
    kind       VARCHAR(32) NOT NULL,
    name       VARCHAR(255) NOT NULL,
    deleted    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_crm_ref_items_kind
    ON crm_ref_items (kind, name) WHERE deleted = FALSE;

INSERT INTO crm_ref_items (kind, name)
SELECT v.kind, v.name
FROM (VALUES
    ('work_type', 'ТО'),
    ('work_type', 'Ремонт'),
    ('work_type', 'Пусконаладка'),
    ('work_type', 'Диагностика'),
    ('work_type', 'Монтаж'),
    ('work_type', 'Аварийный выезд'),
    ('work_type', 'Прочее'),
    ('manufacturer', 'Baxi'),
    ('manufacturer', 'Bosch'),
    ('manufacturer', 'Vaillant'),
    ('manufacturer', 'Protherm'),
    ('manufacturer', 'Navien'),
    ('manufacturer', 'Viessmann'),
    ('equipment_type', 'Газовый котёл'),
    ('equipment_type', 'Бойлер'),
    ('equipment_type', 'Газовая колонка'),
    ('equipment_type', 'Автоматика'),
    ('special_note', 'Требуется замена запчасти'),
    ('special_note', 'Истекает срок поверки счётчика'),
    ('special_note', 'Рекомендована пролонгация договора'),
    ('special_note', 'Аварийная остановка / утечка')
) AS v(kind, name)
WHERE NOT EXISTS (SELECT 1 FROM crm_ref_items);
