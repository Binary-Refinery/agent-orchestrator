# OpenCode Backbone auxiliary validation

Status: all locally runnable auxiliary commands were attempted. MCP, native CLI and the corrected-input upgrade command passed; frontend/cloud failures and desktop/browser/native-runner gaps remain explicitly open. This is not an all-checks-passed attestation.

These checks were performed on 2026-10-16 during reconstruction. The auxiliary input manifest is in the ignored local `artifacts/opencode-ci/auxiliary-input-manifest.json`. Manifest SHA-256: `ec9a47150693687facf85512491ae1a1a5e9db90e1a8fb05f348283a2d8536e8`. It records SHA-256 for tracked frontend, packages, cloud, contract and root dependency inputs. Every completed normal command also has a JSON execution record and full log under `artifacts/opencode-ci/`, including the then-current Git HEAD, tracked diff hash, and untracked source hashes. Product implementation continued while the independent frontend/cloud checks ran. At frozen candidate `2e10d4a6c4a386281d94891e5ebe88fdf8f5116c` (tree `267a34d645381b9e4fbbc2ffdbe477ff87f16c38`), every auxiliary input hash and path still matched, including generated API types; no added, removed or changed inputs were found. Cloud’s imported `backend/pkg` and module inputs were also unchanged. Therefore unchanged completed checks were not repeated. The backend owner’s complete source manifest also matched the frozen files.

Environment: Ubuntu 24.04.3 Linux amd64, Go 1.27.1, Node 24.19.0, npm 11.9.0, 8 GiB memory limit. The Linux desktop CI job uses Ubuntu 22.04; this host is not that glibc baseline. Required npm installs were completed by the parent before these runs. No publishing or production deployment command was run.

## Completed commands

| Check | Exact command | Exit | Result | Qualification |
| --- | --- | ---: | --- | --- |
| `browser-runtime` | `npm run browser-runtime:prepare -- --quiet` | 0 | PASS |  |
| `cloud-generate` | `npm run generate` | 0 | PASS |  |
| `cloud-generated-drift` | `git diff --exit-code -- src/schema.ts` | 0 | PASS |  |
| `cloud-go-build` | `go build ./...` | 0 | PASS |  |
| `cloud-go-race` | `go test -race -count=1 ./...` | 1 | FAIL | Two checkpoint-bridge tests failed; other tested packages passed. |
| `cloud-go-vet` | `go vet ./...` | 0 | PASS |  |
| `cloud-gofmt` | `unformatted=$(gofmt -l .); if [ -n "$unformatted" ]; then echo "$unformatted"; exit 1; fi` | 0 | PASS |  |
| `cloud-pack` | `npm pack --dry-run` | 0 | PASS |  |
| `cloud-tests` | `npm test` | 0 | PASS | 22 tests passed. |
| `cloud-typecheck` | `npm run typecheck` | 0 | PASS |  |
| `docs-build` | `npm run build --prefix src/docs` | 0 | PASS |  |
| `frontend-e2e-typecheck` | `npm run typecheck:e2e` | 0 | PASS |  |
| `frontend-renderer` | `CI=true npm run test:e2e:renderer` | 1 | FAIL | 76 failed before assertions: pinned Chromium executable absent. |
| `frontend-typecheck` | `npm run typecheck` | 0 | PASS |  |
| `frontend-vitest` | `AO_AGENT_BROWSER_TEST_BINARY="$PWD/agent-browser/agent-browser" npx vitest run` | 1 | FAIL | 5569 passed, 5 failed, 6 existing explicit skips. See failure analysis. |
| `mac-helper-script-tests` | `node --test scripts/build-update-helper.node-test.mjs` | 0 | PASS | 4 tests passed; native macOS compile is not covered. |
| `mac-repair-script-tests` | `node --test scripts/build-mac-repair-installer.node-test.mjs` | 0 | PARTIAL | 8 passed, 1 existing native macOS-only skip. |
| `mcp-tests` | `AO_MCP_TEST_DAEMON=/workspace/scratch/6e019b0851cc/artifacts/ao-backbone-candidate npm test --prefix packages/ao-mcp` | 0 | PASS | 124 tests passed, zero skipped, including the real isolated daemon. |
| `mcp-typecheck` | `npm run --prefix packages/ao-mcp typecheck` | 0 | PASS |  |
| `playwright-curl` | `curl --http1.1 -fL --retry 2 --retry-delay 1 --connect-timeout 20 --max-time 180 -C - https://cdn.playwright.dev/builds/cft/148.0.7778.96/linux64/chrome-linux64.zip -o artifacts/opencode-ci/chrome-linux64.zip` | 0 | BLOCKED | Exit 0 only means HTTP transfer completed; 195-byte HTML is not a valid ZIP. |
| `playwright-install` | `npx playwright install chromium` | 1 | FAIL | Pinned Chromium download failed. |
| `product-ui-pack` | `npm pack --dry-run` | 0 | PASS |  |
| `product-ui-tests` | `npm test` | 0 | PASS | 133 tests passed. |
| `product-ui-typecheck` | `npm run typecheck` | 0 | PASS |  |
| `cli-e2e` | `go test -tags e2e -v ./internal/cli/...` | 0 | PASS | 404 top-level passed, one existing darwin-only skip; no failures. |
| `upgrade-continuity` | `AO_UPGRADE_OLD_BINARY="$PWD/../artifacts/opencode-ci/upgrade-N/ao" AO_UPGRADE_NEW_BINARY="$PWD/../artifacts/opencode-ci/upgrade-N-plus-1/ao" go test -tags upgrade -count=1 -timeout=8m -v ./internal/integration/upgrade` | 0 | PASS | 4 tests passed, zero skipped; CI-equivalent binary filenames, unchanged hashes. |
| `desktop-build-owner-restore` | `AO_RELEASE_REPO=Binary-Refinery/agent-orchestrator AO_DATA_DIR="$PWD/../artifacts/opencode-ci/desktop-data" npm run build` | 1 | BLOCKED | Initial attempt: tar could not restore archive ownership to unmapped uid/gid1000. |
| `desktop-build` | `TAR_OPTIONS=--no-same-owner AO_RELEASE_REPO=Binary-Refinery/agent-orchestrator AO_DATA_DIR="$PWD/../artifacts/opencode-ci/desktop-data" npm run build` | 1 | BLOCKED | Ownership-only retry: tmux configure stopped at missing yacc; package not built. |

## Failure analysis and existing skips

- Full frontend Vitest ran all 356 discovered test files. Four `supervisor-link` failures report `listen EPERM: operation not permitted` for their temporary Unix sockets. The pinned agent-browser native compatibility test exits during daemon startup without useful error output; an additional narrow `--debug` diagnostic produced the same opaque error. Its precise native cause remains unresolved locally. One bounded `strace` attempt was itself rejected by `PTRACE_TRACEME`/`PTRACE_SETOPTIONS` EPERM; that denial is not proof of the daemon’s original failure cause. An independent narrow baseline run reproduced the identical daemon-startup error using exact original Git blobs for the test, runtime and CDP bridge from `6b1ba43f1df9415c1ec937876ce54c86aa37484e`, the unchanged Vitest config/lockfile and the same pinned native binary. All three blobs also equal the candidate files. This establishes baseline failure and no demonstrated connection to the change; it does not turn the required full suite into a pass or establish a socket cause. No new dependencies, symlinks, product edits or sandbox changes were used. The temporary baseline files were removed before packaging. Six explicit `ConnectMobileModal.test.tsx` skips already exist in the repository; no skips or exclusions were added for this task.
- Renderer smoke attempted all 76 selected CI tests. All failed before test assertions because Playwright's pinned Chromium headless-shell v1223 is absent. The standard installer exhausted retries on a non-ZIP response. A bounded HTTP/1.1 attempt to the exact installer URL returned 195-byte HTML headed “Site Unavailable”, so it was not accepted as a browser archive. This host also lacks the workflow's pinned Playwright container.
- Cloud's full uncached race command failed two checkpoint-bridge tests. The poke test reports Unix-socket `EPERM`. The safety-net test uses the same bridge, whose listener must start before the timer, and times out. This is consistent with the same environment restriction; both tests and bridge source are unchanged. The command is recorded as failed, not passed.
- The full desktop package first failed because root-mode tar attempted to restore archive uid/gid 1000, which this user namespace cannot map. A bounded complete retry used `TAR_OPTIONS=--no-same-owner`, matching normal non-root extraction behavior without changing permissions or archive hash verification. Extraction succeeded, the daemon build and bundled dependency steps progressed, and tmux configure then failed with `yacc not found`. No compiler/package prerequisite was installed, no sandbox restriction was changed, and no release artifact was published. The Linux package gate needs its Ubuntu 22.04 CI runner with the workflow’s bison/pkg-config prerequisites.
- The macOS repair-installer script suite has one built-in native macOS skip. Swift compilation, `lipo`, native helper state tests and native packaging require the macOS CI runner.

## Pending local/remote gates

The frozen native candidate binary has SHA-256 `1c2c1b7f592ddfcadf8130b3f63d80b62d86a2a62e6295b68815944cf2fe2632`. It was built with Go 1.27.1 from the frozen product bytes before the checkpoint commit. Its embedded Go build metadata is `vcs.revision=e99ecde34469c70323381b1867958c578455c0bb` and `vcs.modified=true`; the exact source manifest binds the built product bytes to the frozen candidate, not to an embedded `2e10d4a6` revision. Complete MCP tests passed against this exact binary. The native CLI E2E command exited 0 with 404 top-level passes and one existing darwin-only skip. The complete upgrade suite passed all four tests, including native Instructor handoff and three restart recoveries, after correcting the test binary filenames to the CI convention. The full desktop package command was attempted after resource release and remains locally blocked at the missing yacc/bison prerequisite. Heavy jobs were serialized or explicitly coordinated with the parent’s backend checks on this 8 GiB host. The detached previous worktree at `d61634321d8d3a8ba3cf933033b168331310785a` was verified, and its `go build -o <isolated previous binary> ./cmd/ao` completed with exit 0 in 8.987 seconds. Previous binary SHA-256: `3e8d0ac1b0b7c199fbd3287717c2e140d3b048f8bab78265f68e87bff44c908f`. The upgrade run reused byte-identical copies of the verified binaries named `ao`, matching the CI paths; another equivalent daemon build was unnecessary.

Docker is unavailable, so the clean-container CLI install job and pinned renderer container are not locally verified. Windows and macOS jobs require native CI. This host has no `bison` or `pkg-config`; privileged package installation was unavailable. No privilege or sandbox bypass was attempted.

Backend core build/vet/full race/lint, API/sqlc drift, focused OpenCode tests, Full reviews and remote CI are owned by the parent/source owner and are not certified by this auxiliary report.

## Evidence identities

The source-record hash covers the command record’s HEAD, tracked binary diff hash and individual untracked-file hashes. These are intermediate candidate identities, not a claim that all commands ran on one final revision. The previous-daemon command separately verifies the frozen previous commit in its command.

| Check | Then-current candidate HEAD | Source-record SHA-256 | Output-log SHA-256 |
| --- | --- | --- | --- |
| `browser-runtime` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `e75a258ecaf08a7109cacfed31350ad9595c2ee67f1c3c9e096cbed75249574d` | `4e5598d78f351ef9df3c98ea2bae69e92b94cd56da2a88b5188e9e4016311551` |
| `cloud-generate` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `a60bb01408bf0add49732a5238b398ef46f5efc9b96cb0082916354a6620dc5c` | `4ac5b9b239b0e3015d4a053a979ff62f726f6be2e9948a53137f2f64a588fc9c` |
| `cloud-generated-drift` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `72609a841e0248a8e6f01924bbe3b6e16e18eeee27a1d2c39679bade6fa6ed79` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `cloud-go-build` | `e99ecde34469c70323381b1867958c578455c0bb` | `06a66d11756687951ebe6d480e170f46545e2d2f418b948604c5b7a9f5329013` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `cloud-go-race` | `e99ecde34469c70323381b1867958c578455c0bb` | `06a66d11756687951ebe6d480e170f46545e2d2f418b948604c5b7a9f5329013` | `0796acae732b83edddcc71c0944feaec9545cfd5b633d99b98c54ddf763187cb` |
| `cloud-go-vet` | `e99ecde34469c70323381b1867958c578455c0bb` | `06a66d11756687951ebe6d480e170f46545e2d2f418b948604c5b7a9f5329013` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `cloud-gofmt` | `e99ecde34469c70323381b1867958c578455c0bb` | `06a66d11756687951ebe6d480e170f46545e2d2f418b948604c5b7a9f5329013` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `cloud-pack` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `63a042b98eb2022f9311d9598f3eade8c6c6bea382579cccb1e088198e611d32` | `d496936154a27258e9998e1f877d8994f53090f73bce2104363ce5ff7b50b83f` |
| `cloud-tests` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `72a6845ac69dfa73f6580850ac20e0678a5c5eb99e1cae7aa22ead553663f7cc` | `995eb242678b82a4d43b5608aadd51f3bf61bbb7672e74d9dec1189cba3c9cdd` |
| `cloud-typecheck` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `36bb1f192a0d19c757e03f0496448a9a76d00119f4e0b3aaf6ff1c0406175896` | `1a056035cbcb0707ca0dd46f918a49e89541b149f121b72f2fc191091e09502c` |
| `docs-build` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `140416dac1bc200f500b157750f1be7ce0a8a3c5a468ff4e5ac7b1edebe19baf` | `c9066c781aea4610909c503cee589232da13626f645f493fa940fcf7e2381b94` |
| `frontend-e2e-typecheck` | `e99ecde34469c70323381b1867958c578455c0bb` | `807a6823346d4a5a40e983542dafe4209bf633462f3b22fa07abb1a7acd4ea03` | `cda3e37f78e97fc1a75a02f1d6da96428f721f43fad92a377f8088ece960044f` |
| `frontend-renderer` | `e99ecde34469c70323381b1867958c578455c0bb` | `06a66d11756687951ebe6d480e170f46545e2d2f418b948604c5b7a9f5329013` | `f5542ae174fc17afdb218082701381f23d1948f574d45a6822b1152c1ed7c571` |
| `frontend-typecheck` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `140416dac1bc200f500b157750f1be7ce0a8a3c5a468ff4e5ac7b1edebe19baf` | `cfde4862cd5d5152532f1bf0301ca8478a6bd47a3c595ddd647a45a471d6eb95` |
| `frontend-vitest` | `e99ecde34469c70323381b1867958c578455c0bb` | `807a6823346d4a5a40e983542dafe4209bf633462f3b22fa07abb1a7acd4ea03` | `95df2b90c4dc61fc7e9922e12990cd957a9a783037ecedcd494577b527100153` |
| `mac-helper-script-tests` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `140416dac1bc200f500b157750f1be7ce0a8a3c5a468ff4e5ac7b1edebe19baf` | `dd09c0d2bef29f6e0f1cb7c4f88119b498b7cc4f3ebb0ea9044d2977d93bf792` |
| `mac-repair-script-tests` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `140416dac1bc200f500b157750f1be7ce0a8a3c5a468ff4e5ac7b1edebe19baf` | `1cdecbad25cd08dae0eb135146afab80cb2f399252aedb0f94dc916b72e05e18` |
| `mcp-typecheck` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `140416dac1bc200f500b157750f1be7ce0a8a3c5a468ff4e5ac7b1edebe19baf` | `2939833063b3eb41f9bd4eba3c3be09b01f860ad8db3d903a7caaa56063c0522` |
| `product-ui-pack` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `b6e7331432b3a717f51e0dea9a010635c106377339ceb11e0e916d581d786821` | `2e946a63da522e895d225b8920677e144e52e44aeef2c318513c27a15e43bacc` |
| `product-ui-tests` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `33fd45f0ebd052fca25e88fbed7236962ac65b31aa1679e23b7798a590516b8a` | `8dc3d64e365f7ffcc045b538b59d05f9c002fa448a7a724534f2a7bc038e96dc` |
| `product-ui-typecheck` | `fd9bffd4649d48b4182ea3c5382e151252691973` | `50667870df46a89efde0e3b34f4e5cbccb0c0911957b05d0fa48e6099beff1cf` | `a62396912439d4cc48d95e4b47116f8c336f889f42fc697e80083da4a8c2cdf4` |
| `upgrade-previous-build` | `e99ecde34469c70323381b1867958c578455c0bb` | `4670bf91ca95607c130e44d03cee960ac017e9b6ac3b3d7b7fcb996705d83ec7` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `mcp-tests` | `2e10d4a6c4a386281d94891e5ebe88fdf8f5116c` | `c6502fe458ca94d26d02d512ef14684a8e80ab8defef73b544eae41304f696af` | `2c9ec283170fdf1fa526822bc0ca94d2f1b6c6cadd542c6ca0b7d08124145688` |

Frozen-input comparison: `artifacts/opencode-ci/frozen-input-check.json`; SHA-256 `95e4b5a9a3437e076b0d9c53bf5b51fd17dcf0dab1945bf81699ff1c7168f734`. Restored `go.work.sum` stayed at SHA-256 `7df8d5343bc3d2b60d3738124c93b2047348af8e405c7235dcac95bccdfca355` through the MCP gate.

## Native browser failure diagnostics

These bounded diagnostics explain a failed required gate; their selected-test runs are not replacements for the complete Vitest suite. The existing `--debug` attempt remained opaque. No additional tracing was attempted after EPERM.

| Evidence | Result | SHA-256 |
| --- | --- | --- |
| `agent-browser-strace-test.log` | Exit 1; tracing itself denied, original native cause unknown | `567b3a07642e25b0517c886fb1e511811708e0841330a2a18464dda51d1aae3d` |
| `agent-browser-baseline-inputs.json` | Original baseline Git blob and SHA-256 identities; candidate equality | `e136821bace604911e73a06816f1e88802299d5c7c958af030c476de3592cc9e` |
| `agent-browser-baseline-test.log` | Exit 1; the exact selected baseline test reproduces the same startup failure | `df68db40cf81efeb009f75d807cfa2c0c370a66616956bc1fec108c97f8b858a` |

The baseline command was `AO_AGENT_BROWSER_TEST_BINARY="$PWD/agent-browser/agent-browser" npx vitest run .opencode-baseline-evidence/agent-browser-runtime.test.ts -t 'connects the pinned native daemon and keeps native tab lifecycle commands aligned'` from `frontend`. The three original source files were materialized in that temporary child directory, so their existing dependencies resolved naturally from the unchanged parent installation. Only this one requested diagnostic test was selected; 23 unrelated tests are consequently shown as skipped in that diagnostic, whereas the full-suite result above remains unchanged.

## Upgrade input correction

The first full upgrade run exited 1 after 59.363 seconds. Core continuity and the two diagnostic tests passed; native handoff failed because the reviewer fixture could not find Git. The reused candidate was named `ao-backbone-candidate`, whereas CI names both daemons `ao`. The unchanged reviewer launcher rejects a non-`ao` executable for `HookPATH`; its shim fallback then prepends to an empty project PATH, omitting inherited system tools. This was a test-preparation mismatch, not a product change. The original binaries were copied byte-for-byte to isolated `upgrade-N/ao` and `upgrade-N-plus-1/ao` paths, their hashes were rechecked, and the entire upgrade suite was rerun. It passed in 175.560 seconds. No launcher change was made. The original failure is preserved alongside the corrected run.

| Additional evidence | Source-record SHA-256 | Log SHA-256 |
| --- | --- | --- |
| `cli-e2e` | `2ceacfc281004f86d8effeb13c345f545907932a301160f446f33595ea0e06fb` | `9fc9cac7e969c0c8a87a940756af26c0cb2cbcb3960cae2a189b038d6b3a7c13` |
| `upgrade-continuity-noncanonical-name` | `2ceacfc281004f86d8effeb13c345f545907932a301160f446f33595ea0e06fb` | `e444d5a53caf953a7820bdcbaf8c05a9f282771b3f3661fd6ecb2bfe6bf94486` |
| `upgrade-continuity` | `2ceacfc281004f86d8effeb13c345f545907932a301160f446f33595ea0e06fb` | `431864250f9eae35b4de976580efa147f287341ec83eb91e8b26913cfe450187` |

## Desktop attempt and final input binding

| Additional evidence | Exit | Source-record SHA-256 | Log SHA-256 |
| --- | ---: | --- | --- |
| `desktop-build-owner-restore` | 1 | `341063d020b8c1189ddb663e1890030e8e6252f6a2e53806daf6dc4d14fe948e` | `86906ef91a7c51c30313103a3b2557d1d0a810d9be799c9efa0fcaa814c3d2b9` |
| `desktop-build` | 1 | `341063d020b8c1189ddb663e1890030e8e6252f6a2e53806daf6dc4d14fe948e` | `c2e1e8b1ddcc4092eff97dace9731d7cabb985e93b5520844f731639eeb8ac3f` |

At final auxiliary verification, HEAD was `4567b45911d121c093af8f345471b1ad8b2bdebc`. The complete auxiliary input path/hash set still matched the earlier checked inputs, and every file in the backend owner’s frozen source manifest still matched. No product source changed during these jobs. `go.work.sum` stayed byte-for-byte at SHA-256 `7df8d5343bc3d2b60d3738124c93b2047348af8e405c7235dcac95bccdfca355`; no restoration was needed. Only this report was modified. Final comparison record SHA-256: `fe52b1ebfec683f2f5902dc4c563198a62f97979c98da0afe59b54641569f10f`.

The desktop lifecycle did successfully build its daemon before the native prerequisite failure. That executable has SHA-256 `dedd477400e2a2eb0eda50551585b009b102187b325ee60eebfc44acc1f988f6`; it is a separate build with current VCS metadata and is not claimed to be byte-identical to the earlier frozen MCP/upgrade binary. Its Go build metadata is retained in `artifacts/opencode-ci/desktop-daemon-build-info.log`. The overall desktop package result remains blocked.
