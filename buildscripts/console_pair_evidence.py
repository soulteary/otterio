"""Pure validation of structural Console evidence; no I/O or process execution."""
import re


SCENARIOS = ['single-http', 'dual-http', 'single-tls', 'dual-tls']

TLS_SCENARIOS = ['single-tls', 'dual-tls']

PROFILES = ['storage', 'settings', 'features-version-copy', 'native',
            'relationship-iam', 'real-protocol', 'oidc-browser-single', 'oidc-browser-dual']

BROWSER_CHECKS = {'different_sites', 'alice_binding_cookie_flags', 'bob_binding_cookie_flags',
                  'alice_cross_site_top_level_callback_binding', 'bob_cross_site_top_level_callback_binding',
                  'alice_ui_session', 'bob_ui_session', 'alice_ui_logout', 'bob_logout_isolation',
                  'bob_session_after_initial_token_expiry', 'fixed_deadline_401'}

RELATIONSHIP_COVERAGE = {'user.lifecycle-parent-service-sts-cascade', 'user.lifecycle-self',
                         'user.rotate-other', 'user.rotate-self', 'disabled-user.rotate',
                         'user.policies', 'group.policies', 'group.status-members-delete',
                         'service-account.status-secret-policy-delete'}

def require(condition, reason):
    if not condition:
        raise ValueError(reason)

def check_hashes(report_hashes, programs):
    require(report_hashes == {key: value['binarySHA256'] for key, value in programs.items()},
            'child-report-binary-mismatch')

def cleanup_ok(value):
    return (isinstance(value, dict) and value.get('status') == 'passed'
            and value.get('errors') == [] and isinstance(value.get('processes'), list)
            and bool(value['processes']) and all(row.get('stopped') is True for row in value['processes']))

def validate_integration(report, profile, programs):
    scopes = {'storage': 'read-only-and-writes', 'settings': 'read-only-and-protected-settings',
              'features-version-copy': 'console-five-features-and-version-copy',
              'native': 'native-iam-https-read-only', 'relationship-iam': 'relationship-iam-native'}
    require(report.get('status') == 'passed', 'fixture-not-passed')
    require(report.get('scope') == scopes[profile], 'wrong-or-unsupported-profile')
    require(not report.get('serverPatches'), 'patched-fixture-not-allowed')
    require(report.get('serverSource') == programs['server']['source'], 'child-report-source-mismatch')
    require(report.get('sdkPin') == programs['oc']['sdkPin'], 'child-report-sdk-mismatch')
    check_hashes({{'oc': 'oc', 'oc-console': 'console', 'otterio': 'server'}[key]: row['sha256']
                  for key, row in report['binaries'].items()}, programs)
    require(report.get('identityEvidence') == {'sdkPin': 'verified-client-build-info',
                                             'serverSource': 'verified-vcs-revision'}, 'child-identity-not-clean')
    require(report.get('storage') == 'single-node-four-disk-erasure', 'wrong-storage-fixture')
    scenarios = report.get('scenarios', [])
    expected = TLS_SCENARIOS if profile in ('native', 'relationship-iam') else SCENARIOS
    names = [row['scenario'] for row in scenarios]
    require(len(names) == len(expected) and set(names) == set(expected), 'missing-or-duplicate-scenarios')
    for row in scenarios:
        require(row.get('status') == 'passed' and cleanup_ok(row.get('processCleanup')), 'scenario-or-cleanup-failed')
        checks = row.get('checks', [])
        require(isinstance(checks, list) and len(checks) >= 10
                and all(isinstance(item, str) and item for item in checks), 'scenario-checks-missing')
        if profile == 'storage':
            require(isinstance(row.get('writeMetrics'), dict) and bool(row['writeMetrics']), 'write-evidence-missing')
        elif profile == 'settings':
            require(report.get('settingsProfile') == 'v1-protected-settings'
                    and row.get('settingsMetrics', {}).get('profile') == 'v1-protected-settings', 'settings-profile-unsupported')
        elif profile == 'features-version-copy':
            require(isinstance(row.get('featureMetrics'), dict) and bool(row['featureMetrics']), 'feature-evidence-missing')
            metric = row.get('versionCopyMetrics', {})
            require(metric.get('profile') == 'copy-source-version-authorization'
                    and metric.get('confirmedCopies', 0) > 0 and metric.get('accessDeniedWithoutDestination', 0) > 0,
                    'version-copy-evidence-missing')
        elif profile == 'native':
            metric = row.get('nativeMetrics', {})
            require(metric.get('iamCreation') == 'native user/group/service-account; permission and duplicate-target checks verified'
                    and metric.get('browserTLS') == 'verified' and metric.get('storageTLS') == 'verified'
                    and metric.get('rootLogin') == 'denied', 'native-profile-unsupported')
        else:
            metric = row.get('relationshipIAMMetrics', {})
            coverage = metric.get('positiveMutationCoverage', [])
            require(metric.get('capabilityMode') == 'required-supported'
                    and set(coverage) == RELATIONSHIP_COVERAGE and len(coverage) == len(RELATIONSHIP_COVERAGE)
                    and cleanup_ok(metric.get('processCleanup')), 'relationship-positive-evidence-missing')
    return {'scenarios': names, 'acceptanceGroups': sum(len(row['checks']) for row in scenarios)}

def validate_protocol(report, programs):
    require(report.get('schemaVersion') == 1 and report.get('passed') is True, 'protocol-not-passed')
    server = report['server']
    require(server.get('source') == programs['server']['source'] and server.get('kind') == 'module-build'
            and server.get('sourceState') == 'clean', 'protocol-source-mismatch')
    check_hashes({'oc': report['clientBinarySHA256']['cli'], 'console': report['clientBinarySHA256']['console'],
                  'server': server['binarySHA256']}, programs)
    checks = report.get('checks', [])
    require(len(checks) == 3 and {row['name'] for row in checks} == {'range', 'diagnostics', 'oidc-sts'}
            and all(row.get('status') == 'passed' and row.get('exitCode') == 0 for row in checks), 'protocol-skipped-or-failed')
    for row in checks:
        if row['name'] != 'range':
            expected = 'TestDiagnosticsRealServer' if row['name'] == 'diagnostics' else 'TestOtterIOSTSIntegration'
            require(row.get('reason') == 'named-test-ran-and-passed' and row.get('test') == expected,
                    'protocol-test-absent-or-skipped')
        else:
            profiles = row.get('profiles', [])
            require(len(profiles) == 3 and {p['profile'] for p in profiles} == {'single-http', 'dual-http', 'dual-tls'}
                    and all(p.get('historicalVersionStatus') == 206 and p.get('restrictedHistoricalStatus') == 403
                            for p in profiles), 'range-profile-missing')
    return {'protocolChecks': [row['name'] for row in checks]}

def validate_oidc(report, dual, programs):
    require(report.get('status') == 'passed' and report.get('cleanup') == 'passed', 'oidc-or-cleanup-failed')
    for name, identity in programs.items():
        child = report['programs'][name]
        require(child.get('declaredSource') == identity['source'] and child.get('binaryVCSRevision') == identity['source']
                and child.get('binaryVCSModified') is False and child.get('sha256') == identity['binarySHA256'],
                'oidc-program-mismatch')
    require(report.get('storageTopology') == ('dual HTTPS S3/Admin listeners' if dual else 'single HTTPS S3/Admin listener'),
            'oidc-topology-missing')
    require(report.get('browserSiteTopology') == 'localhost IdP and 127.0.0.1 Console (different sites)', 'oidc-cross-site-missing')
    ca = report['certificateIdentities']
    require(bool(re.fullmatch(r'[0-9a-f]{64}', ca['primaryCAsha256']))
            and bool(re.fullmatch(r'[0-9a-f]{64}', ca['adminCAsha256']))
            and (ca['primaryCAsha256'] != ca['adminCAsha256']) == dual, 'oidc-ca-topology-mismatch')
    checks = report.get('checks', [])
    require(isinstance(checks, list) and len(checks) >= (14 if dual else 13)
            and 'Bob saved pre-expiry cookie is rejected after fixed TTL despite successful refresh' in checks,
            'oidc-fixed-expiry-not-run')
    if dual:
        require('independent Admin HTTPS CA accepted; S3 primary CA is rejected for Admin listener' in checks, 'oidc-wrong-ca-not-tested')
    refresh = report['consoleRefreshEvidence']
    require(refresh['waitSeconds'] > refresh['idTokenSeconds']
            and refresh['keycloakEventsAfter'] > refresh['keycloakEventsBefore'], 'oidc-provider-refresh-not-observed')
    browser = report.get('browserAcceptance', {})
    require(browser.get('status') == 'passed' and browser.get('persistentProfile') is False
            and browser.get('successfulRefreshEventsDelta', 0) > 0 and bool(browser.get('browser')), 'real-browser-missing')
    rows = browser.get('checks', [])
    names = [row['name'] for row in rows]
    require(BROWSER_CHECKS.issubset(names) and len(names) == len(set(names))
            and all(row.get('status') == 'passed' for row in rows), 'browser-check-absent-or-skipped')
    require(str(report.get('consoleAcceptance', '')).startswith('passed:')
            and report.get('consoleFixedExpiryReference') and 'consoleFixedExpiry' not in report, 'console-profile-not-run')
    return {'browserChecks': len(rows), 'protocolChecks': len(checks), 'browserVersion': browser['browser'],
            'keycloakImage': report['keycloak']['image']}
