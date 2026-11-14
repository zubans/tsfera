-- Historical incomplete documents are valid data; do not invent customers or dates.
ALTER TABLE crm_applications ALTER COLUMN contractor_id DROP NOT NULL,
    ALTER COLUMN address_id DROP NOT NULL,
    ALTER COLUMN plan_datetime_from DROP NOT NULL,
    ALTER COLUMN plan_datetime_to DROP NOT NULL;
ALTER TABLE crm_tasks ALTER COLUMN plan_datetime_to DROP NOT NULL;
ALTER TABLE crm_applications ADD COLUMN number BIGINT,
    ADD COLUMN workflow_status INTEGER;
ALTER TABLE crm_certificates ADD COLUMN number BIGINT;
ALTER TABLE crm_tasks ADD COLUMN number BIGINT;
ALTER TABLE crm_calls ADD COLUMN number BIGINT;
ALTER TABLE users_accounts ADD COLUMN file_id BIGINT REFERENCES users_files(id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE crm_application_phones (
    id BIGSERIAL PRIMARY KEY,
    application_id BIGINT REFERENCES crm_applications(id),
    phone_id BIGINT REFERENCES crm_contractor_phones(id),
    unresolved_phone_id BIGINT,
    source_data JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX ON crm_application_phones(application_id);
CREATE TABLE crm_certificate_equipments (
    id BIGSERIAL PRIMARY KEY,
    certificate_id BIGINT REFERENCES crm_certificates(id),
    equipment_id BIGINT REFERENCES crm_equipments(id),
    serial_number TEXT,
    source_data JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX ON crm_certificate_equipments(certificate_id);

-- Additional business attributes and original relationship rows remain attached
-- to their new entity. They are not executable legacy code or session state.
DO $$ DECLARE t text; BEGIN
  FOREACH t IN ARRAY ARRAY[
    'users_accounts','users_files','users_inventory_items','crm_subdivisions',
    'crm_sip_accounts','crm_traffic_sources','crm_contractors','crm_contractor_addresses',
    'crm_contractor_phones','crm_contractor_emails','crm_equipments','crm_address_equipments',
    'crm_legal_entities','crm_applications','crm_maintenance_contracts','crm_certificates',
    'crm_tasks','crm_task_pool_settings','crm_calls','crm_miss_call_pool',
    'crm_analytic_questions','crm_analytics','crm_banners','crm_app_settings',
    'crm_notifications','crm_ref_items','crm_change_logs'
  ] LOOP
    EXECUTE format('ALTER TABLE %I ADD COLUMN source_data JSONB NOT NULL DEFAULT ''{}'', ADD COLUMN source_relations JSONB NOT NULL DEFAULT ''{}''',t);
  END LOOP;
END $$;
CREATE TABLE crm_import_enums (
    source_table TEXT NOT NULL,
    id BIGINT NOT NULL,
    data JSONB NOT NULL,
    PRIMARY KEY(source_table,id)
);

ALTER TABLE crm_contractors ADD COLUMN client_category_id INTEGER;
