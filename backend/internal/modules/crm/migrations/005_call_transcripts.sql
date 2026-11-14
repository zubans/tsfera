-- Расшифровка записей аудиозвонков в текст (STT).
ALTER TABLE crm_calls
    ADD COLUMN IF NOT EXISTS transcript_text TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS transcript_status TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS transcript_error TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS transcribed_at TIMESTAMPTZ;
