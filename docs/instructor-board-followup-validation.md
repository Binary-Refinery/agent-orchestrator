# Instructor Board follow-up validation

## Acceptance covered

- The legacy Instructor route redirects to the Board while retaining work selection.
- Project cards use the approved palette, typography, summary cards and workflow indicators; model settings and project operations remain reachable in narrow and short containers.
- Projects with no work are visible. Project registration saves provider/model/native effort and standing rules before the first orchestrator launch.
- `orchestratorRulesFile` is optional and repository-relative. Its current content follows inline `orchestratorRules` in system instructions. Rooted reads reject outside-repository symlinks and paths, and missing or unreadable files fail clearly.
- `orchestratorPrompt` is a separate initial task for fresh creation. Explicit spawn tasks override it; opening, restoring and sending do not replay the default.
- Tests change only a repository rules file between two fresh launches and assert the second launch prompt contains the changed sentinel without a configuration PUT.
- Real GitHub issue selection is explicit, repository-bound and idempotent; manual work still needs no issue. Intake does not publish, close issues or start workers.
- Cancellation keeps work and runtime history. Unproven native effects remain pending. Atomic proof distinguishes never-admitted delivery from leased, reserved or accepted delivery and fences late sends.
- The Board and documented loopback API use the same daemon operations. No new listener or authentication claim is introduced.

## Independent review and delta chain

Separate reviewers examined configuration, the Board/API and cancellation. Confirmed findings were fixed and rechecked with delta reviews:

1. Known blocked, waiting-input and exited orchestrators appeared unknown: fixed with explicit state mapping and uncertain probes remain unknown.
2. Never-admitted target-bound directives prevented cancellation completion: fixed with an atomic negative-admission receipt; uncertain/admitted effects still require native proof.
3. A narrow project settings label broke across lines: fixed and checked in eight viewport/theme combinations.
4. A lint correction made malformed YAML mappings look like tolerated partial decoding: corrected and the full legacy import regression suite passed.

No confirmed review findings remain open. Typecheck and plural-form failures discovered by integration checks were also corrected and their affected tests passed.

## Local checks

Passed: backend build and vet; pinned golangci-lint 2.13.2; full frontend and E2E typechecks; API/SQL regeneration drift; CLI E2E; shared Cloud client/product UI checks; targeted configuration/launch/restore tests; full Instructor and SQLite store race suites; 72 renderer E2E tests; actual Board screenshots in light/dark and narrow/wide views.

Full native suites are executed without excluding failures. This environment rejects Unix sockets: the backend Herdr/browser/control sockets, Cloud worker checkpoint bridge and Electron supervisor-link socket tests cannot pass locally. The optional pinned browser native test and full-suite outcomes are recorded with the PR's final validation results. Earlier unrelated frontend failures passed both current and unchanged-base focused reruns; they are not claimed to have failed on the base.

The Linux desktop package was attempted: initial tar extraction cannot chown mapped filesystem owners; retrying with `TAR_OPTIONS=--no-same-owner` reaches tmux configuration but the environment lacks yacc. Docker is unavailable. Windows/macOS and native packaging must be confirmed by CI on the published tree. No release or production deployment is used as validation.

## Remaining operating boundaries

The installed-machine pilot and upstream rebase are separate. Protected publication stays unavailable until the isolated Publisher boundary exists. This PR does not modify the user's local PyCharm configuration, install the daemon or change live project instructions.
