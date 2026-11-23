CREATE TABLE IF NOT EXISTS docs_boiler_parts (
    id                    BIGSERIAL PRIMARY KEY,
    document_id           BIGINT NOT NULL REFERENCES docs_documents(id) ON DELETE CASCADE,
    brand                 VARCHAR(120) NOT NULL DEFAULT '',
    boiler_model          VARCHAR(255) NOT NULL DEFAULT '',
    assembly              VARCHAR(255) NOT NULL DEFAULT '',
    position              VARCHAR(32) NOT NULL DEFAULT '',
    part_number           VARCHAR(64) NOT NULL DEFAULT '',
    name                  TEXT NOT NULL DEFAULT '',
    notes                 TEXT NOT NULL DEFAULT '',
    replaced_by           VARCHAR(64) NOT NULL DEFAULT '',
    page_number           INT NOT NULL DEFAULT 0,
    diagram_page          INT NOT NULL DEFAULT 0,
    diagram_attachment_id BIGINT NOT NULL DEFAULT 0,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_docs_boiler_parts_doc
    ON docs_boiler_parts (document_id, page_number, id);

CREATE INDEX IF NOT EXISTS idx_docs_boiler_parts_part_no
    ON docs_boiler_parts (lower(part_number));

CREATE INDEX IF NOT EXISTS idx_docs_boiler_parts_model
    ON docs_boiler_parts (lower(brand), lower(boiler_model));

CREATE INDEX IF NOT EXISTS idx_docs_boiler_parts_fts
    ON docs_boiler_parts
    USING GIN (to_tsvector('russian', brand || ' ' || boiler_model || ' ' || assembly || ' ' || part_number || ' ' || name || ' ' || notes || ' ' || replaced_by));
