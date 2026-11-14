-- Старые идентификаторы и свободный текст сохраняются без усечения.
ALTER TABLE crm_address_equipments ALTER COLUMN serial_number TYPE TEXT;
ALTER TABLE crm_maintenance_contracts ALTER COLUMN number TYPE TEXT;
ALTER TABLE crm_certificates ALTER COLUMN receipt_number TYPE TEXT;
ALTER TABLE crm_equipments ALTER COLUMN name TYPE TEXT;
ALTER TABLE crm_contractor_phones ALTER COLUMN note TYPE TEXT;
ALTER TABLE crm_subdivisions ALTER COLUMN name TYPE TEXT;
ALTER TABLE crm_sip_accounts ALTER COLUMN name TYPE TEXT;
ALTER TABLE crm_calls ALTER COLUMN external_call_id TYPE TEXT, ALTER COLUMN ext_tracking_id TYPE TEXT;
