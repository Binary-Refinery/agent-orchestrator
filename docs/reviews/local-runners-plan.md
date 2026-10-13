# Independent plan review: local CI runners

Review date: 2026-10-13. Role: independent reviewer, read-only. This is a plan review, not an implementation approval, a human signature, or runner activation evidence.

## Reviewed snapshot

| Item | Revision or digest |
| --- | --- |
| AO base and checkout HEAD | `f60f432a32b9f72370dafab2c58b703182dc8026` |
| Backbone base and checkout HEAD | `2cd4d9bd947fbd1aff25c6bec28e98f3fc1b1c13` |
| Both `plans/local-runners.md` files, SHA256 | `6b15b894f64b7833696a298b0faca5c132b9fe30ebf648f75bb9ec8abc010cf2` |
| AO inherited tracked draft, `git diff --binary` SHA256 | `290b3412a129758a5f5a97fc1f510a26d540bb511d7ffca38c36c0a4f912d978` |
| Backbone inherited tracked draft, `git diff --binary` SHA256 | `0be809563d9d25ba6a3c7cb6bd988e765a2376471882863ab2d57416a23a1ec8` |

The tracked draft digests identify context inspected to understand the plan; they do not include untracked plan files and do not attest implementation correctness. The parent identified the existing implementation as an unfinished, unapproved draft. No source, workflow, task packet, or plan was changed in this session.

## Conclusion

One confirmed plan finding requires triage and correction before implementation proceeds. No confirmed Backbone-specific plan defect was found within the coverage below. The proposed missing-evidence stop and draft-PR delivery are appropriate; neither repository is certified ready for activation.

### LR-PLAN-001 — P2 — Preserve PR merge-result validation

- Category: verification coverage / behavior preservation. Confidence: high.
- Location: `plans/local-runners.md:11` in both repositories, specifically the AO instruction to check out the PR head SHA under `pull_request_target`, in conjunction with the preservation requirement at line 8.
- Trigger and path: an AO same-repository PR remains based on an earlier main revision; candidate tests pass on its head, but interaction with newer base changes breaks the merged result. The proposed target dispatcher checks out the isolated head and runs the same tests there, so it misses an integration failure previously exercised by PR CI.
- Evidence: at the AO base, `go.yml:6,29`, `frontend.yml:8,40`, and `cli-e2e.yml:6,31` use `pull_request` and an unqualified `actions/checkout`. Their default test subject is the generated PR merge commit. The proposed plan explicitly substitutes the head commit; the inherited draft implements that substitution in those checkouts. This is a change to the tested source, even if each test command is retained.
- Impact: a green PR validation can no longer establish the former merge-compatibility evidence. This weakens existing CI coverage during a migration whose acceptance requires behavior and gates to be preserved.
- Diff causation: the requirement to replace the PR test subject is new in this migration plan, rather than pre-existing CI debt.
- Minimal correction: retain the original `pull_request` test subject where that event remains, or, if a trusted target dispatcher is retained, resolve and verify the generated same-repository PR merge snapshot and test it explicitly. Bind its parents to the intended base/head revisions; a missing or conflicting merge snapshot must remain blocked or fail, not fall back silently to head-only or default-branch tests. Source-specific checks that deliberately inspect the head may continue to do so.
- Minimal regression check: distinguish candidate-head and merged-result trees in a small Git fixture, and verify that a candidate which passes alone but fails when integrated with the base does not receive the preserved passing PR gate. Check dispatcher checkout selection for PR versus push without changing the native test commands.
- Contract: plan line 8 and Backbone `policy/core.md`, Checks and evidence, require preservation of checks and meaningful workflow failure-path verification.
- Primary reference: [GitHub events documentation](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows), “How the merge branch affects your workflow,” explicitly distinguishes default PR merge testing from head-only checkout. The runtime difference is documented behavior; the failure scenario and impact above are the reviewer's inference from that behavior and the inspected base.

## Conditions for safe implementation and draft delivery

These are existing acceptance boundaries or implementation review watchpoints, not additional confirmed plan defects.

- Runner evidence proves that `af-ci-build-v1` exists and that the `ci-heavy` label alone also reaches a different fast group. Binding Backbone to the build group is appropriate. Evidence supplied by the parent is not an independent observation of Backbone repository access; that access remains unverified.
- AO is a user-owned fork. Organization-runner visibility elsewhere is not evidence of AO access. Dedicated registrations with `ao-ci`, Linux x64, Windows x64, macOS ARM64, and macOS x64 remain proposed prerequisites. Labels cannot manufacture hardware or access. Missing native registrations must keep the relevant jobs blocked and the PR draft.
- Native architecture must follow the original tier, independent of legacy job display labels. The inherited CLI E2E draft maps `macos-latest` to x64, while the inspected artifact and macOS update workflows describe that tier as Apple silicon. This requires implementation correction or actual historical runner-architecture evidence before Full review; preserving only the `matrix.os` display text is insufficient.
- Both Linux desktop paths must retain Ubuntu 22.04 userspace/glibc, native dependency bootstrap, and the original package/smoke commands. Container syntax alone does not prove packaging succeeds or that host runner dependencies are available. Docker and an actual build run remain activation evidence.
- Backbone reusable workflows can preserve external caller hosted defaults because this user's scope is the two source repositories. Source/consumer routing must depend on `github.repository`, not `source_mode`, and tests must cover forged source mode, the source identity, another consumer identity, and failed/skipped/missing mandatory aggregate dependencies.
- Candidate-editable guards are mitigation, not an immutable repository-wide trust boundary. The AO plan acknowledges this. Clean isolated runner operation and independently verified repository controls remain prerequisites for activation in both repositories. No source changes here can certify hosting settings.
- If target triggers are retained, the inherited Version Floor Guard lacks explicit token permissions. Its implementation must establish the plan's read-only-token promise explicitly rather than assume repository defaults. Preserve the existing leaderboard's deliberately limited commenting permission and trusted code reference separately.
- The first AO PR cannot validate its new target dispatcher merely by receiving PR checks: the target workflow definition is taken from the default branch. Until that default contains the new trigger, runs may still execute the previous hosted definitions. Such results are evidence about the previous workflow, not local-runner execution of the proposed dispatcher. Record this bootstrap gap and keep the PR draft if no separate safe runtime evidence is available.
- Current [GitHub target-event security guidance](https://docs.github.com/en/actions/reference/security/securely-using-pull_request_target) says the default public-repository target-event policy is evaluating and is scheduled for enforcement on 2026-11-02 for affected repositories. If target events are retained, independently inspect the actual fork's applicable policy before claiming sustained activation. Whether that default applies to AO was not observed. Any policy setting change requires separate authorization.
- Preserve original fork/upstream-only job exclusions and paused release behavior; a runner migration does not authorize enabling deployment, publication, or previously excluded jobs. No merge, release, deployment, runner registration, or repository-control write occurred.

## Coverage and limits

Inspected the root `AGENTS.md` and `CONTRIBUTING.md` in both repositories; Backbone `policy/core.md`, `docs/repository-controls.md`, and the active local-runner Task Packet; both complete plan files; AO workflow inventory and inherited tracked workflow diffs; Backbone source and reusable-workflow routing diffs and base source quality workflow. No additional ancestor `AGENTS.md` was found in the workspace path.

Read-only commands included `rg --files`, `rg -n`, `git status --short`, `git rev-parse HEAD`, `git show HEAD:<workflow>`, `git diff -- .github/workflows`, `git diff --binary | sha256sum`, and `sha256sum` for the plans. Successful reads and digest checks exited 0. Attempted reads of the not-yet-created `docs/local-runners.md` files reported missing files; documentation remains unfinished draft work, not an executed acceptance check.

Public primary documentation was retrieved for PR source selection and target-event behavior. Searches did not inspect any user's private runtime or alter repository controls. No repository tests, dependency installation, native builds, GitHub workflow dispatch, commits, pushes, PR mutations, or settings mutations were executed by this reviewer.

Unreviewed: implementation correctness of the inherited draft as a whole; all proposed routing/negative tests; full test results; runtime container packaging; native macOS/Windows execution; actual runner registration and repository/group access; fresh/ephemeral runner behavior and cache isolation; applicable live GitHub approval/event policies; required-check settings; remote Codex results. These remain work for implementation, quality checking, and frozen Full/Delta review rather than claims in this plan review.

## Triage handoff

Triage `LR-PLAN-001` before editing. If confirmed, update the identical plans and implement only the correction within the existing behavior-preservation scope, then obtain the required scoped plan recheck. Preserve this report and the original plan digest. Full reviewers must receive the final same base/head/patch snapshot and actual check evidence. Missing activation evidence is compatible with publishing explicitly blocked draft PRs, never with declaring runtime migration complete.

## Scoped plan Delta: LR-PLAN-001

The parent reported `LR-PLAN-001` CONFIRMED, revised only the two plans, and requested a scoped read-only recheck. The prior review above is preserved unchanged.

| Item | Delta observation |
| --- | --- |
| Prior reviewed plan SHA256 | `6b15b894f64b7833696a298b0faca5c132b9fe30ebf648f75bb9ec8abc010cf2` |
| Revised plan SHA256, identical in both repositories | `ed544a34a1e3e12ed351a8927033ebff47d80d2968c1b203f86db2fb24774d13` |
| AO HEAD and tracked draft digest | Unchanged from the original review |
| Backbone HEAD and tracked draft digest | Unchanged from the original review |
| Reviewed finding | `LR-PLAN-001` |

Result: `LR-PLAN-001` is RESOLVED in the revised plan. There are NO CONFIRMED IN-SCOPE DELTA FINDINGS. The plan has no remaining confirmed review blocker; implementation, checking, and independent frozen Full review may proceed within the existing scope. This result does not approve or activate either inherited implementation draft.

The revised line 11 retains AO's original `pull_request` triggers and default merged-result checkout, expressly prohibits newly privileged target-event candidate execution, and preserves deliberately trusted/default refs. Line 17 now requires regression coverage of default PR merge checkout and actual runner/group records before classifying candidate CI as local execution. These changes directly close the identified coverage regression.

Direct repair watchpoints were also checked: line 10 now distinguishes ARM64 native/latest/macOS14 tiers from the x64 Intel artifact tier; line 11 applies independent runner-access/approval/isolation prerequisites to BOTH repositories and describes same-repository guards as candidate-editable mitigation. These refinements stay inside runner behavior preservation and missing-evidence delivery, and they do not authorize hosting settings or registration changes. They introduce no confirmed plan regression.

The prior target-event first-PR and target-policy advisories are inapplicable to the new candidate-execution plan because its target conversion is withdrawn. The existing trusted leaderboard remains its original distinct workflow and does not gain candidate execution. The inherited tracked workflows still implement the earlier, unapproved target-event draft and have not been treated as corrected or certified by this Delta. Full reviewers must verify actual implementation convergence with the revised plan, including explicit candidate token permissions and each native tier.

Delta commands: read both plan files, `sha256sum` for each, `git rev-parse HEAD` and `git diff --binary | sha256sum` in each repository. All exited 0. The Delta executed no tests or runtime jobs and performed no source changes. Remaining unreviewed areas and activation evidence gaps are those listed above.
