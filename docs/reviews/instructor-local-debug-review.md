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
