# OpenCode Backbone resumed independent Full security review

Date: 2026-10-18. Reviewer: independent `ao_resumed_full_security` session.
Role: Full authority/security reviewer, separate from implementation, behavior
review and triage. This is a fresh review after workspace maintenance removed the
unpublished review artifacts. It does not reconstruct or impersonate the lost
earlier review, and it does not claim to rerun the historical quality checks.

## Immutable reviewed snapshot

- Repository: `Binary-Refinery/agent-orchestrator`.
- Base: `6b1ba43f1df9415c1ec937876ce54c86aa37484e`.
- Head: `dcd187616f8b92cd668e75184fd5d697ac40fc66`.
- Tree: `166946128c6104a640edac5c301660067feb6ebc`.
- Fresh patch identity: `git diff --binary 6b1ba43f1df9415c1ec937876ce54c86aa37484e HEAD | sha256sum`
  returned `8b8a9a50832853a083229c287ed477c2e10b8f426b65dcd26939889b0507e870`.
  This explicitly binds the current command output; no historical patch digest
  with unspecified rendering options is substituted.

The checkout initially had no tracked modifications. The parallel resumption
record is outside this frozen commit. This reviewer changed only this new report;
no product, test, workflow, remote state or commit was changed.

| Inspected authority/evidence | SHA-256 |
| --- | --- |
| Approved plan | `458647f41d6a982f2b66c78a9dcad1a41505da38de5613a2ce3744f24c0e7e2b` |
| Task Packet | `85c10817af006fa665afa5e543746c510cabf3869b1627712c2f16a11062ebe7` |
| Plan review, including isolation clarification | `244ce89a6e3a36f248c58c1ed5f4a9788a64a93532adad7dab06e7ef1c04ec53` |
| Backend checks | `bf37914d743cb6ef22d0de7f11eb819158e4eb02195333860e4223303a47fff8` |
| Independent quality audit | `7eb3f0b3e80e33a9e0b2f4bf806107b897933ab65cd246647de8b67ce6cfb3d8` |
| Auxiliary checks | `371ad0f96e52e09ee10dc228790e8630b61418383250ccbb07b7df306f0ec9f9` |
| Windows workflow | `7583f57eb6d5a7e0229c2643fc6006c3c2aca043777730e712c8490494e423ef` |
| Actual producer Git bundle | `69cb616afcc14efc7c25a667a797d4e17a3b1ec6e4e26ea9df75ac896e7709ad` |

## Conclusion

**One confirmed in-scope P2 finding: REC-AO-SEC-001.** The Windows workflow's
test-name filter omits most of the new verifier package. The finding requires
separate triage and a bounded repair before review closure. No additional
confirmed in-scope runtime authority bypass was found in this Full review.
This conclusion is not an all-checks-passed, real-model-qualified or
OS-confinement attestation.

## REC-AO-SEC-001 — Run the complete verifier package on Windows

Priority: **P2**. Location: `.github/workflows/go.yml`, the newly added
`Test Backbone governed OpenCode boundaries` step in `windows-workspace`
(lines 107–108 in this snapshot).

The new command applies `-run 'Backbone'` to `./internal/policy/backbone` along
with seven other packages. The verifier package has ten top-level tests. Static
enumeration of every `func Test...(t *testing.T)` declaration in its two test
files and application of that literal regular expression selects only
`TestActualBackboneProducerReceipt`. It omits all nine tests below:

| Omitted top-level test | Relevant boundary |
| --- | --- |
| `TestVerifyBoundContextAndNativeClosure` | Full prompt, enclosing role and all canonical native inputs |
| `TestVerifyRejectsChangedContextInputs` | Selected/nonselected roles, skills, anchored file and Task bytes |
| `TestVerifyRejectsMissingMandatoryDocumentsAndStaleHead` | Missing required documents and stale repository revision |
| `TestStrictJSONRejectsAmbiguityAndNulls` | Duplicate/unknown/case-folded fields, nulls, Unicode, size and depth |
| `TestPacketNullableRevisionsAndFrozenGrammar` | Frozen Task identity, revision and packet grammar |
| `TestPortablePathsAndFences` | Portable paths, traversal and Markdown fence handling |
| `TestVerifyRejectsSymlinksCaseAmbiguityAndOversize` | Symlink, case mismatch and file-size rejection |
| `TestCanonicalGitReadIgnoresReplacementAndInheritedRepositoryOverrides` | Replacement objects and inherited Git overrides |
| `TestCanonicalDigestUnicodeMatchesProducerEncoding` | Producer-compatible canonical JSON bytes |

The Linux full suite covers these names, but it cannot establish Windows path,
case, reparse or Git/environment behavior. The actual-producer positive test
alone does not supply the negative boundary evidence required by the approved
plan and Task AC-6/AC-7. No other Windows workflow command runs this verifier
package. Thus the newly added Windows job can succeed while omitting the intended
verifier coverage.

Recommended bounded repair: give `./internal/policy/backbone` a separate complete
Windows command with the existing race detector, uncached execution and bounded
timeout, without a test-name filter. Use verbose output so individual negative
subtests and skips remain visible. Remove that package from the existing filtered
command and retain its other seven packages and existing options. No product
change or broad test-selection rewrite is needed for this finding.

The existing symlink subtest explicitly skips if the OS refuses symlink creation.
Do not convert such a skip into a verified Windows symlink result. Actual Windows
CI must show the ten top-level tests and the individual symlink/case/size results;
a package-level PASS alone does not close a platform-specific evidence gap.

This finding retains the earlier communicated stable ID for the same defect, but
the confirmation above comes from fresh inspection of the recovered immutable
source, not from a recreated earlier report. No Windows test was executed in
this reviewer session.

## Full security coverage

Read `AGENTS.md`, the approved plan and clarification, Task, plan review, operator
documentation, all changed product Go files, the workflow change and the relevant
regression tests. Traced the changes against their existing call sites rather
than treating added helper functions as proof that an admission path uses them.

- **Policy authority and qualification:** optional omitted raw binding preserves
  the unbound serialization path; existing policy digest remains the authority.
  Binding keys are restricted to enclosing roles. OpenCode roles in a bound
  policy require a matching explicit implementer/model/runtime/platform/Chat
  attestation and all six exact cases. Unmapped other harnesses retain their
  ordinary behavior. No default qualified record is introduced. Evidence URL
  truth, signatures and optional model-snapshot metadata are not independently
  authenticated, as explicitly documented by the approved contract.
- **Data-only context verification:** bounded strict JSON and schema validation,
  context digest, safe portable paths, ancestor/file checks, full-file hashes,
  Markdown anchors, mandatory documents, explicit Task identity and revision
  grammar, actual Git HEAD and all thirteen canonical role/skill inputs. Fixed
  raw Git object reads suppress replacement/lazy fetch and inherited Git
  overrides; candidate resolver/check declarations are not executed. The real
  producer fixture and its complete receipt are included.
- **Native bootstrap and effective configuration:** closed project config shape,
  checked native namespaces and implicit instructions, isolated per-session
  home/XDG/state, selected-provider credential allowlist, ownership marker,
  managed/global configuration rejection, native auth/account-state refusal,
  offline package metadata constraints and empty package-code directories.
  The verified full prompt is carried in a checked AO-owned file, with native
  variable expansion escaped. Source validation precedes each bounded native
  readback. The binary, cwd and merged environment used for readback and launch
  are connected through the native ACP launch path.
- **Permissions and native selection:** the generated `ao-default` primary is
  the checked selection; explicit stricter denies survive projection, edit/bash
  remain ask-or-deny, and delegation/external-directory rules are constrained.
  Effective ordered permission readback is compared after the final wildcard.
  Initial ACP configuration establishes model/effort and checks provider-reported
  mode/model/effort. Reachable service mutation routes reject bound controllers
  before provider setters; resume refuses incompatible stored settings. Native
  slash commands and explicit compaction are refused, and automatic compaction
  is disabled for this fixed contract.
- **Lifecycle and dispatch:** preflight avoids ordinary native discovery for
  governed launches; workspace verification follows workspace creation and
  reaches async launch, recovery and fresh-process resume. Governed TUI,
  background inference outside the controller and interface handoff are refused.
  Live host adoption detaches rather than claims a fresh verified process. The
  exact policy/role/model/effort binding and current document bytes are checked at
  controller admission and queued dispatch, again after durable provider-ID
  binding, and at native deferred submission. History replacement paths carry
  the binding and existing semantic/controller-generation fences. Existing
  ownership and durable work controls remain in use.

For the steering question, this reviewer independently fetched the exact upstream
OpenCode source at commit `733b2c627e5aec39cb33cc22373bdce33053ef2c` through GitHub:
`packages/opencode/src/acp/agent.ts` blob
`a7c59a2615519d6ebe5b237181bd5fadd8233b90` and
`packages/opencode/src/acp/service.ts` blob
`c542e2b75320b656549740c293794103908bc16f`. The agent interface and initialization
response do not implement or advertise the steering extension used by AO's generic
ACP steering path. A hypothetical extension in a different runtime is not a
demonstrated bypass in the exact admitted 1.19.0 contract. Also inspected the
pinned configuration-loading module, blob
`9e10b67fe703609a3ccf243ebe1801bf338961d3`, and instruction-loading module, blob
`7f593550d468fa3ae5dbc6c04ce53f317bb72533`, for bootstrap ordering, remote auth/org
configuration, implicit instructions and managed inputs. These were source reads,
not execution or a new native provenance/behavior qualification.

## Evidence and execution limits

Fresh work in this review consists of source/diff inspection, static test-name
selection analysis, byte hashes and read-only pinned upstream source retrieval.
No native executable, provider, model, complete test suite, malicious adversarial
reproduction or production operation was run. No implementation or workflow
repair was performed by this reviewer.

The durable backend, auxiliary and audit reports remain historical evidence with
their original source and command bindings. Their raw logs lost in maintenance
were not recreated or independently rehashed here. The recorded complete local
backend race gate failed with 23 exact-baseline counterparts; the report's final
lint/build/vet and affected adapter results do not rewrite that full-suite
outcome. Frontend/cloud failures, renderer/desktop blockers and unavailable
native platform/Docker checks stay distinct from successful commands.

The recorded Linux native positive readback made no inference request. The AO
Windows workflow does not provision `AO_BACKBONE_NATIVE_BINARY`, so even a fixed
Windows verifier command does not establish AO native Windows readback. The
paired Backbone native smoke is separate evidence. Operator model qualification,
provider snapshot verification and OS process confinement are not established by
this review. Actual remote CI and visible Codex review remain publication gates.

Separate triage must adjudicate REC-AO-SEC-001. A bounded workflow repair needs
independent quality/Delta verification, followed by actual Windows result and
skip inspection. No merge, release, activation or settings change is authorized
by this report.
