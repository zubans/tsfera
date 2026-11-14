"""Explicit mappings; all original rows remain in legacy_import unchanged."""
from __future__ import annotations
import json


def q(value: str) -> str:
    return '"' + value.replace('"', '""') + '"'


def lit(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def aggregate(table: str, value: str, parent: str = 'parent_id') -> str:
    return f"COALESCE((SELECT jsonb_agg({value} ORDER BY t.id) FROM legacy_import.{table} t WHERE t.{parent}=s.id),'[]'::jsonb)"


def build_mapping(runner, dsn: str, permission_map: dict) -> tuple[str, dict, list[str]]:
    target = runner.json(dsn, "SELECT table_name,column_name,is_nullable,column_default,data_type FROM information_schema.columns WHERE table_schema='public' ORDER BY ordinal_position")
    source = runner.json(dsn, "SELECT table_name,column_name FROM information_schema.columns WHERE table_schema='legacy_import'")
    src = {}
    dst = {}
    for c in source: src.setdefault(c['table_name'], set()).add(c['column_name'])
    for c in target: dst.setdefault(c['table_name'], []).append(c)
    statements = [f'CREATE INDEX ON legacy_import.{q(table)} ({q(column)});' for table,columns in src.items() for column in ('parent_id','analytic_question_id') if column in columns]
    statements.append('ANALYZE;')
    mapped = {}
    warnings = []
    checks = []
    # Only the temporary staging database is modified here, never an existing target.
    seed = ['crm_subdivisions','crm_equipments','crm_task_pool_settings','crm_ref_items','crm_traffic_sources','crm_app_settings','users_inventory_items']
    statements.append('TRUNCATE '+','.join(seed)+' CASCADE;')

    def copy(old: str, new: str, overrides: dict | None = None) -> None:
        if old not in src: raise ValueError('Required source table missing: '+old)
        overrides = dict(overrides or {})
        overrides['source_data'] = "to_jsonb(s) - 'auth_key'" if old == 'ref_user' else 'to_jsonb(s)'
        cols, expr = [], []
        for c in dst[new]:
            name = c['column_name']
            value = overrides.get(name)
            if name in overrides and value is None: continue
            if value is None:
                if name in src[old]: value = 's.'+q(name)
                elif name == 'created_by' and 'create_user_id' in src[old]: value = 's.create_user_id'
                elif name == 'deleted' and 'reference_status_id' in src[old]: value = 's.reference_status_id=2'
                elif name == 'deleted' and 'document_status_id' in src[old]: value = 's.document_status_id=3'
                elif name == 'active' and 'reference_status_id' in src[old]: value = 's.reference_status_id=1'
                elif name == 'status' and 'document_status_id' in src[old]: value = 'CASE WHEN s.document_status_id=3 THEN 4 ELSE s.document_status_id END'
                else: continue
            if c['is_nullable']=='NO':
                default = c['column_default']
                if default and not default.startswith('nextval('): value = f'COALESCE({value},{default})'
                elif c['data_type'] in ('text','character varying'): value = f"COALESCE({value},'')"
            cols.append(q(name)); expr.append(value)
        statements.append(f"DO $$ BEGIN RAISE NOTICE 'Mapping {old} -> {new}'; END $$;")
        statements.append(f'INSERT INTO public.{q(new)} ({",".join(cols)}) SELECT {",".join(expr)} FROM legacy_import.{q(old)} s;')
        statements.append(f'ANALYZE public.{q(new)};')
        mapped[old] = new
        key = 'contractor_id' if new == 'crm_legal_entities' else ('key' if new == 'crm_app_settings' else 'id')
        source_key = overrides.get(key, 's.'+q(key))
        comparison = ' OR '.join(f't.{q(col)} IS DISTINCT FROM ({value})' for col,value in zip([c.strip('"') for c in cols],expr))
        checks.append(f"DO $$ BEGIN IF EXISTS(SELECT 1 FROM legacy_import.{q(old)} s LEFT JOIN public.{q(new)} t ON t.{q(key)}={source_key} WHERE t.{q(key)} IS NULL OR {comparison}) THEN RAISE EXCEPTION 'Field reconciliation failed: {old}'; END IF; END $$;")

    copy('ref_subdivision','crm_subdivisions', {
        'phones':aggregate('tab_subdivision_phones',"jsonb_build_object('id',t.id::text,'name',t.name,'phone',t.phone::text,'is_main',COALESCE(t.is_main,false),'is_duty',false)"),
        'payment_options':aggregate('tab_subdivision_payment_option',"jsonb_build_object('payment_option_id',t.payment_option_id,'file_id',t.file_id,'is_main',COALESCE(t.is_main,false))")})
    copy('ref_user','users_accounts', {'full_name':'s.name',
        'work_clothes_json':aggregate('tab_user_work_clothes',"(SELECT name FROM legacy_import.ref_work_clothes WHERE id=t.work_clothes_id)"),
        'instruments_json':aggregate('tab_user_engineer_instrument',"(SELECT name FROM legacy_import.ref_engineer_instrument WHERE id=t.engineer_instrument_id)"),
        'equipments_json':aggregate('tab_user_engineer_equipment',"(SELECT name FROM legacy_import.ref_engineer_equipment WHERE id=t.engineer_equipment_id)")})
    copy('ref_file','users_files',{'original_name':'COALESCE(s.original_name,s.name)','size_bytes':'s.size','storage_path':'s.storage_key'})
    copy('ref_sip_account','crm_sip_accounts',{'abonent_id':'s.abonent_id::text'})
    copy('ref_traffic_source','crm_traffic_sources')
    copy('ref_contractor','crm_contractors',{'type':'CASE WHEN s.is_legal_entity THEN 2 ELSE 1 END','client_category_id':'s.client_type_id','note':'s.comment'})
    copy('ref_address','crm_contractor_addresses',{'address':'s.name'})
    phone="regexp_replace(s.name,'[^0-9]','','g')"
    copy('ref_phone','crm_contractor_phones',{'phone':f"CASE WHEN length({phone})=11 AND left({phone},1) IN ('7','8') THEN right({phone},10) ELSE {phone} END",'note':'s.title'})
    copy('ref_email','crm_contractor_emails',{'email':'s.name'})
    copy('ref_equipment','crm_equipments',{'type':'(SELECT name FROM legacy_import.ref_type_equipment WHERE id=s.type_equipment_id)','model':'s.model_name','manufacturer':'(SELECT name FROM legacy_import.ref_manufacturer WHERE id=s.manufacturer_id)'})
    copy('cros_link_address_equipment','crm_address_equipments')
    copy('tab_legal_entity_information','crm_legal_entities',{'contractor_id':'s.parent_id','ogrn':'s.ogrn::text'})
    copy('doc_application_for_technical_work','crm_applications',{
        'workflow_status':'s.status_id','engineer_id':'s.executor_user_id','plan_datetime_from':'s.date_from','plan_datetime_to':'s.date_to',
        'executor_ids':aggregate('tab_application_for_technical_work_executor','t.user_id'),
        'equipment_ids':aggregate('tab_application_for_technical_work_equipment','t.equipment_id'),
        'serial_numbers':aggregate('tab_application_for_technical_work_equipment',"COALESCE(t.serial_number,'')"),
        'work_types':aggregate('tab_application_for_technical_work_types_of_job',"(SELECT name FROM legacy_import.ref_type_of_job WHERE id=t.type_of_job_id)")})
    categories=['standard','plus','all_inclusive','economy','economical','optimal','maximum','warranty','standard_s','plus_s']
    copy('doc_maintenance_contract','crm_maintenance_contracts',{'price':'s.sum','signed_at':'s.date_from','valid_from':'s.date_from','valid_to':'s.date_to','note':'s.comment','application_id':'s.application_for_technical_work_id',
        'category':"(ARRAY["+','.join(lit(v) for v in categories)+"])[s.category_id]",
        'equipments':aggregate('tab_maintenance_contract_equipment',"jsonb_build_object('equipment_id',t.equipment_id,'serial_number',COALESCE(t.serial_number,''))")})
    results = "CASE WHEN jsonb_typeof(s.results)='string' THEN (s.results#>>'{}')::jsonb ELSE s.results END"
    for key in ['contract_eligible','has_air_conditioner']:
        results = f"({results}) || jsonb_build_object('{key}',CASE ({results})->>'{key}' WHEN 'Да' THEN true WHEN 'Нет' THEN false ELSE NULL END)"
    copy('doc_certificate_of_completion','crm_certificates',{'application_id':'s.application_for_technical_work_id','engineer_id':'s.executor_user_id','work_date':'s.date','total_amount':'s.sum','results':results,
        'payment_method':"CASE s.payment_option_id WHEN 1 THEN 'cash' WHEN 2 THEN 'bank' WHEN 3 THEN 'qr_code' WHEN 4 THEN 'card' ELSE '' END",
        'spare_parts':"'[]'::jsonb",
        'work_types':aggregate('tab_certificate_of_completion_types_of_job',"(SELECT name FROM legacy_import.ref_type_of_job WHERE id=t.type_of_job_id)"),
        'special_notes':"COALESCE((SELECT string_agg(n.name,E'\\n' ORDER BY t.id) FROM legacy_import.tab_certificate_of_completion_special_note t JOIN legacy_import.ref_special_note n ON n.id=t.special_note_id WHERE t.parent_id=s.id),'')",
        'executors':aggregate('tab_certificate_of_completion_executor',"jsonb_build_object('user_id',t.user_id,'sum',t.sum)")})
    copy('tab_application_for_technical_work_phone','crm_application_phones',{'application_id':'s.parent_id','phone_id':'(SELECT id FROM crm_contractor_phones WHERE id=s.phone_id)','unresolved_phone_id':'CASE WHEN NOT EXISTS(SELECT 1 FROM crm_contractor_phones WHERE id=s.phone_id) THEN s.phone_id END'})
    copy('tab_certificate_of_completion_equipment','crm_certificate_equipments',{'certificate_id':'s.parent_id'})
    copy('doc_task','crm_tasks',{'title':"'Задача №'||s.number::text",'description':'s.comment','note':'s.comment','assigned_user_id':'s.executor_user_id','contract_id':'s.maintenance_contract_id','execution_datetime':'s.fact_datetime_to',
        'task_type':"CASE s.task_type_id WHEN 1 THEN 'prolongation' WHEN 2 THEN 'quarterly_call' WHEN 3 THEN 'app_limit' WHEN 4 THEN 'call_feedback' ELSE 'other' END",
        'task_result':"CASE s.task_result_id WHEN 1 THEN 'answered' WHEN 2 THEN 'no_answer' WHEN 3 THEN 'application_created' WHEN 4 THEN 'refused' ELSE '' END",
        'status':"CASE WHEN s.document_status_id=3 THEN 6 WHEN s.task_result_id=2 THEN 4 WHEN s.task_result_id=4 THEN 5 WHEN s.fact_datetime_to IS NOT NULL THEN 3 ELSE 1 END",
        'decision_time_sec':'(SELECT decision_time FROM legacy_import.enum_task_type WHERE id=s.task_type_id)',
        'prev_task_id':None})
    statements.append('UPDATE crm_tasks t SET prev_task_id=s.prev_task_id FROM legacy_import.doc_task s WHERE t.id=s.id;')
    copy('sys_pool_setting','crm_task_pool_settings',{'time_from':"to_char(s.time_from,'HH24:MI')",'time_to':"to_char(s.time_to,'HH24:MI')",'task_types':"COALESCE((SELECT jsonb_agg(CASE v::int WHEN 1 THEN 'prolongation' WHEN 2 THEN 'quarterly_call' WHEN 3 THEN 'app_limit' WHEN 4 THEN 'call_feedback' ELSE 'other' END) FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(COALESCE(NULLIF(s.task_type_ids,''),'[]')::jsonb)='array' THEN COALESCE(NULLIF(s.task_type_ids,''),'[]')::jsonb ELSE jsonb_build_array(COALESCE(NULLIF(s.task_type_ids,''),'[]')::jsonb) END) v),'[]'::jsonb)"})
    copy('doc_call','crm_calls',{'external_call_id':"'legacy:'||s.id::text",'direction':"CASE s.direction_id WHEN 2 THEN 'outbound' WHEN 3 THEN 'internal' ELSE 'inbound' END",
        'status':"CASE WHEN s.end_time IS NOT NULL AND s.answer_time IS NULL THEN 'missed' WHEN s.end_time IS NOT NULL THEN 'ended' WHEN s.answer_time IS NOT NULL THEN 'answered' ELSE 'ringing' END",
        'caller_phone':"CASE WHEN s.direction_id=2 THEN COALESCE(s.abonent_id::text,'') ELSE COALESCE(s.remote_party_address,'') END",
        'callee_phone':"CASE WHEN s.direction_id=2 THEN COALESCE(s.remote_party_address,'') ELSE COALESCE(s.abonent_id::text,'') END",
        'abonent_id':'s.abonent_id::text','started_at':'COALESCE(s.start_time,s.created_at)','ended_at':'s.end_time',
        'duration_sec':'GREATEST(0,EXTRACT(EPOCH FROM (s.end_time-COALESCE(s.answer_time,s.start_time)))::int)',
        'caller_type':'1000+s.caller_type_id','call_result':'1000+s.call_result_id','raw_payload':'to_jsonb(s)',
        'type_of_appeal':"CASE WHEN s.type_of_appeal_id IS NULL THEN '' ELSE 'import_'||s.type_of_appeal_id::text END"})
    statements.append("""INSERT INTO crm_miss_call_pool(id,call_id,user_id,is_viewed,created_at,source_relations)
      SELECT min(id),call_id,user_id,bool_and(COALESCE(is_viewed,false)),min(date),
      jsonb_build_object('original_rows',jsonb_agg(to_jsonb(s) ORDER BY id))
      FROM legacy_import.reg_miss_call_pool s GROUP BY call_id,user_id;""")
    mapped['reg_miss_call_pool']='json:crm_miss_call_pool'
    checks.append("""DO $$ BEGIN IF EXISTS(SELECT to_jsonb(s) FROM legacy_import.reg_miss_call_pool s
      EXCEPT ALL SELECT value FROM crm_miss_call_pool p CROSS JOIN LATERAL jsonb_array_elements(p.source_relations->'original_rows'))
      THEN RAISE EXCEPTION 'Missed call reconciliation failed'; END IF; END $$;""")
    copy('ref_analytic_question','crm_analytic_questions',{'options':aggregate('ref_analytic_question_option',"jsonb_build_object('id',t.id,'name',t.name,'sort',t.sort)",'analytic_question_id')})
    copy('doc_analytic_of_technical_work','crm_analytics',{'number':'s.number::text','certificate_id':'s.certificate_of_completion_id',
        'answers':aggregate('tab_analytic_of_technical_work_answer',"jsonb_build_object('question_id',t.analytic_question_id,'question',(SELECT name FROM legacy_import.ref_analytic_question WHERE id=t.analytic_question_id),'text',t.text,'option_names',COALESCE((SELECT jsonb_build_array(name) FROM legacy_import.ref_analytic_question_option WHERE id=t.analytic_question_option_id),'[]'::jsonb),'option_ids',CASE WHEN t.analytic_question_option_id IS NULL THEN '[]'::jsonb ELSE jsonb_build_array(t.analytic_question_option_id) END)")})
    copy('ref_banner','crm_banners')
    copy('ref_app_setting','crm_app_settings',{'data':"CASE WHEN jsonb_typeof(s.data)='string' THEN (s.data#>>'{}')::jsonb ELSE s.data END"})
    copy('reg_user_notification','crm_notifications',{'created_at':'s.date','message':'s.description','title':"'Уведомление из прежней системы'",'is_read':'s.is_viewed'})
    # These catalogues share one target sequence; original IDs remain in the archive.
    for old,kind in [('ref_manufacturer','manufacturer'),('ref_type_equipment','equipment_type'),('ref_type_of_job','work_type'),('ref_special_note','special_note')]:
        statements.append(f"INSERT INTO crm_ref_items(kind,name,deleted,created_at,source_data) SELECT {lit(kind)},name,reference_status_id=2,created_at,to_jsonb(s) FROM legacy_import.{old} s ORDER BY id;")
        mapped[old]='json:crm_ref_items'
        checks.append(f"DO $$ BEGIN IF EXISTS(SELECT to_jsonb(s) FROM legacy_import.{old} s EXCEPT ALL SELECT source_data FROM crm_ref_items WHERE kind={lit(kind)}) THEN RAISE EXCEPTION 'Catalogue reconciliation failed: {old}'; END IF; END $$;")
    for old,kind in [('ref_work_clothes','work_clothes'),('ref_engineer_instrument','instrument'),('ref_engineer_equipment','engineer_equipment')]:
        statements.append(f"INSERT INTO users_inventory_items(category,name,deleted,created_at,source_data) SELECT {lit(kind)},name,reference_status_id=2,created_at,to_jsonb(s) FROM legacy_import.{old} s ORDER BY id;")
        mapped[old]='json:users_inventory_items'
        checks.append(f"DO $$ BEGIN IF EXISTS(SELECT to_jsonb(s) FROM legacy_import.{old} s EXCEPT ALL SELECT source_data FROM users_inventory_items WHERE category={lit(kind)}) THEN RAISE EXCEPTION 'Inventory reconciliation failed: {old}'; END IF; END $$;")
    for table in ['tab_subdivision_phones','tab_subdivision_payment_option','tab_user_work_clothes','tab_user_engineer_instrument','tab_user_engineer_equipment','tab_application_for_technical_work_executor','tab_application_for_technical_work_equipment','tab_application_for_technical_work_types_of_job','tab_maintenance_contract_equipment','tab_certificate_of_completion_types_of_job','tab_certificate_of_completion_special_note','tab_certificate_of_completion_executor','tab_analytic_of_technical_work_answer','ref_analytic_question_option']:
        mapped[table]='json:parent'
    parents = {
        'ref_subdivision':'crm_subdivisions','ref_user':'users_accounts',
        'doc_application_for_technical_work':'crm_applications',
        'doc_maintenance_contract':'crm_maintenance_contracts',
        'doc_certificate_of_completion':'crm_certificates',
        'doc_analytic_of_technical_work':'crm_analytics','ref_analytic_question':'crm_analytic_questions'}
    for child in sorted(src):
        if child not in mapped or not mapped[child].startswith('json:'): continue
        parent = next((old for old in parents if child.startswith('tab_'+old.removeprefix('ref_').removeprefix('doc_')+'_')), None)
        fk = 'parent_id'
        if child == 'ref_analytic_question_option': parent, fk = 'ref_analytic_question','analytic_question_id'
        if not parent: continue
        target_table = parents[parent]
        statements.append(f"UPDATE {target_table} p SET source_relations=source_relations || jsonb_build_object({lit(child)},(SELECT jsonb_agg(to_jsonb(s) ORDER BY s.id) FROM legacy_import.{q(child)} s WHERE s.{fk}=p.id)) WHERE EXISTS(SELECT 1 FROM legacy_import.{q(child)} s WHERE s.{fk}=p.id);")
        checks.append(f"DO $$ BEGIN IF EXISTS(SELECT to_jsonb(s) FROM legacy_import.{q(child)} s EXCEPT ALL SELECT value FROM {target_table} p CROSS JOIN LATERAL jsonb_array_elements(COALESCE(p.source_relations->{lit(child)},'[]'::jsonb))) THEN RAISE EXCEPTION 'Relationship reconciliation failed: {child}'; END IF; END $$;")
    for table in sorted(src):
        if table.startswith('enum_'):
            statements.append(f"INSERT INTO crm_import_enums SELECT {lit(table)},id,to_jsonb(s) FROM legacy_import.{q(table)} s;")
            mapped[table]='json:crm_import_enums'
            checks.append(f"DO $$ BEGIN IF EXISTS(SELECT to_jsonb(s) FROM legacy_import.{q(table)} s EXCEPT ALL SELECT data FROM crm_import_enums WHERE source_table={lit(table)}) THEN RAISE EXCEPTION 'Enum reconciliation failed: {table}'; END IF; END $$;")
    entity_map = {'Contractor':'contractor','Address':'address','Equipment':'equipment','ApplicationForTechnicalWork':'application','CertificateOfCompletion':'certificate','MaintenanceContract':'contract','Task':'task','Call':'call','AnalyticOfTechnicalWork':'analytic','User':'user','Subdivision':'subdivision'}
    cases = ' '.join('WHEN '+lit(k)+' THEN '+lit(v) for k,v in entity_map.items())
    copy('reg_entity_change_log','crm_change_logs',{
        'created_at':'s.date','user_id':'s.user_id','entity_id':'s.entity_id',
        'entity_type':f"COALESCE((SELECT CASE regexp_replace(class_name, '^.*[\\\\]', '') {cases} ELSE class_name END FROM legacy_import.enum_entity WHERE id=s.entity_type_id),'unknown')",
        'action':"CASE WHEN s.is_insert THEN 'create' ELSE 'update' END",
        'changes':"s.changed_attributes::jsonb"})
    # Never equate legacy administrator names to the unrestricted superadmin role.
    permissions = {x['key'] for x in runner.json(dsn,'SELECT key FROM users_permissions')}
    items = runner.json(dsn,'SELECT id,name,type FROM legacy_import.sys_auth_item ORDER BY id')
    for item in items:
        old = item['name']; role = 'legacy_'+str(item['id'])
        statements.append(f"INSERT INTO users_roles(name,description,is_system) VALUES ({lit(role)},{lit(old)},false);")
        values = permission_map.get(old, [old] if old in permissions else [])
        if not isinstance(values,list) or any(p not in permissions for p in values): raise ValueError('Invalid permission mapping for '+old)
        if item['type']==2 and not values and old not in permission_map: warnings.append('Unmapped permission: '+old)
        for value in values:
            statements.append(f"INSERT INTO users_role_permissions VALUES ({lit(role)},{lit(value)}) ON CONFLICT DO NOTHING;")
    statements.append('''WITH RECURSIVE tree(root,child) AS (
      SELECT name,name FROM legacy_import.sys_auth_item
      UNION SELECT t.root,c.child FROM tree t JOIN legacy_import.sys_auth_item_child c ON c.parent=t.child
    ) INSERT INTO users_role_permissions(role_name,permission_key)
    SELECT DISTINCT 'legacy_'||parent.id::text,p.permission_key FROM tree t
    JOIN legacy_import.sys_auth_item parent ON parent.name=t.root
    JOIN legacy_import.sys_auth_item child ON child.name=t.child
    JOIN users_role_permissions p ON p.role_name='legacy_'||child.id::text ON CONFLICT DO NOTHING;
    INSERT INTO users_user_roles(user_id,role_name)
    SELECT a.user_id,'legacy_'||i.id::text FROM legacy_import.sys_auth_assignment a JOIN legacy_import.sys_auth_item i ON i.name=a.item_name;
    ''')
    for old in ['sys_auth_item','sys_auth_assignment','sys_auth_item_child']: mapped[old]='json:users_roles'
    # Keep all original settings/queues/change logs losslessly; do not reinterpret old PHP routes or state.
    warnings.append('Archive-only operational state: sys_token, sys_auth_rule, reg_task_pool, reg_queue_user, reg_user_setting; not activated automatically.')
    warnings.append('File metadata imported; payloads require external storage. SIP credentials are not present in this dump.')
    warnings.append('Legacy call IDs use legacy:<row id>; original remote_call_id remains in raw_payload. Historical calls are not live provider calls.')
    statements.append('''DO $$ DECLARE r record; n bigint; BEGIN
      FOR r IN SELECT table_name,column_name,pg_get_serial_sequence(format('public.%I',table_name),column_name) seq
        FROM information_schema.columns WHERE table_schema='public' AND column_default LIKE 'nextval(%'
      LOOP
        EXECUTE format('SELECT COALESCE(max(%I),0)+1 FROM public.%I',r.column_name,r.table_name) INTO n;
        EXECUTE format('ALTER SEQUENCE %s RESTART WITH %s',r.seq,n);
      END LOOP;
    END $$;''')
    statements.append('-- RECONCILIATION')
    statements.extend(checks)
    # Compare money as well as record counts, without logging customer data.
    statements.append('''DO $$ BEGIN
      IF (SELECT COALESCE(sum(sum),0) FROM legacy_import.doc_certificate_of_completion) <> (SELECT COALESCE(sum(total_amount),0) FROM crm_certificates)
      THEN RAISE EXCEPTION 'Certificate amount reconciliation failed'; END IF;
      IF (SELECT COALESCE(sum(sum),0) FROM legacy_import.doc_maintenance_contract) <> (SELECT COALESCE(sum(price),0) FROM crm_maintenance_contracts)
      THEN RAISE EXCEPTION 'Contract amount reconciliation failed'; END IF;
    END $$;''')
    return '\n'.join(statements), mapped, warnings
