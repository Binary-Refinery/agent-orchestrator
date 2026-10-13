# Independent scoped Delta review: runner contract repairs

Date: 2026-10-13. Role: independent read-only trust/governance/test contract reviewer. This is a scoped Delta of the two confirmed AO runtime finding repairs and directly introduced regressions, together with changed closeout evidence. It does not reopen unchanged Full scope or constitute human approval, runner activation, merge or release permission.

## Frozen repair snapshots

| Repository | Prior reviewed head | Repair head | Delta binary-patch SHA256 |
| --- | --- | --- | --- |
| AO | `aa517803ed8803e4238260747dcb9ddae2b5f5a2` | `a9450a0aa52644ef1bacbdf834f1c5daf4e268fa` | `2e2933e0276e8f26e46611bff4626e5b4e316d99236dc460f00b3e5717d2624c` |
| Backbone | `9064f09b9aff88ae8a14e359f3d6ec82457ba582` | `be3f75f2426dc5c0f4382e791be74add16ba7104` | `f2ba711876a7c7f30681a96520b41bb6bb3a8f5a5f345f57b7c7750b7c9eb805` |

The values independently match `delta-review-manifest.json`. AO repair tree is `d846d6cafb3c2a232e564d278e809fc14d6dd365`; Backbone repair tree is `1e32e7fa8a3941797f8af91e399eb4096e7fec5e`. Both current heads, trees and recomputed delta digests match, and both tracked worktrees match their frozen heads. The Full reports remain bound to their original base/head revisions; neither is represented as a Full review of the repaired head.

## Conclusion and closure

`NO CONFIRMED IN-SCOPE DELTA FINDINGS`.

| Confirmed finding | Scoped contract-review result | Basis and limit |
| --- | --- | --- |
| `LR-AO-RUNTIME-001` | Resolved in the prerequisite/preflight contract | UID1001 and writable work/temp/tool/HOME/file-command mounts are required before certification; the actual renderer step checks these before checkout/setup. Permission-boundary tests exercise the actual script, while real Docker/host access remains unverified and activation stays blocked. |
| `LR-AO-RUNTIME-002` | Resolved in the prerequisite/runtime-selection contract | Explicit per-tier host tools, CGO/native-addon/cache requirements and actual native command evidence are required before certification. The trusted leaderboard now selects shell Node24 through an exact setup-node action pin. No claim is made that missing native hosts or tools were provisioned or executed. |

Backbone has no confirmed Full finding to repair. Its Delta is documentary/acceptance closeout only. No new repair is requested by this reviewer. These closures address the original findings' omitted prerequisite contracts; they do not certify deployed runners or full application CI. Actual source failures and missing evidence remain explicit stops, not waived or relabelled PASS.

## Inspected repairs and direct regressions

- AO `.github/workflows/frontend.yml` preserves the browser image, `--user 1001`, native routing, PR mitigation, tests and original checkout/setup steps. The added first step uses `working-directory: .` and Bash, checks the actual UID, all four required directories and the four command files, and fails before checkout/setup if access is unavailable. It neither modifies host permissions nor introduces a fallback or skipped renderer tier.
- The added test executes the actual workflow Bash with explicitly simulated UID and denied-write boundaries. Supported UID succeeds; UID1000, denied tool-cache access and denied command-file access fail. The test inherits HOME and does not repurpose it. The documentation labels this as a contract experiment rather than Docker/UID isolation proof.
- `docs/local-runners.md` adds the UID1001 certification floor, fresh-workspace/root-created-file cleanup evidence, complete host utility/compiler/native-addon/setup-cache tiers and version/PATH/actual-command verification. Its prohibition on applying `ao-ci` before that evidence keeps the original conditional failures out of the certified deployment contract. Native versions and actual compiler compatibility still need independent host execution.
- AO `.github/workflows/pr-review-leaderboard.yml` adds `actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020` with Node24 before the existing Node test. Trusted default-branch checkout, `persist-credentials: false`, target-event behavior, permissions and commenting payload remain unchanged. The added action does not fetch or execute PR candidate source.
- Structural comparison against each prior reviewed head verifies that the two changed workflows retain events, permissions, concurrency, guards, runner selection, container configuration, dependency/timeouts/environments/strategy and every prior step dictionary. The source/consumer routing and existing trust/gate contracts are unchanged by the repairs. Backbone has no workflow, toolkit, template, schema or dependency change in this Delta.
- Both repository Full contract report copies are byte-identical to the reviewer originals. Triage identifies the same two confirmed runtime IDs and the previously resolved plan ID; no new scope or automated merge authority is introduced.
- The final AO audit copies are byte-identical to the independent originals: Markdown SHA256 `f4e0ceca767212c03c60813971f762bd74ab042c1a2c37f0fdb758f05e36374c`; JSON SHA256 `d887fc8184d983209e3b4a1d6a5ce23e2924895d164ae5b4ddffad6bec83da0d`. The JSON verdict is `NOT_GREEN_FULL_CI_UNVERIFIED`; publication/deployment execution is false. Changed check documentation accurately reports broad source failures, timeout, unavailable native/container/Node20 paths and explicit supplementary evidence without calling full CI green.
- Backbone's updated full-suite claim is corroborated by the ignored `artifacts/local-runners/full-tests.log`: `Ran 239 tests in 642.854s`, followed by `OK`. The quality owner supplied command `/workspace/scratch/59eed579c382/backbone-venv/bin/python -m unittest discover -s tests -v` and process exit0; this Delta reviewer inspected the saved log rather than rerunning the suite. The updated Task Packet records verified contract/check acceptance, keeps AC4 pending until review closeout, marks activation evidence AC5 blocked, and preserves all human gates/forbidden actions. It does not claim live runner access.

## Actual reviewer checks

- Independent manifest/head/tree/delta-SHA verification and `git diff --exit-code <repair-head>` in both repositories: exit0, exact matches.
- Scoped read-only Python structural comparison of the changed workflow definitions against prior Full heads: exit0, retained contracts/steps verified.
- `python -B -m unittest discover -s test/ci -p test_local_runners.py -k actual_renderer -v`: exit0, one new test covering four declared boundary cases passed. This is neither Docker nor real UID switching.
- `node --version`: `v24.19.0`; `node --test .github/scripts/pr-review-leaderboard.test.mjs`: exit0, all five existing tests passed.
- `git diff --check <prior-head> <repair-head>` in both repositories: exit0.
- SHA256 comparison of independent AO audit files and both Full contract report copies: exact byte matches.
- Inspected the actual Backbone complete-suite log and all changed workflow/test/documentary evidence. Validator/compilation/actionlint successes were supplied by the quality owner; they were not independently rerun in this scoped Delta.

## Coverage, unreviewed paths and disposition

Covered both confirmed-ID repairs, their directly changed workflows/tests/docs, copied review/evidence identity and the Backbone documentary acceptance update. Unchanged Full conclusions and constraints remain as originally recorded.

Unreviewed/unexecuted here: actual runner registrations/group repository access, approval settings and immutable isolation; real renderer container UID/mount access and persistent workspace cleanup; Docker browser/package/scanner execution; Windows/macOS compiler, CGO, Python/action-cache, native-addon and installer execution; real GitHub setup-node download/action execution; full unchanged application suites and source-failure diagnosis; remote own-runner job records/required checks and Codex results. The verified logs are evidence of the recorded executions, not execution on the intended own runners.

Draft-only delivery and activation stops remain appropriate. Full CI remains non-green for AO, and live activation remains blocked for both repositories. No source/application repair, repository-setting write, registration, commit, remote operation, merge, release, deployment or nested agent occurred in this review. The only write is this report outside both repositories.
