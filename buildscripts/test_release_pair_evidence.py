"""The reviewed local validator must perform only pure structural checks."""
import ast
import copy
import importlib.util
from pathlib import Path
import unittest


SOURCE = Path(__file__).with_name('console_pair_evidence.py')
SPEC = importlib.util.spec_from_file_location('reviewed_console_pair_evidence', SOURCE)
evidence = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(evidence)


class PureEvidenceTests(unittest.TestCase):
    def test_module_contains_only_reviewed_pure_checks(self):
        tree = ast.parse(SOURCE.read_text())
        functions = {'require', 'check_hashes', 'cleanup_ok', 'validate_integration', 'validate_protocol', 'validate_oidc'}
        constants = {'SCENARIOS', 'TLS_SCENARIOS', 'PROFILES', 'BROWSER_CHECKS', 'RELATIONSHIP_COVERAGE'}
        self.assertEqual({node.name for node in tree.body if isinstance(node, ast.FunctionDef)}, functions)
        imports = [node for node in ast.walk(tree) if isinstance(node, (ast.Import, ast.ImportFrom))]
        self.assertEqual(len(imports), 1)
        self.assertIsInstance(imports[0], ast.Import)
        self.assertEqual([name.name for name in imports[0].names], ['re'])
        for node in tree.body:
            if isinstance(node, ast.Expr):
                self.assertIsInstance(node.value, ast.Constant)
                self.assertIsInstance(node.value.value, str)
            elif isinstance(node, ast.Assign):
                self.assertTrue(all(isinstance(target, ast.Name) and target.id in constants for target in node.targets))
                ast.literal_eval(node.value)
            else:
                self.assertIsInstance(node, (ast.Import, ast.FunctionDef))
        pure_calls = functions | {'ValueError', 'isinstance', 'bool', 'len', 'set', 'sum', 'all', 'str'}
        pure_methods = {'get', 'items', 'fullmatch', 'issubset', 'startswith'}
        for node in ast.walk(tree):
            if isinstance(node, ast.Call):
                if isinstance(node.func, ast.Name):
                    self.assertIn(node.func.id, pure_calls)
                else:
                    self.assertIsInstance(node.func, ast.Attribute)
                    self.assertIn(node.func.attr, pure_methods)
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                self.assertEqual(node.decorator_list, [])
                self.assertEqual(node.args.defaults, [])
                self.assertEqual(node.args.kw_defaults, [])

    def test_successful_and_rejected_validation_do_not_modify_raw_reports(self):
        programs = {name: {'source': 'a' * 40, 'binarySHA256': 'b' * 64} for name in ('oc', 'console', 'server')}
        report = {'schemaVersion': 1, 'passed': True, 'server': {'source': 'a' * 40, 'kind': 'module-build',
                  'sourceState': 'clean', 'binarySHA256': 'b' * 64},
                  'clientBinarySHA256': {'cli': 'b' * 64, 'console': 'b' * 64}, 'checks': [
                      {'name': 'range', 'status': 'passed', 'exitCode': 0, 'profiles': [
                          {'profile': profile, 'historicalVersionStatus': 206, 'restrictedHistoricalStatus': 403}
                          for profile in ('single-http', 'dual-http', 'dual-tls')]},
                      {'name': 'diagnostics', 'status': 'passed', 'exitCode': 0,
                       'reason': 'named-test-ran-and-passed', 'test': 'TestDiagnosticsRealServer'},
                      {'name': 'oidc-sts', 'status': 'passed', 'exitCode': 0,
                       'reason': 'named-test-ran-and-passed', 'test': 'TestOtterIOSTSIntegration'}]}
        original_report, original_programs = copy.deepcopy(report), copy.deepcopy(programs)
        self.assertEqual(evidence.validate_protocol(report, programs)['protocolChecks'], ['range', 'diagnostics', 'oidc-sts'])
        self.assertEqual(report, original_report)
        self.assertEqual(programs, original_programs)
        report['checks'][1]['status'] = 'skipped'
        rejected = copy.deepcopy(report)
        with self.assertRaises(ValueError): evidence.validate_protocol(report, programs)
        self.assertEqual(report, rejected)
        self.assertEqual(programs, original_programs)


if __name__ == '__main__':
    unittest.main()
