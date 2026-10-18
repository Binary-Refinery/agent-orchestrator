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
