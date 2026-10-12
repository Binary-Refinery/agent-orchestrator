# Instructor board: visual parity and operator actions

Operator request: 12 October 2026. Base: merged PR #16, fork `main` at
`291dbbb97c9604c3eeb6870e637ead63a8ec34af`.
This is one follow-up work package. The PR is its durable reference while
repository issues are disabled.

## Required behavior

| Request | Acceptance |
| --- | --- |
| Remove the old Instructor page | One visible Board navigation entry. Old `/instructor` links redirect to the Board and retain the selected work. Shared intake, evidence and reconciliation dialogs remain usable. No work/session/repository data is deleted. |
| Match the approved design | Use the earlier visual prototype's palette, font family/weights/sizes, three summary cards, project initials/name/work count, coordinator configuration/change control, state chips, relative progress times and segmented workflow indicator. Use actual daemon facts rather than the prototype's example content. |
| Keep the layout usable | Reflow at narrow/wide and short/tall desktop sizes, retain reachable controls and keyboard focus, and provide a corresponding legible dark palette. English and existing locales use the same visual hierarchy. |
| Cancel work | A distinct, snapshot-bound cancellation action with explicit UI confirmation, durable admission and history. Stop remains pause. Uncertain native effects stay visible; cancellation does not pretend that an unconfirmed runtime has stopped. No branches, worktrees or sessions are deleted. |
| Create/start projects | Show registered projects even with zero works. Reuse existing project registration/clone UI and API. Start/open/resume a project orchestrator through the existing governed action, using its saved provider/model/native effort. |
| Edit coordinator settings | The project header exposes the existing project settings editor. Preserve unrelated configuration and remembered native effort per exact model. Configured future launch settings and observed running settings remain distinct. |
| Configure an orchestrator at creation | Project registration and settings include provider/model/native effort, inline orchestrator rules, an optional repo-relative `orchestratorRulesFile`, and an optional initial `orchestratorPrompt`. Save settings before the first start. Load the rules file afresh on spawn/restore, reject missing/unreadable/escaping files clearly, and keep the one-time start task separate from standing rules. An existing session is not silently given the start task again. |
| Take in GitHub issues | Browse actual open issues for the registered project's repository through the daemon. Explicit selection creates a stable Instructor work with issue provenance and current policy. Repeated intake returns the existing work. A changed/mismatched repository is rejected. Manual work without an issue remains supported. |
| Communicate with Instructor | A clear question/answer and follow-up interface over existing durable native planning jobs. Explain which answer is advice and which proposal requires explicit admission. Technical request/receipt metadata is secondary. |
| Use the Board by API | Document project registration/configuration, orchestrator start, work snapshots/details/members, manual and issue intake, commands/cancellation, native strategic questions and asynchronous result polling. The UI uses those same daemon contracts. |

The state palette preserves distinct meanings: running, ready, needs a decision,
blocked, paused/cancelling, completed and cancelled. A workflow position is not
substituted for verified check/review evidence. Missing native model or effort
confirmation remains unknown.

## Boundaries

- Keep the primary daemon listener on unauthenticated `127.0.0.1`.
  This change adds no network listener or invented operator-authentication claim.
- Keep the underlying inventory and governance APIs. Removing a diagnostic page
  does not remove project discovery, runtime correlation or audit history.
- Project/orchestrator operations reuse the existing project/session services.
  A UI click or HTTP admission is not proof of native completion.
- GitHub issue content is task data. It cannot relax review policy, manufacture
  approvals or authorize publication. Issue intake does not start a worker,
  close an issue, publish or modify remote repository state.
- Protected publication remains unavailable without the isolated Publisher
  boundary documented in PR #16. This slice does not weaken that refusal.
- The large upstream rebase and the installed-machine pilot remain separate.

## Delivery evidence

Run focused behavioral tests before integration, regenerate the code-first API,
then execute applicable full workflow checks without excluding failures.
Compare screenshots from the actual renderer with the approved prototype;
prototype screenshots alone are not implementation proof.

Implementation and independent review have separate owners. Fix confirmed
findings, then review those fixes and regressions caused by them. Do not restart
the whole review loop. Record native-environment gaps and verify the published
commit's CI results before handing over the PR. Do not merge or deploy as a test.
