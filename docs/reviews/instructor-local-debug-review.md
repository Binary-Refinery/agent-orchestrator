# Independent local review and finding assessment

Base: `f60f432a32b9f72370dafab2c58b703182dc8026`.
Implementation: local Codex root. Independent reviewer: `/root/error_path_review`.
Reviewers did not edit the implementation or attest unavailable checks.

## Plan review

The independent reviewer identified the real failure-preservation gap in native
history reconciliation, plus the missing detail in semantic delivery diagnostics.
Plan accepted with the requirement to use a real SQLite/controller restart test,
preserve explicit replay errors and successful outcome upgrades, and prove that
stable-key reads do not execute native work again.

## First implementation review

Verdict: PASS, no P0/P1/P2 finding. The reviewer independently verified all four
implementation/test file hashes. One P3 test finding: the diagnostic bound was
not demonstrated with Unicode, and the final retry result was not asserted.

Finding assessment: accepted. The correction adds a greater-than-2048-character
Unicode error, verifies the preserved prefix, omitted suffix and valid UTF-8,
and asserts all three stable-key observations. This is correction round one;
the production implementation did not change after the full review.

## Delta review

Verdict: PASS; P3 closed, no new findings. The reviewer independently confirmed
the corrected test hash and unchanged hashes of the other three files.

| File | First delta snapshot SHA256 |
| --- | --- |
| `backend/internal/service/chat/controller.go` | `b321c7dc8261f06fd5a0884c029f56f5e8f8df93e3b206e950a28b0bb5d4ca7f` |
| `backend/internal/service/chat/semantic_delivery.go` | `dae69b0a459cd46b5298dd78360d3c8099c30a0ffba5346ee742cba72a607d78` |
| `backend/internal/service/chat/provider_failure_history_test.go` | `fc31494c736d58642d104118a93cdbca76103e07d518486fdbde0b690486d6c3` |
| `backend/internal/service/chat/semantic_delivery_race_test.go` | `bd8ee5e1fb437129683e11c7563f011f39c20b4027ea02af9b6e49f58529aefa` |

## Check evidence and limits

The restart/failure-detail regressions reproduced both losses before the fix and
passed afterward. The final targeted set passed in 3.745 seconds. Complete chat,
Instructor, ACP and OpenCode-ACP package tests passed, as did backend build and
affected-package vet. Frontend typecheck passed after installing the shared
product-ui package's declared dependencies; four relevant renderer files passed
all 45 tests. No source dependency change was made.

Three Windows HTTP controller failures were reproduced unchanged on a separate,
unmodified checkout of this exact base: relative file-path classification,
concurrent mobile-config rename, and local Git clone URL handling. They remain
explicit baseline gaps, not passed checks. Native macOS/Linux platform checks,
race validation and remote CI are not attested here.

The earlier Work configuration route error is absent in the current installed
build: actual GUI Network inspection identifies the expected project-policy GET
with HTTP 200 and the current pinned policy. The installed daemon is unmodified
at this base; relevant installed frontend code matches the source.

The real local provider baseline and corrected Electron/daemon lab are recorded
separately. No simulated provider is counted as authenticated native execution.
Historical HTTP 400 request parameters remain unknown. Previously erased
historical errors are not reconstructed by this narrowly scoped fix.

No merge or publication authority is conveyed by these reviews.

## Real-chain findings and second correction round

The genuine nonce chain exposed lost reviewer events: the provider completed its
exact review turn, but AO retained only the submitted user prompt and a running
turn. Independent review identified the worker-generation fence used for review
projection and cleanup, suppressed reviewer activity, missing reviewer IDs in
read models, and reviewer prompts overwriting the worker's human checkpoint.
Automatic attribution discovery also omitted actual worker harness/model/effort.

The implementation now resolves the durable reviewer owner, keeps stale-controller
fencing, scopes live cleanup to conversation/session/reviewer, records reviewer
activity using its own generation, preserves reviewer IDs and leaves worker
checkpoints untouched. Cold native-history activity reconciliation changes display
facts only; it does not drain queues, approve decisions or send a prompt.
Attribution candidates use actual stored native configuration; verification remains
strict and unavailable session facts cannot become evidence.

The independent full scope review found one P2: cleanup initially also filtered on
the turn's generation, although live reconnect retains an older running turn under
the new controller. Finding accepted. A real-store/service regression reproduced
the retained running turn, tool and pending approval after stop. The correction
removes that turn-generation filter while retaining the current-controller fence
and owner scope. Worker and sibling-reviewer facts remain unchanged, with zero
provider sends or history reads in this test.

Independent delta verdict: PASS, P2 closed, no new findings. Reviewer
`/root/error_path_review` independently matched all 14 source/test hashes in
`instructor-local-debug-source-hashes.json`, captured at
`2026-10-14T08:15:55.6481693Z`. This is the second bounded correction round;
neither this scope expansion nor the new fixture resets any prior work budget.

Final targeted regressions passed: chat 2.658s, store 2.155s. Full corrected chat
and store suites passed in 59.169s and 27.085s; complete review and Instructor
suites passed in 1.363s and 43.404s. Targeted discovery/attribution boundary tests
passed. Build and affected vet passed after the correction. SQL query code was
generated with the pinned repository command, without schema or migration changes.

The broader daemon selection also exposed an existing Windows Git-inventory path
comparison failure; it was reproduced on the unmodified base. Alongside the three
baseline HTTP failures and unavailable CGO race validation, it remains an explicit
check limitation. The updated runtime evidence records the actually started
Electron/daemon binary and native chain separately from these regression tests.

## Authorized third correction round: queued directive revisions

The real second-build chain queued a review directive at 08:29:00.913 UTC with
Work revision 14. A worker-result wake-up queued six milliseconds later and
advanced the Work to revision 15, leaving the earlier directive stale. The next
dispatch tick rejected that directive with a revision conflict before native
admission. This is a distinct, directly recorded queue-bookkeeping defect.

The operator explicitly granted one additional correction round with
`Eine zusätzliche Korrekturrunde genehmigen`. No previous work or review budget
was reset. The change is limited to the queue store and its focused regressions.
Within the existing transaction, exact-current pending/leased siblings can move
to the new revision only for consecutive, transport-only queue bookkeeping on
the same Work/project. Task, phase, SHA, policy, evidence, grants, gates,
cancellation, phase actions and budgets must remain unchanged. Snapshots,
native identity, leases, attempts, ordinary saves and admission fences are kept.

Independent full review found one P2: a valid `worker_result` outbox may retain
an older revision after an ordinary worker checkpoint, while its authority is
still fenced by phase, phase sequence and policy. Finding accepted; a positive
real-store regression failed before correction. The correction checks those
persisted worker-result authority fields against both prior and current Work;
operator directives remain revision-bound and siblings move only from exactly
`expected`. Negative scope and authority cases still roll back atomically.

Independent delta verdict: PASS, P2 closed, no new findings. Documentary final
confirmation by `/root/error_path_review` matched these SHA256 values and all
14 previously frozen source/test files:

| File | Final SHA256 |
| --- | --- |
| `backend/internal/storage/sqlite/store/instructor_queue_store.go` | `988d8589724f047df7fe9fafcb136b41e80c9f2e1a5a56879fc72c7fc157abe2` |
| `backend/internal/storage/sqlite/store/instructor_queue_revision_test.go` | `ed9a88c64586712af729994de54c8182ff7bf5489f5926d45b45ccb912f48a40` |

The complete 16-file snapshot is in
`instructor-local-debug-queue-source-hashes.json`. Focused queue regressions
passed, including 39 matrix/subcases. Complete final store and Instructor suites
passed in 25.330s and 37.259s; existing receipt/queue/stop/cohort/wake tests passed.
Final backend build and affected-package vet returned exit 0. No frontend source
changed after its successful typecheck and 45 relevant renderer tests.

The rebuilt real Electron/daemon completed a fresh native chain through the
independent Codex review and actual Orchestrator feedback to the Instructor's
regular triage/fix human gate. The deliberate live two-queue overlap missed its
timing; this specific queue interleave is covered by real-store regression,
not claimed as a successful live interleave. See the runtime evidence for
exact process, native identities, reports, and remaining policy boundaries.

## Final complete-suite regression and authorized fourth round

The final complete Windows backend run and a clean detached-base run used
Go 1.27.1 and `go test -count=1 -timeout=20m ./...`. Both exited 1 with the same
40 failed packages. Candidate 219 and base 201 top-level failures share 200
names; different counts do not imply all extra failures are regressions.
Seventeen extra Codex history tests use unchanged `/tmp/ws` fixtures; the base
package panics earlier. An unchanged system-install timeout test and one
baseline-only session-manager deadline test also have timing/substrate gaps.
Those differences are not certified harmless or resolved.

One extra candidate failure is a confirmed source regression:
`TestInstructorFailedCIEvidenceRequiresCurrentNativeFailure`. The targeted test
separately returned candidate RED/exit 1 (1.485s), clean base GREEN/exit 0
(1.410s). Candidate log SHA256:
`6310a1d20ebc74936d95967a2cd8790b105aa3bb5270efbe2c4b9d1a970ac251`;
base log SHA256:
`d1bdc5beb9561426e95bfefa402221f4832b1e4e1308cdd25364e92f053593b8`.

Independent reviewer `/root/error_path_review` confirms P2, not a fixture defect.
The new missing-session `continue` skips independently persisted PR, CI-failed
and traceability facts. `Assess` can verify the same CI failure without a native
session lookup; `Discover` now loses it. Automatic reaction to RED CI can be
missed, although no false-green verifier result is demonstrated. Earlier delta
reviews did not cover this side effect; their scoped PASS does not close this P2.

Proposed minimal correction: gate attribution and committed task-packet discovery
on confirmed native session facts, then retain independent same-SHA PR/CI
discovery. Preserve explicit session-read errors and unknown native configuration;
never fabricate a session or use requested settings as actual evidence. Add the
missing-session/current-failed-CI regression and retain negative attribution and
noncurrent/nonfailed-CI coverage.

At this finding snapshot the correction was outside the authorized queue-specific
third round. Overall source-fix readiness was RED and a separate one-round
exception was requested before any source edit. No budget or authority was reset.

The pinned golangci-lint v2.13.2 returned 0 issues under static Linux-target
analysis on Windows (GOOS=linux, GOARCH=amd64, CGO disabled). Native Windows lint
fails in unchanged Windows files and `syscall.Kill` typechecking. Formatting found
zero unformatted files. Static cross-target analysis is not Linux test execution
or race coverage, and does not override the open P2 or failed full suites.

The human subsequently answered
`Eine Korrekturrunde für CI-Discovery genehmigen`. Exactly the Discovery and its
focused test file changed in this fourth round. The new regression first
reproduced five failing missing-session/SCM cases; after the minimal source change,
13 focused tests and 69 subcases passed in 4.027s. Native configuration remains
unknown when no session exists; independently stored current-SHA PR/CI facts
remain discoverable. Claims stay unverified, and true session-read errors remain
errors. Current/old SHA and failed/pending/running/successful-check boundaries
are covered without inventing session or configuration evidence.

Independent implementation review: PASS, no new findings. Root finding
assessment: accepted, the P2 is closed. Independent final delta: PASS, all 16
hashes match `instructor-local-debug-final-source-hashes.json`; the other 14
source/test files remain unchanged from the third-round snapshot. No tests or
budget were reset during this documentary delta check.

| File | Final fourth-round SHA256 |
| --- | --- |
| `backend/internal/daemon/instructor_discovery.go` | `ecb9fb3e95bcdd662f87d66ff2576e20d16cfc281063019c9233e0806ca4ffba` |
| `backend/internal/daemon/instructor_discovery_native_test.go` | `0832f69f087c4939d8494b91aa74c95ac67a4e737e9017a19bae71fae8993564` |

The complete affected daemon suite returned exit 1 (27.488s), with exactly four
failures already present in the complete clean-base run: working-directory
stabilization, two Git-inventory cases and isolated native Instructor planning.
No candidate-only failure remains in that package; the failed-CI Discovery test
now passes. This is not a blanket daemon, backend or CI PASS.

Final backend `go build ./...`, `go vet ./internal/daemon` and isolated desktop
daemon build returned exit 0. The pinned v2.13.2 linter was rerun after these
two source changes under Linux-target analysis and returned 0 issues/exit 0.
Native Windows/full-vet, race, native OS execution, remote CI and GitHub-Codex
review limitations remain explicit. Actual restarted-build verification is
documented separately; no publication or merge occurred.

## Independent final real-evidence assessment

Reviewer `/root/error_path_review` independently evaluated the final native
queue proof without editing source, repeating tests or consuming another
correction round. Verdict: PASS in its bounded scope. Atomic read-only SQLite
snapshots 15/16/17 show Work and both sibling directives advancing 2→3→4 with
other authority fields, original payloads, snapshots and digests unchanged.
The own QueuedAt values differ by 7.9885ms, each own deadline remains +120s,
and the first timestamp is retained across the second queue transaction.

At the queue snapshots, ledger states are pending and turns are queued without
provider IDs/start times. Separate later records prove three genuine completed
provider turns and six accepted/progress CLI activities, each correlated with
the unique Instructor event in its actual command interval. The report Event DTO
does not have a dispatch foreign key; that stronger direct-FK claim is not made.
Normal Stopp addresses only the positively idle diagnostic Work, parked at
revision 18. The older real-chain Work retains its actual API core/gate facts
at revision 27; no unavailable pretest DB-body byte identity is asserted.

Final structured proof SHA256:
`385147a4f33530e902fb33765840090e7a2950854c2bf09697e7ce81d3d30bee`.
Raw atomic snapshot log SHA256:
`6c635768c2d921f982983f16251e8597cda7c11dcdbb5743fd930b8f494763cf`.
This proves the generic carry path using actual inspection directives; it does
not relabel it as a repeated worker-result/review race. All 16 reviewed final
source hashes and the real binary identity were also confirmed. No new finding,
grant, publication authority or budget reset follows from this assessment.

## GitHub-Codex review and authorized authentication round5

GitHub-Codex reviewed exact published HEAD 3f251d871c21f62727a59f74f1667fc9039b146b:
https://github.com/Binary-Refinery/agent-orchestrator/pull/23#discussion_r4269939235
Review 5516903800 is COMMENTED with one P2, not approval. The durable auth demand
survives the recovered history but the untyped preserved error does not carry
ErrChatAuthRequired. Independent real-store probe reproduces true demand with
idle activity; explicit ReauthRecovered correctly clears it. The human grants
exactly the additional authentication correction round recorded in the plan.

Initial source correction retains current account.ReauthRequiredAt under c.mu.
Independent review finds one P2 in the same explicit-recovery scope: a concrete
typed terminal auth error followed by explicit recovery still wins through the
old error-sentinel OR. Its targeted test is RED with false current demand but
waiting_input. Final correction removes that historical-error override; the
current account and pending interactions govern activity after all events.
A fresh typed auth failure still records the demand before the final guard.

Final five cases pass: durable auth/recovered replay; explicit recovery;
ordinary non-auth error containing sign-in prose; fresh typed auth without any
initial account flag; typed auth then explicit recovery. Original failed turn,
error, owner/provider identity, worker/sibling isolation, stale-generation CAS,
one history read and zero provider Sends are checked. Targeted related tests
pass; complete final chat suite passes in59.016s. Final backend build, chat vet,
pinned Linux-target static lint (0 issues) and real daemon build pass. Initial
checks/builds have their own historical hashes and are not final-source proof.

Independent delta reviewer error_path_review accepts the exact final two-file
delta, closes the scoped P2 and reports no further P1/P2. Controller SHA256
020489b91ffd32035b212a66a6bcb6b78c06eef465c230c33e3a96fc1e02beee;
Lifecycle test48cb8a3f0b61145f0ecc1bb115be35891c31a908e679cfd5d09f2ef0a414f457.
Other14 source/test files remain byte-equivalent to published3f251d8.
The round5 source manifest is retained separately; round4 final-source-hashes
continues to describe the historical fourth build. No budget is reset.

Remote checks on3f251d8 finish17success/1failure. The failed Ubuntu native
handoff restart hits shared.git/config lock, also reproduced by the actual base
f60 job. Relevant recovery/workspace/test/workflow blobs are identical. This is
a bounded preexisting-path assessment; the lock holder is unknown and the check
remains FAIL. All remote results concern3f and its identical merge tree, not the
unpublished round5 sources. The old GitHub thread remains open pending a newly
authorized publication and fresh GitHub-Codex review; local delta PASS does not
resolve or replace that review.

## Authorized Git-Recovery round6: local configuration read before write

The exact human authorization is
`Eine zusätzliche Git-Recovery-Korrekturrunde genehmigen`. The narrow change is
19 lines in `enableWorktreeConfig` plus 228 regression-test lines in the existing
workspace test file. The earlier 16 source files, including final round5 auth
replay fixes, are byte-unchanged. No lock removal, retry/wait, recovery-caller,
admission, model, policy, gate or schema change is included; no budget is reset.

A real Git repository begins with no local setting. Ordinary Workspace.Create
sets local `extensions.worktreeConfig=true`; the fixture then introduces a
concurrent-writer `.git/config.lock`. The unchanged Restore first fails with
exit 255 although the successful local bool read remains true. This is the
proper RED, not a fixture setup failure. After the minimal guard, Restore
succeeds without changing config or lock bytes, path or branch identity.

The guard uses `git config --local --bool --get extensions.worktreeConfig`.
Only exit 0/canonical true skips the shared-config write. Exit 0/canonical false
or genuine unset (exit 1 with byte-empty output) follows the original write and
error path. Unexpected output, read/start/parse errors and cancelled context
remain errors before any write. Global true cannot substitute for a missing
local setting. A locked first activation for unset/false still fails with 255;
configuration-first initialization remains outside this read/skip correction.

All 13 new control cases pass in four groups (22.183s):

| Group | Controls | Observed boundary |
| --- | --- | --- |
| Real already-enabled Restore | 1 | Create establishes local true; Restore succeeds with the fixture lock and preserves bytes/identity |
| Real local initialization | 5 | Unset, false, global-true/local-unset initialize locally; unset+lock and false+lock retain required-write failure 255 and unchanged bytes |
| Injected read errors | 5 | Unexpected successful output, read permission error, missing Git/start error, exit 1 with diagnostic output, cancelled read stop without write |
| Real read errors | 2 | Malformed bool returns Git 128; cancelled context remains context.Canceled; configuration is unchanged |

Four existing focused regressions pass in 6.861s:
`TestWorkspaceIntegrationCreateRestoreDestroy`,
`TestWorkspaceIntegrationEnablesPerWorktreeConfig`, `TestCommandArgs` (ten
subcases) and `TestGitWorktreeExitStatusOneHelper`. This includes real
Create/Restore/Destroy and per-worktree configuration isolation. Toolchain:
Go 1.27.1 windows/amd64, CGO disabled; Git 2.45.1.windows.1.

| Evidence under task-local `work/diagnosis/` | Result | SHA256 |
| --- | --- | --- |
| `round6-worktree-config-red.log` | Expected RED; old Restore fails 255 | `41e3b277f869374d36c06ddc84d76bfcc132409f422f995a6dff93b6eb809be1` |
| `round6-worktree-config-green.log` | PASS, 13 controls, 22.183s | `ac4f5956c03929706cfc7cd8bee378fac37a41bc0df4eeb6454a2a65c76f2243` |
| `round6-worktree-config-related-green.log` | PASS, four existing checks, 6.861s | `766c306c23f307e4c1fec9a0c1d16158e0fdf1a839b0513a910bf1e990f2d78a` |
| `round6-full-workspace.log` | FAIL, two assertions, exit 1, 220.525s | `758dd9e2b6da1639b9b8545cd61b1b42fd26f4913fcc02d9409b0b944efbe59f` |
| `round6-workspace-baseline-sourcecompare.log` | Same two FAIL on clean f60, exit 1, 7.135s | `eca2be258a4aefb4629b1c14ac8d05a43547c927213987dc87a57cb8dfeb9626` |

The full workspace suite remains FAIL. Its exact two failures reproduce on clean
detached base `f60f432a32b9f72370dafab2c58b703182dc8026`, with the same
assertions after removing only numeric per-test TempDir suffixes. The unchanged
integration-test blob at base and published
`3f251d871c21f62727a59f74f1667fc9039b146b` is
`9fb051d593b6f633dad4bd865da817d0f8d08ce6`:

- `TestWorkspaceIntegrationAutoRejectsUnmarkedRemotelessRepo` has the correct
  default-branch error and remote-HEAD remediation; its raw path assertion
  mismatches quoted/escaped Windows backslashes.
- `TestWorkspaceIntegrationWorkspaceProjectCopiesAssetsAndCleansSessionCopy`
  passes copied-content checks but expects `nested/context.txt` while Windows
  Readlink returns `nested\context.txt`.

This baseline assessment applies only to those two failures. It grants no full
workspace, backend-suite or CI PASS. Backend `go build ./...`, workspace vet
and the real isolated daemon build return exit 0. Pinned v2.13.2 Linux-target
static lint returns exit 0 and `0 issues.` on Windows with GOOS=linux,
GOARCH=amd64 and CGO disabled. Static cross-target analysis is not native Linux
test execution, race coverage, native Windows lint or remote CI.

Independent implementation review and final delta review: PASS, no remaining
P1/P2 in this scope. Reviewer `/root/error_path_review` verifies all 18 frozen
hashes, the unchanged earlier 16 files and the bounded baseline assessment;
no tests or budgets are reset by that delta check. The governance round6
manifest preserves every frozen task-local JSON fact, normalized to LF without
BOM, SHA256 `001b457fe91f2d6d67bb9ca3078e687beeefc757bdc5ef91e2cb6de1b1d4bb1a`.
The unchanged CRLF task-local original has SHA256
`7ed9768b2d451b9fc7fdb4dbfb0b4525471b3016dfda56cf4163b8a81b57b1a0`;
byte identity across those two line-ending forms is not asserted. Previous
manifests and round5 evidence remain historical and unchanged.

| File | Frozen round6 SHA256 |
| --- | --- |
| `backend/internal/adapters/workspace/gitworktree/workspace.go` | `b3827d98b20a281e5cbae1dbbf3e4b596af263380c6f99db5c547038ac2beebd` |
| `backend/internal/adapters/workspace/gitworktree/workspace_test.go` | `bfd5dc12f85d4b5347a8d489337f0c549d4c506dd47966d0a0a4a75a48b9c1de` |

The working-tree correction is unpublished on top of 3f251d8. Existing remote
checks and GitHub-Codex review concern that published HEAD and do not attest
round5/round6 source. Actual isolated app restart and genuine native recovery
are separately recorded in runtime evidence. No new publish, PR, merge, release
or human business-gate approval follows from these checks or reviews; a new
publication still requires the exact applicable `PUBLISH-GO [Branch]`.

## Authorized Lifecycle-Reconnect round7: current owned facts repair obsolete exited

The exact human exception is
`Eine zusätzliche Lifecycle-Reconnect-Korrekturrunde genehmigen`. Exactly five
source/test files change. The earlier 18 frozen files, including round5 auth and
round6 Git recovery fixes, remain byte-unchanged. Normal activity projection,
Start/Send/drain/admission, API/schema, model/policy, Work/queue/gates and budgets
retain their existing behavior.

The initial detached 3f probe is genuinely RED for completed_root and
running_root: durable exited persists while the actual reconnected controller
is ready or busy. The second probe uses the real lifecycle Activity recorder
and project orchestrator scope and reproduces both failures in 2.107s. The
integrated candidate regression reproduces them before the source fix in
1.744s. In each case the original native-turn identity and rows are retained,
History reads are zero, reconnect Sends are zero and ControllerReady runs once.
These are controlled-provider real-store tests, not a newly executed native
agent chain. Compile/setup failures retained separately are not regression REDs.

| RED evidence under task-local `work/diagnosis/` | SHA256 |
| --- | --- |
| `live-reconnect-exited-probe-red.log` | `bf10b5620f877ee85b566f3627e6ff03ad0079e414dae29f259d377386527031` |
| `live-reconnect-exited-real-activity-probe-red.log` | `34b9e2056de50e02587367f04557b81c08d271b087b5dce36267a33041b09d63` |
| `round7-live-reconnect-red.log` | `f7543bd67ecd68449bbc2645d467e65ca5b6b6b65b2bdf253f20545b12de352c` |

MarkChatReconnected remains NOOP for normal active/idle/blocked/waiting activity.
Only obsolete exited invokes the purpose-bound SQLite commit. Session revision,
harness/project/mode/nontermination/provider/controller generation, current
conversation and active/native fact branches are checked in one write
transaction. Retained own unrolled running turns keep their old generation;
current pending approval/input or durable ReauthRequiredAt takes precedence as
the existing Chat waiting_input state. Otherwise a current own running root is
active and consistent quiescence is idle. Missing/mismatched identities,
invisible pending debt, parse/read/write errors and cancellation remain errors.
Original GetSessionRow values go directly to the existing activity CAS; no
normalization or ordinary Activity-method refactor survives the final design.

The initial formal source freeze is SHA256
`2b37422292e98cb9a61211a53f1bccdc925e3b5c6369a02e1f0222f6243de5cc`.
Its 11 focused tests have 47 new and 9 existing leaf controls, 56 total:
five actual service reconnect cases; 38 Store cases across durable state,
owner/revision, normal activity, unknown/read/write/cancel and Raw-Column facts;
four new lifecycle capability/race/error cases plus nine existing reconnect,
ownership and cold-spawn controls. Focused package PASS times are Chat 2.235s,
Lifecycle 0.650s and Store 3.426s. Complete affected suites PASS at this initial
manifest: Chat 62.366s, Lifecycle 0.704s and Store 34.356s, combined exit 0.
Backend build and affected three-package vet also return exit 0 at that stand.
`round7-full-affected-packages.log.result.json`, `round7-backend-build.log.result.json`
and `round7-affected-vet.log.result.json` retain the execution outcomes; these
complete checks are not represented as rerun after the final test-only delta.

Initial Linux-target lint returns exit 1 for exactly one sqlclosecheck in the
new Raw-Column helper. Root accepts this necessary scoped cleanup in
`round7-lint-finding-assessment.json`: exactly one `defer rows.Close()` line,
with the explicit Close before the following MaxOpenConns(1) query retained.
Early scan/error/Fatal paths are now protected. This is within the same R7
exception; no production code or budget changes. The other 22 source/test files
remain byte-identical to the independently reviewed initial candidate.

Final source manifest SHA256 is
`cd647e0aa94c9dc14c9e2a29b14cf5d1ed102aa92f6eee9aad13e559837f1206`.
Only the necessary Raw-Column controls and pinned Linux-target lint are rechecked
after that one-line cleanup. Raw three cases PASS in 1.976s; lint returns exit 0
and 0 issues in 12.624s. Their `.log.result.json` records independently bind the
final manifest and verify all 23 source hashes before and after each run.
Valid zero/offset timestamps, NULLs and legacy checkpoints retain every
quote()-serialized session column except authorized activity_state/revision.
Current schema rejects blank checkpoints: the deliberately corrupt blank case
uses a CHECK-disabled external temp-fixture connection, while the real writer
must fail closed and preserve facts. It is not represented as a valid normal row.

| Final delta evidence | SHA256 |
| --- | --- |
| `round7-lint-cleanup-delta.diff` | `f9b36022fe3ace44742c05ecd59e2949a48db16fee9209e492b0a4ae71a2d788` |
| `round7-raw-column-final-delta-green.log` | `8c4e2ed22d47e006854dfceaad610876ad0592920b91e8597d0881b9142b5d9e` |
| `round7-linux-static-lint.log` | `e92606b0bf483111dff0a120c315ea165821348f31365020e2468a0059095c47` |

Independent source reviewer `/root/error_path_review`: PASS, no P1/P2 at the
initial formal freeze. Root accepts that source verdict in
`round7-root-review-assessment.json`, SHA256
`96236846799a410cdcdf72bbfa181b5edbd861f29a9c9e0e6815088c9432a94c`;
the lint finding is separately assessed and corrected. Independent final delta:
PASS, no P1/P2, all final 23 hashes verified, previous 18 unchanged and other 22
unchanged from initial review. This is recorded by
`round7-independent-delta-review.json`, SHA256
`66a2f2fa31b8249e7ae8b282caeb9e20ef34075b0762f1c5204445e09d51c391`.
The reviewer neither repeats tests nor edits source/runtime or resets budgets.

| File | Frozen final round7 SHA256 |
| --- | --- |
| `backend/internal/lifecycle/manager.go` | `678dbb4d3729bdf29534c41fdd0655b721d4c0df61a65a8603030233cb1b432a` |
| `backend/internal/lifecycle/chat_reconnect_test.go` | `c171bd01948a924a7a49b73b83eb5c2e674faed491a76a748bbf622c443f3965` |
| `backend/internal/storage/sqlite/store/session_store.go` | `de9b46ea349204625a9a014e6571e9e770d62c0670d3b0334348480ebd03a384` |
| `backend/internal/storage/sqlite/store/activity_revision_test.go` | `a76109436e6e5a261a89b6fa483835b97a9dcdf1629005f0f5150df9c0a8cbd8` |
| `backend/internal/service/chat/controller_test.go` | `58acd60f62d3e1f9ef3194dd67e35da870901d82148346caa0e16cf3fea2ceb5` |

The new governance round7 manifest is LF/noBOM and JSON-fact-identical to the
unchanged final frozen manifest. Its pre-review capture flag remains historical;
the later independent review/delta artifacts supply the final source verdict.
Every earlier source manifest and the initial R7 freeze remain historical.

At the source-review snapshot, the actual restarted R7 build and native-chain
proof were pending. The restart guard blocked on old unsent queues before any
Stop; read-only classification of their authority was in progress. That snapshot
claimed no R7 binary start. The later actual start, preservation and completed
native handoff are evidenced in the separate runtime document. Linux-target
analysis on Windows does not
attest native Linux execution, race coverage or remote CI. Existing remote CI
and GitHub-Codex results concern published 3f only; no new remote verdict or
blanket final full-suite rerun is claimed. No publication, merge, productive
replacement, Work gate Go or budget reset follows from this source review.
