# Independent Full review: AO runner contract

Date: 2026-10-13. Reviewer role: independent read-only Full reviewer, trust/governance/test contract catalog. This report is separate from the runtime review. It is neither a human approval nor evidence of runner activation.

## Frozen snapshot

- Repository: `Binary-Refinery/agent-orchestrator`.
- Base: `f60f432a32b9f72370dafab2c58b703182dc8026`.
- Head: `aa517803ed8803e4238260747dcb9ddae2b5f5a2`.
- Head tree: `07e8e31d13066fb8585547ec78f905de5b8098c6`.
- Binary patch SHA256: `4e5c075951c4a9366f9d263687a64eccb61be1ff8c0608b5be72dfb948ed2cd7`.
- Reviewed manifest: `full-review-manifest.json` outside both repositories.

The actual HEAD, head tree and recomputed binary patch digest match the manifest. `git diff --exit-code <head>` passed. AO has pre-existing untracked Python cache directories from quality checking; these are outside the reviewed tracked snapshot and were not modified or removed by this reviewer. No ancestor instruction file or additional applicable path `AGENTS.md` was found. Base `AGENTS.md` and `CONTRIBUTING.md` were read through `git show`; AO has no base `policy/core.md`.

## Conclusion

`NO CONFIRMED IN-SCOPE FINDINGS`.

The conclusion is bounded to the frozen diff and the coverage below. No defect ID is opened and no repair is requested by this reviewer. It does not certify full application CI, runner availability, isolated execution, repository settings or readiness to merge. Source-check failures and unavailable validation must remain visible in the final evidence; this review does not waive or convert them into a PASS.

## Contract observations

- All 29 job definitions in the 17 source workflow files select dedicated self-hosted native labels, directly or through the retained native matrices. No hosted selection or runtime fallback remains in source routing. Missing registrations are explicitly treated as queued/blocked work.
- Base/head comparison retained every workflow event, job ID, concurrency contract, dependency, existing timeout, environment, existing step dictionary and checkout reference. The sole workflow-level permission difference is the explicit `contents: read` restriction added to Version Floor Guard. No validation command or assertion was removed. Linux package bootstrap and explicit Node22 setup are additions rather than replacements for original tests.
- Existing `pull_request` triggers and default merged-result checkout are retained. The same-repository PR guards do not substitute PR head-only tests or add privileged `pull_request_target` candidate execution. The original leaderboard target event, trusted default-branch checkout and limited commenting permission are unchanged apart from runner placement. Fork/upstream-only publishing and deployment conditions are retained.
- Native/latest macOS tiers select ARM64, and the separate Intel artifact selects x64. Matrix platform/artifact identities and display names are retained. Both desktop Linux paths declare Ubuntu22.04 containers, preserving the intended userspace baseline, and supply bootstrap build/digest tools. The actual package result and native OS behavior require the separate runtime review and real execution evidence.
- The independently reviewed plan and its scoped Delta are preserved. Both repositories contain identical revised plans with SHA256 `ed544a34a1e3e12ed351a8927033ebff47d80d2968c1b203f86db2fb24774d13`; the plan-review documents are also byte-identical. Actual implementation converges with the recorded resolution of `LR-PLAN-001` concerning PR merge checkout.
- The new six-test suite checks the real workflow files against the pinned trusted base, inventory/routing, retained commands/events/timeouts, PR checkout references, native architecture/artifact tiers and Linux bootstrap. Mutated hosted selection, missing required registration label and absent native tier are rejected. These tests cover a static routing contract rather than a GitHub scheduler or runtime.
- Documentation correctly distinguishes required `ao-ci` registrations from observed organization runners. It does not claim the user-owned fork can access organization runners, nor that a label establishes isolation. Same-repository guards are expressly candidate-editable mitigation, not immutable security protection. Independent repository-wide access/approval controls, clean isolation, registrations and actual job records remain prerequisites for activation. Keeping the PR draft is the recorded disposition for missing evidence.

## Independent check evidence

Python runtime used for the review experiments: 3.12.14.

- `git rev-parse HEAD`; `git rev-parse <head>^{tree}`; `git diff <base> <head> --binary | sha256sum`: all matched the frozen identity, exit 0.
- `git diff --exit-code <head>`: exit 0.
- Read-only Python base/head structural comparison across all 17 workflows: events, job IDs, concurrency, permissions subject to the stated restriction, job dependencies/timeouts/environments/permissions and every original step dictionary retained; exit 0.
- `python -B -m unittest discover -s test/ci -p test_local_runners.py -v`: exit 0; 6 tests passed. Bytecode generation was disabled; no tracked repository write was made.
- File/diff/instruction reads and plan/review digest comparison: successful. A deliberate lookup confirmed `policy/core.md` is absent at the AO base; this is not missing applicable AO policy.

The quality check document reports actionlint and routing checks and explicitly states the separate AO source audit is still pending. Those reported wider checks were not independently rerun by this reviewer, and no complete source-application PASS is inferred from them.

## Coverage and unreviewed areas

Inspected every tracked change, both revised plan files, the active plan-review/Delta record, AO activation/check documentation, the trusted-base instructions and complete workflow inventory. Reviewed changed runner selections, PR restrictions, permissions, checkouts, existing validation steps, native matrix/artifact contracts, package bootstrap declarations and the complete new routing tests.

Unreviewed or unexecuted here: real GitHub expression/scheduling execution; live runner registrations, settings, group access or approval policies; clean/ephemeral host behavior; Docker and package builds; Windows/macOS toolchain/runtime execution; complete unchanged Go/frontend/cloud/mobile suites; source audit failures; actual release/update/deployment execution; remote required checks and Codex review results. These are explicit evidence gaps rather than claims of a green or activated migration.

No source edits, commits, pushes, remotes, settings writes, release/deployment actions or nested agents were performed. The only write is this redacted report outside the repository.
