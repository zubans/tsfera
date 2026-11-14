CREATE TABLE IF NOT EXISTS docs_documents (
    id          BIGSERIAL PRIMARY KEY,
    space       VARCHAR(64) NOT NULL DEFAULT 'general',
    doc_type    VARCHAR(32) NOT NULL DEFAULT 'article',
    title       VARCHAR(300) NOT NULL,
    slug        VARCHAR(300) NOT NULL,
    content     TEXT NOT NULL DEFAULT '',
    status      VARCHAR(32) NOT NULL DEFAULT 'active',
    tags        JSONB NOT NULL DEFAULT '[]'::jsonb,
    version     INT NOT NULL DEFAULT 1,
    visibility  VARCHAR(32) NOT NULL DEFAULT 'internal',
    created_by  BIGINT NOT NULL DEFAULT 0,
    updated_by  BIGINT NOT NULL DEFAULT 0,
    deleted     BOOLEAN NOT NULL DEFAULT FALSE,
    archived_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_docs_space_slug_active
    ON docs_documents (space, slug) WHERE deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_docs_space_updated
    ON docs_documents (space, updated_at DESC) WHERE deleted = FALSE;
CREATE INDEX IF NOT EXISTS idx_docs_tags
    ON docs_documents USING gin (tags);

CREATE TABLE IF NOT EXISTS docs_revisions (
    id           BIGSERIAL PRIMARY KEY,
    document_id  BIGINT NOT NULL REFERENCES docs_documents(id) ON DELETE CASCADE,
    version      INT NOT NULL,
    title        VARCHAR(300) NOT NULL,
    content_raw  TEXT NOT NULL DEFAULT '',
    content_gz   BYTEA,
    diff_summary TEXT NOT NULL DEFAULT '',
    created_by   BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_docs_revisions_doc
    ON docs_revisions (document_id, version DESC);

CREATE TABLE IF NOT EXISTS docs_entity_links (
    id          BIGSERIAL PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES docs_documents(id) ON DELETE CASCADE,
    entity_type VARCHAR(64) NOT NULL,
    entity_id   BIGINT NOT NULL,
    note        VARCHAR(300) NOT NULL DEFAULT '',
    created_by  BIGINT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_docs_entity_link
    ON docs_entity_links (document_id, entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_docs_entity_lookup
    ON docs_entity_links (entity_type, entity_id);

CREATE TABLE IF NOT EXISTS docs_comments (
    id          BIGSERIAL PRIMARY KEY,
    document_id BIGINT NOT NULL REFERENCES docs_documents(id) ON DELETE CASCADE,
    author_id   BIGINT NOT NULL DEFAULT 0,
    author_name VARCHAR(200) NOT NULL DEFAULT '',
    body        TEXT NOT NULL,
    quote       TEXT NOT NULL DEFAULT '',
    anchor      VARCHAR(200) NOT NULL DEFAULT '',
    line_from   INT NOT NULL DEFAULT 0,
    line_to     INT NOT NULL DEFAULT 0,
    resolved    BOOLEAN NOT NULL DEFAULT FALSE,
    resolved_by BIGINT,
    resolved_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_docs_comments_doc
    ON docs_comments (document_id, resolved, created_at ASC);

CREATE TABLE IF NOT EXISTS docs_attachments (
    id             BIGSERIAL PRIMARY KEY,
    document_id    BIGINT NOT NULL REFERENCES docs_documents(id) ON DELETE CASCADE,
    filename       VARCHAR(255) NOT NULL,
    mime_type      VARCHAR(120) NOT NULL DEFAULT 'application/octet-stream',
    size_bytes     BIGINT NOT NULL DEFAULT 0,
    storage_path   TEXT NOT NULL,
    extracted_text TEXT NOT NULL DEFAULT '',
    created_by     BIGINT NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_docs_attachments_doc
    ON docs_attachments (document_id, created_at DESC);

INSERT INTO docs_documents (space, doc_type, title, slug, content, status, tags, version, visibility)
SELECT
    'reglaments',
    'sop',
    'Регламент выезда инженера на ТО и заполнения акта',
    'reglament-vyezda-to',
    E'# Регламент выезда инженера на ТО\n\n## 1. Подготовка к выезду\n- Проверить заявку в календаре и особые отметки (notes_viewed).\n- Убедиться, что адрес и серийный номер котла совпадают с карточкой контрагента.\n\n## 2. Обязательные замеры при ТО\n1. Разрежение в дымовом канале\n2. Давление газа в подводящем газопроводе и на горелке\n3. Давление в расширительных баках (отопление и ГВС)\n4. Расход ГВС и напряжение сети\n\n## 3. Завершение\nПосле сохранения акта выполненных работ заявка автоматически закрывается (`is_complete = true`).',
    'active',
    '["ТО","инженеры","акты"]'::jsonb,
    1,
    'internal'
WHERE NOT EXISTS (SELECT 1 FROM docs_documents WHERE slug = 'reglament-vyezda-to');
