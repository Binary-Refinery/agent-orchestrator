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

## Separate repair closure — 2026-10-18

Triager: `ao_resumed_triage`, again separate from the repair author, validation
owner and Delta reviewer. This entry is appended after reading the final check
and Delta reports and the updated Task/publication metadata. The complete
pre-repair text above is preserved byte-for-byte; its SHA-256 is
`d1e4779623993fa2180a9e8c105f21e1846f44de8366abd79101a9ffcec95b5d`.

**Both confirmed findings are closed at their repaired code/selection boundaries.
The locally checked change may proceed to PR publication and visible Codex review.**
Actual Windows execution, required remote CI and the resulting review still need
inspection; they are not certified by this local closure. Root remains the sole
publisher. No merge or activation authority is added.

### Final source and evidence binding

The independently tested repair is local commit
`f478fc100e854634f369655d067c752ea639819c`, tree
`57dd202ec151a674f39237148bcb628b1b43db2e`. Root recorded its exact remote-tree
equivalent as `03ea7ac6e80e84e675bdc2cc576a3dbf063436db`.
The final evidence checkpoint reviewed by Delta is
`53f23301347c52b61bc676942af6b06589414024`, tree
`cca674a371f65a543809941e1ab1a973d8463424`, with root-recorded remote equivalent
`43792ec05c71a0bf99c695b8f0622cca7bef2522`. This triager verified the local
HEAD/tree and source equivalence; root owns the remote verification.

| Final record independently read and hashed | SHA-256 |
| --- | --- |
| Author repair | `6fdf4d49c30e91145577147149a16722f7d2f59b74b0e84879e415dbee031940` |
| Independent repair checks | `77ff1cee2e8fa5b5b4e21ee2c4ce1529b486353604387108a6f65a91e01dc55a` |
| Independent Delta security review | `6f3fe417c0be2af27310e8b08ab5117373b59a84c155dfa3347b4cc901ee26a8` |
| Updated Task before closure | `15081b548c23bfa2c1599fc3cec073977a61495b2c528ec7a288fb213491796f` |
| Updated publication state before closure | `a05f90704cfc8c7e4ccf3840f9d3b20f663d8929740fedc98fbd7e273e951577` |

I independently inspected the three-path product/test/workflow repair and
rechecked its file hashes against the quality report. Comparing the current
checkout with the tested repair found no differences outside `docs/`; only
report/acceptance metadata has advanced. The final Delta independently confirms
the same scope and no new direct-regression finding. No test, product edit,
commit or remote write was performed by this triager.

### Closure decisions

| Finding | Final disposition | Verified closure basis |
| --- | --- | --- |
| `REC-AO-BEH-002` | Repaired and independently verified; closed | The three-line governed guard restores explicit harness/model/effort before accepting selection, including fixed/maximum effort. Unbound/draft/constraint/native paths are unchanged. All six missing-effort registration negatives and three valid governed positives pass with the required persistence and zero-native-validator assertions. The original unchanged external diagnostic also passes after repair. |
| `REC-AO-SEC-001` | Selector repaired and independently verified; closed | The separate unfiltered verbose Windows verifier step selects the complete package; only that package moved out of the prior filtered command. Complete parsed-workflow comparison preserves all other behavior. The independent local command executes all ten top-level verifier tests with explicit symlink/case/size passes. Actual Windows execution remains separately pending. |

The independent checker ran the complete Instructor race package on the frozen
repair: 179 top-level tests and 447 total pass events, zero failures/skips/race
warnings, 200.013 seconds wall. The complete verifier race package passed ten
top-level tests and 42 total pass events, zero failures/skips/race warnings,
2.558 seconds wall. The unchanged external registration overlay passed in
4.354 seconds wall. I inspected the relevant raw log outcomes and rechecked
their hashes against the finalized quality/Delta reports; these are the
checker's executions, not fresh executions by this triager.

Full backend build and vet both exited zero. Full golangci-lint 2.13.2 exited
zero with `0 issues.`. Their current raw log hashes and exact command/source/exit
records were independently checked. Formatting, actionlint, diff checks and the
parsed-workflow/source comparison also satisfy the pre-repair requirements.
The final Delta independently rechecked these results and both repairs.

### Acceptance and required follow-through

The updated Task accurately records AC-1 through AC-5 as checked implementation
outcomes. AC-6 remains pending actual Windows boundary execution; AC-7 remains
pending complete remote CI and platform accounting; AC-8 remains pending PR
publication, exact head/tree/category verification and visible Codex outcome.
The Task stays active with a null head revision at this pre-publication stage.
These states permit publication to obtain the outstanding evidence; they do not
claim completed remote gates. Any new concrete remote finding requires its own
triage and bounded correction before being marked resolved.

Root must inspect actual Windows results for all ten verifier tests and the
symlink/case/size subtests. Any symlink capability skip must remain an explicit
gap. No AO native Windows readback binary is provisioned by this workflow.
The paired Backbone smoke, historical Linux native readback, operator model
qualification, provider snapshot verification and OS confinement remain distinct.
No real-model qualification or OS confinement is established here.

The historical complete local backend race failure and recorded frontend/cloud/
browser/desktop gaps retain their original dates and source attribution. Fresh
complete affected-package and full build/vet/lint passes do not erase that
history or constitute a new full-repository race pass. Actual head-bound CI and
Codex results belong in the PR conversation. This closure authorizes neither
merge, release, deployment, settings changes nor downstream activation.
