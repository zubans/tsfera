"""Explicit RBAC translation with transitive role closure and SQL reconciliation."""
from __future__ import annotations
import json
from pathlib import Path

DEFAULT_MAP = Path(__file__).resolve().parents[2] / 'backend/pkg/platform/migration_permissions.json'

def default_mapping() -> dict[str, list[str]]:
    return json.loads(DEFAULT_MAP.read_text())

def effective_permissions(items: list[dict], links: list[dict]) -> dict[str, set[str]]:
    nodes = {i['name']:i for i in items}
    if len(nodes) != len(items): raise ValueError('Duplicate RBAC item')
    children = {name:[] for name in nodes}
    for link in links:
        if link['parent'] not in nodes or link['child'] not in nodes:
            raise ValueError('Dangling RBAC hierarchy link')
        children[link['parent']].append(link['child'])
    resolved, visiting = {}, set()
    def walk(name: str) -> set[str]:
        if name in visiting: raise ValueError('Cyclic RBAC hierarchy')
        if name in resolved: return resolved[name]
        visiting.add(name)
        result = {name} if nodes[name]['type']==2 else set()
        for child in children[name]: result |= walk(child)
        visiting.remove(name); resolved[name] = result
        return result
    for name in nodes: walk(name)
    return resolved

def translate(items: list[dict], links: list[dict], available: set[str], overrides: dict | None = None) -> tuple[dict[str,set[str]], list[str]]:
    mapping = default_mapping()
    overrides = overrides or {}
    source_permissions={i['name'] for i in items if i['type']==2}
    if set(overrides)-source_permissions: raise ValueError('Override refers to unknown source permission')
    mapping.update(overrides)
    warnings=[]
    for name in sorted(source_permissions):
        if name not in mapping:
            mapping[name]=[name] if name in available else []
            if not mapping[name]: warnings.append('Unmapped permission: '+name)
        values=mapping[name]
        if not isinstance(values,list) or any(not isinstance(p,str) or p not in available for p in values):
            raise ValueError('Invalid permission mapping for '+name)
    result={}
    for name, source in effective_permissions(items,links).items():
        target=set().union(*(set(mapping[p]) for p in source)) if source else set()
        # An engineer-scoped role never gains unrestricted contractor access.
        if ('common.contractor.view' in source and 'crm.contractor.view' in mapping['common.contractor.view']
                and ('common.contractor.engineer_view' not in source or name=='superadmin')):
            target.add('crm.contractor.view_all')
        result[name]=target
    return result,warnings

def build_permissions(runner, dsn: str, overrides: dict) -> tuple[list[str],list[str]]:
    from mapping import lit
    available={x['key'] for x in runner.json(dsn,'SELECT key FROM users_permissions')}
    items=runner.json(dsn,'SELECT id,name,type FROM legacy_import.sys_auth_item ORDER BY id')
    links=runner.json(dsn,'SELECT parent,child FROM legacy_import.sys_auth_item_child')
    assignments=runner.json(dsn,'SELECT user_id,item_name FROM legacy_import.sys_auth_assignment')
    translated,warnings=translate(items,links,available,overrides)
    nodes={i['name']:i for i in items}
    if any(a['item_name'] not in nodes for a in assignments): raise ValueError('Unknown assigned RBAC item')
    selected={i['name'] for i in items if i['type']==1}|{a['item_name'] for a in assignments}
    sql=['CREATE TEMP TABLE expected_role_permissions(role_name text,permission_key text,PRIMARY KEY(role_name,permission_key)) ON COMMIT DROP;',
         'CREATE TEMP TABLE expected_user_roles(user_id bigint,role_name text,PRIMARY KEY(user_id,role_name)) ON COMMIT DROP;']
    for name in sorted(selected):
        role='legacy_'+str(nodes[name]['id'])
        sql.append(f'INSERT INTO users_roles(name,description,is_system) VALUES({lit(role)},{lit(name)},false);')
        for permission in sorted(translated[name]):
            sql.append(f'INSERT INTO expected_role_permissions VALUES({lit(role)},{lit(permission)});')
    for assignment in assignments:
        role='legacy_'+str(nodes[assignment['item_name']]['id'])
        sql.append(f"INSERT INTO expected_user_roles VALUES({int(assignment['user_id'])},{lit(role)});")
    sql+=['INSERT INTO users_role_permissions SELECT * FROM expected_role_permissions;',
          'INSERT INTO users_user_roles SELECT * FROM expected_user_roles;',
          """DO $$ BEGIN
          IF EXISTS((SELECT role_name,permission_key FROM users_role_permissions WHERE role_name LIKE 'legacy_%' EXCEPT SELECT * FROM expected_role_permissions)
          UNION ALL (SELECT * FROM expected_role_permissions EXCEPT SELECT role_name,permission_key FROM users_role_permissions))
          THEN RAISE EXCEPTION 'Role permission reconciliation failed'; END IF;
          IF EXISTS((SELECT user_id,role_name FROM users_user_roles WHERE role_name LIKE 'legacy_%' EXCEPT SELECT * FROM expected_user_roles)
          UNION ALL (SELECT * FROM expected_user_roles EXCEPT SELECT user_id,role_name FROM users_user_roles))
          THEN RAISE EXCEPTION 'User role reconciliation failed'; END IF;
          END $$;"""]
    return sql,warnings
