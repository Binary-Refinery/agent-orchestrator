"""Runner-only migration regressions; requires PyYAML for workflow parsing."""

from copy import deepcopy
from pathlib import Path
import subprocess
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]
BASE = 'f60f432a32b9f72370dafab2c58b703182dc8026'


def read_workflow(path, base=False):
    text = subprocess.check_output(
        ['git', 'show', f'{BASE}:{path.relative_to(ROOT)}'], cwd=ROOT, text=True,
    ) if base else path.read_text()
    # BaseLoader preserves the Actions `on` key instead of treating it as YAML 1.1 true.
    return yaml.load(text, Loader=yaml.BaseLoader)


def runner_variants(job):
    value = job['runs-on']
    if isinstance(value, list):
        return [value]
    if value != '${{ matrix.runner_labels }}':
        raise ValueError('Hosted or unsupported runner selection')
    matrix = job['strategy']['matrix']
    rows = matrix['include']
    if 'os' in matrix and {row['os'] for row in rows} != set(matrix['os']):
        raise ValueError('Missing native OS tier')
    return [row['runner_labels'] for row in rows]


def require_self_hosted(job):
    for labels in runner_variants(job):
        values = {label.lower() for label in labels}
        if not {'self-hosted', 'ao-ci'} <= values:
            raise ValueError('A dedicated self-hosted registration is required')
        if len(values & {'linux', 'windows', 'macos'}) != 1:
            raise ValueError('An exact native OS is required')
        if len(values & {'x64', 'arm64'}) != 1:
            raise ValueError('An exact native architecture is required')


class LocalRunnerTests(unittest.TestCase):
    def test_all_workflow_jobs_use_dedicated_native_runners(self):
        count = 0
        for path in sorted((ROOT / '.github/workflows').glob('*.yml')):
            for name, job in read_workflow(path)['jobs'].items():
                with self.subTest(workflow=path.name, job=name):
                    require_self_hosted(job)
                    count += 1
        self.assertEqual(count, 29)

    def test_original_jobs_commands_events_and_timeouts_are_retained(self):
        for path in sorted((ROOT / '.github/workflows').glob('*.yml')):
            current, old = read_workflow(path), read_workflow(path, base=True)
            with self.subTest(workflow=path.name):
                self.assertEqual(current['on'], old['on'])
                self.assertEqual(set(current['jobs']), set(old['jobs']))
            for name, job in old['jobs'].items():
                candidate = current['jobs'][name]
                with self.subTest(workflow=path.name, job=name):
                    self.assertEqual(candidate.get('timeout-minutes'), job.get('timeout-minutes'))
                    self.assertEqual(candidate.get('needs'), job.get('needs'))
                    old_runs = [step['run'] for step in job.get('steps', []) if 'run' in step]
                    candidate_runs = [step['run'] for step in candidate.get('steps', []) if 'run' in step]
                    for command in old_runs:
                        self.assertIn(command, candidate_runs)

    def test_prs_keep_merge_result_checkout_and_same_repo_guard(self):
        for path in sorted((ROOT / '.github/workflows').glob('*.yml')):
            current, old = read_workflow(path), read_workflow(path, base=True)
            if 'pull_request' not in old['on']:
                continue
            self.assertNotIn('pull_request_target', current['on'])
            self.assertEqual(current['permissions'], {'contents': 'read'})
            for name, job in current['jobs'].items():
                with self.subTest(workflow=path.name, job=name):
                    self.assertIn("github.event_name != 'pull_request'", job['if'])
                    self.assertIn('github.event.pull_request.head.repo.full_name == github.repository', job['if'])
                    old_checkouts = [step for step in old['jobs'][name].get('steps', []) if step.get('uses', '').startswith('actions/checkout@')]
                    checkouts = [step for step in job.get('steps', []) if step.get('uses', '').startswith('actions/checkout@')]
                    self.assertEqual(len(checkouts), len(old_checkouts))
                    for before, after in zip(old_checkouts, checkouts):
                        self.assertEqual(after.get('with', {}).get('ref'), before.get('with', {}).get('ref'))

    def test_mac_native_tiers_and_artifact_identities_are_preserved(self):
        cli = read_workflow(ROOT / '.github/workflows/cli-e2e.yml')['jobs']['native']
        rows = {row['os']: row['runner_labels'] for row in cli['strategy']['matrix']['include']}
        self.assertIn('ARM64', rows['macos-latest'])
        helper = read_workflow(ROOT / '.github/workflows/frontend.yml')['jobs']['mac-update-helper']
        self.assertIn('ARM64', helper['runs-on'])
        path = ROOT / '.github/workflows/build-artifacts.yml'
        current, old = read_workflow(path)['jobs']['build'], read_workflow(path, base=True)['jobs']['build']
        fields = ('os', 'platform', 'artifact')
        self.assertEqual([{key: row[key] for key in fields} for row in current['strategy']['matrix']['include']], old['strategy']['matrix']['include'])
        artifacts = {row['artifact']: row for row in current['strategy']['matrix']['include']}
        self.assertIn('ARM64', artifacts['mac-arm64']['runner_labels'])
        self.assertIn('x64', artifacts['mac-x64']['runner_labels'])

    def test_linux_packages_keep_glibc_baseline_and_digest_tools(self):
        artifact = read_workflow(ROOT / '.github/workflows/build-artifacts.yml')['jobs']['build']
        linux = next(row for row in artifact['strategy']['matrix']['include'] if row['platform'] == 'linux-x64')
        self.assertEqual(linux['container'], 'ubuntu:22.04')
        frontend = read_workflow(ROOT / '.github/workflows/frontend.yml')['jobs']['linux-desktop-package']
        self.assertEqual(frontend['container'], 'ubuntu:22.04')
        for job in (artifact, frontend):
            prepare = job['steps'][0]
            self.assertEqual(prepare['working-directory'], '.')
            for command in ('git', 'jq', 'openssl', 'bison', 'build-essential'):
                self.assertIn(command, prepare['run'].split())

    def test_hosted_fallback_missing_label_and_missing_os_tier_are_rejected(self):
        job = read_workflow(ROOT / '.github/workflows/cli-e2e.yml')['jobs']['native']
        variants = [deepcopy(job) for _ in range(3)]
        variants[0]['runs-on'] = 'ubuntu-latest'
        variants[1]['strategy']['matrix']['include'][0]['runner_labels'].remove('self-hosted')
        variants[2]['strategy']['matrix']['include'].pop()
        for candidate in variants:
            with self.assertRaises(ValueError):
                require_self_hosted(candidate)

    def test_actual_renderer_mount_preflight_rejects_wrong_uid_and_write_denials(self):
        import os
        import tempfile
        job = read_workflow(ROOT / '.github/workflows/frontend.yml')['jobs']['renderer-smoke']
        step = job['steps'][0]
        self.assertEqual(step['name'], 'Verify renderer container mount access')
        self.assertEqual(step['working-directory'], '.')
        shim = '''id() { printf '%s\\n' "$AO_TEST_UID"; }
test() {
  if [[ "$1" == -w && "$2" == "$AO_TEST_DENIED" ]]; then return 1; fi
  builtin test "$@"
}
'''
        with tempfile.TemporaryDirectory() as directory:
            env = {**os.environ, 'AO_TEST_UID': '1001', 'AO_TEST_DENIED': ''}
            # HOME is inherited and only inspected; never repurpose it for a fixture.
            for key in ('GITHUB_WORKSPACE', 'RUNNER_TEMP', 'RUNNER_TOOL_CACHE'):
                path = Path(directory) / key
                path.mkdir()
                env[key] = str(path)
            for key in ('GITHUB_ENV', 'GITHUB_PATH', 'GITHUB_OUTPUT', 'GITHUB_STEP_SUMMARY'):
                path = Path(directory) / key
                path.touch()
                env[key] = str(path)
            for uid, denial in (('1001', ''), ('1000', ''), ('1001', env['RUNNER_TOOL_CACHE']), ('1001', env['GITHUB_ENV'])):
                with self.subTest(uid=uid, denial=denial):
                    result = subprocess.run(['bash', '-c', shim + step['run']],
                                            env={**env, 'AO_TEST_UID': uid, 'AO_TEST_DENIED': denial},
                                            text=True, capture_output=True)
                    self.assertEqual(result.returncode == 0, uid == '1001' and not denial, result.stdout)


if __name__ == '__main__':
    unittest.main()
