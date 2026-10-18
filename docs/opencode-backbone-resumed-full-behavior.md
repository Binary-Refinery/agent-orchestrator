# OpenCode Backbone resumed independent Full behavior review

Date: 2026-10-18. Reviewer: independent `ao_resumed_full_behavior` session,
separate from implementation, security review, validation execution and triage.

This is a fresh Full review after workspace maintenance removed the unpublished
review records. It does not reconstruct or claim the bytes of the lost behavior
report. The product snapshot was restored from its remote checkpoint; historical
command results remain attributed to the source and date in their durable reports.

## Immutable review binding

- Base: `6b1ba43f1df9415c1ec937876ce54c86aa37484e`.
- Reviewed head: `dcd187616f8b92cd668e75184fd5d697ac40fc66`.
- Reviewed tree: `166946128c6104a640edac5c301660067feb6ebc`.
- Fresh patch SHA-256: `8b8a9a50832853a083229c287ed477c2e10b8f426b65dcd26939889b0507e870`,
  from `git diff --binary 6b1ba43f1df9415c1ec937876ce54c86aa37484e HEAD | sha256sum`.
  This is the digest produced by the restored checkout's explicit command,
  not the differently rendered historical patch digest.
- Approved plan SHA-256:
  `458647f41d6a982f2b66c78a9dcad1a41505da38de5613a2ce3744f24c0e7e2b`.
- Task SHA-256:
  `85c10817af006fa665afa5e543746c510cabf3869b1627712c2f16a11062ebe7`.
- Plan-review SHA-256:
  `244ce89a6e3a36f248c58c1ed5f4a9788a64a93532adad7dab06e7ef1c04ec53`.

All paths and line references below address that immutable snapshot. No product
file, dependency, gate, permission, branch or remote state was changed by this
reviewer. The only repository output is this report. A separately authorized
diagnostic overlay was prepared outside the checkout and handed to the validation
owner; it neither modifies the product tree nor invokes OpenCode.

## Findings

### REC-AO-SEC-001 — P2: Windows command omits nine verifier test groups

Location: `.github/workflows/go.yml:107-108`.

The new Windows step applies `-run 'Backbone'` to
`./internal/policy/backbone`. Of that package's ten top-level tests, only
`TestActualBackboneProducerReceipt` contains `Backbone`. Static enumeration of
the actual declarations and the exact expression confirms that all nine groups
in `context_test.go:104-355` are excluded: selected/native closure, changed
inputs, missing documents/stale HEAD, strict JSON, frozen packet grammar,
portable paths/fences, symlinks/case/size, Git replacements/environment, and
Unicode canonical encoding.

The planned native Windows boundary evidence therefore cannot be obtained by
this step even if it passes. The verifier has Windows-specific reparse handling
in `files_windows.go:8-14`; Linux results do not close that platform obligation.
This finding concerns CI selection, not an observed Windows runtime bypass.

Bounded correction: run the complete verifier package in a separate Windows
step, retaining race detection, uncached execution and the existing timeout;
keep the remaining integration-package selection. Verbose actual Windows output
should distinguish the symlink subtest's result from a possible explicit skip.
This independently observed issue uses the existing canonical Windows-selection
identifier `REC-AO-SEC-001`; it is one issue shared with the security review, not
an additional independent defect.

### REC-AO-BEH-002 — P2: governed project resolution can persist an invalid work policy

Location: `backend/internal/service/instructor/project_policy.go:137-148`.

The governed branch avoids `ResolveProjectRole`, which is necessary to defer
native discovery. It also bypasses that function's explicit nonempty effort
requirement (`project_policy.go:168-170`). Its replacement
`validateSelectionConstraint` deliberately accepts incomplete project drafts
when `selection.Effort == ""` (`project_policy.go:229-230`). That permissive draft
check is insufficient at durable work registration.

With an otherwise valid project-sourced orchestrator role using
`Effort: "configured"`, an explicit OpenCode model, a matching synthetic admission
attestation and no configured/native-default effort, the new branch copies the
empty selection into `resolved.Effort`. `Register` validates the template before
resolution (`service.go:274-282`) and then persists the resolved work
(`service.go:300-324`) without revalidating the result. The resulting policy fails
the existing required-effort rule (`policy.go:95-96`). The unbound path rejects
the same incomplete selection before persistence.

This is a registration correctness regression: the operation can report success
and leave a work whose own pinned policy is invalid. It is not evidence that
OpenCode runs without an explicit effort; later launch/resume admission still
rejects the invalid or incomplete selection. A bounded correction should retain
the data-only requirement for explicit harness/model/effort before entering the
governed resolution path, while continuing to defer native capability discovery.

The separate validation owner reproduced this using Go 1.27.1 on the exact
reviewed product snapshot. I inspected the completed result and raw log and
rechecked the log hash. The diagnostic's operator qualification is synthetic
test data only; no OpenCode process or model provider was invoked.

Command, from `backend/`:

```text
go test -overlay=/workspace/scratch/6e019b0851cc/review-behavior/overlay.json -p=1 -count=1 -timeout=2m -run '^TestResumedBackboneRegistrationRejectsMissingProjectEffort$' -v ./internal/service/instructor
```

Started `2026-10-18T08:54:27.319131Z`; exit 1, 78.860 seconds wall including
initial dependency compilation, 0.819 seconds for the test. `unbound-control`
passed: validation error and no work persisted. `governed` failed: registration
returned nil error, persisted effort was empty, `ValidatePolicy` reported
`role orchestrator needs explicit effort`, and one invalid work was persisted.
The validator confirmed all 2,043 backend source hashes and dependency inputs
unchanged before and after this diagnostic.

| Diagnostic evidence | SHA-256 |
| --- | --- |
| Scratch `review-behavior/missing_effort_test.go` | `d6c9e6d715300d29675eaf5508bdcdaf1714f7a14e2a90d8bf8e9f57feb58338` |
| Scratch `review-behavior/overlay.json` | `a009b3abe775d8272f8a9321b04ab4a4253262e10842a43360c575a308c03c86` |
| Scratch `validation-restore/missing-effort-before/go-test.log` | `c8a41d3c3a4a7c544f50bb1ea1a15ac62686c80fc2360bd605cc5a9a084683d4` |

## Full behavior coverage

| Boundary | Inspected behavior and evidence |
| --- | --- |
| Optional policy and compatibility | `domain/instructor.go:224-229`, `service/instructor/policy.go:53-65`, `backbone.go:44-57,97-116`, and the complete changed project-policy/launch code. Absent `backbone` remains omitted, explicit null is rejected, and policy hashing keeps the existing mechanism. Unmapped other harnesses retain ordinary admission. Every OpenCode role under a binding needs an explicit context; mapped unsupported harnesses and non-implementer roles fail closed. The project-resolution exception above is the confirmed defect. |
| Qualification and actual work identity | `service/instructor/backbone.go:59-116,120-205,216-265`; existing `launch.go:256-364` and membership/dispatch callers. Exact runtime/model/role/platform/interface/configuration contract and all six cases are required; evidence strings/digest/time are checked. This is operator attestation, not independent evidence retrieval or a provider snapshot check. Controller binding retains policy bytes/version, current Instructor role, model and effort. Existing work/session-incarnation lookup remains authoritative. |
| Data-only context verification | Complete `policy/backbone/{context,json,files,files_unix,files_windows,git,packet}.go` and their new tests/producer fixture. Strict bounded parsing, exact field spelling, duplicate/null handling, current Git HEAD, full-file hashes/heading anchors, mandatory native documents, Task Packet identity and frozen/nullable revision grammar were traced. Canonical selected inputs use receipt hashes; other fixed role/skill inputs use bounded raw Git blobs with inherited overrides and replacement/lazy-fetch behavior disabled. No declared check or candidate resolver executes. |
| Fresh/async launch and fallback | `session_manager/manager.go:948-1077` and both synchronous/background routes to `launchChatController`; `chat_spawn.go:224-263`; `service/chat/service.go:290-311,396-405,470-477,635-645,673-771`. Preflight precedes ordinary native discovery, context is checked after workspace materialization, the verified prompt suffix reaches the actual driver, and governed default Chat cannot fall back to TUI. Admission still uses existing durable launch ownership and controller publication fences. |
| Resume/recovery/interface/background work | `daemon/instructor.go:342-399`, `instructor_recovery_guard.go:14-45`, `session_manager/manager.go:2551-2613,2882-2910`, `chat_spawn.go:96-109,445-532`, and `interface_transition.go:168-179`. Controlled recovery defers native discovery until its guarded ACP boundary; other roles retain their original validation. Stored changed settings, TUI handoff, unqualified background inference and live-host adoption are refused. A refusal does not assert that an existing process died. |
| Queue dispatch and child contexts | `service/chat/backbone.go:64-94`, `controller.go:1727-1759,1818-1874`, `service/instructor/admission.go:17-89`, and `launch.go:117-197,292-314`. Both ordinary queued work and managed directives retain their existing authority/admission route. The exact running binding is checked before send and again after durable ACP turn preparation. Child roles select their own pinned role context; unbound other harness children gain no verified Backbone claim. ACP rechecks fixed input bytes before preparation, deferred start and actual prompt submission (`acp/conversation.go:390-433,605-665`). |
| History and configuration | Complete changed `service/chat/history.go`, including edit, fork, branch activation and failed-handoff recovery; `service.go:1950-2035,2228-2261`; `controller.go:1653-1664`; `acp/backbone.go`, `config_options.go`, and initial setting application. Replacements carry cloned binding bytes and current validation hooks; stale history operations are refused before provider work. Public configuration mutation is rejected before native setters/catalog calls. Initial controlled session setup must establish and read back exact model/effort/`ao-default`; subsequent turns check reported values rather than accepting an acknowledged setter. |
| Native source closure and actual prompt | Complete `agent/opencode/backbone.go`, `backbone_sources.go`, and `chatdriver/{opencodeacp,nativeacp,acp}` delta. All selected document bytes are rendered into `ao-default`; role edit/bash asks retain stronger project denies, internal task/delegation and external inputs stay denied. Environment/state isolation, owned config bytes, alternate/global/ancestor sources, package/account state, bounded native readback and equal probe/launch inputs were traced. Native slash commands and compaction are refused. No OS confinement or arbitrary platform compatibility is inferred. |
| Public contracts and tests | The generated OpenAPI/TypeScript delta only adds the optional policy field; docs describe the same initial contract and limitations. Reviewed all added boundary-test files, fixture provenance and the Windows workflow. No migration, listener, dependency, default activation, release or deployment change is present. The workflow-selection omission is recorded above. |

## Rejected speculation and evidence limits

The generic ACP steering extension bypasses ordinary prompt preparation, so I
checked whether it supplies a supported route in the exact pinned runtime.
Fresh read-only retrieval of `anomalyco/opencode` commit
`733b2c627e5aec39cb33cc22373bdce33053ef2c` returned:

- `packages/opencode/src/acp/agent.ts`, blob
  `a7c59a2615519d6ebe5b237181bd5fadd8233b90`: the complete agent methods have no
  steering/extension handler.
- `packages/opencode/src/acp/service.ts`, blob
  `c542e2b75320b656549740c293794103908bc16f`: the service interface and
  initialization response contain no steering handler/advertisement.

I therefore did not report an unproven steering bypass for the pinned 1.19.0
runtime. This is static source evidence, not an executed protocol or future-runtime
compatibility test. No previously blocked malicious native reproduction was retried.

Read and hash-checked durable records:

| Record | SHA-256 |
| --- | --- |
| `opencode-backbone-backend-checks.md` | `bf37914d743cb6ef22d0de7f11eb819158e4eb02195333860e4223303a47fff8` |
| `opencode-backbone-auxiliary-checks.md` | `371ad0f96e52e09ee10dc228790e8630b61418383250ccbb07b7df306f0ec9f9` |
| `opencode-backbone-quality-audit.md` | `7eb3f0b3e80e33a9e0b2f4bf806107b897933ab65cd246647de8b67ce6cfb3d8` |
| Actual producer `workspace.bundle` | `69cb616afcc14efc7c25a667a797d4e17a3b1ec6e4e26ea9df75ac896e7709ad` |

The October 16 reports retain failed complete backend/frontend/cloud gates,
their baseline diagnoses, positive bounded/Linux readback results, and missing
browser/desktop/native-platform evidence. This review did not recover and reaudit
their removed raw logs or rerun those suites, and does not convert them to green.
Actual Windows execution, normal remote CI and visible Codex review remain
separate gates. A native Windows verifier pass would still not establish AO native
Windows readback, actual model qualification, a provider snapshot or OS confinement.

The two findings require separate triage and scoped correction/Delta review.
No merge, release, deployment, downstream activation or productive qualification
is authorized by this report.
