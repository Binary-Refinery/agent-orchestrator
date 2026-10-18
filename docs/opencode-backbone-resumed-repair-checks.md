# OpenCode Backbone resumed independent repair checks

Date: 2026-10-18. Checker: `ao_validation_restore`, independent of the repair author.

Outcome: all bounded post-repair gates below pass on the frozen repair source.
Both confirmed findings have fresh before/after evidence at their
respective boundaries. This record is independent validation, not Delta review,
triage closure, approval to merge, or a claim that all repository gates passed.

## Frozen source and evidence attribution

- Local repair commit: `f478fc100e854634f369655d067c752ea639819c`.
- Tree: `57dd202ec151a674f39237148bcb628b1b43db2e`.
- Root's separately verified remote equivalent: `03ea7ac6e80e84e675bdc2cc576a3dbf063436db`, with the same tree.
- Reviewed source before repair: `dcd187616f8b92cd668e75184fd5d697ac40fc66`, tree `166946128c6104a640edac5c301660067feb6ebc`.
- Full-review-to-repair binary patch SHA-256: `cb768f82b209be9685bc50a0aab2d5dd6014e868795e6e94ec212be646dd36e4`.
- Trusted-base-to-repair binary patch SHA-256: `d4ee568c30b8a672576a785afdc99b7d2a56bd27befe24328c659f66ae1f3791`.
- Author record `opencode-backbone-resumed-repair.md`, read before validation: `6fdf4d49c30e91145577147149a16722f7d2f59b74b0e84879e415dbee031940`.

The new tracked-file source manifest has SHA-256
`4b9a18424bb60162bbe6ee99d9890ef4625dd93be8f114650a3d97f08ff80212`. It records every tracked file's SHA-256 and
the existing Gitlink identities. Every tracked file hash was checked before and
after each command, and again before writing this report; none changed. The
source snapshot was already committed. Untracked documentation added by root or
the author is outside the tested product inputs.

This is fresh evidence from the restored checkout. The old temporary logs and
original backend manifests lost during workspace maintenance were not recreated
or represented as newly verified. Their durable reports remain historical
records with their original source attribution.

## Commands and results

Environment: Linux amd64; Go 1.27.1 with `GOTOOLCHAIN=local`, golangci-lint 2.13.2,
actionlint 1.7.12, and isolated scratch build/module/lint caches. Official Go,
linter and actionlint archives were checked against their published SHA-256
checksums. No dependency versions or module/workspace inputs were changed.

Commands below ran serially from `backend/`, beginning at
`2026-10-18T09:05:00.174490+00:00`. `-p=1` and lint `--concurrency=1` bound compilation
memory. Race flags, uncached execution and stated timeouts remain enabled.
Neither complete affected package uses a test-name filter.

| Command | Exit | Wall seconds |
| --- | ---: | ---: |
| `go test -race -p=1 -count=1 -timeout=20m -v ./internal/service/instructor` | 0 | 200.013 |
| `go test -race -count=1 -timeout=10m -v ./internal/policy/backbone` | 0 | 2.558 |
| `go test -overlay=/workspace/scratch/6e019b0851cc/review-behavior/overlay.json -p=1 -count=1 -timeout=2m -run ^TestResumedBackboneRegistrationRejectsMissingProjectEffort$ -v ./internal/service/instructor` | 0 | 4.354 |
| `go build -p=1 ./...` | 0 | 56.992 |
| `go vet -p=1 ./...` | 0 | 70.960 |
| `golangci-lint run --concurrency=1 --timeout=20m --path-mode=abs` | 0 | 218.892 |

The complete Instructor race package passed **179 top-level tests**, with 447
pass events including nested subtests, **zero failures, zero skips and no race
warnings**. Its package runtime was 198.484 seconds. Both committed regression
tests pass all nine subcases: six governed/unbound missing-effort negatives
across configured/high/maximum policy forms and three valid explicit-effort
positives. The negatives assert no persistence and no native capability probe;
the positives assert durable selection/digest validity and unchanged templates.

The complete verifier race package passed **all ten top-level tests**, with 42
pass events including subtests, **zero failures, zero skips and no race warnings**.
Its package runtime was 2.220 seconds. `symlink`, `case` and `size` each passed.
The original independent external missing-effort overlay also passed both the
unbound and governed controls on the repaired source, with no persistence.

Full lint reports exactly `0 issues.`. Full build and full vet exited zero. The
checks did not run a native OpenCode executable or provider qualification, and
no previously blocked adversarial native reproduction was retried.

## REC-AO-BEH-002: preserved before evidence and after confirmation

The independent diagnostic used an external Go overlay, leaving all 2,043
original backend files and dependency inputs unchanged. Its exact command was:

```text
go test -overlay=/workspace/scratch/6e019b0851cc/review-behavior/overlay.json -p=1 -count=1 -timeout=2m -run ^TestResumedBackboneRegistrationRejectsMissingProjectEffort$ -v ./internal/service/instructor
```

On the Full-review source, it began at `2026-10-18T08:54:27.319131Z`, exited 1
after 78.860 seconds wall time, and reached actual assertions in 0.819 seconds.
The extra wall time included first dependency compilation. The following is an
actual compact excerpt from that failed-before log:

```text
    zz_resumed_missing_effort_test.go:34: missing project effort was not rejected: registerErr=<nil> persistedEffort="" resultingPolicyError=invalid instructor request: role orchestrator needs explicit effort workID="work-442513d6-744d-4ca0-b9a1-5fe6e88d0636"
    zz_resumed_missing_effort_test.go:38: invalid selection persisted 1 work(s)
--- FAIL: TestResumedBackboneRegistrationRejectsMissingProjectEffort (0.81s)
    --- PASS: TestResumedBackboneRegistrationRejectsMissingProjectEffort/unbound-control (0.75s)
    --- FAIL: TestResumedBackboneRegistrationRejectsMissingProjectEffort/governed (0.02s)
```

Thus the unbound control rejected before persistence, while the governed branch
accepted and persisted an incomplete selection. This was a data-only admission
fixture with synthetic qualification, local Git and SQLite; no native runtime
or provider was invoked. Running the same unchanged overlay after repair exited
zero and passed both controls. Overlay JSON SHA-256:
`a009b3abe775d8272f8a9321b04ab4a4253262e10842a43360c575a308c03c86`;
overlay test SHA-256:
`d6c9e6d715300d29675eaf5508bdcdaf1714f7a14e2a90d8bf8e9f57feb58338`.

The author's final committed regression source separately failed all three
governed missing-effort cases before the guard, while the three unbound negative
and three valid positive cases passed. I verified that retained log's SHA-256 as
`ad869eb5063676148002e1c7609e416db63c7fad8fdb7a90e7cec7bbfdf4f410`.
The complete independent Instructor race run now covers those same final tests.

## REC-AO-SEC-001 and exact repair scope

Before the workflow repair, the fresh command
`go test -race -count=1 -timeout=10m -v -run Backbone ./internal/policy/backbone`
exited zero after 13.528 seconds and ran exactly one top-level test:
`TestActualBackboneProducerReceipt`. That pass demonstrated selector omission,
not a failed assertion. The other nine top-level verifier tests were not selected.
The repaired unfiltered command above executes all ten.

An initial setup attempt failed before any test because the restored `vet` file
was mode 0644 despite matching the official archive's bytes and 0755 mode. Its
exact published executable mode was restored; the failed attempt and subsequent
successful selector run remain separate logs. No test or validation gate was
weakened, and no access-control setting was changed.

The complete parsed workflow equals the original parsed workflow after reversing
exactly two changes: remove the new complete verifier step and reinsert the
verifier package into the original filtered command. The seven other packages,
order, selector, race flag, uncached flag and timeout are identical.

The restoration-manifest comparison checks all 2,043 prior backend paths. Only
`backend/internal/service/instructor/project_policy.go` changed, and
`backend/internal/service/instructor/backbone_project_policy_test.go` is the only
new backend path, bringing the count to 2,044. No path was removed. The three
product/test repair identities match the author's frozen bytes:

| Path | SHA-256 |
| --- | --- |
| `.github/workflows/go.yml` | `d9cb58ba5018b35c026e200fe46ef42d32924f79b829dd2ebf1af7e15b1aadd9` |
| `backend/internal/service/instructor/project_policy.go` | `39cf16deb05f5e3ed20e75d7180c13953e800030f23f6a696b515ff330688784` |
| `backend/internal/service/instructor/backbone_project_policy_test.go` | `2101ccfaf449c9343d2bd206d61adc9d1c5acdb4daa8a122cf0732ab2d9d76a6` |

The four dependency/workspace inputs match the reviewed source byte-for-byte;
`go.work.sum` remains `7df8d5343bc3d2b60d3738124c93b2047348af8e405c7235dcac95bccdfca355`.
Native adapters, verifier source/tests, generated API/schema and sqlc inputs were
unchanged. Full-backend `gofmt -l backend`, actionlint on the changed workflow,
base-to-repair `git diff --check`, and the working-tree diff check all exited zero
with empty output. Parsed-YAML/source/static record SHA-256:
`1aaf31220164f91a4200666c6dd45dec0bca22bd26a47e48649d8bb0c18f1214`.

## Raw evidence identities

The following current raw logs and JSON execution records are retained outside
the checkout under `validation-restore/independent-repair/`. Each JSON record
contains the exact command, start time, source identity, exit, wall time, output
hash and post-command source comparison. The prose and failed-before excerpt
above remain durable if temporary raw files are later pruned.

| Log | SHA-256 |
| --- | --- |
| `instructor-race.log` | `b552a189973bd995eda3ac09933c26b6a60864422435256d33438eb2b760a209` |
| `verifier-race.log` | `18349a9503f1176f8eb7aed95657673614fb7058676dee188d1b74eadd4f02f1` |
| `overlay-after.log` | `f4bfd6c18996285f04fd90b415964142c9e2cb462fd1cd7dc1bea3b0086da8c2` |
| `full-build.log` | `ff4b484314d354a37d54bf43fb106d920d298676bc3ba11b45f11cb44c83b6e8` |
| `full-vet.log` | `43ffba441c9e22101ec10fcbd4859feba8252c676557bd367972573003db94bb` |
| `full-lint.log` | `e92606b0bf483111dff0a120c315ea165821348f31365020e2468a0059095c47` |
| `missing-effort-before/go-test.log` (sibling directory) | `c8a41d3c3a4a7c544f50bb1ea1a15ac62686c80fc2360bd605cc5a9a084683d4` |
| `before-selection-retry/go-test.log` (sibling directory) | `551bd0443150ab4f9479ed656bd8dc535e6324f663b05aa2ef0552b635a21102` |
| `before-selection/go-test.log` (initial setup failure, sibling directory) | `73dc45be94c92c683ab4e8c1f0eeb5d3247d9e1e1c21c0c40be3849b601f2faa` |

| Execution record | SHA-256 |
| --- | --- |
| `instructor-race.json` | `a93e9fb1fdb5ef0e8d42880e98780760ac172a281cf3c5ede07a4d0b4b5870bc` |
| `verifier-race.json` | `24ab52f782a4aa8b9f16cef4555aa2fe2342008aeda935dcd5645430de45db97` |
| `overlay-after.json` | `25d3a84eef1830cc4f0300420c95e64cd84f5513e7835637a5a833f7390480c8` |
| `full-build.json` | `4f3d7547fb3edc16cc7b2a8e03256d8d1b51abe39d6ab25b912b9e9af9454fca` |
| `full-vet.json` | `e495e5dc8470f29cb79bfd24061153779d81f443e7fddf6e0e432d263dcd8eaf` |
| `full-lint.json` | `a23abc365f86ff240dbc432ad68114d1423457cd601427485c9bf8583b8fafd9` |

All four static-command logs are empty, SHA-256
`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`.

## Remaining evidence and limits

The earlier complete backend race gate remains **FAIL** on its documented
original source: 179 packages passed, eight failed, 13 had no tests, and all 23
failing top-level tests were reproduced on the unchanged baseline. That history
has not been relabeled as a pass. The entire backend race suite was not rerun
locally after this bounded repair; the fresh results here cover the complete
affected Instructor and verifier packages plus full build/vet/lint. Required
remote full-suite CI remains a separate gate.

Actual Windows must still execute all ten verifier tests and expose the
symlink/case/size outcomes; a Windows symlink skip remains an explicit gap even
when the package succeeds. The workflow does not provision the AO native Windows
readback binary. Native macOS/Windows, Docker-dependent and other recorded
frontend/cloud/desktop gaps remain governed by their durable original reports
and actual remote CI. No real-model qualification, provider snapshot validation,
OS process confinement, merge, release, deployment or downstream activation is
certified here. Independent Delta review, separate triage closure and visible
remote Codex review remain distinct gates.
