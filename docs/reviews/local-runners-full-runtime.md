# Independent Full review: AO local runners — runtime and platform catalog

Date: 2026-10-13. Role: independent read-only runtime/platform reviewer. This report is review advice, not human approval, a Publish-Go signature, or runner activation evidence.

## Immutable snapshot

| Item | Observed value |
| --- | --- |
| Repository | Binary-Refinery/agent-orchestrator |
| Base | `f60f432a32b9f72370dafab2c58b703182dc8026` |
| Head | `aa517803ed8803e4238260747dcb9ddae2b5f5a2` |
| Tree | `07e8e31d13066fb8585547ec78f905de5b8098c6` |
| `git diff --binary BASE..HEAD` SHA256 | `4e5c075951c4a9366f9d263687a64eccb61be1ff8c0608b5be72dfb948ed2cd7` |

These values independently match the frozen `full-review-manifest.json`. Tracked source/workflow bytes were not edited. Untracked Python `__pycache__` directories were observed and left alone. No additional ancestor or applicable nested `AGENTS.md` was found, including a hidden-file instruction search.

Read the root `AGENTS.md`, `CONTRIBUTING.md`, the local-runner plan, its independent review and LR-PLAN-001 Delta, the local-runner activation/check evidence, and the Instructor POLICY/RULEBOOK boundaries. The narrower user-authorized workflow migration does not authorize unrelated runtime fixes or activation/settings writes.

## Conclusion

Two confirmed conditional P2 runtime/prerequisite-contract defects require triage. Their triggers are concrete permitted self-hosted environments; this review does not claim that either trigger was observed on a particular user machine. The draft-only activation stop is appropriate and remains necessary after these defects are corrected.

### LR-AO-RUNTIME-001 — P2 — Renderer container retains the hosted runner UID without a matching host contract

- Category: container/workspace compatibility. Confidence: high for the stated trigger.
- Location: changed `.github/workflows/frontend.yml:199`, retained container option at `:204`, and new `docs/local-runners.md:26-37`.
- Trigger/path: register an isolated Linux runner as UID 1000, with its normal work/temp/tool directories owned by that UID and mode 0755. That satisfies the documented label, Docker, Bash/Git and isolation requirements. `renderer-smoke` then forces Docker UID 1001. The runner bind-mounts the workspace, temp, tools, actions and HOME paths without normalizing ownership for that container user. UID 1001 cannot create the checkout, action file commands or tool-cache files in those host-owned mounts. Checkout/setup fails before the unchanged renderer suite runs.
- Evidence: the job uses the original pinned Playwright image and `options: --user 1001`; its changed self-hosted selection has no UID/access condition, preparation, or stated deployment requirement. The new host requirements never mention UID 1001 or writable mounts. GitHub's [runner container implementation](https://github.com/actions/runner/blob/main/src/Runner.Worker/ContainerOperationProvider.cs), `MountWellKnownDirectories`, mounts these host directories and sets HOME without a UID ownership correction. The permission failure follows from that implementation and ordinary Unix owner/mode checks. The former hosted runner environment supplied the UID convention; `linux`/`x64`/`ao-ci` labels do not.
- Impact: a documented runner registration can dispatch the mandatory smoke job successfully but fail its initialization/checkouts instead of executing its retained test tier. A Docker availability check alone cannot close this gap.
- Diff causation/contract: migrating this existing UID-sensitive job from a hosted runner to arbitrary dedicated self-hosted registrations creates the incompatibility. The plan requires preserved renderer behavior and required bootstrap/workspace needs.
- Minimal correction: either define and verify an explicit dedicated Linux runner UID/mount-access contract compatible with 1001 before applying `ao-ci`, or adapt the container execution/provisioning so its selected user can write every required mount. Add an activation preflight/evidence requirement for work, temp, tools, HOME and action-file-command access. Preserve the pinned browser image and renderer tests; no hosted fallback or skipped smoke tier is needed.
- Minimal regression check: on an isolated runner, verify the required mounts as the actual container UID before checkout/setup, and demonstrate that an incompatible non-1001 owner is rejected before registration/activation or works with the corrected ownership strategy. Also verify cleanup after root-created desktop package files if the same runner/workspace is reused.
- Reproduction limit: an isolated local Unix permission fixture was attempted outside source. `chown` returned EINVAL and process UID switching was denied by this workspace, so it did not provide executable permission/Docker proof. Those failed fixture attempts are not PASS; no real runner UID was inspected. The finding rests on the explicit job option, missing prerequisite contract, primary runner source and the stated conditional permission path.

### LR-AO-RUNTIME-002 — P2 — Host jobs still depend on hosted-image tools that the new prerequisite contract omits

- Category: host bootstrap and native toolchain compatibility. Confidence: high.
- Location: new `docs/local-runners.md:32-37`, and changed host routing at `.github/workflows/pr-review-leaderboard.yml:16`, `pricing-catalog-validate.yml:19`, `build-artifacts.yml:65,250`, `go.yml:25,80,122`, and `mac-update-e2e.yml:86`.
- Trigger/path: provision a clean dedicated runner with the documented Docker/Bash/Git requirements and the Node/Go/Python toolchains actually selected by its existing setup actions, but without unrelated hosted-image utilities/compilers. Examples below execute unchanged commands on that newly selected host and fail before producing the original evidence.

| Affected host job/path | Requirement not supplied by its setup steps or declared specifically in the new host contract |
| --- | --- |
| PR review leaderboard | Shell `node --test` at line 25 has no `setup-node`; the runner's private JavaScript-action Node does not establish a shell `node` in PATH |
| Pricing catalog validation | `gh api` at line 42 and `curl` at line 47 require host GitHub CLI and curl; `setup-go` installs neither |
| Desktop digest aggregate | `jq -s` at line 259 executes on the host, outside the Ubuntu 22.04 package container that installs jq |
| Desktop artifact Windows/macOS tiers | Digest steps use `jq` and `openssl`; the Linux-only apt bootstrap cannot install native Windows/macOS tools |
| Linux/Windows Go race tiers | `go test -race` needs CGO and a compatible C compiler; `setup-go` alone does not supply it. Windows needs a compatible MinGW-w64 runtime and the Go 1.25+ DWARF5/binutils requirement |
| Native desktop package/make tiers | `forge.config.ts` rebuilds `better-sqlite3` in `prePackage`; Windows needs supported Python and Visual C++ build tools, macOS needs supported Python alongside the listed Xcode tools |
| macOS update E2E | `gh release download` requires native GitHub CLI; installing Node/Xcode/Swift does not supply it |

- Evidence: the complete changed workflow job bodies were read. All these host command/setup payloads are identical to base, but their guaranteed hosted-image environment is removed. The new documentation lists only generic setup-selected toolchains and Git Bash/Xcode. It accurately installs native/digest tools inside Linux package containers, which does not fulfill host jobs or other native tiers. [Go's race requirements](https://go.dev/doc/articles/race_detector#Requirements) document CGO/C-compiler requirements and Windows runtime compatibility; [Go minimum requirements](https://go.dev/wiki/MinimumRequirements#cgo) document Windows DWARF5 support. [node-gyp's installation instructions](https://github.com/nodejs/node-gyp#installation) document supported Python and platform compiler requirements for native addons. The exact installed tool inventory of the user's machines is unknown; none is asserted missing there.
- Impact: runners can meet the documented activation requirements and still fail routine checks, artifact aggregation, Windows race verification or installer generation for environmental reasons. Native labels and successful setup-node/setup-go cannot certify these prerequisites.
- Diff causation/contract: those utilities were supplied by the previous hosted images. This migration removes that supply and adds an incomplete replacement activation contract. Preserving command text without its indispensable environment does not preserve runnable behavior.
- Minimal correction: add a complete explicit per-tier host prerequisite/preflight catalog, bound to the dedicated registration contract, for all commands left outside containers. Alternatively bootstrap narrowly missing tools with trusted steps where suitable. Include host Node for the leaderboard, gh, jq/openssl/curl, Linux C/C++/CGO tools, compatible Windows Go-race compiler, and Windows/macOS native-addon build tools. Cover setup-python installation/cache/extraction requirements when it must download on Windows. Record actual version/PATH/native-build evidence before activation; do not install onto the user's machine or alter settings as part of this PR.
- Minimal regression check: use clean isolated hosts or a reviewed preflight that rejects each absent required tool and confirms action-selected runtimes plus native compiler compatibility. Run the retained affected commands on the intended native runners before activation. Static tests can verify setup/preflight coverage, but cannot label these native paths passed without execution.

## Inspected runtime/platform coverage

All 29 job definitions were inspected, including the inactive upstream-only jobs. Native matrix row expansion and explicit names retain the former OS/platform/artifact identities; the latest/macOS14 native tiers are ARM64 and the separate Intel artifact tier is x64.

| Workflow | Reviewed jobs and principal runtime needs |
| --- | --- |
| artifaktory-windows-build | build-windows: Windows x64, Git Bash, Node22/Go, native Electron make, installer/hash upload |
| build-artifacts | build: all four native tiers, explicit names, unchanged ref/version and artifact/digest identity; Linux Ubuntu22.04 bootstrap; digests: same dependency and aggregate JSON contract |
| cli-e2e | native: Linux x64/macOS ARM64/Windows x64 and unchanged native e2e command; container: retained Docker fresh-install build/run |
| deploy-docs | build/deploy: fork exclusions, trusted publish permissions and publication behavior remain unchanged |
| e2e-gate-tests | test: retained Node20 pure verdict-contract command |
| frontend | Linux package: Ubuntu22.04 userspace, bootstrap before checkout, Bash and working-directory override; mac helper: Swift/Xcode/lipo; test: Node24 dependencies/typechecks/full vitest; renderer: pinned image and UID requirement |
| gitleaks | scan: unchanged history/range selection and pinned Docker scanner |
| go | build-test/windows-workspace/cloud-build-test/lint/sqlc-drift/api-drift: retained complete/scoped suites, race commands, pinned Go/lint/Node setup and drift gates |
| instructor-recovery | configuration-recovery: Linux x64/Windows x64 matrix, original displayed names and Python3.12 suite |
| mac-update-e2e | update-e2e: ARM64, retained release/feed/signature/runtime checks; global /Applications replacement remains a disposable dedicated-machine prerequisite |
| mobile | check: Node20 typecheck and unit commands |
| optional-private-submodule | unauthenticated-recursive-clone: credential-free checkout and original clone smoke |
| pr-review-leaderboard | leaderboard: trusted default-branch checkout and existing limited commenting token; no PR candidate code added |
| pricing-catalog-validate | validate: full retained generation/runtime fetch/preview steps |
| release-latest-guard | latest-release/package-version: original upstream-only conditions, GH/jq guards and explicit trusted ref retained |
| upgrade-continuity | daemon-upgrade: Linux/Windows original display names, candidate default merged checkout and frozen previous ref retained |
| version-floor-guard | validate: original validation command, new explicit read-only contents token and Node22 setup |

Both new Ubuntu22.04 package containers install the inspected Linux build/digest prerequisites before checkout, use `working-directory: .` for bootstrap and retain the original package/smoke commands. Their runtime Docker packaging, host cleanup and permissions remain unexecuted. No confirmed userspace/glibc-baseline defect was found in the static bootstrap beyond the separate host/mount contracts above.

AO `pull_request` events remain unchanged; unqualified candidate checkouts still test the generated merge result. Explicit previous/main/default-branch/dispatch refs remain deliberate exceptions. Same-repository guards are candidate-editable mitigation, as documented, not immutable runner protection. Names, needs, artifacts, timeouts and every old run command are retained. No new publication/deployment behavior was activated.

## Evidence and limits

Reviewer commands: instruction/file reads with `rg`, manifest reads, `git rev-parse HEAD`/tree, `git diff --binary`, SHA256 calculation, complete workflow diffs/reads, and parsed per-job comparison with base. These completed successfully. The comparison confirms unchanged step payloads except the two added Linux bootstrap steps and version-floor Node setup. No production suite was rerun by this reviewer.

The parent supplied six routing/negative tests, actionlint 1.7.7 with custom labels and whitespace PASS. Those remain supplied quality evidence, not independently executed native-runner proof. The frozen check record explicitly leaves complete source auditing pending. The parent identified pre-existing ACP/sandbox failures with source identical to base; this runtime review neither fixes nor calls those failures green.

Unreviewed/unexecuted: Docker package/browser/scanner execution; native Windows/macOS suites and installers; matching Node20 runtime locally; actual AO registrations, hardware/OS versions, UID/tool inventory, access, approvals and isolation; safe workspace cleanup; live required-check enforcement; remote job runner/group records; advisory GitHub Codex results. No network credentials, host settings, runner registrations, remote state, commits or source files were changed. Global macOS application/data paths require dedicated disposable-machine evidence, not only a second login account, before execution.

## Triage handoff

Triage both stable IDs before any correction. A narrow prerequisite/provisioning/preflight correction can be reviewed by Delta against this exact head and patch; expanding trust boundaries, events, commands or supported tiers requires separately scoped Full review. Missing live activation evidence remains a stop even after both findings are resolved.
