# Instructor operating board: approved implementation scope

Operator approval: 12 October 2026. Baseline: merged fork PRs #14 and #15,
`main` at `f24bfcf265d5f9df657e71145a842423b3cb7f19`. This is one work package
with independent implementation slices and one integrated review chain.
Repository issues are disabled (GitHub rejected issue creation with HTTP 410),
so the PR and this document provide its durable implementation reference.

## Scope and acceptance

| Original review point | Required result |
| --- | --- |
| 1. Wake-up | Managed Done reports enter a durable, correlated orchestrator outbox. Submission, native acceptance and observed turn start remain distinct. Recovery retries definite failures, fences uncertain effects and exposes unsuccessful delivery. |
| 3. Chain driver | Existing Instructor phases and the existing review engine own mechanical progression. Local review works before PR publication. Required checks, independent review, triage, bounded fixes and delta evidence retain their governance. |
| 4. Budgets | Configurable session, concurrent-session, delegation and turn limits have durable admission reservations. Restarts, retries and replacement sessions cannot reset lifetime limits. Existing two-fix-round admission remains. |
| 5. Validator/sync | Inventory and reports use the current Work/phase/policy/revision contract. Uncalled old execution engines are deliberately adapted or removed, with a coverage decision. No inactive flag is advertised as a running subsystem. |
| 6. Publication | Exact Work/SHA/branch/policy/snapshot authorization and once-only controlled execution. Expose unavailable isolation rather than claiming protection for same-user arbitrary shell processes. No fake capability environment flag. |
| 8. Gate surface | Add the approved responsive Instructor board alongside existing boards. Work cards group by project and project orchestrator; members load in details. State-appropriate single/batch click actions, general gates, exact publication authorization, immutable approval snapshot and live post-command refresh. |
| 9. Baseline/pilot | Actual launch-process flag/build/policy readback, reproducible pilot and non-destructive migration-lineage guard. Record current upstream divergence and a safe follow-up integration plan. |

The existing stall watchdog (point 2) and neutral/skipped CI handling (point 7)
remain in place and receive regression coverage, rather than a second
implementation.

## Approved GUI contract

- Stable works are the main unit. Session deletion/replacement must not change
  work identity or attach an unrelated reused session number.
- Current and historical worker/reviewer membership is explicit. Unknown
  provenance remains unknown; valid member actions reuse existing agent/review
  views. The existing sidebar agent list is not duplicated inside the board.
- Keep the existing Instructor/table and project/session boards. The new board
  has an independent route and navigation entry.
- Remain legible as the desktop window becomes narrower, wider, shorter or
  longer. Cards and controls reflow; modal content stays reachable with keyboard
  and scrolling. Do not hide essential actions behind hover.
- Provider, model and model-native effort can be chosen for Instructor,
  orchestrator, worker and reviewer at the appropriate policy scope. Remember
  native choices per model; never silently choose minimal effort. Preserve
  pinned work policy and require explicit scoped exceptions where necessary.
- Use genuine work facts, check evidence and native capability catalogues in the
  product. The design preview's example works/models are not production data.
- Live state can refresh after commands. A displayed approval snapshot stays
  frozen and is revalidated before the daemon applies the selected decisions.

## Explicit integration boundaries

The October 12 upstream head changes more than 2,300 files relative to this
fork. A full upstream merge is a separately validated integration, not an
unreviewed addition to the board change. Existing migrations 0165–0170 cannot
be rewritten; see the upstream integration plan and migration-lineage tests.

An arbitrary native worker sharing the host user can read the same host
credentials and invoke host Git/gh. UI text, request headers and prompt rules
do not create OS isolation. Protected publication must deny execution when its
required isolation/credential/authorization boundary is unavailable and show
the concrete reason. Normal non-publication gates remain usable.

The optional PyCharm MCP entry is in external per-machine agent configuration,
not this fork. Preserve ordinary PyCharm editor support. Provide precise,
explicit-path cleanup with backup, without starting an IDE or claiming a local
Windows configuration was inspected. See the recovery guide below.

## Review and validation

1. Run meaningful focused tests for each slice, then integrate the source and
   regenerate SQL/OpenAPI/TypeScript contracts.
2. Run every applicable full workflow check with the documented pinned
   runtimes. An unavailable native/platform check is a gap, not a local pass.
3. Independently review the integrated implementation and record confirmed
   findings with severity and concrete evidence.
4. Fix those findings; subsequent reviews cover the fixes and regressions caused
   by those changes. Do not restart the full review loop without a new reason.
5. Freeze the final source revision, publish to the fork, create the PR and verify
   the remote checks for that revision. Do not merge or deploy as validation.
