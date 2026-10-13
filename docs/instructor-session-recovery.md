# Configuration changes and session recovery

Work IDs, policy receipts, session IDs and native conversations remain durable
across daemon updates. A configuration change and a controller reconnect are
different operations: neither implicitly approves a waiting Work.

## One selection source per role

For a policy role with `selectionSource: "project"`, the project's worker,
reviewer or orchestrator settings select its harness, model and native effort.
The policy retains workflow gates, budgets, independence requirements and any
explicit approved native mapping required by `maximum`. Selecting a model does
not approve a new maximum-effort mapping. The governed reviewer uses the first
configured reviewer slot, matching the existing review engine.

The Policy dialog exposes this source choice. New fallback policies use project
settings; existing saved policies keep their previous behavior until explicitly
changed. An omitted `selectionSource` retains the legacy policy constraints.
The daemon rejects conflicting project settings or global policy changes before
saving them. Repository policy files retain precedence over global policy.

Saving settings for an existing project validates native capabilities when its
repository/environment discovery scope is unchanged. New projects and changed
discovery scopes receive structural and policy checks before saving; their
native capability check occurs at registration of Work, launch or rebind using
the saved scope. A successful settings save is not a provider execution receipt.

`GET /api/v1/instructor/projects/{projectId}/policy` returns the resolved policy
and its source. The Board compares this with each Work's pinned policy. Existing
Work and running sessions do not silently adopt changed project defaults.

## Move an existing Work to the current configuration

Use the Board's configuration-change action, or these API/CLI operations:

1. Capture `GET /api/v1/instructor/status`. Pause the Work and settle outstanding
   dispatches or native effects before attempting a change.
2. POST `snapshotId`, `workId` and a nonempty `reason` to
   `/api/v1/instructor/policy-rebind/preview`. The response contains the old and
   proposed policies, target version, invalidated authority and exact approval.
3. Review that proposal. POST the same fields plus its `targetPolicyVersion`
   and the human-entered `approval` to `/api/v1/instructor/policy-rebind`.

The CLI forwards the same JSON files without manufacturing approval:

```bash
ao instructor policy-rebind preview --input preview.json
ao instructor policy-rebind apply --input approved-change.json
```

The exact `REBIND-GO` phrase binds the decision to the snapshot, Work revision,
old/new policy and reason. Changed inputs require a fresh preview. A completed
request can be retried unchanged: its durable receipt prevents a second change.

The atomic update preserves the Work ID, sessions, evidence history, consumed
budgets and fix rounds. Old execution approvals and exceptions do not transfer.
Pending business gates remain pending with fresh identities. Otherwise the Work
gets a separate `RESUME-GO` gate; an already parked Work stays parked. Recovery
of a controller does not satisfy either gate. A session whose native selection
no longer matches the new policy must use the existing explicit agent-change
flow before execution. Independent review checks its own selected reviewer.

Terminal/cancelling Work, unresolved native effects and publication history
cannot be rebound. The daemon explains the blocking condition; it does not
reset history or manufacture proof that execution has stopped.

## Reconnect an existing conversation

The Board checks controller availability before offering execution approval.
When recovery is needed, its recovery action captures the Work revision, pinned
policy and exact session/conversation/controller identity. It reconnects that
conversation through the existing resume/restore path. A compatible surviving
host is adopted; a new host must resume verified existing native history.
Uncertain ownership or missing history cannot fall back to a fresh conversation.

Programmatic clients read
`GET /api/v1/instructor/works/{workId}/recovery`, then POST only these captured
fields to the same route: `workId`, `workRevision`, `policyVersion`, `sessionId`,
`sessionCreatedAt`, `providerConversationId`, `controllerGeneration`. Do not
copy the read-only `ready`, `action` or reason fields into the request.

```bash
ao instructor recovery WORK_ID
ao instructor recovery WORK_ID --input captured-recovery.json
```

An already recovered identical conversation is an idempotent success. A stale
identity is a conflict. `SESSION_RECOVERY_IN_PROGRESS` asks the client to refresh
while automatic recovery finishes. Other failures retain an actionable code,
cause and request ID. Where recovery is unavailable, the Board explains why.

Reconnecting a surviving host preserves queued input and pending native approval.
Replacing a proven dead owner preserves unsent queued input. After verifying
native history, it invalidates requests belonging to the old controller; those
native approvals cannot be answered through its replacement. Neither path
authorizes a new turn for a paused Work. Loss of a controller before a new
Instructor submission leaves that dispatch pending for recovery rather than
repeatedly destroying its approval cohort. Uncertain submissions still require
the existing reconciliation procedure, since blindly replaying them could
execute an instruction twice.

See [upgrade-continuity.md](upgrade-continuity.md) for the automated Windows/Linux
two-build acceptance check and the separate desktop installer release smoke.
