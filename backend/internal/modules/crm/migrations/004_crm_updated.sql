-- Обновление схемы CRM под расширенную функциональность (m230604..m260827).

-- 1. Подразделения: руководитель и способы оплаты (QR/референсы на файлы).
ALTER TABLE crm_subdivisions
    ADD COLUMN IF NOT EXISTS supervisor_user_id BIGINT,
    ADD COLUMN IF NOT EXISTS payment_options JSONB NOT NULL DEFAULT '[]'::jsonb;

-- 2. SIP-аккаунты: послабление обязательности устаревших SoftPhone-полей.
ALTER TABLE crm_sip_accounts
    ALTER COLUMN domain SET DEFAULT '',
    ALTER COLUMN login SET DEFAULT '',
    ALTER COLUMN password SET DEFAULT '',
    ALTER COLUMN server SET DEFAULT '';

-- 3. Источники трафика.
CREATE TABLE IF NOT EXISTS crm_traffic_sources (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    traffic_source_type_id SMALLINT NOT NULL DEFAULT 1,
    sort INT NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO crm_traffic_sources (name, traffic_source_type_id, sort, active)
SELECT v.name, v.tid, v.sort, TRUE
FROM (VALUES
    ('Сайт / Поиск', 1, 10),
    ('Авито', 2, 20),
    ('Яндекс / Карты', 1, 30),
    ('2ГИС', 1, 40),
    ('Рекомендация / Повторный клиент', 3, 50),
    ('Телефония / Входящий звонок', 4, 60)
) AS v(name, tid, sort)
WHERE NOT EXISTS (SELECT 1 FROM crm_traffic_sources LIMIT 1);

-- 4. Контрагенты: признак юрлица, источник трафика и таблица реквизитов юрлица.
ALTER TABLE crm_contractors
    ADD COLUMN IF NOT EXISTS is_legal_entity BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS traffic_source_id BIGINT;

UPDATE crm_contractors
SET is_legal_entity = TRUE
WHERE type = 2 AND is_legal_entity = FALSE;

CREATE TABLE IF NOT EXISTS crm_legal_entities (
    contractor_id BIGINT PRIMARY KEY REFERENCES crm_contractors(id) ON DELETE CASCADE,
    name TEXT NOT NULL DEFAULT '',
    director_name TEXT NOT NULL DEFAULT '',
    actual_address TEXT NOT NULL DEFAULT '',
    legal_address TEXT NOT NULL DEFAULT '',
    bank_name TEXT NOT NULL DEFAULT '',
    inn VARCHAR(32) NOT NULL DEFAULT '',
    kpp VARCHAR(32) NOT NULL DEFAULT '',
    bik VARCHAR(32) NOT NULL DEFAULT '',
    checking_account VARCHAR(64) NOT NULL DEFAULT '',
    corresponding_account VARCHAR(64) NOT NULL DEFAULT '',
    okpo VARCHAR(32) NOT NULL DEFAULT '',
    ogrn VARCHAR(32) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_crm_legal_entities_inn ON crm_legal_entities(inn) WHERE inn <> '';

-- 5. Заявки: источник трафика, плановая сумма, окно выполнения, соисполнители.
ALTER TABLE crm_applications
    ADD COLUMN IF NOT EXISTS traffic_source_id BIGINT,
    ADD COLUMN IF NOT EXISTS planned_sum NUMERIC(12,2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS work_window_from TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS work_window_to TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS executor_ids JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS idx_crm_applications_traffic_source ON crm_applications(traffic_source_id);

-- 6. Акты выполненных работ: соисполнители с распределением суммы.
ALTER TABLE crm_certificates
    ADD COLUMN IF NOT EXISTS executors JSONB NOT NULL DEFAULT '[]'::jsonb;

-- 7. Звонки: тип звонящего, результат звонка, источник трафика.
ALTER TABLE crm_calls
    ADD COLUMN IF NOT EXISTS traffic_source_id BIGINT,
    ADD COLUMN IF NOT EXISTS caller_type SMALLINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS call_result SMALLINT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_crm_calls_caller_result ON crm_calls(caller_type, call_result);
CREATE INDEX IF NOT EXISTS idx_crm_calls_traffic_source ON crm_calls(traffic_source_id);

-- 8. Пул пропущенных звонков.
CREATE TABLE IF NOT EXISTS crm_miss_call_pool (
    id BIGSERIAL PRIMARY KEY,
    call_id BIGINT NOT NULL REFERENCES crm_calls(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL DEFAULT 0,
    is_viewed BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (call_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_crm_miss_call_pool_viewed ON crm_miss_call_pool(is_viewed, created_at DESC);

-- 9. Аналитика технических работ: вопросы и анкеты обратной связи.
CREATE TABLE IF NOT EXISTS crm_analytic_questions (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    analytic_document_type_id SMALLINT NOT NULL DEFAULT 1,
    sort INT NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    options JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS crm_analytics (
    id BIGSERIAL PRIMARY KEY,
    number VARCHAR(64) NOT NULL DEFAULT '',
    certificate_id BIGINT NOT NULL REFERENCES crm_certificates(id) ON DELETE CASCADE,
    rating SMALLINT NOT NULL DEFAULT 0,
    comment TEXT NOT NULL DEFAULT '',
    status SMALLINT NOT NULL DEFAULT 1,
    answers JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by BIGINT,
    deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_crm_analytics_cert ON crm_analytics(certificate_id) WHERE deleted = FALSE;

-- 10. Баннеры для инженеров и настройки приложения.
CREATE TABLE IF NOT EXISTS crm_banners (
    id BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    text TEXT NOT NULL DEFAULT '',
    file_id BIGINT,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS crm_app_settings (
    key VARCHAR(128) PRIMARY KEY,
    name TEXT NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO crm_app_settings (key, name, data)
VALUES ('app.banners_for_engineer', 'Баннеры для инженера (до и после создания акта)', '{"before_create":[],"after_create":[]}'::jsonb)
ON CONFLICT (key) DO NOTHING;
