# Instructor implementation acceptance

This matrix preserves the complete approved scope. **Implemented** means the
listed executable boundary exists in this branch; coverage records source and
regressions separately, including any pending case. This does not mean
final-head checks or native provider integration have passed.
**Partial** identifies a remaining predicate. **Pending** identifies incomplete
acceptance, including final verification. Final acceptance requires independent
reviewers and the repository's applicable checks to inspect the frozen publication
head. The live verification record is
linked below, so a later commit cannot inherit an earlier head's green result.

The baseline is [the consolidated Rulebook](artifaktory/instructor/RULEBOOK.md).
The work is opt-in; existing unassigned sessions are visible legacy work, not
retrospectively certified managed execution.

| ID | Acceptance behavior | Implementation and coverage |
| --- | --- | --- |
| I1 | Daemon-owned Instructor routes orders to project orchestrators, never direct worker execution | Implemented: [Daemon] delivers to a same-project orchestrator with a durable target; [Runtime tests] cover stable target/key and fail-closed delivery. |
| I2 | Status and another command remain responsive during long work | Implemented: [Planner] journals global native strategic/intake jobs outside command admission; [Planner tests] cover responsive Status during blocked inference, queue/idempotency, restart interruption and shutdown join. [Verify] independently schedules bounded inspections. |
| I3 | Allowed chain advances without another operator chat turn | Implemented: [Engine] advances eligible pinned-policy phases and enqueues the project orchestrator; [Core tests] cover genuine evidence, human grants, a bounded second fix and trusted terminal CI failure returning from closeout to triage/fix/delta. |
| I4 | Project orchestrator owns review, fixes, missing evidence and escalation | Implemented daemon routing and pinned role/evidence instructions in [Daemon]; [Launch], [Reviewer guard] and [Engine] enforce actual managed boundaries. Agent execution still requires a live supported project orchestrator and its reports. |
| W1 | Stable work ID survives replacement sessions, automation and restart | Implemented: [Storage] origin identity and session association, [Automation] work lookup; [Core tests] frozen work/command restart and repeated-origin checks, [Automation tests] association without a session. |
| W2 | Sessions/issues/PRs/branches/worktrees/evidence attach to work; unassociated resources visible | Implemented: [Core], [Inventory], [UI]; [Runtime tests] and [Inventory tests] cover unassigned facts and read-only Git discovery. SCM links are known attached-session facts, not a crawl of all remote issues. |
| W3 | GUI/CLI/manual/automation/delegation/resume/restore use managed policy checks | Implemented: [Launch] at actual manager admission/recovery, [Reviewer guard], [Chat], [Automation]; [Launch tests] and [Native launch tests] cover rejection before runtime creation, native mapping and restoration. Existing unassigned sessions remain explicit migration cases. |
| W4 | Periodic inventory covers projects, sessions, worktrees, branches, dirty work and linked SCM items | Implemented: [Inventory cache] captures bounded real registered Git sources immediately and every 60 seconds without an open GUI; [Inventory cache tests] cover background capture, restart, failure, responsive reads and fenced reconciliation. Disappeared observations remain visible with their last-seen time. |
| W5 | Discovered outside-AO execution is imported unverified | Implemented visible unassigned inventory and explicit attach/register entry points in [API]/[CLI]; importing grants no verified evidence. AO does not discover or govern arbitrary operating-system commands outside registered sources. |
| W6 | Closed audit retained; parked rows retain reason and start condition | Implemented: append-only [Storage] events/snapshots and [Core] pause/terminal predicates; [Core tests] stop cancels queued work and progress cannot release a pause. |
| E1 | Requested/queued/delivered/accepted/progress/completed/failed distinguished | Implemented work/outbox/readback states in [Core], [Storage], [UI]; [Core tests] receipt is not progress and receipt producer must be the actual bound target. E2 records the separate native Chat transport boundaries. |
| E2 | Durable delivery/recovery loses no accepted command and avoids duplicate effect | Implemented: [Transport] preserves governed native reservations and accepted keys; [Transport tests] cover TUI restart, replacement and uncertainty. [Chat transport] distinguishes durable queue from actual native receipt, rejects failed duplicates, and rechecks [Admission] before queued dispatch; [Chat transport tests] and [Admission tests] cover those boundaries. Uncertain effects require explicit reconciliation rather than blind replay. |
| E3 | Event and timeout updates distinguish heartbeat from progress | Implemented: [Heartbeat] persists an availability-only report separately from work revision, ProgressAt and NudgedAt; [Core tests] cover availability without progress/snapshot mutation, receipt and progress boundaries. |
| E4 | One bounded stall nudge followed by escalation; human waits preserved | Implemented: [Core] Tick tracks ProgressAt/NudgedAt and deadlines; [Core tests] human waits do not spend nudges and worker reports cannot release gates. |
| E5 | RED stop, two fix rounds and verification budgets enforced | Implemented blocked/stop predicates, default two fix rounds in [Core]/[Engine], and two deep verification passes per immutable source/day in [Verify]; [Core tests] reject third baseline fix and preserve publication gates under delivery uncertainty. Scoped human exceptions can extend fix budget up to ten. |
| S1 | GUI and Instructor share verified work facts and freshness | Implemented: [API] projects [Core]/[Verify] state, [UI hook] has no local work reducer; [UI tests] expose revoked/failed verification and unknown readback. |
| S2 | Light Status, change/manual/daily deep verification and cached unchanged evidence | Implemented: [Verify] claims a persistent source/day cache, runs at most two concurrent inspections, and [Discovery] hashes relevant durable native facts; [Runtime evidence tests] cover source changes and ignored observation churn. [Core tests] cover persistent heartbeat/restart/source/day budgets and late-attempt lease fencing; see the final-head verification record below. |
| S3 | Immutable version/timestamp/revision snapshots, corrections and deltas auditable | Implemented: [Storage] immutable snapshot insert, [Core] revision compare-and-swap, [Catalog] explicit deltas; [Core tests] restart/stale-scope/concurrent-retry tests and [Catalog tests]. |
| S4 | Complete accessible default Status; explicit delta, no twenty-row coverage limit | Implemented: [Core] lists the full work set, [Catalog] delta is separate, [UI] filters only its presentation; [UI tests] retain the displayed command snapshot across delta display. |
| S5 | Grouped human-readable table, plan stations, last/current/next, legend and details | Implemented: [UI] groups projects, shows policy phase stations, summary/next, state legend and details/event/evidence panels; [UI tests]. Plan stations are the effective workflow graph, not a separately editable free-form project plan. |
| S6 | Accurate Go/Weiter availability and honest blocked/parked/wait actions | Implemented: [Core] computes actions and [UI] renders those exact actions; [Core tests] reject invalid gates and [UI tests] use server-allowed actions only. |
| C1 | Typed extendable registry powers chat commands and forms | Implemented typed [Catalog] and [API]/[CLI]/[UI] consumers; [Catalog tests] describe actual effects and metadata. [Native planner] interprets strategic requests into bounded advisory proposals; [Planner tests] prohibit executing advice without existing typed admission. |
| C2 | Status/Tore/Weiter/Go/Stopp/Details/Prüfen/Neu and supported individual/batch scopes | Implemented [Core]/[Catalog] operations with [API]/[CLI] routes and [UI] controls; [Core tests], [CLI tests], [API tests]. |
| C3 | Ambiguous/unknown names return choices; Go without eligible gate rejected | Implemented [Catalog] frozen-scope name resolution and [Core] eligibility; [Catalog tests] and [Core tests] cover ambiguity/empty selectors, stable-origin retry and SQLite duplicate-name rejection. The daemon rejects duplicate explicit names and generates distinct short names for unnamed launches; [Core]/[Spawn admission] and the storage unique index enforce the naming boundary. |
| C4 | Snapshot/revision binding excludes newly added or changed gates | Implemented [Core] full selection validation before enqueue, transactional [Storage] batch; [Core tests] stale snapshot before any batch mutation. |
| C5 | Go all handles eligible continuation, exclusions, ordered execution and first-failure stop | Implemented [Core]/[Storage] frozen ordered batch and exclusions; [Core tests] failed second item cancels remaining deliveries, [UI tests] all-command uses frozen cohort. |
| C6 | Exact human PUBLISH-GO remains separate, fork-only target checks enforced | Implemented [Publication] literal branch approval and durable work/policy/SHA/branch/snapshot grant, [Traceability] approved repo/base proof; [Publication tests] distinguish local readiness from later remote proof and reject retrospectively approving an observed publication. [UI tests] require operator-entered exact phrase. AO gates managed admission; arbitrary external git/gh commands remain outside its control. |
| C7 | New document commands compose implemented operations; no implied new adapter | Implemented [Policy]/[Catalog] known-action validation and authority derivation; [Core tests] conflicting repository aliases cannot grant the wrong effect. |
| G1 | Existing guidance plus validated versioned structured repository contract | Implemented [Policy] strict JSON or single ao-policy fenced JSON, canonical structured hash, repo-relative containment and locked baseline; [Traceability tests] load the checked-in policy example. |
| G2 | Document-defined roles/workflows/checks/gates/evidence and agreed budgets | Implemented roles/transitions/profiles/commands/evidence, fixed argv check timeouts, fix/stall limits and human work exceptions in [Policy]/[Engine]/[Checks]/[Exceptions]; [Verify] enforces two deep passes per immutable source/day. Token/cost caps were not required by the approved Rulebook and are not supported or implied by this schema. |
| G3 | Defined precedence and locked constraints; invalid policy cannot weaken enforcement | Implemented [Daemon]/[Policy]: explicit repository file, conventional .ao/governance.json, then stored/global policy; invalid present file fails, never silently falls back. Work pins structured hash; [Core tests] launch policy/role checks and [Catalog tests] conflicting command checks. |
| G4 | Launch/delegation/resume/transition/approval/publication/closeout checked in daemon | Implemented [Launch], [Reviewer guard], [Chat], [Core], [Engine]; [Launch tests], [Core tests] and reviewer scope regressions. Corrective worker admission requires the current human fix grant before spawn, resume or queued execution. [Native Chat admission tests] cover ordinary and relayed empty-key messages, changed authority at queued drain, and unchanged unmanaged behavior; see the final-head verification record below. Legacy public session attachment validates persisted native kind and configuration without inventing managed launch provenance. Non-control parent delegation remains bound to its current work and authority, including explicit Work IDs and native orchestrator shortcuts; [Adoption tests] and [Parent guard tests] cover these boundaries. |
| G5 | Task packet, push/full summaries, SHA, review/triage/fixes/delta/CI bound to final revision | Implemented [Checks], [Discovery], [Evidence], [Traceability], exact SHA/policy/phase binding in [Core]; [Core tests] and [Runtime evidence tests]. A no-change fix still has an explicit verification/delta phase. |
| G6 | Trusted result verification rather than worker pass assertions | Implemented daemon profile execution and actual stored native review/check facts in [Checks]/[Evidence]; [Core tests] client verified flag rejected and failed native profile produces no accepted proof. |
| G7 | Independent review chain and required human triage gates enforced | Implemented [Reviewer guard] selection/scope/reuse checks, [Core]/[Engine] grant/evidence checks; [Runtime evidence tests] actual launch/run, negative triage, initial-vs-delta distinction; [Review tests] same-SHA delta cannot reuse old verdict. |
| G8 | Missing/stale checks, review or gate evidence blocks completion | Implemented [Core] canClose and [Verify] native-source fingerprint/inspection fencing and revocation; [Core tests] wrong phase/missing final proof/critical findings/unsupported worker launch block acceptance. Retained native evidence is rechecked even when discovery omits its old source; [Core tests] cover vanished accepted-source revocation. |
| G9 | Per-phase issue comments and board mirroring, exactly one configured board | Implemented verification of actual assignee, exact configured board, phase/status and phase-comment protocol in [Traceability]; [Traceability tests] missing/stale/order/current-checkpoint failures. Orchestrators/workers perform tracker updates; this verifier does not fabricate comments or board membership. |
| G10 | Assignee, human-linked author, worker/session/issue trailers and executed-by metadata | Implemented immutable Git author/trailer and actual native role/session checks in [Traceability]; [Traceability tests] reject body-only trailer text and launch impersonation, preserve real historical ancestors. |
| M1 | Per-role harness/provider/model/native effort with supported catalogue values | Implemented [Model catalog] OpenCode native variants and capability state, [Launch] exact-model approved maximum map, [Policy UI] native selection; [Model tests], [Launch tests], [Policy UI tests]. |
| M2 | Effort reaches start/automation/child/reviewer/resume/restore/restart | Implemented resolved/pinned manager config, [OpenCode adapter] native agent config and runtime plugin, typed [Review launcher] settings; [Native launch tests], [Review tests], [Plugin tests]. |
| M3 | Desired/passed/confirmed separate; unsupported confirmation remains unknown | Implemented [Daemon] launch provenance, [Chat] desired vs complete/partial native config report, [UI] separate columns; [Chat tests] partial report cannot confirm retained desired fields, real SQLite reviewer owner regression in [Reviewer config tests]. Provider config readback is not proof of inference reasoning. |
| M4 | Mandatory unsupported effort fails visibly, no silent lower fallback | Implemented: capability failures, missing/setter-clamped/unknown native ACP choices reject before inference in [Launch]/[ACP]/[Native selection]; [Native selection tests] and [ACP tests]. OpenCode plugin rejects alternate selectable primary agents, wrong model and unavailable/disabled variant; [Plugin tests]. Unsupported ACP read-only sandbox fails before launch/adoption in [Read-only tests]. Managed setter RPC uncertainty blocks the controller in [Chat tests]. |
| M5 | Native effort uses the provider's own vocabulary and stays distinct from execution limits | Implemented: per-model native choices in [Model catalog]/[Policy] are separate from profile/planning timeouts, fix rounds and verification limits. This branch does not introduce optional token/cost caps or equate an effort word with such a cap. |
| M6 | Configured independent Codex review/OpenCode worker profile and authorized exceptions | Implemented locked [Policy] baseline and [Exceptions] exact work/snapshot/proposal human approval, reason and at most 24-hour expiry; [Core tests] cover scoped persistence, native capability validation, policy rebinding and expiry at execution/queued continuation boundaries. A work with any valid publication grant refuses later policy rebinding; use a follow-up work. [API tests]/[CLI tests] preserve exact payload; see the final-head verification record below. |
| U1 | Existing automation forms/schedules preserved and runs linked to work | Implemented [Automation] actual origin-based WorkID plus governed business acceptance before completion, existing automation form retained; [Automation tests] association survives missing/deleted session, frontend automation regressions. |
| U2 | Global Instructor UI, filters/details/evidence/gates/commands/model configuration | Implemented [UI], [UI hook], [Policy UI] and [Planning UI], sidebar/route above projects; [UI tests]/[Policy UI tests]/[Planning UI tests]. Strategic history/proposals remain bound to the displayed frozen snapshot and require explicit command/intake admission. |
| U3 | Real runtime data, all states/resources/settings/build/policy versions visible | Implemented [API] capability/build/policy metadata, real [Inventory]/[Core] projections and [UI]; [API tests]/[UI tests] disabled/unknown/failure states remain honest. Renderer E2E tests use deterministic HTTP fixtures; they do not prove a live native provider. |
| R1 | Fork includes implementation/contracts/examples/install docs, no secrets/runtime state | Implemented source/schema/examples/docs in this branch; [Operator guide] gives exact global-policy export/load/import steps, including local-version removal and policy precedence. Live GUI settings require that explicit export; credentials/runtime state are per machine. Publication diff, secret scanning and build results are recorded against the PR head. [Install] remains the platform-specific build guide. |
| R2 | Opt-in capability and non-destructive migration preserve current daemon | Implemented AO_INSTRUCTOR default-off [Daemon]/[API], additive SQLite migrations and existing route behavior; [API tests] disabled mutations and [Reviewer config tests] feature-off reviewer choices. Full compatibility and migration results are recorded against the PR head. |
| R3 | Final-head independent review, resolved P0/P1, required local and remote checks recorded | Final acceptance gate: the linked PR records the exact reviewed head, resolved independent findings, applicable check results and remaining native integration limits. A draft or incomplete/failing head is not accepted. |

## Remaining scope and verification limits

All mandatory implementation predicates above now have executable paths and
source/regression coverage. Final-head independent review, full checks and
remote CI are separate acceptance gates; documentation alone cannot close
verification.
Optional token/cost caps are outside this change and are not enforced.

AO enforces its managed native launch, workflow and admission boundaries. It
cannot prevent an arbitrary shell outside AO from pushing a branch or editing a
repository. Existing unassigned sessions and discovered Git resources do not
acquire trustworthy provenance merely by appearing in the inventory.
Exact approval phrases use the existing trusted local operator API, not a new
cryptographic human-presence mechanism. A process sharing the operator's local
credentials remains inside that trust boundary; native strategic planning has
no AO API authority and produces advisory output only.

CI/review/traceability depend on configured native tools and real provider
integrations. Missing authentication, board/assignee configuration or required
native capability must remain a visible blocked state. No placeholder identity,
client green flag or invented provider confirmation counts as acceptance.
OpenCode ACP currently cannot enforce the requested read-only sandbox, so it
cannot run native strategic planning; that choice is refused before inference.
The native planner requires a provider with enforceable read-only execution and
an actual empty external-tool inventory, currently implemented by the Codex
app-server adapter.

## Final-head verification record

[PR #14](https://github.com/Binary-Refinery/agent-orchestrator/pull/14) is the
verification record for this change. Its description identifies the exact
reviewed commit, independent findings and local results; its Checks tab records
the remote results for the current head. Keep it a draft until all applicable
final-head checks pass and no unresolved P0/P1 finding remains. Re-run affected
checks and independent review after a correction; a prior head's result does not
certify the correction.

Required gates include build, full tests, race, vet, lint, frontend types and
unit tests, renderer smoke, API/sqlc drift, desktop packaging, supported-platform
checks and secret scanning. Record environment-limited local failures alongside
the corresponding clean-runner results rather than labelling them local passes.
Do not widen assertions, suppress tests or fabricate native tool confirmation to
obtain a green result.

Focused tests exercise native catalogue/OpenCode effort policy, ACP, typed
reviewer launch/scope, partial native readback, actual SQLite reviewer ownership,
legacy attachment admission and empty-key Chat admission. These are protocol and
runtime-boundary regression proofs. Authenticated live Codex/OpenCode inference
and the operator's currently running local daemon are separate deployment
validation; this workspace has not inspected that daemon or provided such a live
provider proof.

## End-to-end acceptance scenarios

1. Register manual work and an automation run; show durable work identity and
   real unassigned inventory. Replacement/deleted sessions preserve identity.
2. Configure the exact model-native effort variant; inspect actual adapter
   payloads and repeat after automation, reviewer launch and restoration.
3. Dispatch Weiter to the existing project orchestrator, acknowledge the exact
   native dispatch, then advance using genuine worker/reviewer evidence without
   another operator chat turn.
4. Present a snapshot-bound human gate; reject stale/wrong authorization,
   accept the exact authorization and resume without another permission round.
5. Restart during queued delivery; accepted intent survives. An uncertain
   native effect requires correlated evidence or explicit reconciliation and
   cannot be silently repeated on a new controller generation.
6. Reject closeout with missing full-profile proof, wrong SHA, open findings,
   expired exception or missing independent delta. Accept only final proof.
7. Fail the second item in Go-all; later items do not execute. Already completed
   effects remain auditable and publication still requires its own literal Go.
8. Change supported role/workflow/command policy without rebuilding AO; new
   work uses the new hash, existing work retains its pinned governance revision.
9. Keep Status and command admission responsive during native strategic planning,
   execution and evidence verification; keep monitoring without an open GUI.
10. Run every applicable repository check on the frozen final head; report native
    platform and unavailable live integration gaps honestly and verify in CI.

## Non-equivalent implementations

Prompt text alone is not gate enforcement. A queue with no orchestrator
acknowledgment is not orchestration. A registry disconnected from existing
launch paths is not universal coverage. A fixture-backed table is not a runtime
view. A requested setting is not native confirmation. A worker-written green
claim is not verified CI. A project chat waiting for human nudges is not a
persistent, available Instructor.

[Planner]: ../backend/internal/service/instructor/planner.go
[Operator guide]: instructor.md
[Planner tests]: ../backend/internal/service/instructor/planner_test.go
[Native planner]: ../backend/internal/daemon/instructor_planner.go
[Native planner tests]: ../backend/internal/daemon/instructor_planner_test.go
[Publication]: ../backend/internal/service/instructor/publication.go
[Publication tests]: ../backend/internal/service/instructor/publication_test.go
[Admission]: ../backend/internal/service/instructor/admission.go
[Admission tests]: ../backend/internal/service/instructor/admission_test.go
[Chat transport]: ../backend/internal/service/chat/semantic_delivery.go
[Chat transport tests]: ../backend/internal/service/chat/semantic_delivery_test.go
[Inventory cache]: ../backend/internal/daemon/instructor_inventory_cache.go
[Inventory cache tests]: ../backend/internal/daemon/instructor_inventory_cache_test.go
[Planning UI]: ../frontend/src/renderer/components/InstructorPlanning.tsx
[Planning UI tests]: ../frontend/src/renderer/components/InstructorPlanning.test.tsx
[Core]: ../backend/internal/service/instructor/service.go
[Core tests]: ../backend/internal/service/instructor/instructor_service_test.go
[Engine]: ../backend/internal/service/instructor/engine.go
[Heartbeat]: ../backend/internal/service/instructor/heartbeat.go
[Spawn admission]: ../backend/internal/service/instructor/launch.go
[Policy]: ../backend/internal/service/instructor/policy.go
[Exceptions]: ../backend/internal/service/instructor/exceptions.go
[Catalog]: ../backend/internal/service/instructor/catalog.go
[Catalog tests]: ../backend/internal/service/instructor/catalog_test.go
[Checks]: ../backend/internal/service/instructor/evidence.go
[Verify]: ../backend/internal/service/instructor/verification.go
[Storage]: ../backend/internal/storage/sqlite/store/instructor_store.go
[Daemon]: ../backend/internal/daemon/instructor.go
[Runtime tests]: ../backend/internal/daemon/instructor_test.go
[Discovery]: ../backend/internal/daemon/instructor_discovery.go
[Evidence]: ../backend/internal/daemon/instructor_evidence.go
[Runtime evidence tests]: ../backend/internal/daemon/instructor_evidence_test.go
[Inventory]: ../backend/internal/daemon/instructor_inventory_git.go
[Inventory tests]: ../backend/internal/daemon/instructor_inventory_git_test.go
[Traceability]: ../backend/internal/daemon/instructor_traceability.go
[Traceability tests]: ../backend/internal/daemon/instructor_traceability_test.go
[Launch]: ../backend/internal/session_manager/instructor.go
[Launch tests]: ../backend/internal/session_manager/instructor_test.go
[Native launch tests]: ../backend/internal/session_manager/provision_test.go
[Transport]: ../backend/internal/session_manager/semantic_delivery.go
[Transport tests]: ../backend/internal/session_manager/semantic_delivery_test.go
[Reviewer guard]: ../backend/internal/review/instructor.go
[Review launcher]: ../backend/internal/review/launcher.go
[Review tests]: ../backend/internal/review/run_scope_test.go
[Reviewer config tests]: ../backend/internal/daemon/instructor_chat_config_test.go
[Chat]: ../backend/internal/service/chat/service.go
[Chat tests]: ../backend/internal/service/chat/native_config_readback_test.go
[Native Chat admission tests]: ../backend/internal/service/chat/native_worker_admission_test.go
[Adoption tests]: ../backend/internal/service/instructor/legacy_admission_test.go
[Parent guard tests]: ../backend/internal/daemon/instructor_parent_admission_test.go
[Model catalog]: ../backend/internal/adapters/agent/modelcatalog/opencode_models.go
[Model tests]: ../backend/internal/adapters/agent/modelcatalog/opencode_models_test.go
[OpenCode adapter]: ../backend/internal/adapters/agent/opencode/opencode.go
[Plugin tests]: ../frontend/src/shared/opencode-effort-plugin.test.ts
[ACP]: ../backend/internal/adapters/chatdriver/acp/driver.go
[ACP tests]: ../backend/internal/adapters/chatdriver/acp/driver_test.go
[Native selection]: ../backend/internal/adapters/chatdriver/acp/conversation.go
[Native selection tests]: ../backend/internal/adapters/chatdriver/acp/native_selection_test.go
[Read-only tests]: ../backend/internal/adapters/chatdriver/acp/read_only_test.go
[Automation]: ../backend/internal/service/automation/service.go
[Automation tests]: ../backend/internal/service/automation/instructor_test.go
[API]: ../backend/internal/httpd/controllers/instructor.go
[API tests]: ../backend/internal/httpd/controllers/instructor_test.go
[CLI]: ../backend/internal/cli/instructor.go
[CLI tests]: ../backend/internal/cli/instructor_test.go
[UI]: ../frontend/src/renderer/components/InstructorView.tsx
[UI tests]: ../frontend/src/renderer/components/InstructorView.test.tsx
[UI hook]: ../frontend/src/renderer/hooks/useInstructor.ts
[Policy UI]: ../frontend/src/renderer/components/InstructorPolicyDialog.tsx
[Policy UI tests]: ../frontend/src/renderer/components/InstructorPolicyDialog.test.tsx
[Install]: artifaktory/10-bauen-und-installieren.md
