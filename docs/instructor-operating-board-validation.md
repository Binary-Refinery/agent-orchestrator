# Instructor operating board: validation and remaining boundaries

Validation date: 12 October 2026. Fork baseline:
`f24bfcf265d5f9df657e71145a842423b3cb7f19`. Final implementation source:
`93ebde1591d1b17e7572cd27360c39fda81effdd`.
Documentation-only commits after that revision do not change the tested source.
This record distinguishes observed results from unavailable native validation;
it does not certify all seven original planning points as fully closed.

## Resulting behavior

The additive Instructor board groups stable works by project and coordinator.
Current/historical members load in work details; opening a member requires a
current, verified target. Existing boards remain available. Cards, dialogs and
controls reflow in narrow/wide and short/tall windows. Single/cohort decisions
capture one immutable snapshot and ordered work selection, then refresh live
state after commands. A late preview cannot overwrite a newer selection or
retain a budget exception against changed usage.

Provider/model settings preserve the selected model's actual native effort,
indexed by provider and qualified model identity. Managed session, concurrent
session, turn and delegation budgets have durable reservations and exclusive
native-effect fences. Restart or reusing a display session ID does not reset
lifetime accounting. Positive termination releases concurrent capacity;
uncertain effects retain their reservations.

Managed Done reports atomically create the correlated Instructor handoff.
Submission, acceptance and observed native start remain distinct. Definite
non-submission has bounded retry; unknown execution requires visible
reconciliation. The existing review engine drives asynchronous push/full
profiles, independent review, triage, bounded fixes and delta evidence. The
unused legacy sync/reaction engines were removed with a coverage decision.
Pause attempts every eligible current producer and preserves uncertainty after
partial native effects. Only certified pre-effect supersession cancels an older
Stop without creating a failure gate over the newer operator intent.

## Independent review

Implementation authors and four independent Codex reviewers were separate.
After the initial review, subsequent passes covered the fixes and regressions
introduced by those fixes, rather than repeating the whole review.

| Review area | Confirmed corrections and final delta evidence |
| --- | --- |
| Work identity/membership | Report and owner incarnation races, reused-ID role attribution and inventory error handling fixed. Delta at `3aae1144` used real SQLite/session replacement and normal/race tests. |
| Board/publication admission | Ordered batch decisions, immutable governed repository association, expired exceptions, settings navigation, visible wait conditions and retained scalar API/CLI compatibility fixed. Final narrow delta at `78f041c5`; real renderer, HTTP/SQLite and CLI probes passed. |
| Budgets/native model settings | Admission before replacement effects, restored/adopted session accounting, no refund for an adopted native instance, pre-handoff fences, native effort defaults, config round-trip and snapshot invalidation fixed. Final deltas at `aadf046c`, including concurrent real-store admission and manager effect ordering, passed. |
| Runtime chain | Action/directive revision races, cancelled wake-up obligations, best-effort multi-producer Stop, asynchronous check completion, certified no-send and final pre-effect Stop supersession fixed. Final boundary delta at `93ebde15` checks the original daemon/SQLite race and negative partial-effect cases. |

The final deltas report no unresolved P0, P1 or P2 findings in those reviewed
changes. This finding status does not remove the runtime limitations below.

## Local checks

Toolchain: Go 1.27.1, Node 24.19.0, npm 11.9.0, Python 3.12.14 and pinned
golangci-lint 2.13.2. Browser checks used Playwright 1.60's real Chromium
headless shell. Complete suites were executed without exclusions; environment
failures were retained as failures.

| Scope and source | Command | Observed result |
| --- | --- | --- |
| Backend, final `93ebde15` | `go build ./...`; `go vet ./...`; `golangci-lint run --path-mode=abs --timeout=10m` | All passed on the frozen final source. |
| Full backend, frozen `3aae1144` | `go test -json -count=1 ./...` | 11,287 passing test events, 26 failing events, 94 skipped; exit 1. One missing migration-ledger mapping was corrected in the integration branch. Remaining failures concern native facilities below. |
| Full backend race, frozen `3aae1144` | `go test -json -race -count=1 -timeout=20m -overlay=<test-ledger-overlay> ./...` | 11,288 passing test events, 25 failing events, 94 skipped; zero race-detector warnings. Overlay changes only the two test ledger mappings for migrations 195/197, subsequently committed to the integration source. |
| Integrated affected backend, frozen `96d882d0` | `go test -count=1 <affected packages>` and `go test -race -count=1 -timeout=20m <affected packages>` | Both exit 1 solely for `TestWiringLifecycleOwnsHerdrSocket`: AF_UNIX `operation not permitted`. All other selected packages passed; no race warning. |
| Last boundary change, final `93ebde15` | `go test -count=1 ./internal/daemon ./internal/service/instructor` and corresponding `-race -count=1 -timeout=20m` run | Both exit 1 solely for the same Herdr AF_UNIX EPERM test. Instructor passed normally and with race detection; independent boundary reproduction and negative cases passed. |
| Frontend, final frontend source | `npm run typecheck`; `npm run typecheck:e2e` | Both passed. No frontend source changed in the final backend boundary fix. |
| Full frontend Vitest | `npx vitest run --maxWorkers=2` | 351/353 files passed; 5,523 tests passed, 5 failed, 6 skipped. Four failures explicitly report AF_UNIX EPERM in supervisor-link; the native agent-browser process exited during startup without diagnostic output. That fifth cause is not established. |
| Final affected frontend | `npx vitest run <six Instructor/settings test files> --maxWorkers=1` | 121/121 tests passed across board, retained view, exception dialog, project settings, global settings and policy dialog. |
| Complete renderer suite | `npm run test:e2e:renderer -- --workers=1` | 69/69 passed, including responsive board/action contracts. This is browser-renderer validation, not a real Electron desktop run. |
| Native CLI E2E, final `93ebde15` | `go test -tags e2e -v ./internal/cli/...` | Passed again after the final runtime boundary fix. |
| Cloud static checks, final `93ebde15` | `go build ./...`; `go vet ./...`; `gofmt -l .` | Passed with no format drift. |
| Cloud full race, final `93ebde15` | `go test -race -count=1 ./...` | Exit 1 for `TestCheckpointBridgeRunsOnPoke` (Unix EPERM) and `TestCheckpointBridgeSafetyNetFires` (no checkpoint). All other packages passed; no race warning. |
| Generated contracts, final `93ebde15` | `npm run sqlc`; `npm run api`; generated-file diff | Both passed with no generated-file drift. |
| Optional IDE recovery | `python -m unittest discover -s scripts -p test_remove_optional_ide_mcp.py -v` | 7/7 tests passed. Windows execution still requires its CI runner. |
| Recovery workflow, final `93ebde15` | `actionlint .github/workflows/instructor-recovery.yml` | Passed for the newly added workflow. |
| Shared packages, final `93ebde15` | `npm run shared:check` | Generated schema drift, typechecks, 22 cloud-client tests, 133 product-ui tests and both builds passed. |
| Shared package packaging, final `93ebde15` | `npm pack --dry-run` in `packages/cloud-client` and `packages/product-ui` | Both passed; no publishing performed. |
| Documentation frontend | `npm run build --prefix src/docs` | Passed. |
| Native installer helper contracts | `node --test scripts/build-update-helper.node-test.mjs scripts/build-mac-repair-installer.node-test.mjs` | Passed. Native Swift/lipo and OS packaging remain separate checks. |
| Linux desktop package | `npm run build` | Blocked first by unmapped archive ownership; a precise extraction retry reached missing yacc/bison. No package success is claimed. |

The affected-backend package set above is:
`./internal/daemon ./internal/service/instructor ./internal/service/chat
./internal/review ./internal/service/review ./internal/service/report
./internal/session_manager ./internal/storage/sqlite/store ./internal/domain
./internal/httpd/... ./internal/cli ./internal/gitread`.

The full race failures comprise 14 direct Unix-socket failures, their one failed
parent, and 10 process/PID/PTY assertions. The ten process assertions were also
reproduced unchanged on baseline `f24bfcf2` with
`go test -race -count=1 ./internal/adapters/runtime/conpty
./internal/previewserver ./internal/processalive`. Host procfs PID values and
namespace process IDs differ in this environment. Neither those results nor
AF_UNIX EPERM justify suppressing tests or claiming a local full-suite pass.

Remote CI results must be recorded on the PR for its exact published revision. Native
macOS/Windows, Docker-dependent workflows and real Electron desktop validation
cannot be labelled locally passed in this environment.

## Remaining operational boundaries

- **Protected publication is unavailable on the current host runtime.**
  `hostPublicationBoundary` reports unavailable isolation, credential separation
  and protected operator authorization. Grant admission/execution and ordinary
  governed merge paths fail closed; the board displays the reason. There is no
  production isolated Publisher adapter and no OS prohibition on same-user raw
  `git`/`gh`. Therefore this does not fully implement the original publication
  enforcement point. See [publication boundary](instructor-publication-boundary.md).
- **Native wake-up proof depends on the provider.** Supported Chat protocols
  provide correlated execution evidence. The TUI contract proves prompt
  submission only; an unconfirmed native start escalates visibly. The installed
  daemon/provider pilot is still required, using actual launch-process flag,
  build and policy readback. See [pilot](instructor-pilot.md).
- **Budgets govern AO-managed registered execution.** Provider-internal agents,
  undeclared direct provider calls and raw terminal keystrokes are outside the
  measured ledger. The unauthenticated primary loopback listener is preserved.
  See [budgets](instructor-budgets.md).
- **Upstream integration is a separate migration/rebase.** The lineage guard
  rejects conflicting upstream schemas without mutation; it does not convert
  them. Existing fork/upstream migration collisions 165–170 and divergent model
  defaults are documented, not silently rebased. See
  [upstream plan](instructor-upstream-plan.md).
- **The operator's external PyCharm configuration was not inspected or edited.**
  The explicit-path, exact-server cleanup helper previews by default, creates
  an exact backup on apply, and preserves unrelated settings. Its source change
  cannot remove an entry from another machine. The Git ownership recovery is
  implemented for registered reads: one bounded retry with the exact canonical
  repository's per-command `safe.directory`, without global/wildcard trust.
  See [local recovery](instructor-local-recovery.md).

No merge, deployment or production publication was performed as validation.
