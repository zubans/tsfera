-- Запись может прийти раньше события вызова, поэтому внешний ключ к crm_calls не нужен.
CREATE TABLE IF NOT EXISTS crm_telephony_recordings (
 provider TEXT NOT NULL,
 call_id TEXT NOT NULL,
 recording_id TEXT NOT NULL,
 seq BIGINT NOT NULL,
 state TEXT NOT NULL,
 PRIMARY KEY(provider,call_id,recording_id)
);
CREATE TABLE IF NOT EXISTS crm_telephony_command_results (
 provider TEXT NOT NULL,
 command_id TEXT NOT NULL,
 result TEXT NOT NULL,
 received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 PRIMARY KEY(provider,command_id)
);
