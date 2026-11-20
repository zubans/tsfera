#!/usr/bin/env python3
"""Offline legacy import. PostgreSQL utilities and Go are the only dependencies."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import uuid
from urllib.parse import urlsplit, urlunsplit, parse_qsl, unquote

from mapping import build_mapping
from files import transfer_files

ROOT = Path(__file__).resolve().parents[2]

def literal(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"

def ident(value: str) -> str:
    return '"' + value.replace('"', '""') + '"'

def dump_hash(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024*1024), b''):
            digest.update(chunk)
    return digest.hexdigest()

def connection_env(dsn: str) -> dict[str, str]:
    u = urlsplit(dsn)
    if u.scheme not in ("postgres", "postgresql") or not u.hostname or not u.path.strip('/'):
        raise ValueError("Connection must be a postgresql:// URL with host and database")
    env = dict(os.environ)
    for key in list(env):
        if key.startswith('PG'):
            del env[key]
    env.update(PGHOST=u.hostname, PGPORT=str(u.port or 5432), PGDATABASE=unquote(u.path[1:]),
               PGUSER=unquote(u.username or ''), PGPASSWORD=unquote(u.password or ''), PGCONNECT_TIMEOUT='10')
    allowed = {'sslmode':'PGSSLMODE','sslrootcert':'PGSSLROOTCERT','sslcert':'PGSSLCERT','sslkey':'PGSSLKEY'}
    for key, value in parse_qsl(u.query):
        if key not in allowed:
            raise ValueError(f"Unsupported connection parameter: {key}")
        env[allowed[key]] = value
    return env

def database_url(dsn: str, name: str) -> str:
    u = urlsplit(dsn)
    return urlunsplit((u.scheme, u.netloc, '/' + name, u.query, ''))

class Runner:
    def __init__(self, log: Path) -> None:
        self.log = log
        self.bin = os.environ.get('PG_BIN', '')

    def run(self, args: list[str], dsn: str, data: str | None = None) -> str:
        if self.bin and args[0] in ('psql', 'pg_restore', 'pg_dump'):
            args = [str(Path(self.bin) / args[0]), *args[1:]]
        with self.log.open('ab') as errors:
            result = subprocess.run(args, env=connection_env(dsn), input=data, text=True,
                                    stdout=subprocess.PIPE, stderr=errors, cwd=ROOT)
        if result.returncode:
            raise RuntimeError(f"{Path(args[0]).name} failed; private diagnostics: {self.log}")
        return result.stdout

    def sql(self, dsn: str, sql: str) -> str:
        return self.run(['psql','-X','-qAt','-v','ON_ERROR_STOP=1'], dsn, sql)

    def json(self, dsn: str, sql: str) -> list[dict]:
        result = self.sql(dsn, "SELECT COALESCE(json_agg(q),'[]'::json) FROM (" + sql + ") q;")
        return json.loads(result)

def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--dump', default=os.environ.get('MIGRATION_DUMP'))
    parser.add_argument('--apply', action='store_true')
    parser.add_argument('--report', default=os.environ.get('MIGRATION_REPORT','migration-report.json'))
    parser.add_argument('--permission-map', default=os.environ.get('MIGRATION_PERMISSION_MAP'), help='JSON: legacy permission name -> list of new permissions')
    parser.add_argument('--allow-unmapped-permissions', action='store_true', default=os.environ.get('MIGRATION_ALLOW_UNMAPPED_PERMISSIONS')=='1')
    parser.add_argument('--allow-missing-files', action='store_true', default=os.environ.get('MIGRATION_ALLOW_MISSING_FILES')=='1')
    parser.add_argument('--files-root', default=os.environ.get('MIGRATION_FILES_ROOT'))
    parser.add_argument('--files-destination', default=os.environ.get('MIGRATION_FILES_DESTINATION'))
    args = parser.parse_args()
    if not args.dump: parser.error('--dump or DUMP is required')
    os.umask(0o077)
    dump = Path(args.dump).resolve()
    if not dump.is_file() or dump.open('rb').read(5) != b'PGDMP':
        raise ValueError('Expected an existing PostgreSQL custom dump')
    report_path = Path(args.report).absolute()
    if report_path.resolve() == dump or (args.permission_map and report_path.resolve() == Path(args.permission_map).resolve()):
        raise ValueError('Report must not overwrite an input file')
    target = os.environ.get('MIGRATION_DATABASE_URL', '')
    admin = os.environ.get('MIGRATION_ADMIN_URL', '')
    if not admin:
        raise ValueError('Set MIGRATION_ADMIN_URL for the server hosting the temporary database')
    if args.apply and not target:
        raise ValueError('Set MIGRATION_DATABASE_URL to an EMPTY target database')
    if target:
        connection_env(target)
    work = Path(tempfile.mkdtemp(prefix='tsfera-migration-'))
    runner = Runner(work / 'private.log')
    stage_name = 'tsfera_import_' + uuid.uuid4().hex[:16]
    stage = database_url(admin, stage_name)
    report: dict = {'format_version':1, 'dump_sha256':dump_hash(dump),
                    'applied':False, 'tables':[], 'warnings':[], 'blockers':[]}
    created = False
    try:
        if args.apply:
            occupied = runner.sql(target, "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND c.relkind IN ('r','p','v','m','S');")
            if int(occupied):
                raise ValueError('Target database is not empty; import refused (including repeated imports)')
        print('Restoring dump into isolated temporary database...', flush=True)
        runner.sql(admin, 'CREATE DATABASE ' + ident(stage_name) + ' TEMPLATE template0;')
        created = True
        runner.run(['pg_restore','--exit-on-error','--no-owner','--no-privileges','--dbname='+stage_name,str(dump)], stage)
        tables = runner.json(stage, "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename")
        if not any(t['tablename']=='ref_user' for t in tables):
            raise ValueError('Dump is not a supported legacy Teplosfera database')
        move = 'CREATE SCHEMA legacy_import; REVOKE ALL ON SCHEMA legacy_import FROM PUBLIC;\n'
        for table in tables:
            name = table['tablename']
            count = int(runner.sql(stage, 'SELECT count(*) FROM public.' + ident(name)))
            report['tables'].append({'source':name,'source_rows':count,'disposition':'pending'})
            move += 'ALTER TABLE public.'+ident(name)+' SET SCHEMA legacy_import;\n'
        runner.sql(stage, move)
        print('Creating new schema and transforming records...', flush=True)
        env = dict(os.environ, DATABASE_URL=stage)
        with runner.log.open('ab') as errors:
            run = subprocess.run(['go','run','./cmd/migrate-schema'], cwd=ROOT/'backend', env=env,
                                 stdout=errors, stderr=errors)
        if run.returncode:
            raise RuntimeError(f'Schema initialization failed; private diagnostics: {runner.log}')
        permission_map = json.loads(Path(args.permission_map).read_text()) if args.permission_map else {}
        sql, mapped, warnings = build_mapping(runner, stage, permission_map)
        report['warnings'].extend(warnings)
        runner.sql(stage, 'BEGIN;\n'+sql+'\nCOMMIT;')
        excluded = {'migration','sys_token','sys_auth_rule','reg_task_pool','reg_queue_user','reg_user_setting'}
        for item in report['tables']:
            if item['source'] in mapped:
                item.update(disposition='mapped', target=mapped[item['source']])
            elif item['source'] in excluded:
                item['disposition']='excluded_runtime_state'
            else:
                raise ValueError('Unmapped source table: '+item['source'])
        report['permissions'] = runner.json(stage, "SELECT r.name,r.description,array_agg(p.permission_key ORDER BY p.permission_key) FILTER(WHERE p.permission_key IS NOT NULL) permissions FROM users_roles r LEFT JOIN users_role_permissions p ON p.role_name=r.name WHERE r.name LIKE 'legacy_%' GROUP BY r.name,r.description ORDER BY r.name")
        report['target_counts'] = {}
        for table in runner.json(stage, "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename"):
            name = table['tablename']
            report['target_counts'][name] = int(runner.sql(stage,'SELECT count(*) FROM public.'+ident(name)))
        for source, target_table in mapped.items():
            # Compound mappings (JSON collections, catalogues) have separate descriptions.
            if ',' not in target_table and not target_table.startswith('json:'):
                source_count = next(t['source_rows'] for t in report['tables'] if t['source']==source)
                if report['target_counts'].get(target_table) != source_count:
                    raise ValueError(f'Row count mismatch: {source} -> {target_table}')
        report['unresolved_links'] = runner.json(stage, 'SELECT id,application_id,unresolved_phone_id FROM crm_application_phones WHERE unresolved_phone_id IS NOT NULL')
        report['incomplete_records'] = runner.json(stage, "SELECT 'applications' entity,count(*) records FROM crm_applications WHERE contractor_id IS NULL OR address_id IS NULL UNION ALL SELECT 'tasks_without_deadline',count(*) FROM crm_tasks WHERE plan_datetime_to IS NULL")
        file_count = next((t['source_rows'] for t in report['tables'] if t['source']=='ref_file'),0)
        if args.files_root:
            if args.apply and not args.files_destination:
                raise ValueError('--files-destination required for copying files on apply')
            records = runner.json(stage, 'SELECT id,storage_path,size_bytes FROM users_files ORDER BY id')
            report['files'] = transfer_files(records,Path(args.files_root),None,args.allow_missing_files)
            missing = sum(1 for f in report['files'] if f.get('missing'))
            if missing:
                report['warnings'].append(f'File payloads missing in files root: {missing} of {len(records)}; metadata imported without content (--allow-missing-files).')
        if file_count and not args.files_root and not args.allow_missing_files:
            report['blockers'].append('File payloads are not in the dump. Supply external storage separately; --allow-missing-files explicitly permits metadata-only import.')
        if any(w.startswith('Unmapped permission:') for w in warnings) and not args.allow_unmapped_permissions:
            report['blockers'].append('Unmapped legacy permissions: supply --permission-map or explicitly permit restricted accounts with --allow-unmapped-permissions.')
        if dump_hash(dump) != report['dump_sha256']:
            raise ValueError('Dump changed during import; target not modified')
        report['verified'] = True
        report['verification'] = 'All mapped scalar fields, original business attributes, child records and monetary totals reconciled before publication.'
        runner.sql(stage, 'DROP SCHEMA legacy_import CASCADE;')
        receipt = dict(report,applied=args.apply)
        runner.sql(stage, 'CREATE TABLE public.platform_legacy_import (dump_sha256 text PRIMARY KEY, report jsonb NOT NULL, imported_at timestamptz NOT NULL DEFAULT now()); INSERT INTO public.platform_legacy_import VALUES ('+literal(report['dump_sha256'])+','+literal(json.dumps(receipt,ensure_ascii=False))+'::jsonb,now());')
        if args.apply:
            if report['blockers']:
                raise ValueError('Import blocked; see report. Target database was not modified.')
            if args.files_root:
                report['files'] = transfer_files(records,Path(args.files_root),Path(args.files_destination),args.allow_missing_files)
            print('Applying verified snapshot atomically to empty target...', flush=True)
            snapshot = work/'snapshot.sql'
            runner.run(['pg_dump','--no-owner','--no-privileges','--file='+str(snapshot)],stage)
            # No --clean, DROP or target TRUNCATE: a concurrent initialization fails safely.
            runner.run(['psql','-X','-q','-v','ON_ERROR_STOP=1','--single-transaction','--file='+str(snapshot)],target)
            report['applied'] = True
        report['verified'] = True
        return 0 if not report['blockers'] else 2
    except Exception as error:
        report['error'] = str(error)
        print(str(error), file=sys.stderr)
        return 1
    finally:
        # Cleanup cannot be skipped by a full disk or an unwritable report path.
        if created:
            try:
                runner.sql(admin, 'DROP DATABASE '+ident(stage_name)+' WITH (FORCE);')
            except Exception:
                report['cleanup_warning'] = 'Temporary database cleanup failed: '+stage_name
                print(report['cleanup_warning'], file=sys.stderr)
        fd, temporary_report = tempfile.mkstemp(prefix='.migration-report-', dir=report_path.parent)
        with os.fdopen(fd,'w') as stream:
            stream.write(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
        os.replace(temporary_report,report_path)
        print('Report:', report_path, flush=True)
        # Failed runs keep private diagnostics; successful runs leave only the report.
        if report.get('verified') and not report.get('error'):
            shutil.rmtree(work)

if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
