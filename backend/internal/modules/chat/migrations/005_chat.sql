CREATE TABLE IF NOT EXISTS chat_conversations (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL,
    title       VARCHAR(255) NOT NULL,
    mode        VARCHAR(16) NOT NULL DEFAULT 'agent',
    model_id    BIGINT NOT NULL DEFAULT 0,
    entity_type VARCHAR(64) NOT NULL DEFAULT '',
    entity_id   BIGINT NOT NULL DEFAULT 0,
    deleted     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_chat_conv_user
    ON chat_conversations (user_id, updated_at DESC) WHERE deleted = FALSE;

CREATE TABLE IF NOT EXISTS chat_messages (
    id              BIGSERIAL PRIMARY KEY,
    conversation_id BIGINT NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
    role            VARCHAR(16) NOT NULL,
    content         TEXT NOT NULL DEFAULT '',
    steps           JSONB NOT NULL DEFAULT '[]'::jsonb,
    sources         JSONB NOT NULL DEFAULT '[]'::jsonb,
    tokens_used     INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_chat_msgs_conv
    ON chat_messages (conversation_id, id ASC);

CREATE TABLE IF NOT EXISTS chat_shares (
    id              BIGSERIAL PRIMARY KEY,
    conversation_id BIGINT NOT NULL REFERENCES chat_conversations(id) ON DELETE CASCADE,
    owner_id        BIGINT NOT NULL,
    share_token     VARCHAR(64) NOT NULL UNIQUE,
    revoked         BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_chat_shares_owner
    ON chat_shares (owner_id, revoked);
