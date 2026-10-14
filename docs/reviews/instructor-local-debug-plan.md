# Local Instructor failure diagnosis and correction

Base: `f60f432a32b9f72370dafab2c58b703182dc8026` (`fork/main`).
Branch: `fix/instructor-local-debug-20261014`.
Executing role: local Codex implementation, with independent read-only review.

## Verified trigger

Work `work-42a3cc42-89db-45ee-b54e-850524b76879` received three native
OpenCode HTTP 400 failures on 2026-10-13. The messages reached the provider;
there were no assistant/tool outputs. Provider events retain the errors, but
the stored failed turns have empty `error_message`, and Instructor shows only
that native acceptance was not observed. The work is parked and its original
controller is terminated. Do not revive or alter that work.

The installed daemon is an unmodified build of this base. Its relevant bundled
frontend route matches the source. On 2026-10-14 the affected work configuration
loads in the real installed GUI and `GET /api/v1/instructor/projects/artifaktory.de/policy`
returns HTTP 200. The earlier `ROUTE_NOT_FOUND` is not reproducible in this build;
do not manufacture a routing change. Direct native OpenCode with the configured
model and `xhigh` answered successfully today; the rejected request parameter
from yesterday is unknown.

## Bounded change

Preserve the durable failed-turn error when native history replay reports only
an unknown recovered outcome. Keep a concrete replay error authoritative and
allow a known successful provider outcome to replace the earlier failure.
Include the existing bounded turn error in the uncertain semantic-delivery
diagnostic. Keep the uncertainty classification, exact receipt identity and
acceptance checks; do not relabel failure as accepted or permit automatic resend.
No schema, migration, workflow, credential, model/effort or publication change.

## Validation and review

Add a real SQLite/controller restart regression for failed live event followed
by recovered history. Cover explicit replay error and successful upgrade.
Exercise terminal/receipt interleaving, missing acceptance and stable-key retry
without duplicate execution. Run complete affected chat/Instructor/adapter
suites, relevant backend build/vet and frontend typecheck/tests.

Run the actual Electron frontend and built daemon in an isolated worktree with
dedicated data, run file, Electron profile and port. Exercise a disposable real
Instructor/project/worker/reviewer chain using installed authenticated providers;
record native IDs, acknowledgements, outputs and returned reports. Scripted
provider tests are regression evidence only, not the real execution proof.

Independent review -> finding assessment -> bounded corrections -> delta review.
Maximum two correction rounds. Preserve existing source and productive work.
Do not merge. Publication requires the Rulebook's exact human branch grant.

## Additional causes exposed by the real chain

The real OpenCode worker committed the bounded nonce fixture and the real Codex
reviewer returned an approved exact-SHA review to the Instructor. That execution
exposed two additional causes within the original local-debug scope:

- Reviewer provider events are fenced against the worker's controller generation,
  so the independent review conversation loses all output and its terminal event.
  Resolve the reviewer owner from the durable conversation. Fence live cleanup to
  its conversation, session, reviewer identity and controller generation; preserve
  unrelated worker/reviewer work. Record reviewer activity with a separate chat
  generation fence, including cold native-history import without prompt replay.
- Automatic attribution discovery omits the actual harness/model/effort fields
  required by its strict verifier. Read the attached worker's recorded native
  configuration; missing or unreadable session facts must not become attribution.

Add real-store generation/isolation tests and controller live/cold-history tests,
plus discovery-to-verifier regressions. Generate SQL query code with the pinned
repository command; no schema migration. Build and restart only the isolated lab,
then verify the actual native review history and real execution in the new build.
The protected triage/fix gate remains human-owned. These fixes constitute the
second bounded implementation/correction round; do not reset prior review budgets.

## Explicit additional-round authorization

The final native chain reproduced a distinct queue revision conflict: queuing a
worker wake-up advanced the Work revision six milliseconds after a review
directive was queued, leaving that first directive's revision stale. It then
entered delivery uncertainty before native admission. A directly correlated
cancelled native turn allowed one ordinary negative technical reconciliation,
without claiming acceptance or approving a business transition.

The human explicitly answered `Eine zusätzliche Korrekturrunde genehmigen` to
one additional round scoped to this proven queue conflict, regression tests,
independent review and real verification. This is a one-round policy exception;
the previous two rounds and consumed work budgets remain recorded. It grants
neither publication nor merge.

Change only the queue transaction and its focused regression: carry exact-current
pending/leased directives in the same Work/project across transport-only queue
bookkeeping, while keeping all task, phase, SHA, policy, evidence, producer,
grant, gate, cancellation, phase-action and budget authority unchanged. Preserve
immutable snapshots and native identity. Do not relax Tick/admission or ordinary
work saves, and do not change provider selection or queue deadlines. Prove both
the real-store two-queue interleave and its negative authority matrix, then
exercise genuine native queueing in the rebuilt isolated application.

## Explicit CI-discovery correction authorization

The complete final backend comparison and focused candidate/base test proved
that the missing-native-session guard also hides independently persisted failed
CI. Independent assessment confirmed a new P2 source regression. The human then
explicitly answered `Eine Korrekturrunde für CI-Discovery genehmigen` to exactly
one further round limited to the Discovery implementation and its regression
test, appropriate checks, independent review, delta review and verification in
a newly started local build. Publication and merge remain excluded.

Keep actual native session facts mandatory for attribution and committed task
packet discovery, but preserve independent current-SHA PR/CI/traceability facts
when a session is missing. Keep actual session-read errors explicit; requested
model/effort never substitutes for recorded configuration. Preserve prior rounds,
budgets, gates and immutable native execution evidence. Only these two source/test
files may change in this fourth correction round.

## Explicit GitHub-Codex authentication replay correction authorization

After publication of exact commit 3f251d871c21f62727a59f74f1667fc9039b146b,
GitHub-Codex completed its review with a P2: recovered/nil-error reviewer
history loses the auth sentinel and overwrites waiting_input despite a durable
reauthentication demand. Independent source assessment and isolated real-store
reproduction confirmed the failure; explicit recovery is the negative control.

The human explicitly answered `Eine zusätzliche Auth-Replay-Korrekturrunde genehmigen` to exactly one additional local correction round. Its scope is the
Controller and existing reviewer lifecycle regression test, appropriate checks,
independent review, assessment, delta review and the actually restarted isolated
build. Previous rounds and the two earlier exceptions remain consumed. New
publication, merge, release and productive replacement are excluded.

Use the current account projection after all history events under the existing
mutex. Preserve explicit ReauthRecovered, original error text and failed turn,
pending interactions, durable reviewer identity and generation isolation.
Do not infer authentication from prose, invent error types, change schema,
drain/send/retry, relax admission or alter policy/model/gates. An independent
review finding that an old typed auth error overrides later explicit recovery
is corrected within this same named scope, then independently delta-reviewed.
The separate preexisting git config-lock recovery failure is not authorized by
this authentication exception.

## Explicit Git-Recovery correction authorization (round6)

The isolated native recovery and the published/base CI comparison exposed a
redundant write to shared Git configuration while restoring an already enabled
repository. A competing `.git/config.lock` makes that write fail with exit 255,
even though `extensions.worktreeConfig` is already locally true. A real-Git
fixture first verifies that ordinary Workspace.Create enables that setting,
then reproduces the Restore failure without deleting or changing the lock.

The human explicitly answered
`Eine zusätzliche Git-Recovery-Korrekturrunde genehmigen` to exactly one
additional round. Only `enableWorktreeConfig` in the existing workspace adapter
and regressions in its existing test file change. Preserve all earlier rounds,
consumed budgets, final authentication fixes and the other 16 frozen source
files. This exception grants no publication, merge, release, policy change,
model substitution or productive replacement.

After the existing `.git` stat exception, read repository-local configuration
with Git's bool parser. Skip the redundant write only for successful canonical
true. Canonical false or actual unset (exit 1 and byte-empty output) retains the
existing required write. All other read, start, parse, output and cancellation
failures stop with an error. Preserve configuration/lock bytes and workspace
identity; add no lock deletion, retry, wait or recovery-caller change. The first
activation of unset/false configuration still requires a shared writer and can
legitimately fail under a competing lock; this is not a general initialization
race fix.

Bind checks, independent review and delta review to the separate 18-file
[instructor-local-debug-round6-source-hashes.json](instructor-local-debug-round6-source-hashes.json).
Earlier manifests remain historical. Record the two reproduced Windows baseline
failures as FAIL, alongside the focused passes and appropriate build/vet/static
lint results. Actual restarted-build/native-chain verification belongs in the
runtime evidence; source checks alone do not attest recovery or publication.

## Explicit Lifecycle-Reconnect correction authorization (round7)

The human explicitly answered
`Eine zusätzliche Lifecycle-Reconnect-Korrekturrunde genehmigen` to exactly one
additional round limited to the demonstrated LiveReconnect/lifecycle path and
its regression controls. An independent real-store probe on published 3f first
shows ready/exited and busy/exited despite verified attachment to the same live
provider. Repeating it with the actual lifecycle Activity recorder and project
orchestrator scope retains both failures; missing activity wiring is not the
cause. Controlled-provider tests do not attest new native execution.

The five-file scope is `backend/internal/lifecycle/manager.go`, its existing
`chat_reconnect_test.go`, `backend/internal/storage/sqlite/store/session_store.go`,
its existing `activity_revision_test.go`, and
`backend/internal/service/chat/controller_test.go`. Only obsolete exited
activity is repaired. Keep ordinary active/idle/blocked/waiting reconnects
unchanged. Preserve exact owner, provider, generation, session revision and
current native lineage; a retained running native turn can have its old turn
generation. Read current Running/Pending/Account facts in the same transaction
and reuse original SQL-row values without activity/checkpoint normalization.
Unknown, mismatched or unreadable facts fail closed.

Do not substitute MarkSpawned, relax Instructor readiness, expand admission,
drain/send/replay, or change policy/model/Work gates/budgets. The approved
correction includes checks, independent review, Root assessment and delta.
The final necessary lint cleanup is exactly one test-only defer line; keep the
initial full checks and hashes historical, then bind the targeted Raw-Column
and lint rechecks to the final 23-file
[instructor-local-debug-round7-source-hashes.json](instructor-local-debug-round7-source-hashes.json).
The earlier 18 source files and every previous manifest remain unchanged;
prior correction rounds and budgets remain consumed.

Source review and final delta are PASS. Runtime verification remains separate:
the planned restart guard stopped before any application Stop because old unsent
queues require read-only authority classification. No R7 binary has started at
this documentary capture. Resolve that boundary through the ordinary governed
path before Root's actual isolated restart and fresh native-chain verification.
Neither this additional round nor source/check/review PASS grants publication,
merge, release, productive replacement or a human business-gate decision.
