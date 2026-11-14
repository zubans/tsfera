-- Хранилище метаданных загруженных файлов (баннеры, QR-коды оплаты подразделений, аватары).
CREATE TABLE IF NOT EXISTS users_files (
    id BIGSERIAL PRIMARY KEY,
    original_name TEXT NOT NULL,
    mime_type TEXT NOT NULL DEFAULT 'application/octet-stream',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    storage_path TEXT NOT NULL,
    created_by BIGINT REFERENCES users_accounts(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_users_files_created_at ON users_files(created_at DESC);
