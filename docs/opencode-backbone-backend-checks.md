# OpenCode Backbone backend validation

Status: the complete local backend race gate **failed**, with all 23 failing top-level tests reproduced on the exact unchanged baseline. No race detector warning or panic was reported. Full build and vet passed. Full lint found one new path-expression issue; its final equivalent correction passes complete lint/build/vet and the complete affected OpenCode adapter race package. This report does not turn the failed full suite into a pass, qualify a model, or approve release. Independent Full review and remote CI remain separate gates.

## Source and execution identities

Checks were run on 2026-10-16 using Go 1.27.1 linux/amd64 and golangci-lint 2.13.2 on the 8 GiB Linux host. The full commands use `-p=1` and lint `--concurrency=1` to bound compile memory; no package or test was excluded, the race run uses `-count=1`, and the workflow's 20-minute per-package timeout was unchanged. Commands below ran from `backend/`. Full raw logs and JSON records are retained beside this checkout; their hashes appear below.

The original complete run binds to product commit `2e10d4a6c4a386281d94891e5ebe88fdf8f5116c`, tree `267a34d645381b9e4fbbc2ffdbe477ff87f16c38`, and base-to-head binary patch SHA-256 `6fef84498a28a9a1f77e2e3f5071115920718203e1a5893e7bfe966d055e3fae`. The 2,043-file backend source manifest `artifacts/ao-backend-tested-source.json` has SHA-256 `79663d35b2fefda226f143e934113cdb77390cd565c6ea5a394afd0e18b85710`. Subsequent auxiliary-report commits changed no product inputs.

After the complete run, exactly one source file changed: `backend/internal/adapters/agent/opencode/backbone_sources.go`. Its original SHA-256 was `0e220ec9ef3ebe5dffd583912b7f3d70e51cc9dbf05c0110da83c5c101aebf9c`, final SHA-256 is `c92b8e2817dc5d150acffd629e1e64282d41d04c26e6d22322f81235edbe3bbe` (Git blobs `5e0a73b84b9ac06203666630d083a45b088979c0` and `0425a6eeb740b4c28ab62e421bbf696cf88d7b7e`). The final 2,043-file manifest `artifacts/ao-backend-lintrepair-source.json` has SHA-256 `1f8a38e706150d9ec1cd231668a1cd0de37f5efee96c180f30a89c2d19898400`; comparison finds exactly that one changed entry, no additions or removals. The post-repair checks bind these final bytes. The earlier full race suite was not executed again on the repaired bytes and is not represented as such.

The final tested source was subsequently committed as `19ef9159c1937e6bd6c88f980cf5bdf3a31f7281`, tree `231f62b1acb44aeefa43d11585c2e383d665c13c`. This checkpoint adds only that source correction to the earlier product and already committed reports; the final run manifest binds its backend bytes exactly.

The correction moves the existing `/Library/Managed Preferences` absolute base into a local variable and uses that same variable in the existing `filepath.Join(base, current.Username)`, only inside the existing `runtime.GOOS == "darwin"` branch. It preserves both original checked paths and control flow. The initial lint diagnostic was `gocritic/filepathJoin` against the slash-containing literal. An intermediate spelling that joined `/`, `Library`, and `Managed Preferences` still triggered the same rule for `/`; that failed attempt and its passed build/vet/adapter checks are preserved in `ao-postlint-*` and are not the final evidence. No lint suppression was added.

## Complete command results

| Source | Exact command | Exit | Elapsed seconds |
| --- | --- | ---: | ---: |
| Frozen product | `go build -p=1 ./...` | 0 | 4.225 |
| Frozen product | `go vet -p=1 ./...` | 0 | 25.764 |
| Frozen product | `go test -race -p=1 -count=1 -timeout=20m ./...` | 1 | 2559.603 |
| Frozen product | `golangci-lint run --concurrency=1 --timeout=20m --path-mode=abs` | 1 | 105.810 |
| Final lint repair | `golangci-lint run --concurrency=1 --timeout=20m --path-mode=abs` | 0 | 29.137 |
| Final lint repair | `go build -p=1 ./...` | 0 | 3.837 |
| Final lint repair | `go vet -p=1 ./...` | 0 | 1.083 |
| Final lint repair | `go test -race -p=1 -count=1 -timeout=20m ./internal/adapters/agent/opencode` | 0 | 7.119 |

Final full lint reports `0 issues`. Final `gofmt -l backend` produced no paths, `git diff --check` passed, and generated API/schema/sqlc paths had no unstaged drift. `npm run api` previously completed successfully and generated the committed OpenAPI and frontend TypeScript together; `npm run sqlc` completed successfully with no generated storage changes. Their inputs have not changed since those runs. `go.work.sum` remains byte-for-byte equal to the trusted base with SHA-256 `7df8d5343bc3d2b60d3738124c93b2047348af8e405c7235dcac95bccdfca355`; no dependency change is included.

## Complete race outcome and baseline diagnosis

The complete run took 2,559.603 seconds: 179 packages passed, eight packages failed and 13 reported no test files. There were 23 top-level failing tests. Every failing test was reproduced in a focused diagnostic against unchanged base `6b1ba43f1df9415c1ec937876ce54c86aa37484e` at `artifacts/ao-trusted-base`, using the same host. The diagnostics select the failures to establish their baseline counterpart; they do not replace the complete candidate suite or constitute a complete baseline suite. An independent evidence auditor checked the raw counts and all 23 counterparts.

| Failed package | Count | Observed failure and exact baseline counterpart |
| --- | ---: | --- |
| `internal/adapters/agent/fx/herdr` | 7 | Temporary Unix socket listen returns `operation not permitted`; all seven `TestSocket*` failures reproduced. |
| `internal/adapters/runtime/conpty` | 5 | Existing Linux PTY close/reaper tests cannot observe the expected live processes, process groups or `/proc` identity; the same five failures reproduced. |
| `internal/browserruntime` | 1 | `TestListenUnixUsesShortAliasForLongDataPath`: Unix socket permission failure reproduced. |
| `internal/daemon` | 1 | `TestWiringLifecycleOwnsHerdrSocket`: Unix socket permission failure reproduced. |
| `internal/daemon/supervisor` | 3 | `TestListen_unlinkOnClose`, `TestListen_staleSocket`, `TestListen_basic`: Unix socket permission failures reproduced. |
| `internal/lifecycle` | 1 | `TestHerdrSocketPersistsNativeMetadataAndFencesRuntimeGenerations`: Unix socket permission failure reproduced. |
| `internal/previewserver` | 4 | Existing persisted-process/group identity/reaping assertions fail identically on base. |
| `internal/processalive` | 1 | `TestAliveReportsZombieAsDead`: expected zombie state was not observed before timeout on both candidate and base. |

The seven herdr failures are `TestSocketPermissionsAndCleanup`, `TestSocketReplacesOnlyStaleSockets`, `TestSocketMalformedAndOversizedMessagesAreNonfatal`, `TestSocketConcurrentClients`, `TestSocketBoundsIdleClientsAndStopsPromptly`, `TestSocketCleanupPreservesReplacementPath`, and `TestSocketBoundsConnectionCount`.

The five conpty failures are `TestLinuxPTYCloseReapsTermIgnoringProcessGroup`, `TestLinuxPTYCloseReapsOrphanedBackgroundJobWhenLeaderExitsEarly`, `TestLinuxPTYCloseReapsDescendantWithOwnProcessGroup`, `TestLinuxPTYCloseDoesNotKillUnrelatedProcessAfterPIDReuse`, and `TestLinuxPTYCloseDoesNotKillRecycledPIDDuringGracePeriod`.

The four previewserver failures are `TestReapPersistedProcessesKillsVerifiedProcess`, `TestSignalPreviewGroupRefusesMismatchedStartTime`, `TestSignalPreviewGroupKillsVerifiedGroup`, and `TestForceKillLeaksOrphanedGroupAfterRootIsReaped`. Baseline reproduction establishes that these are not newly introduced failures; it does not identify an unobserved syscall cause for the process-observation cases. The required local gate stays failed pending normal CI evidence.

Directly changed packages in the original complete run passed: OpenCode adapter (5.488 s), ACP (38.278 s), native ACP (1.015 s), OpenCode ACP (1.018 s), Backbone verifier (2.066 s), Instructor service (120.589 s), Chat service (263.584 s), and session manager (209.906 s). The daemon package failed only its baseline socket test. The complete SQLite migration and store packages finished successfully (743.132 s and 252.353 s); no slow tail or test was cancelled. Earlier focused runs are supporting development evidence only and do not substitute for this complete run.

## Native, producer and qualification evidence boundaries

Both the complete original race run and the final affected-package race run set `AO_BACKBONE_NATIVE_BINARY` to the hash-verified Linux OpenCode 1.19.0 binary, SHA-256 `135ac3f86c43f2a22328a3554fec5f0bc57d71576077902661bc38dac34871fd`. Consequently the optional positive native readback test ran instead of skipping. It validates actual effective configuration, primary agent, full producer-context prompt, pinned model/effort and permission projection, without model inference. Unit protocol/lifecycle tests and the canned provider upgrade fixture are separate evidence. They are not a real-model qualification, a verified provider model snapshot, or OS process confinement. No previously blocked dynamic adversarial native reproduction was retried.

The verifier fixture comes from the newly reconstructed Engineering Backbone producer, using a real Copier consumer and installed resolver. Its approved consumer commit is `c77601c5fcd96188ddeca71b3cc9429a2d692a51`, bundle SHA-256 `69cb616afcc14efc7c25a667a797d4e17a3b1ec6e4e26ea9df75ac896e7709ad`, and companion SHA-256 `c469f3b004a73ffc48f26129c6b6239beeed74b9807a521472ef596712738d71`. Fixture provenance is retained with the committed fixture. This establishes producer/consumer compatibility, not qualification of a runtime/model tuple.

No qualified seed is shipped. Productive use requires an explicit, matching operator qualification for the exact model, runtime, mapped role, platform, interface and configuration contract, with all six required cases and evidence. Optional `modelSnapshot` is operator attestation metadata; this implementation does not independently query a provider to resolve or verify that snapshot. Qualified admission initially supports fresh ACP/Chat implementer sessions with default permissions; bound TUI and live-host adoption are refused. Mapped unsupported harness/role contexts fail closed; unmapped other harnesses preserve their existing behavior and carry no verified Backbone chain claim.

The Windows workflow adds actual Windows Go path/closure tests. Those tests have not run locally, and the added job does not provision `AO_BACKBONE_NATIVE_BINARY`, so it must not be described as native AO Windows readback evidence. The paired Backbone's separate native Windows smoke is also not AO readback evidence. Actual Windows/macOS execution, controlled real-model qualification and any unavailable Docker/credential-dependent gates remain distinct. The Linux readback cannot establish them.

The candidate daemon used by the independent MCP and upgrade checks has SHA-256 `1c2c1b7f592ddfcadf8130b3f63d80b62d86a2a62e6295b68815944cf2fe2632`. It was built from the original frozen product bytes just before their source checkpoint, with embedded `vcs.revision=e99ecde34469c70323381b1867958c578455c0bb` and `vcs.modified=true`; it does not embed the later checkpoint revision or contain the final macOS-only lint spelling correction. Source manifests and hashes, not an invented embedded revision, bind that evidence. See `opencode-backbone-auxiliary-checks.md` for complete MCP/CLI/upgrade passes, frontend/cloud baseline failures and browser/desktop/native-runner gaps. That report retains its original source attribution.

## Retained evidence hashes

Paths below are relative to the workspace containing this checkout. Empty successful build/vet logs legitimately share the empty-file hash. JSON records retain exact commands, exit codes, duration and timestamps; no log was rewritten to hide a failed attempt.

| Evidence | SHA-256 |
| --- | --- |
| `artifacts/ao-full-checks.json` | `0e463dd79cfe7a351fb46fa1cc634596afddeb750a3eb74e4e8d382fc9e9bd9a` |
| `ao-full-build.log` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `ao-full-vet.log` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `ao-full-race.log` | `ff6d8917bdcc2fe0ce565ba3e49556f033c3356278ce2ac4b207649d74f10488` |
| `ao-full-lint.log` | `6b7fe430a2be4c7182cfdbcaa9fab62a2020fd8ca4a4879fdbb1acbee33c8186` |
| `artifacts/ao-postlint-checks.json` | `bf08a04f0ba53c44f9b8d00a38be35f2e1628287386aa62c6c91399458ae286b` |
| `ao-postlint-lint.log` | `dc371c05543937b4a6c17381bcdc2bc87eb2d1c63f14a9eb55ee277998c7a1c8` |
| `artifacts/ao-lintrepair-checks.json` | `f2b9330be8f5ffc176b3c6dc827287159e41fa2a1300e15f240d5d6fbaf9bb91` |
| `ao-lintrepair-build.log` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `ao-lintrepair-vet.log` | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| `ao-lintrepair-opencode-race.log` | `0e7601471792cfbc22677bc1e24664dff3a498e820815d010e80d955630bfe5c` |
| `ao-lintrepair-lint.log` | `e92606b0bf483111dff0a120c315ea165821348f31365020e2468a0059095c47` |
| `ao-api.log` | `bdc8eb5d0d53a94c42881d9423ac9750228a07bc9cdd8ad0a65ac0855c793f33` |
| `ao-sqlc.log` | `0e7535e9023e2b84b999eb87242e1c90f4e1daa17e4d7e188d5cf9d86c8f67f5` |
| `ao-baseline-herdr.log` | `d4bb9ef0415eb4bf8f3ebd30366bb01a66320f2772be0fb230feb5e644c2e70e` |
| `ao-baseline-pty-browser.log` | `de26f691d64d09de52d5f41b9d95da435675523af937745b5aeccd19e01a7a06` |
| `artifacts/ao-baseline-socket.log` | `c46b09bdb4c796f93cf7d01dc7e88169ca92fc37e88b9380b7695de276eb3b5d` |
| `ao-baseline-supervisor.log` | `cfa624f69b1e452730e31047a1e478b239d3696f72cab7f00fcc92a23420008b` |
| `ao-baseline-lifecycle.log` | `1d24afa7c71bc2ac134ae9feac93fc32df2102e7a8981b4b5de5c08f66fc2ab9` |
| `ao-baseline-preview-processalive.log` | `58d7b6e5badc27736b39e9b7761cec6ae2a35a024d2fbc77147d2386745642e6` |
