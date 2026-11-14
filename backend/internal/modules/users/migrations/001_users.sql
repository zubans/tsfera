CREATE TABLE IF NOT EXISTS users_accounts (
    id                     BIGSERIAL PRIMARY KEY,
    username               VARCHAR(100) NOT NULL UNIQUE,
    password_hash          TEXT NOT NULL,
    email                  VARCHAR(255),
    first_name             VARCHAR(100) NOT NULL DEFAULT '',
    second_name            VARCHAR(100) NOT NULL DEFAULT '',
    last_name              VARCHAR(100) NOT NULL DEFAULT '',
    full_name              VARCHAR(300) NOT NULL DEFAULT '',
    subdivision_id         BIGINT,
    is_engineer            BOOLEAN NOT NULL DEFAULT FALSE,
    is_dispatcher          BOOLEAN NOT NULL DEFAULT FALSE,
    color                  VARCHAR(32) NOT NULL DEFAULT '#3b82f6',
    corporate_phone_number VARCHAR(32) NOT NULL DEFAULT '',
    office_equipment       TEXT NOT NULL DEFAULT '',
    training               TEXT NOT NULL DEFAULT '',
    car_rubber             TEXT NOT NULL DEFAULT '',
    car_model              TEXT NOT NULL DEFAULT '',
    car_fuel_card_number   TEXT NOT NULL DEFAULT '',
    car_government_number  TEXT NOT NULL DEFAULT '',
    car_sts_number         TEXT NOT NULL DEFAULT '',
    car_contract_number    TEXT NOT NULL DEFAULT '',
    work_clothes_json      JSONB NOT NULL DEFAULT '[]'::jsonb,
    instruments_json       JSONB NOT NULL DEFAULT '[]'::jsonb,
    equipments_json        JSONB NOT NULL DEFAULT '[]'::jsonb,
    active                 BOOLEAN NOT NULL DEFAULT TRUE,
    last_login_at          TIMESTAMPTZ,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users_roles (
    name        VARCHAR(64) PRIMARY KEY,
    description TEXT NOT NULL,
    is_system   BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS users_permissions (
    key         VARCHAR(128) PRIMARY KEY,
    grp         VARCHAR(64) NOT NULL,
    resource    VARCHAR(64) NOT NULL,
    action      VARCHAR(64) NOT NULL,
    description TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users_role_permissions (
    role_name      VARCHAR(64) NOT NULL REFERENCES users_roles(name) ON DELETE CASCADE,
    permission_key VARCHAR(128) NOT NULL REFERENCES users_permissions(key) ON DELETE CASCADE,
    PRIMARY KEY (role_name, permission_key)
);

CREATE TABLE IF NOT EXISTS users_user_roles (
    user_id   BIGINT NOT NULL REFERENCES users_accounts(id) ON DELETE CASCADE,
    role_name VARCHAR(64) NOT NULL REFERENCES users_roles(name) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_name)
);

CREATE TABLE IF NOT EXISTS users_sessions (
    token_hash VARCHAR(64) PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users_accounts(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_users_sessions_user ON users_sessions(user_id);

CREATE TABLE IF NOT EXISTS users_api_tokens (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES users_accounts(id) ON DELETE CASCADE,
    name         VARCHAR(120) NOT NULL,
    token_hash   VARCHAR(64) NOT NULL UNIQUE,
    can_read     BOOLEAN NOT NULL DEFAULT TRUE,
    can_write    BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_users_api_tokens_user ON users_api_tokens(user_id);

CREATE TABLE IF NOT EXISTS users_login_events (
    id         BIGSERIAL PRIMARY KEY,
    event      VARCHAR(16) NOT NULL,
    username   VARCHAR(100) NOT NULL,
    user_id    BIGINT REFERENCES users_accounts(id) ON DELETE SET NULL,
    ip         VARCHAR(64) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    reason     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_users_login_events_created ON users_login_events(created_at DESC);

CREATE TABLE IF NOT EXISTS users_ai_model_access (
    model_id    BIGINT PRIMARY KEY,
    open_to_all BOOLEAN,
    user_ids    JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO users_roles (name, description, is_system)
VALUES
    ('superadmin', 'Полные права (суперадминистратор)', TRUE),
    ('dispatcher', 'Диспетчер', FALSE),
    ('engineer', 'Инженер выездной службы', FALSE),
    ('analyst', 'Аналитик документации и качества', FALSE)
ON CONFLICT (name) DO NOTHING;
