# OpenCode Backbone resumed bounded repair

Date: 2026-10-18. Implementation author: `ao_resumed_repair`.
This is an author implementation/check record, not independent approval or
separate triage closure. No commit or remote write was performed by this author.

## Authority and source binding

The finalized separate triage in `opencode-backbone-resumed-triage.md`, SHA-256
`d1e4779623993fa2180a9e8c105f21e1846f44de8366abd79101a9ffcec95b5d`,
authorized exactly `REC-AO-SEC-001` and `REC-AO-BEH-002`. Work began after root
froze those records at local commit `9bb8edd5f330229624d3552ae73d1463eabbac5b`,
tree `93093bb91254435cf034949c9424a05bbaf7d9dd`. Product bytes at that checkpoint
were those of Full-review head `dcd187616f8b92cd668e75184fd5d697ac40fc66`,
tree `166946128c6104a640edac5c301660067feb6ebc`.

The repaired working tree changes only the following product/test files; this
report is the sole additional author artifact. Exact hashes below bind the
author checks before root commits the repair.

| Path | Repaired SHA-256 |
| --- | --- |
| `.github/workflows/go.yml` | `d9cb58ba5018b35c026e200fe46ef42d32924f79b829dd2ebf1af7e15b1aadd9` |
| `backend/internal/service/instructor/project_policy.go` | `39cf16deb05f5e3ed20e75d7180c13953e800030f23f6a696b515ff330688784` |
| `backend/internal/service/instructor/backbone_project_policy_test.go` | `2101ccfaf449c9343d2bd206d61adc9d1c5acdb4daa8a122cf0732ab2d9d76a6` |

## REC-AO-BEH-002

The governed project-sourced branch now requires explicit nonempty harness,
model and native effort before accepting/pinning the project selection. The
three-line guard uses the existing `ResolveProjectRole` contract and diagnostic.
Binding and qualification validation remain in place; the branch still avoids
ordinary native capability discovery. The unbound resolver, draft validation,
shared constraint checker, persistence and adapter code are unchanged.

The new data-only registration tests use the existing actual producer receipt
and explicitly synthetic admission qualification. They map an implementer
context to the project-sourced orchestrator role, allowing valid `configured`,
fixed `high` and `maximum` policy effort templates. The fixture asserts both
ordinary and locked template validity before exercising registration.

- Six negative subcases cover governed and unbound missing project effort for
  all three policy effort forms. They require `ErrValidation`, zero persisted
  works and zero calls to the native capability validator.
- Three positive governed subcases require explicit `high` effort to be pinned
  durably with the expected model/harness, valid resulting policy and matching
  policy digest. `configured` resolves to `high`; fixed/maximum policy semantics
  and the approved maximum mapping remain intact. They also assert no template
  mutation and zero native capability calls.

Before editing product code, the final regression source above ran with:

```text
go test -p=1 -count=1 -timeout=2m -run '^TestBackboneProjectSelection' -v ./internal/service/instructor
```

It failed as expected on the unchanged product: all three governed negative
subcases returned nil error and persisted one work. The three unbound negatives
and all three explicit governed positives passed. The command exited 1 after
4.034 seconds wall time; the package took 1.390 seconds. Its raw log is retained
at `validation-restore/author-repair-before-final-tests/go-test.log` outside the
checkout, SHA-256
`ad869eb5063676148002e1c7609e416db63c7fad8fdb7a90e7cec7bbfdf4f410`.
Original `project_policy.go` SHA-256 was
`dd1f130ee63cc28fdb4ffd92a5b4116f1ffd3a86377b1fdc2c65ca4c74f7df6e`.

An earlier author test draft used the locked worker role. Its configured/fixed
templates were invalid before registration, so that draft is not acceptance
evidence. The draft attempt is preserved at
`validation-restore/author-repair-before/go-test.log`, SHA-256
`deff7f437d6f4d6b8cd96f6b9718cf1b77418ea111082442718c79b1ded714cf`.
The valid orchestrator fixture replaced it before the final failed-before run.
The independent Full-snapshot diagnostic remains separately preserved with log
SHA-256 `c8a41d3c3a4a7c544f50bb1ea1a15ac62686c80fc2360bd605cc5a9a084683d4`;
none of these earlier logs was overwritten or relabeled as a pass.

## REC-AO-SEC-001

The Windows job now runs the complete verifier package in a separate verbose
step with `go test -race -count=1 -timeout=10m -v ./internal/policy/backbone`.
Only that package was removed from the existing filtered command. Its remaining
seven packages, their order, selector and race/uncached/timeout flags remain
unchanged. No verifier source, test name or skip changed.

The complete parsed YAML equals the original parsed YAML after removing the
new step and reinserting the verifier package in the old filtered command.
Original workflow SHA-256:
`7583f57eb6d5a7e0229c2643fc6006c3c2aca043777730e712c8490494e423ef`.
Actionlint 1.7.12 and `git diff --check` both exited 0 with empty output.

## Author checks after repair

Host: Linux; Go 1.27.1, `GOTOOLCHAIN=local`, shared isolated validation caches.
The following checks began at 2026-10-18T09:00:58.618696Z, using the exact
repaired hashes above. No native OpenCode executable/provider or adversarial
reproduction was invoked.

| Check from `backend/` | Outcome |
| --- | --- |
| `gofmt -l` on both changed Go files | PASS; no formatting output |
| `go test -race -p=1 -count=1 -timeout=10m -run '^TestBackboneProjectSelection' -v ./internal/service/instructor` | PASS; 2 top-level tests, all 9 subcases, zero skips; package 28.192 seconds, 122.238 seconds wall including first race dependency compilation |
| `go test -race -count=1 -timeout=10m -v ./internal/policy/backbone` | PASS; all 10 top-level groups, zero skips; symlink/case/size subcases each PASS; package 2.312 seconds, wall 2.625 seconds |

Raw logs remain outside the checkout under `validation-restore/author-repair-after/`:

| Log | SHA-256 |
| --- | --- |
| `registration.log` | `33fbd3295b8b504636c3eacff591cf19efc6551cafcc74d048657efa1c49f9dd` |
| `verifier.log` | `1ab5da199ad3e437b85ac1c7e3b83e82723fa79a55017649a708e1afd7bc0aac` |

Before/after source hashes for the three changed files and all four module/
workspace dependency inputs match. Comparing all 2,043 original backend paths
with the restored Full-snapshot manifest finds only `project_policy.go` changed;
the regression file is new. No dependency, API/schema, native-adapter or other
existing backend bytes changed.

## Remaining independent and platform evidence

The complete affected Instructor race suite, independent build/vet/lint and
workflow/source checks are still to be run against root's frozen repair snapshot.
Independent Delta and separate triage closure remain required. These author
checks do not replace any of those gates or the historical complete-suite record.

Actual Windows must still show all ten verifier groups and the symlink/case/size
results. A Windows symlink skip is an explicit gap even if the package succeeds.
This workflow does not provision the AO native Windows readback binary; no such
evidence, real model qualification or OS process confinement is claimed. Required
remote CI and visible Codex review remain pending. No merge, release, deployment,
settings change or downstream activation occurred.
