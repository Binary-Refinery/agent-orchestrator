# OpenCode Backbone resumed independent Delta security review

Date: 2026-10-18. Reviewer: `ao_resumed_full_security`, independent of the repair
author, validation-command owner and separate triager. Scope: the two confirmed
P2 repairs and their direct regressions. This is not another Full review.

## Frozen review and evidence binding

- Trusted base: `6b1ba43f1df9415c1ec937876ce54c86aa37484e`.
- Previous Full snapshot: `dcd187616f8b92cd668e75184fd5d697ac40fc66`,
  tree `166946128c6104a640edac5c301660067feb6ebc`.
- Tested repair: `f478fc100e854634f369655d067c752ea639819c`,
  tree `57dd202ec151a674f39237148bcb628b1b43db2e`.
- Reviewed final evidence checkpoint: `53f23301347c52b61bc676942af6b06589414024`,
  tree `cca674a371f65a543809941e1ab1a973d8463424`.
- Full-to-evidence patch SHA-256:
  `5e0c481163e9f2d83e920fcad298ced945aff0714c32b6a88782c7a5f78a92d0`.
- Base-to-evidence patch SHA-256:
  `c0c5c8bb9eb450b8d65ce0c94d9b635efeebbb904e194b0f5fee96e65313d8c8`.

Both patch identities were independently recomputed with `git diff --binary`
between the named commits. The evidence checkpoint adds only
`opencode-backbone-publication-state.md`, `opencode-backbone-resumed-repair.md`
and `opencode-backbone-resumed-repair-checks.md` under `docs/` to the tested repair.
It changes no product, test or workflow input. All 4,313 file hashes in the
tested repair manifest match the current files; manifest SHA-256 is
`4b9a18424bb60162bbe6ee99d9890ef4625dd93be8f114650a3d97f08ff80212`.

| Inspected record | SHA-256 |
| --- | --- |
| Finalized behavior Full | `8b070644c714b18ff02a2ac90a5bdf46d2e74a5d575ed75cc4b97f7d8ac2d209` |
| Finalized security Full | `324b3aa2e03c35b9396db879fbd4fd77da4b61dc8d57403ad321656728c0b026` |
| Separate pre-repair triage | `d1e4779623993fa2180a9e8c105f21e1846f44de8366abd79101a9ffcec95b5d` |
| Author repair record | `6fdf4d49c30e91145577147149a16722f7d2f59b74b0e84879e415dbee031940` |
| Independent repair checks | `77ff1cee2e8fa5b5b4e21ee2c4ce1529b486353604387108a6f65a91e01dc55a` |

## Conclusion

**REC-AO-SEC-001 and REC-AO-BEH-002 are repaired and independently verified at
their respective code/selection boundaries. No new confirmed direct-regression
finding remains in this Delta scope.** The repairs are ready for separate triage
closure. Actual Windows execution and required remote CI/review remain distinct
follow-through obligations; this review does not certify them in advance.

## REC-AO-BEH-002 — Explicit governed project selection

The only product correction is the three-line nonempty harness/model/effort
guard in the governed branch of `resolveProjectSelections`. It runs after the
existing binding/qualification check and before accepting or pinning the
selection. It restores the same explicit-selection condition and diagnostic as
`ResolveProjectRole`, including when policy effort is fixed or `maximum`.

I traced the branch through public `Register` and its persistence boundary.
Failure now returns before creation of the work. Successful governed resolution
continues through the existing constraint check, copied policy role/map and
policy digest; it still avoids ordinary native capability discovery. Removing
only this guard makes `project_policy.go` byte-identical to the Full snapshot.
The shared constraint checker, incomplete project-draft behavior, unbound
resolver, persistence code, qualification checks and native adapters are
unchanged. No fallback effort or automatic policy repair was introduced.

The new external-package tests exercise public registration with valid locked
policy templates and the existing actual producer fixture. Qualification is
explicitly synthetic test data. Six negative subcases cover governed and unbound
missing effort for `configured`, fixed `high` and `maximum`, requiring validation
rejection, zero persisted works and zero native capability-validator calls.
Three governed positive subcases require durable explicit selection, valid policy
and digest, unchanged input templates and no native capability calls. The
maximum mapping remains `provider/worker` to `high`; its constraint code was not
altered. These tests check observable registration outcomes rather than merely
duplicating the new condition.

The independent failed-before overlay and author's final regression log remain
intact. Their recorded governed cases persisted invalid/incomplete selections
before repair; the unbound controls rejected them. On the frozen repair, the
unchanged independent overlay passes both controls, and the complete Instructor
race package passes all committed cases. No native process was required to
establish this defect or its correction.

## REC-AO-SEC-001 — Complete Windows verifier selection

The Windows workflow now invokes exactly
`go test -race -count=1 -timeout=10m -v ./internal/policy/backbone` in its own step.
That package alone was removed from the prior filtered integration command.
All seven other packages, their order, name filter, race/uncached/timeout flags
and every other workflow setting are preserved.

I independently parsed both complete YAML files. Removing the new step and
reinserting the verifier package into the old command yields exact parsed-object
equality with the Full snapshot. The verifier source, test names and existing
skips are unchanged. The fresh failed-selection diagnostic actually ran only
`TestActualBackboneProducerReceipt`; its zero exit was evidence of omitted tests,
not complete coverage. The repaired command's independent Linux log contains all
ten top-level verifier passes and explicit symlink/case/size passes, without skips.

This closes the selector defect. The same command must still execute on Windows.
Any Windows symlink skip must remain visible as a platform evidence gap rather
than being reported as a symlink verification pass.

## Independent quality evidence inspected

I read and rehashed the current raw logs and JSON execution records under
`validation-restore/independent-repair/`, including exact commands, exit codes,
source bindings and unchanged-source results. I independently counted the test
events and inspected the registration and path-boundary subcases. No extra test
execution was necessary for this Delta.

| Completed validation on Linux, Go 1.27.1 | Independently inspected result |
| --- | --- |
| Complete Instructor package, race, uncached, 20-minute timeout | 179 top-level tests; 447 total pass events; zero failures/skips/race warnings. All nine committed registration subcases pass. |
| Complete verifier package, race, uncached, 10-minute timeout, verbose | Ten top-level tests; 42 total pass events; zero failures/skips/race warnings. Symlink, case and size each pass. |
| Original unchanged external missing-effort overlay | Both governed and unbound controls pass after repair. |
| Full backend build and vet | Exit zero with the exact frozen source recorded. |
| Full golangci-lint 2.13.2 | Exit zero; `0 issues.` |
| Formatting, actionlint and diff checks | Independent static execution record reports exit zero and empty output; its SHA-256 matches. |

| Raw evidence independently rehashed | SHA-256 |
| --- | --- |
| Instructor race log | `b552a189973bd995eda3ac09933c26b6a60864422435256d33438eb2b760a209` |
| Verifier race log | `18349a9503f1176f8eb7aed95657673614fb7058676dee188d1b74eadd4f02f1` |
| Original overlay after repair | `f4bfd6c18996285f04fd90b415964142c9e2cb462fd1cd7dc1bea3b0086da8c2` |
| Full build log | `ff4b484314d354a37d54bf43fb106d920d298676bc3ba11b45f11cb44c83b6e8` |
| Full vet log | `43ffba441c9e22101ec10fcbd4859feba8252c676557bd367972573003db94bb` |
| Full lint log | `e92606b0bf483111dff0a120c315ea165821348f31365020e2468a0059095c47` |
| Original independent missing-effort failure | `c8a41d3c3a4a7c544f50bb1ea1a15ac62686c80fc2360bd605cc5a9a084683d4` |
| Author's final regression failure before guard | `ad869eb5063676148002e1c7609e416db63c7fad8fdb7a90e7cec7bbfdf4f410` |
| Original filtered verifier selection | `551bd0443150ab4f9479ed656bd8dc535e6324f663b05aa2ef0552b635a21102` |
| Independent static/source record | `1aaf31220164f91a4200666c6dd45dec0bca22bd26a47e48649d8bb0c18f1214` |

The six current command records' hashes also match the independent quality
report. The external overlay source and JSON retain hashes
`d6c9e6d715300d29675eaf5508bdcdaf1714f7a14e2a90d8bf8e9f57feb58338`
and `a009b3abe775d8272f8a9321b04ab4a4253262e10842a43360c575a308c03c86`.

The complete non-report delta from the Full snapshot is exactly three paths:

| Path | Repaired SHA-256 |
| --- | --- |
| `.github/workflows/go.yml` | `d9cb58ba5018b35c026e200fe46ef42d32924f79b829dd2ebf1af7e15b1aadd9` |
| `backend/internal/service/instructor/project_policy.go` | `39cf16deb05f5e3ed20e75d7180c13953e800030f23f6a696b515ff330688784` |
| `backend/internal/service/instructor/backbone_project_policy_test.go` | `2101ccfaf449c9343d2bd206d61adc9d1c5acdb4daa8a122cf0732ab2d9d76a6` |

No dependency, API/schema, verifier, native-adapter or unrelated product changes
are hidden in this Delta. The source and workflow identities were checked against
both the tested repair and final evidence checkpoint.

## Limits and next gate

This reviewer performed read-only source/evidence analysis and authored only this
report. No product edit, native executable, provider, model, adversarial
reproduction, commit or remote write was performed. Independent validation was
executed by the separately identified checker; its results are not represented
as this reviewer's own test execution.

The historical complete backend race failure remains a failure on its original
source. Fresh complete affected-package and full build/vet/lint passes do not
constitute a new full-repository race pass or erase recorded frontend/cloud/
browser/desktop gaps. Normal remote CI must exercise the published candidate.
Actual Windows path tests, native AO Windows readback and the paired Backbone
native smoke are separate claims. The workflow does not provision AO's optional
native readback binary. Real-model qualification, provider snapshot verification
and OS confinement remain unestablished.

Separate triage should append closure for these two repaired findings while
retaining actual platform/CI/Codex obligations. This Delta grants no merge,
release, deployment, settings change or downstream activation authority.
