# OpenCode Backbone publication state

This record describes the pre-publication checkpoint on 2026-10-18. Actual PR
head/tree verification, category counts, visible Codex request and subsequent CI
outcomes are recorded in the PR conversation. It does not approve activation,
merge, release or deployment.

## Source and preservation checkpoints

GitHub's commit API produces different commit identities from local commits;
each corresponding tree below was compared exactly before the guarded branch
update. No force push was used. Both repository base refs were unchanged when
work resumed.

| Checkpoint | Local commit | Remote commit | Exact common tree |
| --- | --- | --- | --- |
| Recovered Full-review source | `dcd187616f8b92cd668e75184fd5d697ac40fc66` | `dcd187616f8b92cd668e75184fd5d697ac40fc66` | `166946128c6104a640edac5c301660067feb6ebc` |
| Fresh Full reports and separate triage | `9bb8edd5f330229624d3552ae73d1463eabbac5b` | `2a5529c85526dea140ad714d035d209e7947b8a6` | `93093bb91254435cf034949c9424a05bbaf7d9dd` |
| Two bounded source repairs | `f478fc100e854634f369655d067c752ea639819c` | `03ea7ac6e80e84e675bdc2cc576a3dbf063436db` | `57dd202ec151a674f39237148bcb628b1b43db2e` |
| Final repair evidence reviewed by Delta | `53f23301347c52b61bc676942af6b06589414024` | `43792ec05c71a0bf99c695b8f0622cca7bef2522` | `cca674a371f65a543809941e1ab1a973d8463424` |

Base: `6b1ba43f1df9415c1ec937876ce54c86aa37484e`. Branch:
`feat/opencode-backbone-governance`. The repair changes only the explicit
project-selection guard, its registration regressions and the Windows verifier
command selection; other product, native-adapter, generated API and dependency
inputs remain unchanged from the recovered Full snapshot.

The source-repair checkpoint is preservation, not test acceptance. See the
independent repair-check, Delta and separate triage records for their exact
execution and review bindings. Later report/acceptance commits add evidence
without changing those tested product inputs.

## Evidence interpretation

The durable October 16 backend/auxiliary/audit reports retain their original
commands, dates and source identities. The full local backend race run failed
with 23 failures reproduced on the unchanged base. The later equivalent macOS
path-expression correction was checked separately; it does not retroactively
change that full-run result. Frontend/cloud failures and renderer/desktop/native
platform gaps remain explicit. Lost raw historical logs were not recreated.

The October 18 Full reports are newly performed independent reviews of the exact
recovered source. Separate triage confirmed `REC-AO-SEC-001` and
`REC-AO-BEH-002`. Their bounded fixes use fresh failed-before/pass-after evidence
and complete affected-package validation; the independent Delta and closure
must be read alongside the checks. No stale Full report bytes or fictional
complete-suite rerun are presented as evidence.

Fresh independent validation on the frozen repair passed the complete Instructor
race package (179 top-level tests, 447 pass events), the complete verifier race
package (ten top-level groups, 42 pass events), the unchanged external regression
overlay, full backend build, vet and lint (zero issues), formatting, actionlint
and diff checks. Both complete affected packages have zero skips, failures or
race warnings. The independent check report has SHA-256
`77ff1cee2e8fa5b5b4e21ee2c4ce1529b486353604387108a6f65a91e01dc55a`.

The independent Delta on the final repair evidence checkpoint confirmed both
repairs without a new direct-regression finding. Its report has SHA-256
`6f3fe417c0be2af27310e8b08ab5117373b59a84c155dfa3347b4cc901ee26a8`.
Separate closure is appended to the existing triage record, preserving its
pre-repair assessment. Subsequent publication metadata changes no tested product,
test or workflow input.

Task AC-1 through AC-5 record the checked implementation outcomes as pass.
AC-6 remains pending actual Windows path-boundary execution; AC-7 remains pending
complete remote CI with honest platform accounting; AC-8 remains pending actual
PR publication, exact tree/category verification, and the visible Codex result.
The Task stays active and its head revision remains null at this pre-publication
checkpoint. Final head-bound follow-through belongs in the PR conversation;
these historical pending statuses must not be mistaken for final CI outcomes.

Actual Windows CI must expose all ten verifier groups and the symlink/case/size
subtest results. An explicit capability skip remains a gap. This workflow does
not provision `AO_BACKBONE_NATIVE_BINARY`; passing Windows verifier tests cannot
establish AO native Windows configuration readback. The paired Backbone PR 16
has separate successful canned-provider Linux/Windows smoke evidence.

Qualification records are explicit operator attestations, not authenticated
evidence or independent provider-snapshot verification. No qualified seed is
installed. Fresh controlled ACP admission, native smoke, real-model qualification
and OS process confinement are separate claims. Existing user authorization
covers PR publication and visible Codex review; merge and downstream activation
remain separate decisions.
