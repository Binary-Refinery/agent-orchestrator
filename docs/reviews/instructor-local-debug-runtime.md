# Local AO runtime evidence, 2026-10-14

## Source and installed application

Repository: `Binary-Refinery/agent-orchestrator`. Base:
`f60f432a32b9f72370dafab2c58b703182dc8026`, including merged PRs 18 and 20.
Fix branch: `fix/instructor-local-debug-20261014`. Changes are isolated from the
existing orchestrator checkout and productive app/data. Runner PRs 21 and
`Artifaktory/engineering-backbone` 11 remain separate.

The installed daemon reports this exact unmodified base with Go 1.27.1 and CGO
disabled. Its SHA256 is
`16bb476710b4be3d31aa43bd050e6d9d691ea1d6ec50c4938cd2381c9db740dc`.
The installed frontend's relevant Instructor hook matches this base. The
productive status readback remains `ready/ok`, PID 33888, port 3001.

Actual GUI/DevTools inspection of
`work-42a3cc42-89db-45ee-b54e-850524b76879` recorded:

```http
GET http://127.0.0.1:3001/api/v1/instructor/projects/artifaktory.de/policy
HTTP 200 OK
```

The complete independently retained HTTP capture has SHA256
`b2691f61890fa86e24846f6eb1e7a4a7656e950d1b04cb8e17e8d38f059594d3`.
The work's policy binding loads and matches the current pinned policy. The
previous `ROUTE_NOT_FOUND` is not reproducible on this installed revision;
no speculative route change is included.

## Historical work delivery

The three original Instructor messages reached the recorded native OpenCode
conversation byte-for-byte. Its ACP prompts returned non-retryable provider HTTP
400 `invalid_request_error`; there was no tool/assistant execution evidence.
The rejected parameter was not named by the provider and remains unknown.
AO subsequently lost the durable error on recovered history and hid it from
semantic-delivery diagnostics. Those demonstrated feedback losses are fixed;
previously erased historical errors are not fabricated or backfilled.

## Actually started second-round source build

Electron: isolated development app with dedicated data/profile/run file.
Daemon started `2026-10-14T08:18:55.9805959Z`, PID 7232, port 3314,
`ready/ok`, from `frontend/daemon/dev-1791965785789-25992/ao.exe`.
SHA256:
`129260b90d6b538c90127de02b2905139cb802718b9b9ae5e0b9dcbb60eac68c`.
Build metadata: Go 1.27.1, CGO disabled, base f60f432, `vcs.modified=true`.
The tested content is bound to all 14 immutable source/test hashes in
`instructor-local-debug-source-hashes.json`; it is not claimed to be a clean
committed revision. Actual process executable and run-file identities were
checked before dispatching the final chain.

The visible final Electron app opens the new Work and loads its matching current
project/pinned policy. Its UI shows actual command delivery and progress. No
productive application replacement or global AO configuration change occurred.

## Real chain that exposed the reviewer failure

Work `work-b880d360-8c72-4b16-8f99-d8da1b2fc556` used project orchestrator
`ao-chain-lab-2`, genuine OpenCode worker `ao-chain-lab-3`, and Codex reviewer
`ec2652fb-bbc6-4447-81a9-5ed02435fe40`.
Worker commit: `bb38162135cd9cab85046f813056edc8d7e2892c`.
Review run: `496c7d9d-300d-4205-aeec-636e8086e0af`, complete/approved;
its exact-SHA report was verified and returned to the Instructor.
Native Codex provider turn `01a1395d-5832-7f13-8f19-c3c17e3d1cdf` ended at
`2026-10-14T07:46:45.151Z`; AO nevertheless retained only a running turn and
active reviewer. This is the real reproduction for the reviewer projection fix.

The completed reviewer alone was cold-stopped through the normal lab API after
checking that the worker had exactly one reviewer row. Restore was refused with
`REVIEW_INVALID` because that reviewer belongs to an earlier governance phase.
This phase fence was preserved; no historical prompt was re-executed. Cold-history
reconciliation has separate real-store/controller regressions, not a claimed
successful real restoration of this gated historical reviewer.

## Second-round build, fresh real execution

Fresh Work: `work-ec850174-d8df-4b22-a098-15371b5adec8`, registered
`2026-10-14T08:22:42Z`. Existing orchestrator 2 was positively idle before admission.
Its first-attempt directive was accepted/started at `08:22:53Z`, provider turn
`403ccf3a-9eef-4d94-85e8-28d6b03c2ba5`. Exactly one new genuine OpenCode worker,
`ao-chain-lab-4`, started at `08:23:37Z`, provider turn
`7f595cf5-abd3-4692-b75b-effc23f0f134`.

Native worker model/variant remained
`opencode-go/muse-spark-1.3-contributor` / `xhigh` / auto.
The unchanged policy pins independent Codex `gpt-6-astra` / `max` / auto.
No hidden provider/model/effort substitution occurred.

Worker SHA: `7a1d5d576cccfc734eaf86266f6d0e1f650eb4f9`. Its bounded fixture changes
only `AO-LAB-NONCE.txt`; push/full local check profiles were independently verified
at `08:28:16Z` and `08:28:17Z`. These profile names do not represent a remote push.
Normal automatic discovery passed the actual worker-native-configuration fence
and then correctly rejected attribution because the pinned test policy lacks
configured operator/co-author identities. The earlier false native-config mismatch
is absent. No verification budget or identity evidence was reset or invented.

Reviewer `79116846-7181-4d72-a33f-4f91d8927bca`, run
`0e8f6ae0-cc50-4439-9bdc-b7cf7f0792d0`, completed/approved against that SHA.
Native Codex provider turn `01a1398f-0d59-73d0-aace-edcd1f7b9feb` completed
at `08:40:57.446Z`; AO likewise records completed, reviewer idle, controller
ready, five assistant messages and 28 terminal activities. Its visible output
and verified Instructor evidence `7a8a2121-beac-4cba-b378-06106d459c90`
demonstrate the reviewer fix.

This Work exposed the queue revision race described in the independent review
record. One exactly correlated cancelled, unadmitted turn allowed normal negative
technical reconciliation. A later nudge failed before any native binding and
left a regular delivery gate; no fabricated receipt or human Go bypassed it.
Consequently this second Work does not prove final feedback through its gated
Orchestrator. The fresh final-build Work below provides that separate evidence.

## Actually started third-round source build

The isolated Electron app rebuilt and launched daemon PID 33568 on port 3314,
started `2026-10-14T09:05:05.4763128Z`, ready/ok. Actual executable:
`frontend/daemon/dev-1791968457191-11684/ao.exe`; SHA256:
`9bc3a969f7d3432e24a6a839a0f04725dfd0b824aa0d273371c667955dcbf0f7`.
Go 1.27.1/CGO-disabled build metadata identifies base f60f432 and modified content.
All 16 final implementation/test hashes are bound in
`instructor-local-debug-queue-source-hashes.json`; no clean final commit is
claimed for this running binary. The productive PID 33888/port 3001 remains
ready/ok and its executable is separate.

Fresh Work `work-8e12f1e2-4335-4e27-972d-39830d03e3f1` used the same project
Orchestrator 2 and exactly one new OpenCode worker `ao-chain-lab-5` with Muse/xhigh.
Its actual native session is `ses_ec650d86fffebNaAOBhVR50Guu`, provider turn
`85d8063e-84aa-40ef-8508-1a0eda40bf72`. The worker created clean nonce-only commit
`7e42a8378e4576b8b338822741f07f878e5fe412`, parent f60f432, no remotes.
Push/full local check evidence `41660b61-84c9-4f27-87a4-fd8928a4747f` and
`80116711-7ec1-4b16-a500-9d5b9d9e96fb` is verified against its SHA and pinned policy.

Independent real Codex reviewer `b27813c1-1b30-4f27-a4e7-fe1695a4c339`, run
`0c059bff-9d34-4dd6-97af-65cd81a13447`, is complete/approved. Its native session
`01a139b2-8c34-7d01-9021-cfb125f6e067` records actual Astra/max/read-only settings
and provider turn `01a139b2-a1eb-75a1-b53a-bc8fc7c280f8` completed at
`09:20:09.485Z`. AO turn `f8a7c929-3f47-47a5-8952-06997c74658a` is completed;
reviewer idle/controller ready, five messages and 34 terminal activities.
This is a real static nonce review, not the independent source-fix review or
a GitHub review. Its actual verdict reports no tests executed.

Instructor evidence `bbb924d4-8cb5-4ba9-be65-e8d412790f89` is verified/approved
against the exact Work, SHA, policy, reviewer and run. The existing Orchestrator
then natively executed turn `217018d4-6658-4928-93a5-68af38b55f95`, reported
accepted at `09:21:37.861Z` and returned final progress at `09:21:44.568Z`.
An earlier ten-second acceptance uncertainty was resolved by these actual
native reports, with zero manual reconciliation or resend in this fresh Work.
The Instructor summary includes the completed review; dispatch is progress,
error empty. Final Work revision 27 is waiting_human/triage at regular gate
`gate-c3cec6a5-3f3a-4db7-9c77-9f2f5e9da174`, title `triage → fix`.
No agent approved that gate.

The remaining verification error is specifically missing configured operator
author and AO co-author identities in the pinned attribution policy. Actual
native-configuration comparison passes. No author, co-author email, issue,
required trailer or committed task packet was invented to manufacture closeout.

Actual GUI inspection of the third-round Electron build displayed the final
Orchestrator summary, matching current/pinned policy, reviewer complete and
worker/Orchestrator idle. Opening the reviewer shows its genuine final approved
message for the full nonce SHA. A native PyCharm MCP startup warning and transient
provider reconnect warning are visible; the completed review used other actual
tools and does not attest that PyCharm started successfully.

The deliberate live two-queue overlap is NOT PROVEN: the normal checkpoint
supervision ended after nine seconds, so the worker wake-up and review directive
were admitted before the one separate real 60-second read-only busy turn ran.
Both QueuedAt fields are empty. That turn completed once; no repeated wait or
diagnostic nudge was sent after it ended. Real-store transaction regressions
cover the interleave and authority matrix; this successful real native chain
does not substitute for the missing simultaneous-live-queue evidence.

The compact retained `final-proof.json` has SHA256
`f03ee253d45738a371cbd77f688ecaba24b21048895b9f436477f8f4ab76e0e7`.
It lists exact native identities, evidence, reports, gate, and raw snapshot names.

## Actually started final fourth-round build

The final isolated daemon started at `2026-10-14T10:01:39.3161992Z`, PID 36208,
port 3314, ready/ok. Its actual executable is
`frontend/daemon/dev-1791971822369-32132/ao.exe`, SHA256
`0ea2a88c0f3aa3e947497ffff50d58c82b3494edbae72c0d8bc6be3aa20d4068`.
Go 1.27.1, CGO disabled, base f60f432 and modified-content metadata are recorded
alongside all 16 hashes in `instructor-local-debug-final-source-hashes.json`.
No final clean source commit is claimed. The productive executable and PID 33888
were separately checked under the productive run-file/data profile, ready/ok
on port 3001. Only the positively idle isolated app/daemon were restarted.

Independent fourth-round source review and final delta both PASS; the newly
found CI-discovery P2 is closed. The missing-session/current-failed-CI branch
has targeted RED/GREEN regression proof, not a fabricated live GitHub failure
in the no-remote lab. The real native Work/Reviewer evidence above remains
immutable, with the same human gate after restart. Its facts are historical
executions bound to their actual third-round binary; they are not relabeled as
fresh fourth-round worker/reviewer execution.

Actual final Electron inspection displays the preserved Instructor Work at its
triage/fix gate and the correct configured native roles. The separate normal
read-only Status Work below verifies the previously missed live queue path.
This diagnostic task cannot satisfy software closeout or approve a phase gate.

The post-chain native status turns at `09:32:04Z` and `09:42:34Z` have
`origin=automation`, empty client IDs and the supervision text from the existing
lifecycle nudge path. They were not issued by this diagnostic agent and are
not claimed as queue-race evidence. No automation setting was changed.

## Final-build genuine live queue interleave

Fresh status-only Work `work-45dfd939-5202-4212-84bd-13a9d2f56d2b` reused
Orchestrator 2 with the unchanged policy and actual Muse/xhigh configuration.
Its concrete task authorized factual status/queue reads and separate actual
accepted/progress reports. Exactly one normal registration and two snapshot-bound
`Pruefen` commands were sent; no worker, reviewer, check, commit or deliberate
native wait was part of this diagnostic execution.

The initial native status turn `b1fa27f6-79e7-4476-b154-3c8d8e9c90ac`, provider
turn `4d48ddf5-0b97-416b-a512-383c7a525ad4`, was positively running with genuine
tool activity before the two inspection commands. Their immutable snapshots are
`snapshot-958cfd56-c085-4c87-8303-99b46136ef29` and
`snapshot-9ef08021-baf6-46b0-bd4d-90482321a1bc`.

Actual read-only/query-only SQLite `BEGIN` snapshots at lines 15, 16 and 17 of
`round4-status-queue-atomic-live.jsonl` show Work and both pending/leased sibling
outboxes advancing `2 → 3 → 4`. Task, snapshots, command digests, phase/sequence,
policy and queue authority remain unchanged. First QueuedAt is retained:
`10:15:54.1207277Z`; second QueuedAt `10:15:54.1287162Z`, eight milliseconds later.
The second target binding occurs through ordinary delivery, not the carry itself;
absent inspection DTO phase fields are not invented. Instance
`bec9320bd405416cf4061b079ccff6ae` and runtime
`chat:3d998a0c-87c5-4720-a063-34a31ab21233` bind the actual native receipts.

Inspection A: AO turn `934abf44-9e3c-49b1-b562-42aeed488a30`, provider turn
`66e7b48b-4e80-4e86-92b8-068333507075`, real native start/receipt
`10:16:10.7107475Z`. Inspection B: AO turn
`c49d1db3-ef9a-4c9f-b799-aebee00245c0`, provider turn
`449df447-b6ad-469e-8f34-06411f1d3291`, start/receipt `10:16:30.8087483Z`.
Both returned genuine accepted/progress reports and terminal output. All three
native turns completed, nine activities terminal, controller ready, owner idle
and zero unresolved turns. Dispatcher lease polling reused the same keys;
there was no manual resend or reconciliation.

After positive idle, exactly one ordinary `Stopp` with a fresh snapshot parked
only this diagnostic Work at revision 18. Its missing software evidence was not
converted to `done`. The preceding real Worker/Reviewer Work remains at revision
27 with its unchanged `triage → fix` gate; comparison uses actual available API
facts, not an invented pretest persisted-DB-body hash. No policy, clock, database,
human gate or previous budget was manually changed.

This positive final-build test exercises the same generic queue-carry function
with inspection directives. It does not claim a new worker-result/review race
execution; the exact role-specific race and negative authority matrix have
separate real-store regression proof, alongside the genuine native chain above.

The initial ten-second policy read deadline was too short. A measured final-build
GET returned HTTP 200 in 16.8636489s with the same policy; the read client then
used 45s. The native queue's 120s acceptance contract was not changed.

## Review and verification boundaries

See `instructor-local-debug-review.md` for independent review, finding assessment,
two baseline rounds, explicitly authorized third/fourth rounds and successful
delta reviews. The source check manifest
identifies the reviewed and tested content; targeted regression tests do not
substitute for the genuine native executions above.

Four Windows test failures reproduce unchanged at the exact base: three HTTP
controller cases and one Git-inventory path comparison. The final complete suite
then exposed wider Windows/substrate failures plus a separately confirmed new
CI-discovery P2. See the independent review record for exact candidate/base
comparison, focused RED/GREEN proof, explicit fourth-round exception and
successful final delta. That P2 is closed; failed broad Windows checks and the
successful genuine native chain remain separate bounded results. Race
validation is unavailable with CGO disabled;
remote CI and GitHub-Codex code review are not yet attested. No merge occurred.
Publication requires the Rulebook's literal human `PUBLISH-GO [Branch]`.
