import json,hashlib,tempfile,subprocess,unittest
from pathlib import Path
from unittest.mock import patch
import input_scope,host_contracts,behavior_selection,ai_check_plan
class InputScopes(unittest.TestCase):
 def setUp(self):
  t=tempfile.TemporaryDirectory();self.addCleanup(t.cleanup);self.r=Path(t.name)
  for c in [['git','init','-q'],['git','config','user.name','test'],['git','config','user.email','test@example.invalid']]:subprocess.run(c,cwd=self.r,check=True)
 def write(self,p,s):
  f=self.r/p;f.parent.mkdir(parents=True,exist_ok=True);f.write_text(s)
 def commit(self):
  subprocess.run(['git','add','.'],cwd=self.r,check=True);subprocess.run(['git','commit','-qm','fixture'],cwd=self.r,check=True);return subprocess.check_output(['git','rev-parse','HEAD'],cwd=self.r,text=True).strip()
 def declared_choice(self,base,checks):
  self.write('docs/release-checks.json',json.dumps({'schema':1,'baseline_sha':base,'checks':checks}))
  head=self.commit()
  return ai_check_plan.selection(ai_check_plan.load_manifest(self.r,base,head))
 def dependency_checks(self):
  self.write('web/v3/dependency.test.mjs','export {};\n')
  self.write('cmd/aicrm/dependency_chromium_test.go','package main\nfunc TestDependencyChromiumJourney(t *testing.T) {}\n')
  return [{'lane':'frontend','path':'web/v3/dependency.test.mjs'},
          {'lane':'browser','path':'cmd/aicrm/dependency_chromium_test.go','test':'TestDependencyChromiumJourney'}]
 def test_route_consumers_in_both_trees_and_global_schema_fallback(self):
  self.write('api/openapi.yaml','openapi: 3.0.0\npaths:\n  /api/admin/payments:\n    get: old\ncomponents:\n  schemas: same\n')
  self.write('internal/payment/http/a.go','package http\nconst route="/api/admin/payments"\n');b=self.commit()
  self.write('api/openapi.yaml','openapi: 3.0.0\npaths:\n  /api/admin/payments:\n    get: new\ncomponents:\n  schemas: same\n');h=self.commit()
  r=input_scope.resolve(self.r,b,h,['api/openapi.yaml']);self.assertEqual(r['mapped']['api/openapi.yaml'],['internal/payment/http/a.go']);self.assertFalse(r['unknown'])
  self.write('api/openapi.yaml','openapi: 3.0.0\npaths:\n  /api/admin/payments:\n    get: new\ncomponents:\n  schemas: changed\n');h=self.commit();self.assertIn('api/openapi.yaml',input_scope.resolve(self.r,b,h,['api/openapi.yaml'])['unknown'])
 def test_migration_includes_owner_and_read_consumer_and_unknown(self):
  self.write('internal/media/store/a.go','package store\nconst q="SELECT * FROM media_invitation_plans"\n');self.write('internal/groupops/store/a.go','package store\nconst q="JOIN media_invitation_plans"\n');b=self.commit()
  self.write('migrations/0215.sql','ALTER TABLE media_invitation_plans ADD COLUMN options JSONB;');h=self.commit();r=input_scope.resolve(self.r,b,h,['migrations/0215.sql']);self.assertEqual(len(r['mapped']['migrations/0215.sql']),2)
  self.write('migrations/0216.sql','DO $$ BEGIN UNKNOWN; END $$;');h=self.commit();self.assertIn('migrations/0216.sql',input_scope.resolve(self.r,b,h,['migrations/0216.sql'])['unknown'])
 def test_images_and_audit_records_are_documents_but_runtime_unknown_is_not(self):
  self.assertTrue(input_scope.document('docs/design/pay.png'));self.assertTrue(input_scope.document('docs/engineering/dedup/p5-authority-change-approvals.json'));self.assertFalse(input_scope.document('web/v3/pay.png'));self.assertFalse(input_scope.document('docs/governance/capability-impact.json'))
 def test_npm_dependency_requires_manifest_and_uses_only_declared_checks(self):
  self.write('package.json','{}');b=self.commit();self.write('package.json','{"a":1}');h=self.commit()
  with self.assertRaisesRegex(ValueError,'docs/release-checks.json is required'):
   behavior_selection.select(self.r,b,h,['package.json'],{'selected_packages':[]})
  choice=self.declared_choice(b,self.dependency_checks())
  self.assertEqual(choice['lanes'],['frontend','browser']);self.assertEqual(choice['profile'],'ai-selected')
 def test_host_new_or_changed_file_automatically_joins_and_shared_host_is_full(self):
  self.write('cmd/aicrm/pay_test.go','package main\nfunc TestPay(t *testing.T) {}');self.write('cmd/aicrm/other_test.go','package main\nfunc TestOther(t *testing.T) {}')
  registry={'files':{p.relative_to(self.r).as_posix():{'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'domains':[d]} for p,d in [(self.r/'cmd/aicrm/pay_test.go','payment'),(self.r/'cmd/aicrm/other_test.go','radar')]}}
  graph={'selected_packages':[{'dir':'internal/payment/http'}]}
  with patch.object(host_contracts.json,'loads',return_value=registry):
   a=host_contracts.select(self.r,['internal/payment/http/a.go'],graph);self.assertEqual([x['test'] for x in a],['TestPay'])
   self.write('cmd/aicrm/other_test.go','package main\nfunc TestOther(t *testing.T) {}\nfunc TestAdded(t *testing.T) {}');a=host_contracts.select(self.r,['internal/payment/http/a.go'],graph);self.assertIn('TestAdded',[x['test'] for x in a]);self.assertIsNone(host_contracts.select(self.r,['cmd/aicrm/composition.go'],graph))
 def test_schema_change_follows_transitive_refs_to_its_route(self):
  old='openapi: 3.0.0\npaths:\n  /api/admin/products:\n    get: {schema: {$ref: "#/components/schemas/Page"}}\ncomponents:\n  schemas:\n    Options:\n      properties: {enabled: {type: boolean}}\n    Product:\n      properties: {options: {$ref: "#/components/schemas/Options"}}\n    Page:\n      items: {$ref: "#/components/schemas/Product"}\n'
  routes,unknown=input_scope.affected_api_routes(old,old.replace('enabled: {type: boolean}','enabled: {type: boolean}, alipay: {type: boolean}'))
  self.assertEqual(routes,{'/api/admin/products'});self.assertFalse(unknown)
 def test_dependency_change_does_not_omit_simultaneous_go_change(self):
  self.write('package.json','{}');self.write('internal/payment/app/a.go','package app\n');b=self.commit()
  self.write('package.json','{"a":1}');self.write('internal/payment/app/a.go','package app\nconst changed=1\n');h=self.commit()
  with self.assertRaisesRegex(ValueError,'docs/release-checks.json is required'):
   behavior_selection.select(self.r,b,h,['package.json','internal/payment/app/a.go'],{'selected_packages':[{'dir':'internal/payment/app'}]})
  self.write('internal/payment/app/a_test.go','package app\nfunc TestPayment(t *testing.T) {}\n')
  checks=self.dependency_checks()+[{'lane':'backend','path':'internal/payment/app/a_test.go','test':'TestPayment'}]
  choice=self.declared_choice(b,checks)
  self.assertEqual(choice['lanes'],['backend','frontend','browser'])
  self.assertIn('TestPayment',[check.get('test') for check in choice['checks']])

 def test_retention_validator_and_design_review_do_not_force_all_lanes(self):
  self.write('scripts/check-retention-registry.py','old');b=self.commit();self.write('scripts/check-retention-registry.py','new');h=self.commit()
  with self.assertRaisesRegex(ValueError,'docs/release-checks.json is required'):
   behavior_selection.select(self.r,b,h,['scripts/check-retention-registry.py','docs/design-qa.md'],{'selected_packages':[]})
  self.write('scripts/test_retention_registry.py','# selection-only path fixture, not executed\n')
  choice=self.declared_choice(b,[{'lane':'preflight','path':'scripts/test_retention_registry.py'}])
  self.assertEqual(choice['lanes'],['preflight']);self.assertEqual(choice['profile'],'ai-selected')
