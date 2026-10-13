# Scoped independent Delta: local-runner runtime prerequisites

Date: 2026-10-13. Role: independent read-only runtime/platform reviewer. Scope is the two triage-confirmed findings `LR-AO-RUNTIME-001` and `LR-AO-RUNTIME-002`, their repairs and directly introduced regressions. This is not another Full review, human approval, a publication signature, or activation evidence.

## Frozen snapshots independently verified

| Repository | Original task base | Prior reviewed head | Repaired head | Repaired tree | `git diff --binary PRIOR..HEAD` SHA256 |
| --- | --- | --- | --- | --- | --- |
| Binary-Refinery/agent-orchestrator | `f60f432a32b9f72370dafab2c58b703182dc8026` | `aa517803ed8803e4238260747dcb9ddae2b5f5a2` | `a9450a0aa52644ef1bacbdf834f1c5daf4e268fa` | `d846d6cafb3c2a232e564d278e809fc14d6dd365` | `2e2933e0276e8f26e46611bff4626e5b4e316d99236dc460f00b3e5717d2624c` |
| Artifaktory/engineering-backbone | `2cd4d9bd947fbd1aff25c6bec28e98f3fc1b1c13` | `9064f09b9aff88ae8a14e359f3d6ec82457ba582` | `be3f75f2426dc5c0f4382e791be74add16ba7104` | `1e32e7fa8a3941797f8af91e399eb4096e7fec5e` | `f2ba711876a7c7f30681a96520b41bb6bb3a8f5a5f345f57b7c7750b7c9eb805` |

The heads, trees and delta digests match `delta-review-manifest.json`; both tracked working trees were clean. The installed Full runtime reports are byte-identical to this reviewer's original reports, preserving their finding IDs, coverage and limits. No repository source, report, instruction, commit or remote state was changed by this reviewer.

## Result

**Both confirmed findings are RESOLVED in the scoped prerequisite/verification implementation. NO CONFIRMED IN-SCOPE DELTA FINDINGS.** Actual runner provisioning and runtime activation remain blocked; finding closure does not assert that the certified hosts or access evidence already exist.

### LR-AO-RUNTIME-001 — RESOLVED

The renderer workflow preserves its pinned Playwright image, `--user 1001` and every prior test/setup step. It now places `Verify renderer container mount access` before checkout, with `working-directory: .` and explicit Bash rather than the not-yet-present frontend directory. The actual step requires UID1001, writable existing workspace/temp/tool-cache/HOME directories and writable ENV/PATH/OUTPUT/STEP_SUMMARY command files. It fails explicitly on any rejected condition and cannot continue to checkout/setup after that failure.

`docs/local-runners.md` now makes host UID1001 and all these writable container mounts an explicit `ao-ci` certification prerequisite. It requires isolated compatible/incompatible-owner tests before assigning the label, distinguishes static evidence from real Docker evidence, and requires cleanup/ownership recovery for root-created package files before reusing persistent workspaces. This directly closes the former undocumented compatibility assumption without altering runner tiers, browser tests or using a hosted fallback.

Read and independently executed the new regression test against the actual YAML Bash body. Its explicit UID/permission doubles exercise supported UID, rejected UID1000, denied tool-cache write and denied command-file write. These prove the preflight's control logic and placement, not kernel permissions or Docker execution. The documentation accurately states that limitation.

### LR-AO-RUNTIME-002 — RESOLVED

The new per-tier certified-host catalog explicitly separates package-container utilities from host requirements. It covers bare Linux `gh`, curl, jq, OpenSSL and archive/compiler tools; Linux CGO; Windows Git Bash, Python and setup-cache/extraction access; compatible Windows MinGW-w64/CGO/DWARF5/binutils evidence; Windows Visual Studio C++ native-addon rebuild; and both macOS tiers' gh/jq/OpenSSL/Python/Xcode/Swift/lipo/archive/cache needs. It requires actual executable versions, PATH and retained native command results before labelling/activation, and expressly makes no claim of existing provisioning.

The leaderboard now adds only `actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020` with Node24 ahead of its existing shell Node test. The official immutable v4 pin was supplied as independently fetched provenance by the parent. The trusted default-branch checkout, disabled persisted credentials, limited commenting token and original leaderboard tests/commenting steps remain unchanged. No candidate execution or wider token permission is introduced.

This is a bounded correction to the missing host-prerequisite contract. It does not register runners, install onto user hosts, silently assume tools, modify production application behavior, or remove checks.

## Direct regression and evidence checks

| Independently executed check | Actual result |
| --- | --- |
| Manifest/head/tree/delta-SHA256 verification and tracked status | Match; clean |
| `python3 -m unittest discover -s test/ci -p test_local_runners.py -v` | Exit 0; seven tests PASS in 0.791s, including four actual-preflight UID/permission cases |
| Parsed comparison of repaired renderer/leaderboard jobs with their prior-reviewed versions | Every prior step retained in order; exactly one new step in each; all other YAML values unchanged |
| Shell Node runtime version | Exit 0; v24.19.0 |
| `node --test .github/scripts/pr-review-leaderboard.test.mjs` | Exit 0; five tests PASS, zero failures/skips |
| Installed immutable Full-runtime report byte comparisons | Both identical to original reviewer outputs |
| Backbone delta path classification | Documentary review/check/task evidence only; no workflow or implementation change |

The parent supplied actionlint1.7.7 with custom labels, whitespace PASS and the updated trusted validators. Backbone's frozen evidence now records 239 full tests PASS in 642.854s. These supplied checks were not broadly rerun by this Delta reviewer. Backbone's task retains the unresolved activation criterion as `blocked`; runtime/access/isolation claims have not become PASS.

The copied final AO quality audit digests were independently checked:

- Markdown: `f4e0ceca767212c03c60813971f762bd74ab042c1a2c37f0fdb758f05e36374c`.
- JSON: `d887fc8184d983209e3b4a1d6a5ce23e2924895d164ae5b4ddffad6bec83da0d`.

The audit's actual verdict remains `NOT_GREEN_FULL_CI_UNVERIFIED`. Its unchanged-source failures, timed-out/incomplete checks, setup attempts and unsupported runtime paths are preserved as such. This scoped Delta does not reopen those application failures, perform unrelated fixes, or relabel them green.

## Coverage, limits and handoff

Inspected the confirmed triage, both complete binary repair diffs, added workflow steps, modified regression test, complete certified-host requirements, immutable Full-report copies, check evidence/task-state update, and final quality-audit verdict/digests. Prior unchanged Full coverage is not reopened.

Unexecuted/unreviewed live paths remain: Docker mount access and UID ownership under the intended runner; root-created workspace cleanup; actual per-tier tool versions/compiler/native-addon compatibility; runner registration, repository/group access and approval/isolation controls; Windows/macOS and container/package suites; remote source-job names/groups and required-check enforcement; advisory remote Codex results. No claim is made that a shell permission double certifies any of these live paths. The prior UID-switch fixture remains blocked in this workspace, as recorded by Full review.

This result clears only the two scoped review defects and their direct regressions. Draft-only delivery and the documented activation stops remain in force. Obtain real independent provisioning, access, isolation and retained-command runtime evidence before activation; no merge/release/deployment authorization follows from these AI reviews.
