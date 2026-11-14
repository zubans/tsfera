import json
import unittest
from pathlib import Path
from permissions import default_mapping,effective_permissions,translate

FIXTURE=json.loads((Path(__file__).parent/'fixtures/rbac.json').read_text())
AVAILABLE={p for values in default_mapping().values() for p in values}|{'crm.contractor.view_all'}

class PermissionMigrationTests(unittest.TestCase):
    def setUp(self):
        self.roles,self.warnings=translate(FIXTURE['items'],FIXTURE['links'],AVAILABLE)
    def test_exported_http_role_fixture_matches_translation(self):
        rows=json.loads((Path(__file__).parent/'fixtures/translated_roles.json').read_text())
        self.assertEqual(len(rows),9)
        for row in rows:
            self.assertEqual(set(row['permissions']),self.roles[row['role']])
    def test_complete_source_catalogue(self):
        source={i['name'] for i in FIXTURE['items'] if i['type']==2}
        self.assertEqual(len(source),112)
        self.assertEqual(source,set(default_mapping()))
        self.assertEqual(self.warnings,[])
        for name,perms in self.roles.items():
            self.assertNotIn('crm.report.view',perms)
            self.assertNotIn('crm.pool.manage',perms)
            self.assertNotIn('crm.ref.manage',perms)
    def test_engineer_does_not_gain_export_close_rating_or_global_access(self):
        rights=self.roles['inzener']
        self.assertIn('crm.application.update',rights)
        self.assertIn('crm.application.update_comment',rights)
        self.assertIn('crm.contractor.view_restricted',rights)
        for p in ['crm.application.close','crm.application.rating','crm.certificate.export','crm.contractor.view_all','crm.certificate.update_any']:
            self.assertNotIn(p,rights)
    def test_quality_manager_can_rate_but_not_rewrite_application(self):
        rights=self.roles['menedzer_po_kacestvu']
        self.assertIn('crm.application.rating',rights)
        self.assertNotIn('crm.application.update',rights)
        self.assertNotIn('crm.analytic_question.create',rights)
        self.assertIn('crm.analytic.create',rights)
    def test_head_has_view_only_pool_and_catalogue_specific_writes(self):
        rights=self.roles['rukovoditel_podrazdelenia']
        self.assertIn('crm.pool.view',rights)
        self.assertNotIn('crm.pool.update',rights)
        self.assertIn('crm.ref.special_note.create',rights)
        self.assertNotIn('crm.ref.work_type.create',rights)
    def test_accountant_has_only_own_report_types(self):
        rights=self.roles['buhgalter']
        self.assertIn('crm.report.sum_executor.view',rights)
        self.assertNotIn('crm.report.engineers.view',rights)
        self.assertNotIn('crm.report.call_conversion.view',rights)
    def test_operator_execute_is_not_general_task_update(self):
        self.assertIn('crm.task.execute',self.roles['operator'])
        self.assertNotIn('crm.task.update',self.roles['operator'])
    def test_reject_cycle_unknown_edge_and_invalid_override(self):
        items=[dict(id=1,name='role',type=1),dict(id=2,name='p',type=2)]
        for links in [[dict(parent='role',child='role')],[dict(parent='role',child='missing')]]:
            with self.assertRaises(ValueError): effective_permissions(items,links)
        with self.assertRaises(ValueError):translate(FIXTURE['items'],FIXTURE['links'],AVAILABLE,{'common.call.view':['not.registered']})
        with self.assertRaises(ValueError):translate(FIXTURE['items'],FIXTURE['links'],AVAILABLE,{'typo':[]})
    def test_disabling_contractor_view_does_not_grant_global_scope(self):
        roles,_=translate(FIXTURE['items'],FIXTURE['links'],AVAILABLE,{'common.contractor.view':[]})
        self.assertNotIn('crm.contractor.view_all',roles['superadmin'])
        self.assertNotIn('crm.contractor.view_all',roles['buhgalter'])
    def test_unknown_permission_still_blocks_and_explicit_override_can_disable(self):
        items=[dict(id=1,name='new.permission',type=2)]
        _,warnings=translate(items,[],AVAILABLE)
        self.assertEqual(warnings,['Unmapped permission: new.permission'])
        rights,warnings=translate(items,[],AVAILABLE,{'new.permission':[]})
        self.assertFalse(warnings);self.assertEqual(rights['new.permission'],set())

if __name__=='__main__':unittest.main()
