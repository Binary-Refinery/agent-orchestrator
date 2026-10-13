# AO workflow migration: quality evidence audit

Finalized 2026-10-13T15:48:53.566169+00:00. Markdown/JSON are frozen for the repair snapshot and scoped Delta reviews.
Audited 2026-10-13T15:48:53.506581+00:00. Base `f60f432a32b9f72370dafab2c58b703182dc8026`. **Full CI is not green or fully verified.** This is evidence for a reviewable draft PR, not a merge/publish clearance.

## Source provenance

Both historical checkout `/workspace/scratch/a7812b4eafcd/agent-orchestrator` and current candidate match all 4,196 tracked non-workflow blobs at the base, with content SHA256 `f8f896637dda333a6a1f8c2e61209b9a6ff1482d150d66af739fba65fe2ae8c3`. Optional `private/ao-cloud` gitlink is uninitialized in both. Application code, package manifests, lockfiles, existing production tests and generation scripts match the base. New runner-contract tests and routing/docs are separately checked and reviewed by the parent.

Historical runner scripts/result JSON and all 29 recorded log headers were audited. Recorded command/cwd headers match. There are 23 historical zero exits and 6 nonzero exits; every result retains its original execution path, exit and SHA256 in `ao-quality-audit.json`. Reused results are exact-source historical evidence, not new executions.

## Reused completed passing source checks

| Commands / scope | Evidence |
| --- | --- |
| Backend and cloud `gofmt`, `go build ./...`, `go vet ./...` | Recorded exit 0; Go1.27.1 Linux |
| Cloud-client generate/schema diff, typecheck, full Vitest (22), dry-run pack | Recorded exit 0; Node24.19.0 |
| Product-ui typecheck, full Vitest (133), dry-run pack | Recorded exit 0; Node24.19.0 |
| Docs `npm run build` | Recorded exit 0 |
| Optional recursive submodule checkout; version-floor Node validation | Recorded exit 0 |
| Browser runtime preparation; candidate `go build ... ./cmd/ao` | Recorded exit 0 |

## Newly executed passing validations

| Check | Actual exit |
| --- | --- |
| scripts-verdict-contract-node24 | 0 |
| scripts-leaderboard-node24 | 0 |
| scripts-instructor-recovery | 0 |
| scripts-mac-update-helper-config | 0 |
| scripts-mac-repair-installer | 0 |
| sqlc-drift | 0 |
| pricing-catalog-validation | 0 |
| api-drift-completed | 0 |
| backend-upgrade-continuity-executable | 0 |
| frontend-typecheck-ci-dependencies | 0 |
| frontend-e2e-typecheck-ci-dependencies | 0 |
| backend-cli-e2e-independent | 0 |
| backend-golangci-lint-independent | 0 |

The valid pinned golangci-lint v2.13.2 run reports **0 issues**. Properly provisioned full main/e2e TypeScript checks pass. Root/frontend/shared npm installs are isolated (no node_modules symlinks). Supplementary pinned-baseline continuity uses `-buildvcs=false` only for unavailable scratch VCS stamping; it does not turn the failed full upgrade suite into PASS.

## Actual failed or incomplete source executions

| Check | Actual exit | Evidence |
| --- | --- | --- |
| Remaining backend race packages | 124 | 71/83 package results: 60 passed, 6 failed, 5 no-test; 12 unfinished at 1,800s. Initial 116 package results retained. |
| Full frontend Vitest | 1 | 352 files passed, 4 failed; 5,567 tests passed, 7 failed, 6 skipped. |
| Repeated complete Settings/history files | 1 | 46 passed, 3 failed; Updates/Updates channel assertions and 20s history timeout reproduce. |
| Prepared native-browser compatibility file | 1 | 23 passed, 1 failed: daemon exited during startup without stderr. AF_UNIX causation is an inference. |
| ACP large-history race investigation | 1 | Queue overflow at 1,024 notifications, peer disconnected, reproduced on unchanged source. |

## Checker setup and orchestration attempts

| Attempt | Actual exit | Interpretation |
| --- | --- | --- |
| Initial current frontend typecheck / e2e tsc | 124 / 2 | Initial setup omitted shared dependencies and used a short typecheck deadline; proper complete reruns both pass. |
| Extra node --test scripts/*.test.mjs | 1 | Invalid invocation of Vitest files; proper full Vitest includes these scripts. Exact CI Node helper tests pass. |
| Baseline archive default go build | 1 | Scratch VCS stamping failure; supplementary -buildvcs=false build passes. |
| Original queued lint | 1 | Tool parallel-run lock conflict; separate requested lint passes with 0 issues. |
| Original queued CLI e2e | 0 | Accidental duplicate retained. Queue guard was attempted too late; no old record or target was altered. |

Every actual exit and log digest is retained in JSON. The checker did not manually cancel another worker; only its own broad Go group reached its predefined timeout. No additional broad retries are performed.

## Preserved failures and incomplete evidence

- **backend-race-missing-packages**: Actual exit124 at predefined 1800s wrapper limit. Completed daemon/supervisor/lifecycle socket tests fail EPERM, preview/processalive assertions fail, and chat admission tests fail context deadline exceeded. Remaining package list is explicit in ao-quality-backend-broad-summary.json. No whole-backend PASS claimed.
- **backend-golangci-lint**: Original queued lint attempt exits1 because independently requested lint already holds the tool lock. Independent pinned v2.13.2 lint exits0 with 0 issues. Queue guard was too late; neither recorded attempt was altered or erased.
- **frontend-failed-files-investigation**: After all shared dependencies were installed, complete GlobalSettingsForm and browser-history files rerun: 46 passed, 3 failed. History cap still exceeds 20s, Settings cannot find Updates heading and developer-mode test cannot find Updates channel. Correct main/e2e typechecks both pass; no application fix in this PR.
- **frontend-browser-native-compatibility-complete-file**: Current browser preparation exits 0. Complete native-runtime file has 23 passing tests and 1 failure: Daemon process exited during startup with no error output. AF_UNIX sandbox denial could explain it but this causal link is inference, not proven by this log.
- **frontend-vitest-full**: 356 files: 352 passed, 4 failed. 5580 tests: 5567 passed, 7 failed, 6 skipped. Four AF_UNIX supervisor tests fail EPERM; native browser fixture initially lacks prepared current-checkout binary; browser-history test times out at 20s; GlobalSettingsForm cannot find Updates heading. No failures relabelled PASS.
- **frontend-all-node-script-tests**: node --test scripts/*.test.mjs incorrectly invokes Vitest-based files without Vitest and TS extension resolution. All 13 exit 1. The proper full Vitest command already includes these script tests. Exact CI named .node-test.mjs helper commands separately pass.
- **cloud-race**: AF_UNIX sockets are denied with EPERM; worker checkpoint tests fail.
- **backend-race**: Completed socket packages fail EPERM, Linux ConPTY process assertions fail, ACP SDK large-history replay overflows its 1024 notification queue.
- **acp-history-replay-failure-investigation**: Exact-source isolated race test exits 1 with notification queue overflow and peer disconnected; no repair in this PR.
- **backend-upgrade-continuity**: Original baseline binary missing after VCS stamping failure; native handoff scripted reviewer cannot find git in hermetic PATH. Two diagnostic tests passed. Supplementary continuity success does not turn this full suite into PASS.
- **frontend-renderer-smoke**: 58 tests passed, 18 failed (chat/focus/notifications/board assertions), with daemon proxy ECONNREFUSED 3001. The pinned CI Playwright container was unavailable; root cause remains unresolved.
- **npm-ci-mobile**: Offline cache lacks zxing-wasm 3.1.3. Node20 is also unavailable. Mobile checks not claimed passed.
- **npm-ci-scripts**: Offline cache lacks yargs 17.7.3. No publication performed.
- **frontend-typecheck-completed / frontend-e2e-typecheck**: Initial current-checkout attempt omitted sibling dependencies, timed out or reported missing React/motion imports. Isolated shared package npm ci was subsequently completed and full typechecks rerun. Initial exits are preserved.
- **backend-baseline-archive-build**: Exact go build exits 1 while obtaining VCS status in scratch; supplementary -buildvcs=false build preserves source behavior and is labelled separately.
- **backend-race** historical log: incomplete; no recorded exit and original process is gone. Not labelled PASS.
- **frontend-typecheck** historical log: incomplete; no recorded exit and original process is gone. Not labelled PASS.
- **frontend-linux-desktop-package** historical log: incomplete; no recorded exit and original process is gone. Not labelled PASS.
- **backend-old-upgrade-build-corrected** historical log: incomplete; no recorded exit and original process is gone. Not labelled PASS.

## Boundaries and required remote validation

- Whole backend race aggregate remains incomplete at the local 1800-second wrapper deadline; explicit uncompleted packages are in ao-quality-backend-broad-summary.json. No further full unchanged suites are rerun.
- Docker binary unavailable: pinned CLI fresh-install container, pinned gitleaks Docker image, pinned Playwright CI container, and npx @redwoodjs/agent-ci run --all not locally executed.
- Native Windows unavailable: Windows workspace/launch/model/CGO suites, native CLI e2e, upgrade continuity and desktop installer/make need remote Windows CI.
- Native macOS unavailable: native CLI e2e, Swift/update helper build/state tests, mac desktop artifacts/signature verification and full installed-app update e2e need remote macOS CI.
- Node20 unavailable: Mobile and e2e-gate pinned Node20 jobs have no exact-runtime local PASS. Successful Node24 gate tests are supplementary only.
- Mobile and scripts offline npm installation fail for missing cached dependencies; no package-lock or dependency workaround committed.
- Linux desktop package historical log stops while fetching tmux 3.5a and has no exit. Full Ubuntu22.04 artifact/container packaging environment is unavailable locally; no package PASS claimed.
- Latest LiteLLM SHA fetch/sync/generated-catalog-runtime workflow requires external source and API context; checked-in catalog validation and fixture tests are the locally executed subset.
- Canonical release/latest guards require release/API context; publishing, updater installation and production docs deployment are never executed as local validation.
- Workflow routing and bootstrap checks are parent-owned. This audit validates unchanged source; it does not assert runners currently registered or online.

Runtime: Go1.27.1, Node24.19.0, npm11.9.0, Python3.12.14 on Linux. The independent AF_UNIX creation probe returns EPERM. Publishing, release mutation and production deployment were never run. The checker did not repair application/workflow source or cancel another worker.