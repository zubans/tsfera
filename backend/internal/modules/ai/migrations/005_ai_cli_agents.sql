-- Таймаут ответа для консольных агентов и шлюза (ответ может идти минутами).
ALTER TABLE ai_providers ADD COLUMN IF NOT EXISTS timeout_seconds INT NOT NULL DEFAULT 300;

-- Назначение модели задаче (чат, генерация, эмбеддинги, расшифровка), как в KB.
CREATE TABLE IF NOT EXISTS ai_task_assignments (
    task       VARCHAR(32) PRIMARY KEY,
    model_id   BIGINT REFERENCES ai_models(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
