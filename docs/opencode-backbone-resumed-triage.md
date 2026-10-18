# OpenCode Backbone resumed separate triage

Date: 2026-10-18. Triager: independent `ao_resumed_triage` session, separate from
the implementation, both Full reviewers and the validation-command owner.

This is fresh triage of the recovered snapshot and newly completed Full reports.
It does not reconstruct a lost report or turn historical results into new runs.
The triager read `AGENTS.md`, the approved plan and Task, the resumption record,
both finalized Full reports, the preserved backend/auxiliary/quality records,
and the source and diagnostic evidence identified below. No tests, native
executables, providers or adversarial reproductions were run by this triager.
Only this report was authored; no source, workflow, commit or remote state changed.

## Reviewed binding

- Base: `6b1ba43f1df9415c1ec937876ce54c86aa37484e`.
- Full-review head: `dcd187616f8b92cd668e75184fd5d697ac40fc66`.
- Full-review tree: `166946128c6104a640edac5c301660067feb6ebc`.
- Full patch SHA-256: `8b8a9a50832853a083229c287ed477c2e10b8f426b65dcd26939889b0507e870`,
  bound to the restored checkout's `git diff --binary` command documented in the
  resumption and Full reports. Historical patch renderings are not substituted.

Both finalized reports address this same immutable source. Their bytes were
independently hashed before this triage was written:

| Finalized Full report | SHA-256 |
| --- | --- |
| `opencode-backbone-resumed-full-behavior.md` | `8b070644c714b18ff02a2ac90a5bdf46d2e74a5d575ed75cc4b97f7d8ac2d209` |
| `opencode-backbone-resumed-full-security.md` | `324b3aa2e03c35b9396db879fbd4fd77da4b61dc8d57403ad321656728c0b026` |

## Decisions before repair

**Two confirmed P2 findings remain open.** Bounded repairs below are within the
approved task. Neither finding is waived or represented as a native execution
bypass. Implementation may proceed; publication closure requires the subsequent
checks, independent Delta and separate closure entry.

| Canonical finding | Disposition | Basis and impact |
| --- | --- | --- |
| `REC-AO-SEC-001` | Confirmed P2; one finding shared by both Fulls | The Windows test-name selector omits nine verifier test groups, so a green job would not provide the planned platform-boundary coverage. |
| `REC-AO-BEH-002` | Confirmed P2; behavior finding independently traced | Governed project selection bypasses the existing explicit-effort requirement; registration can persist an invalid work policy and report success. Later admission still fails closed. |

The behavior review's early working identifier for the Windows issue was
`REC-AO-BEH-001`; its finalized report already uses `REC-AO-SEC-001`. This is a
duplicate observation, not a third issue. No other confirmed in-scope finding
appears in the finalized Full reports. Their rejected steering speculation does
not create a repair requirement for an extension absent from the pinned runtime.

## REC-AO-SEC-001: confirmation and authorized repair

I independently read the Windows job in `.github/workflows/go.yml` and every
top-level test declaration in `backend/internal/policy/backbone/{context,producer}_test.go`.
The existing command applies `-run 'Backbone'` to this package and seven others.
Only `TestActualBackboneProducerReceipt` of the ten verifier test names contains
that expression. All nine groups in `context_test.go` are therefore omitted;
none of the other Windows steps invokes this verifier package. The approved plan
specifically requires actual Windows path/boundary coverage. Linux evidence
cannot fill that platform obligation.

The only workflow repair needed is:

1. Add a separate Windows step running
   `go test -race -count=1 -timeout=10m -v ./internal/policy/backbone`.
2. Remove that package from the existing `-run 'Backbone'` command, retaining all
   seven remaining packages, race/uncached/timeout options and their order.
3. Preserve every other workflow step, trigger, permission, timeout and runner.

No verifier source change, test renaming, weakened gate or added skip is needed.
`TestVerifyRejectsSymlinksCaseAmbiguityAndOversize/symlink` already skips when
symlink creation is unavailable. Actual Windows logs must expose the result for
all ten top-level tests and the symlink/case/size subtests. A symlink skip remains
an explicit platform evidence gap even if the package succeeds.

## REC-AO-BEH-002: confirmation and authorized repair

I independently traced `resolveProjectSelections`, `ResolveProjectRole`,
`validateSelectionConstraint`, `Register` and `ValidatePolicy`. The governed
project-sourced branch correctly avoids ordinary native capability discovery,
but omits `ResolveProjectRole`'s required nonempty harness/model/effort check.
The shared constraint checker intentionally allows incomplete drafts. For a
`configured` policy effort, resolution therefore replaces the required field
with the missing project effort. `Register` validates before resolution and then
persists the result; it does not reject this resulting invalid policy.

I also inspected the independent validator's external overlay, result JSON and
raw log, rehashing the overlay source and log. On the unchanged Full snapshot,
the unbound control rejected registration with zero persisted works; the governed
case returned nil error and persisted one work with empty orchestrator effort.
Its resulting `ValidatePolicy` error was `role orchestrator needs explicit effort`.
The evidence is data-only and uses synthetic admission attestation, not native
OpenCode execution or real-model qualification.

| Diagnostic evidence inspected | SHA-256 |
| --- | --- |
| External `review-behavior/missing_effort_test.go` | `d6c9e6d715300d29675eaf5508bdcdaf1714f7a14e2a90d8bf8e9f57feb58338` |
| External `validation-restore/missing-effort-before/go-test.log` | `c8a41d3c3a4a7c544f50bb1ea1a15ac62686c80fc2360bd605cc5a9a084683d4` |

The diagnostic began on 2026-10-18 at 08:54:27.319131 UTC and exited 1. Its
78.860-second wall duration includes first dependency compilation; the test took
0.819 seconds. The validator's record reports all 2,043 backend source hashes
and dependency inputs unchanged before and after. This triager did not rerun it.

The bounded repair is to restore the existing explicit project-selection guard
in the governed project-sourced resolution path before accepting/pinning its
selection. Require nonempty harness, model **and effort**, using the same contract
as `ResolveProjectRole`, while preserving the existing binding/qualification
checks and deferral of native capability discovery. Keep the unbound resolution
path and incomplete-project-draft validation behavior unchanged.

This guard applies to every governed project-sourced role, including a fixed or
`maximum` policy effort. Merely calling `ValidatePolicy` after resolution is
insufficient: a fixed policy effort would remain nonempty while the missing
project selection would still be accepted contrary to the existing resolution
contract. No new native probe, fallback effort, automatic policy repair, broad
validation refactor, persistence change or adapter change is authorized by this
finding.

Add bounded data-only registration regressions that assert validation rejection
and zero persistence for missing project effort under both `configured` and fixed
policy effort, alongside the unchanged unbound control. Include successful
explicit governed selection/pinning and prove the governed path does not call
the ordinary native capability validator. Retain maximum-effort mapping and
contradictory-effort semantics; cover them if the implementation alters shared
constraint code. Tests must exercise the registration outcome, not merely mirror
the new condition.

## Evidence required before local repair closure

- Retain the failed-before diagnostic and run the committed registration
  regressions after repair. Bind results to exact source hashes and record
  assertions, failures and skips honestly.
- Run the **complete** affected Instructor package with race detection, uncached
  execution and the existing 20-minute bound:
  `go test -race -count=1 -timeout=20m ./internal/service/instructor` from `backend/`.
  Memory-bounded package parallelism is acceptable; no test filter or exclusion
  may replace this affected-suite result.
- Run the complete verifier package with the new Windows step's options on the
  available host, reporting that host explicitly. Require all ten test groups,
  inspect the symlink/case/size outcomes and report any skips. This Linux check
  validates selection locally; actual Windows remains pending.
- Verify Go formatting, the affected package's build/vet/lint requirements and
  `git diff --check`. Validate workflow syntax and independently compare the
  parsed workflow after reversing the two intended changes, so no unrelated
  workflow behavior is silently altered.
- Compare changed paths/source hashes with the Full snapshot. Keep the repair
  limited to the workflow, project-selection guard, meaningful registration
  tests and reports. Preserve dependency/API/schema/native-adapter bytes.
- An independent Delta reviewer must inspect the frozen repair and completed
  command evidence, confirm both findings fixed and assess direct regressions.
  Separate triage then appends closure to this report, preserving this entry.

The bounded product correction requires the complete affected package, not a
fictional new full-repository pass. Historical complete-suite evidence remains
bound to its actual October 16 source and date; actual PR CI must exercise the
published candidate and resolve or explicitly report remaining failures.

## Outstanding evidence and authority

The preserved October 16 complete backend race result is failed, with all 23
failures documented as reproduced on the unchanged baseline. Later lint/build/vet
and bounded adapter passes do not convert it to a pass. Recorded frontend/cloud
failures, renderer/browser/desktop blockers, existing skips and unavailable
native platform/Docker jobs remain distinct from successful checks. Lost raw
historical logs were not recovered or freshly rehashed by this triage.

Actual Windows execution and inspection, other required remote CI and visible
Codex review remain publication-follow-through obligations. The AO Windows job
does not provision `AO_BACKBONE_NATIVE_BINARY`, so even the repaired verifier step
cannot establish AO native Windows readback. The paired Backbone Linux/Windows
smoke is separate producer evidence. Operator model qualification, independently
verified provider snapshot identity and OS process confinement are not established.
No qualified seed is introduced, and no merge, release, deployment, settings
change or downstream activation is authorized by this triage.
