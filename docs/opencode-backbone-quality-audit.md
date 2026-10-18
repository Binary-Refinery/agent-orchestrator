# OpenCode Backbone independent quality evidence audit

Date: 2026-10-16. Auditor: independent evidence-review agent, separate from the implementation and command owners.

Outcome: the inspected reports accurately distinguish successful commands, failed required suites, baseline reproductions and unavailable platform gates. No material evidence discrepancy was found. This is an audit of retained evidence and source attribution, not a rerun of the suites, either required Full review, or an all-checks-passed attestation.

## Inspected source and evidence

I read `AGENTS.md`, the approved plan and Task, the auxiliary report and the completed backend report. The inspected final source HEAD is `19ef9159c1937e6bd6c88f980cf5bdf3a31f7281`, tree `231f62b1acb44aeefa43d11585c2e383d665c13c`. The backend report was the only untracked file before this audit was added; there were no uncommitted product changes. This audit does not change source or either owner's report.

| Inspected record | SHA-256 |
| --- | --- |
| `docs/opencode-backbone-auxiliary-checks.md` | `371ad0f96e52e09ee10dc228790e8630b61418383250ccbb07b7df306f0ec9f9` |
| `docs/opencode-backbone-backend-checks.md` | `bf37914d743cb6ef22d0de7f11eb819158e4eb02195333860e4223303a47fff8` |
| Original backend source manifest | `79663d35b2fefda226f143e934113cdb77390cd565c6ea5a394afd0e18b85710` |
| Final backend source manifest | `1f8a38e706150d9ec1cd231668a1cd0de37f5efee96c180f30a89c2d19898400` |
| Original full command record | `0e463dd79cfe7a351fb46fa1cc634596afddeb750a3eb74e4e8d382fc9e9bd9a` |
| Final lint-repair command record | `f2b9330be8f5ffc176b3c6dc827287159e41fa2a1300e15f240d5d6fbaf9bb91` |

The 1,513 auxiliary input paths and hashes still match, including generated frontend API types. All 2,043 entries in the final backend manifest match the inspected files. Comparison with the original manifest finds exactly one changed file, with no additions or removals: `backend/internal/adapters/agent/opencode/backbone_sources.go`. The 29 retained auxiliary execution-log hashes, the documented canonical source-record hashes and all 20 hashes in the backend report's evidence table match their retained files. Original failed attempts remain present.

## Exact pre-repair and post-repair attribution

The complete backend run and frozen daemon used by MCP/upgrade bind to product commit `2e10d4a6c4a386281d94891e5ebe88fdf8f5116c`, tree `267a34d645381b9e4fbbc2ffdbe477ff87f16c38`. The daemon's recorded hash matches both the original file and the corrected-name upgrade copy. The previous daemon also matches its recorded hash. These executables are not represented as containing the later lint repair.

I inspected the only subsequent product delta: the macOS-only managed-preferences base literal is assigned to a local variable and reused in the same two paths. Original Git blob `5e0a73b84b9ac06203666630d083a45b088979c0` becomes `0425a6eeb740b4c28ab62e421bbf696cf88d7b7e`; the final file SHA-256 is `c92b8e2817dc5d150acffd629e1e64282d41d04c26e6d22322f81235edbe3bbe`. Both paths and control flow are identical; Linux and Windows branches are unchanged. No lint suppression was introduced. The original lint failure and the unsuccessful intermediate spelling remain recorded.

On the final bytes, complete lint reports zero issues, complete build and vet exit zero, and the complete OpenCode adapter race package exits zero. The earlier complete backend race suite was not rerun on these repaired bytes. Its evidence remains attributed to its original snapshot; semantic equivalence and the affected-package rerun do not rewrite that history.

## Independently checked outcomes and diagnoses

| Gate | Raw evidence verified |
| --- | --- |
| Complete original backend race | Exit 1; 179 packages passed, eight failed, 13 had no tests. Exactly 23 top-level failures; no race-detector warning or panic. |
| Backend baseline diagnostics | All 23 failing names and corresponding diagnostics recur in six retained logs against clean baseline `6b1ba43f1df9415c1ec937876ce54c86aa37484e`. Thirteen concern explicit Unix-socket permission failures; ten concern process/PID observations. The latter evidence does not establish a specific unobserved syscall cause. |
| MCP / CLI / upgrade | MCP 124 passed, no failures or skips; CLI 404 top-level passes and one existing Darwin-only skip; corrected-input upgrade four passes, no failures or skips. Both upgrade binary names now match CI and their bytes are unchanged. |
| Frontend | 5,569 passes, five failures, six existing skips. Four failures explicitly report Unix-socket EPERM; the native browser failure reproduces with exact original test/runtime/bridge blobs. Its precise native cause remains unknown. |
| Cloud | The full race command failed two unchanged bridge tests. Source inspection confirms the listener must start before the safety-net timer. The socket failure and dependent timeout are consistent with that restriction; the gate remains failed. |
| Renderer / desktop | Renderer tests cannot start the absent pinned Chromium; the retained 195-byte download is HTML, not an archive. Desktop's ownership-only extraction retry reaches missing `yacc`; it does not produce a package. |

The six frontend skips and the CLI skip are unchanged baseline code. Workflow definitions substantiate the pinned Playwright container and Ubuntu 22.04 desktop runner with bison/pkg-config prerequisites. Baseline reproduction explains observed failures; it does not convert required failed gates to PASS.

## Remaining gates

Actual remote CI, native Windows/macOS execution and unavailable Docker-dependent jobs remain open. The final affected-package result and the owner's Linux native readback evidence do not establish native AO Windows readback, real-model qualification, provider snapshot verification or OS confinement. The paired Backbone Windows smoke is separate evidence. Both independent Full reviews, separate triage and any required repair/Delta review still follow this audit. No merge, release, deployment or downstream activation is certified.
