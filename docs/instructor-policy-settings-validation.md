# Instructor policy settings regression

Base: fork main `291dbbb97c9604c3eeb6870e637ead63a8ec34af`, after merged PR #16.
Executed by: external Codex, with independent reviewer agents.
Work reference: the follow-up PR; repository issues are disabled.

## Problem and scope

The installed `0.13.0-artifaktory.3` build was reported to show the generic
error page with `Cannot read properties of null (reading 'join')` when opening
Policy settings from the Instructor board. The effective policy had null
commands in both `full` and `push`. The current fork reproduces the same unsafe
render call, although its argv editor now separates arguments with newlines.

Only the profile editor changes: null commands display as empty fields, and an
exactly empty edit produces `[]` rather than an invalid `[""]` executable.
Untouched policy data is not normalized. Nonempty edits preserve argv order,
spaces and nonleading empty arguments. Daemon policy hashes, persistence,
execution admission, native model effort validation and generated API contracts
are unchanged. The pre-existing generated nonnullable slice type does not
describe every legacy policy wire value; the editor handles the actual null
value defensively without changing the shared schema generator in this repair.

## Regression proof

Before the implementation change, the component regression suite produced
five failures and three passes: null profiles failed during render, and clearing
a configured command submitted `[""]`. The repaired cases cover both null
commands, empty arrays, mixed configured/unconfigured profiles, unchanged save
and response rendering, clearing, and entering argv with spaces/empty arguments.

The added Chromium renderer case uses the real Instructor-board route, button,
dialog and typed HTTP client with fixture responses. It checks policy version,
900-second timeout, both empty fields, close without PUT, reopen and unchanged
null-command PUT/response. It is renderer evidence, not proof from the operator's
installed Windows app or a live daemon.

## Checks and independent review

Native Windows installation/deployment is separate from this source repair;
this work does not change the operator's live application.

All four independent reviewer agents inspected implementation revision
`3ec23d352cd584ed478b6c9520d3796a2b64b3f7` against the base above.

| Stage | Independent scope and result |
| --- | --- |
| Initial implementation review | Null/empty wire semantics, unchanged policy/hash values and argv execution boundary; 0 confirmed findings, 8/8 component tests passed. |
| Initial tests/documentation review | Regression assertions, actual route/client fixture and both plans; 0 confirmed findings, 8/8 component tests passed. Its first browser attempt lacked the default cache, so it was not counted as a pass. |
| Triage | No confirmed findings to repair; no fix round consumed. Existing generated-schema limitation retained explicitly above. |
| Fresh policy delta | Targeted no-change verification of null save response, cancel/no PUT, empty command and argv regressions; 0 findings, 8/8 component tests and 1/1 Chromium board test passed. A watcher-limit retry used polling. |
| Fresh coverage delta | Test assertions and documentation boundaries; 0 findings, 8/8 component tests and 1/1 Chromium board test passed. |

Delta reviewers did not restart a broad review. Only this evidence document is
completed after their source snapshot; implementation and regression test code
remain identical to the independently reviewed revision.

Local runtimes: Node 24.19.0, Go 1.27.1, golangci-lint 2.13.2, sqlc 1.31.1,
openapi-typescript 7.4.4 and pinned Playwright Chromium headless-shell 1223.
This host uses Ubuntu 24.04/glibc 2.39; the desktop CI baseline is Ubuntu 22.04.

| Local check | Result |
| --- | --- |
| Focused policy component suite | 8/8 passed, including independently repeated checks. |
| Focused Instructor-board Chromium regression | 1/1 passed, independently repeated by both delta reviewers. |
| Full frontend and E2E TypeScript | Both passed on retry after releasing memory. |
| Renderer production bundle | `npx vite build --config vite.renderer.config.ts` passed. |
| Full frontend Vitest | 353 files executed: 351 passed and 2 failed; 5,537 tests passed, 5 failed, 6 existing skips. Four unchanged supervisor-link cases fail with Unix-socket `EPERM`; the unchanged native agent-browser daemon startup case exits without error output. No full local pass is claimed. |
| Full Chromium renderer suite | 69 passed, one existing Windows terminal-focus timeout. The isolated retry executed all three platform variants and passed 3/3 without changes. All 70 renderer cases were exercised; the initial transient failure is retained here. |
| Docs production build | Passed, 49 generated static pages. |
| Cloud client contract/types/tests/build | Passed, 22 tests; package dry-run passed. |
| Product UI types/tests/build | Passed, 133 tests; package dry-run passed. |
| Backend/Cloud formatting, build and vet | Passed. |
| Pinned backend lint | Passed, zero issues. |
| SQL/OpenAPI/generated TypeScript drift | Regenerated in an isolated source archive and compared; zero drift. |
| Extra Cloud race suite | Failed in two unchanged checkpoint tests because AF_UNIX sockets are prohibited; other Cloud packages passed. |
| Extra backend race suite | Attempted, with Unix-socket/process-namespace failures in unchanged packages; stopped incomplete to release memory. This frontend-only diff does not trigger the Go workflow. No complete local backend race pass is claimed. |
| Linux desktop `npm run build` | Attempted after native prerequisites were installed. Daemon build passed; required tmux smoke failed with `Operation not permitted` on its Unix socket during prepackage, before Electron Forge packaging. The smoke was retained. |

The first typecheck was killed with exit 137 under memory pressure; the retry
and E2E typecheck both exited successfully. Native macOS/Windows and the exact
Linux baseline require CI; Docker is unavailable locally. No publisher or
production deployment was run.

Remote results belong to the follow-up PR's final head and Checks tab. A local
environment failure is not a remote pass, and the installed Windows build still
needs a rebuilt installer and a separate operator smoke test.
