ALTER TABLE docs_attachments
    ADD COLUMN IF NOT EXISTS kind            VARCHAR(32) NOT NULL DEFAULT 'file',
    ADD COLUMN IF NOT EXISTS page_number     INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS analysis_status VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS analysis_error  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS analyzed_at     TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_docs_attachments_analysis_pending
    ON docs_attachments (created_at ASC)
    WHERE analysis_status = 'pending';
