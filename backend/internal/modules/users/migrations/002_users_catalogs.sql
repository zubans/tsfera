ALTER TABLE users_accounts
    ADD COLUMN IF NOT EXISTS avatar_path TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS users_inventory_items (
    id         BIGSERIAL PRIMARY KEY,
    category   VARCHAR(32) NOT NULL,
    name       VARCHAR(200) NOT NULL,
    deleted    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_users_inventory_category
    ON users_inventory_items (category, name) WHERE deleted = FALSE;

INSERT INTO users_inventory_items (category, name)
SELECT v.category, v.name
FROM (VALUES
    ('work_clothes', 'Куртка зимняя (Теплосфера)'),
    ('work_clothes', 'Комбинезон сервисный'),
    ('work_clothes', 'Ботинки защитные'),
    ('instrument', 'Газоанализатор'),
    ('instrument', 'Манометр цифровой'),
    ('instrument', 'Мультиметр'),
    ('engineer_equipment', 'Планшет / смартфон инженера'),
    ('engineer_equipment', 'Переносной термопринтер')
) AS v(category, name)
WHERE NOT EXISTS (SELECT 1 FROM users_inventory_items);
