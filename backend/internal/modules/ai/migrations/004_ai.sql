DO $$
BEGIN
    CREATE EXTENSION IF NOT EXISTS vector;
EXCEPTION WHEN OTHERS THEN
    NULL;
END $$;

CREATE TABLE IF NOT EXISTS ai_providers (
    id         BIGSERIAL PRIMARY KEY,
    name       VARCHAR(120) NOT NULL,
    kind       VARCHAR(32) NOT NULL,
    base_url   TEXT NOT NULL DEFAULT '',
    api_key    TEXT NOT NULL DEFAULT '',
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ai_models (
    id             BIGSERIAL PRIMARY KEY,
    provider_id    BIGINT NOT NULL REFERENCES ai_providers(id) ON DELETE CASCADE,
    name           VARCHAR(160) NOT NULL,
    model_id       VARCHAR(160) NOT NULL,
    capability     VARCHAR(32) NOT NULL DEFAULT 'chat',
    context_window INT NOT NULL DEFAULT 32768,
    is_default     BOOLEAN NOT NULL DEFAULT FALSE,
    open_to_all    BOOLEAN NOT NULL DEFAULT TRUE,
    active         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ai_chunks (
    id           BIGSERIAL PRIMARY KEY,
    source_type  VARCHAR(64) NOT NULL,
    source_id    VARCHAR(128) NOT NULL,
    chunk_index  INT NOT NULL,
    title        TEXT NOT NULL,
    anchor       VARCHAR(255) NOT NULL DEFAULT '',
    content      TEXT NOT NULL,
    path         TEXT NOT NULL DEFAULT '',
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    content_hash VARCHAR(64) NOT NULL,
    embedding    JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_chunks_source
    ON ai_chunks (source_type, source_id, chunk_index);
CREATE INDEX IF NOT EXISTS idx_ai_chunks_fts_ru
    ON ai_chunks USING gin (to_tsvector('russian', title || ' ' || content));

CREATE TABLE IF NOT EXISTS ai_usage_logs (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT NOT NULL DEFAULT 0,
    model_id          BIGINT NOT NULL DEFAULT 0,
    model_name        VARCHAR(160) NOT NULL DEFAULT '',
    operation         VARCHAR(64) NOT NULL DEFAULT 'chat',
    prompt_tokens     INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    latency_ms        BIGINT NOT NULL DEFAULT 0,
    status            VARCHAR(32) NOT NULL DEFAULT 'ok',
    error_text        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ai_usage_created
    ON ai_usage_logs (created_at DESC);
