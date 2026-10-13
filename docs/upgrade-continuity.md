# Upgrade continuity acceptance

An update must retain Work IDs, their pinned policy and audit history, session
identity, native conversation context and pending human decisions. A surviving
compatible chat host is reattached. A conclusively dead host requires native
resume of the existing conversation. Unknown ownership must not authorize a
second provider or automatic replay of an uncertain instruction.

## Automated two-build check

`.github/workflows/upgrade-continuity.yml` runs on Windows and Linux. It builds
the frozen installed baseline `d61634321d8d3a8ba3cf933033b168331310785a` and the
candidate in separate installation directories. The test checks that their
binary hashes differ, then runs each real daemon sequentially over one temporary
data directory. No real `~/.ao`, provider credentials, model account or repository
is used.

The provider substitute in `test/upgrade/fake-codex/` is an offline
Codex app-server and OpenCode ACP protocol executable. It retains Codex thread history and journals
requests so an assertion can distinguish a true reattachment from new context or
duplicate execution. Daemon HTTP, project registration, Instructor governance,
the dispatch outbox, semantic delivery, SQLite, controller fencing and persistent
host processes all use production code.
The project is explicitly governance-managed, exercising strict native-history
continuity. The fixture records provider PIDs and exits when its host-owned stdin
pipe closes; OS process checks confirm that both old host and provider are dead
before a replacement starts. This does not prove real vendor descendant cleanup.

The test proves:

- A policy serialized and hashed by N still verifies in N+1.
- Instructor delivery reaches the native provider once and preserves its
  dispatch identity on retry after the upgrade.
- A live host keeps its process and conversation identity across daemon binary
  replacement, including an outstanding provider approval and queued user input.
- The approval remains pending until explicitly answered. The separate Work
  human gate stays intact throughout recovery and queue draining.
- After a host crash, recovery reuses the native thread, retains prior message
  IDs and text, and does not create a second copy of an executed turn. The old
  provider has exited and exactly one replacement provider owns the thread.
- Work IDs, policy binding, audit events and dispatch count survive both paths.

Run it locally with two separately built binaries:

```bash
cd backend
AO_UPGRADE_OLD_BINARY=/absolute/path/to/previous/ao \
AO_UPGRADE_NEW_BINARY=/absolute/path/to/candidate/ao \
  go test -tags upgrade -count=1 -timeout=8m -v ./internal/integration/upgrade
```

Use `.exe` paths on Windows. The `upgrade` tag is deliberately separate from
unit tests: missing binaries are an error, and running the same build twice is
rejected. Ports are allocated on loopback and all fixture state is temporary.

## Native task handoff check

The same Windows/Linux job runs `TestNativeInstructorHandoff` against the real
candidate daemon. Its scripted Codex orchestrator receives an Instructor goal
and spawns an OpenCode worker through the normal HTTP API with a concrete task.
The assertion requires that exact task once in the worker's native ACP prompt
and durable conversation, independently of standing rules or its Work ID.

The offline worker commits a tiny fixture artifact locally and submits its SHA
and completion claim through the real report APIs. The daemon outbox wakes the
orchestrator, runs the policy's actual local Git check commands, and starts the
existing independent Codex reviewer. That provider reads AO's generated review
task file, inspects the exact commit and fixture content, and submits a scoped
review through the real review API. Instructor must discover and verify those
facts and open its normal triage-to-fix human gate. Restart and command retry
must retain that gate without another worker task or initial review.

These are deterministic scripted agent actions, not a claim about live models'
reasoning or vendor authentication. No database state is seeded, no human gate
is signed, and no remote publication is attempted. The process test stops at
the unapproved triage gate; it does not claim external CI/traceability,
protected publication or final `done`. Service-level workflow acceptance covers
later transitions with its explicitly isolated external adapters.

## Complete service workflow check

`TestInstructorOrganicWorkflowRequiresHumanGatesAndCompleteNativeEvidence`
starts with normal Work registration and drives the real Instructor engine,
SQLite, chat delivery and budget admission, local Git commits, configured check
commands, review engine and evidence verifiers. It covers both project settings:

| Worker | Orchestrator | Independent reviewer |
| --- | --- | --- |
| OpenCode | Codex | Codex |
| Codex | OpenCode | OpenCode |

The chain requires captured intake approval, the exact concrete worker task,
worker reporting, an independent finding, a retained triage gate across service
restart, explicit correction approval, a new commit, checks, a fresh delta review,
exact publication approval and complete remote evidence before `done` appears in
the Instructor snapshot. Empty worker tasks, control-role impersonation,
execution during human triage, foreign review submissions, generic publication
approval and incomplete closeout claims are rejected. Retries retain their
original operation identities. A companion organic regression verifies that a
late native acceptance cannot invalidate the already queued next-phase order.

Run these checks without provider credentials or network access:

```bash
cd backend
go test -race -count=1 ./internal/daemon -run '^TestInstructorOrganic'
```

Native inference and runtime creation are scripted at the provider boundary;
SCM reads and the protected publisher use isolated test adapters. Work phases,
approvals, review results and evidence are produced through their real services,
not seeded into SQLite. An injected clock honors the real verification retry
delay. The test separately asserts that the ordinary host publication boundary
rejects publication because it cannot prove the required isolation. It therefore
proves the complete service workflow under those explicit boundary contracts,
not real vendor judgment, live SCM credentials or an actual publication/release.
The process-level handoff test above supplies the complementary real daemon,
HTTP and provider-protocol coverage.

## Release smoke still required

This daemon check does **not** claim to test Electron's installer/uninstaller,
the updater's process orchestration, a real provider's version compatibility or
an operating-system reboot. Before release, install the currently supported
desktop version and upgrade to the candidate on a disposable Windows profile
with isolated AO data and an authorized provider account. Record both installed
build SHAs and the following outcomes:

1. Upgrade while a provider turn is active; verify the same session and native
   conversation, retained messages and exactly one owning provider host.
2. Repeat with pending provider approval, pending Work approval and a paused
   Work. None may silently become an execution approval.
3. Stop or kill the old host in the disposable profile. Use the Board's guided
   recovery and verify retained history, the actionable result and no repeated
   approval/dispatch failure loop.
4. Introduce an explicit project-versus-policy role restriction conflict. It
   must be shown before execution with the effective policy source and the
   rejected selection, without inventing a fresh session or bypassing the gate.
5. Verify installer rollback/downgrade separately against the supported storage
   compatibility policy. Passing the forward upgrade is not a downgrade claim.

Capture session/Work IDs, native conversation ID, controller/host generations,
pending gate IDs and dispatch receipts before and after each operation. Do not
include host capabilities, provider credentials or private prompt content in the
release evidence. Keep this baseline until the replacement release has passed;
then advance the pinned previous SHA in the workflow as part of the next release
acceptance update.
