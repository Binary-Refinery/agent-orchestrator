# Using the Instructor board through the daemon API

The Board is a renderer of the daemon's project, work, command and planning
contracts. Local agents use the same HTTP API as the GUI; they do not manipulate
cards or need a browser session. The primary listener remains on `127.0.0.1`.
Use the address of the running daemon reported by `ao status`, rather than
assuming that an example port matches another installation.

## Read the current portfolio

| Request | Meaning |
| --- | --- |
| `GET /api/v1/instructor/capabilities` | Whether Instructor is enabled, its command registry and available runtime capabilities. |
| `GET /api/v1/projects` | All registered projects, including projects with no Instructor work. |
| `GET /api/v1/projects/{projectId}` | Resolved repository identity and the complete saved project configuration. A degraded project must not be treated as a healthy launch target. |
| `GET /api/v1/instructor/status` | A captured work snapshot, metrics, states, phases, wait conditions, gates and currently available actions. |
| `GET /api/v1/instructor/status?baseId={snapshotId}` | Changes relative to a previous captured snapshot. |
| `GET /api/v1/instructor/works/{workId}` | Work history, check/review evidence, execution-budget usage and delivery receipts. |
| `GET /api/v1/instructor/works/{workId}/members` | Verified current and historical coordinators, workers and reviewers, with native selection receipts. |
| `GET /api/v1/instructor/inventory` | Technical project/session/Git observations; this remains available after the old diagnostic page is removed. |

Work IDs are durable. A session ID, branch name or issue number is not a
replacement for a work ID. The `actions` returned for each work are the
authoritative available commands. Read the registry instead of inferring
permission from a card color or workflow position.

Saved project settings describe future launches. Member/session receipts
distinguish requested, passed-to-tool and provider-confirmed model/effort.
Missing confirmation stays unknown.

For policy changes on existing Work and guided controller reconnection, see
[configuration changes and session recovery](instructor-session-recovery.md).

## Create a project and start its orchestrator

Register an existing local repository with `POST /api/v1/projects`:

```json
{
  "path": "C:\\Projects\\example",
  "name": "Example"
}
```

For a clone, use `POST /api/v1/projects/clone` with `remoteUrl` and
`destinationParent`. Registration/clone may also include a validated project
`config`. The GUI reuses the existing project creation flow, including its
folder picker; a programmatic client provides an explicit local path instead.

Configure the orchestrator before its first start. The project `config` accepts
the existing `orchestrator` provider/model/native-effort override plus inline
`orchestratorRules`, an optional `orchestratorRulesFile`, and an optional
`orchestratorPrompt`. The rules file is relative to the registered repository,
is read afresh when a session is created or restored, and is appended after the
inline rules. Invalid, escaping, missing or unreadable files fail clearly rather
than starting an orchestrator without its configured rules.

`orchestratorPrompt` is the initial user task for a newly created orchestrator;
it is separate from standing rules. Opening or resuming an existing session does
not replay it. An explicit `prompt` on the generic `POST /api/v1/sessions` spawn
contract overrides this default for that creation. Save complete configuration
before requesting a first start; changes affect subsequent prompt construction,
not an already-running agent's instructions.

For example, include these fields in the registration `config` or merge them
into a complete existing config before saving:

```json
{
  "orchestratorRules": "Coordinate AO workers and preserve the review chain.",
  "orchestratorRulesFile": ".ao/orchestrator-rules.md",
  "orchestratorPrompt": "Inspect existing work and propose the next authorized step."
}
```

The rules file must exist before launch. The Board exposes these same three
fields during project creation and in project settings, alongside the native
provider, model and effort controls.

Read the project configuration before editing it. The existing project config
endpoint replaces the complete supplied configuration, so preserve unrelated
worker, reviewer, workspace and governance fields. Native choices come from
`GET /api/v1/agents/{harness}/models`; use the selected model's advertised
efforts rather than a universal word such as `max`.

Start with `POST /api/v1/orchestrators`:

```json
{
  "projectId": "REGISTERED_PROJECT_ID",
  "clean": false
}
```

This endpoint resolves the saved project orchestrator configuration, including
its remembered native effort and initial task.
Do not retry an ambiguous native spawn by inventing a second start. Inspect the
project/session state and reconcile uncertainty. The spawn response is not
provider confirmation of a completed turn.

## Take in manual work or a GitHub issue

Manual work does not need an issue. Capture a current snapshot, then send
`POST /api/v1/instructor/commands`:

```json
{
  "command": "neu",
  "snapshotId": "CAPTURED_SNAPSHOT_ID",
  "idempotencyKey": "CLIENT_GENERATED_STABLE_KEY",
  "new": {
    "projectId": "REGISTERED_PROJECT_ID",
    "name": "Update settings",
    "goal": "Explain the requested behavior and acceptance criteria.",
    "source": "manual",
    "sourceRef": "CLIENT_STABLE_INTAKE_REFERENCE"
  }
}
```

GitHub issue intake uses the registered project's resolved repository, not an
arbitrary client-provided repository URL. Browse with
`GET /api/v1/instructor/projects/{projectId}/issues`. The response identifies
the repository and capture time, returns bounded actual issue candidates and
marks candidates that already have a work. A truncated result is not the
complete repository issue list; use
`GET /api/v1/instructor/projects/{projectId}/issues?number=123` to retrieve an
older issue for review. Responses show `bodyTruncated` when the displayed issue
body exceeds the bounded preview.

Send the selected issue to
`POST /api/v1/instructor/projects/{projectId}/issues/intake`:

```json
{
  "repository": "OWNER/REPOSITORY_FROM_CANDIDATE_RESPONSE",
  "number": 123
}
```

The daemon rechecks repository association and the actual issue before work
registration. Repository changes, invalid/closed issues and tracker failures
are explicit errors. Repeating the same issue intake returns its existing
work with the same HTTP 201 response contract. Registration pins the applicable policy and records provenance; it does
not launch a worker, close the issue or modify GitHub.

## Continue, pause or cancel a work

`POST /api/v1/instructor/commands` uses an exact captured snapshot and work scope:

```json
{
  "command": "cancel",
  "snapshotId": "CAPTURED_SNAPSHOT_ID",
  "workIds": ["WORK_ID"],
  "idempotencyKey": "CLIENT_GENERATED_STABLE_KEY"
}
```

Cancellation is separate from `stopp` (pause) and `weiter` (continue). The GUI
confirms cancellation of the captured works. A cancellation request is durable;
pending native effects remain visible until they can be reconciled. The final
`cancelled` state does not mean that branches, worktrees or session history were
deleted. An uncertain delivery cannot be reported as a successful stop.

For retries, retain the key and the complete original request. After a stale
snapshot rejection, read a new snapshot and make a new decision with a new key;
do not silently attach an old approval to changed evidence. HTTP acceptance is
queue admission, not proof that a native agent received or completed the action.

## Ask the strategic Instructor and follow up

Post to `POST /api/v1/instructor/plans`:

```json
{
  "request": "Which works are blocked, and what decisions need me?",
  "snapshotId": "CAPTURED_SNAPSHOT_ID",
  "idempotencyKey": "CLIENT_GENERATED_STABLE_KEY"
}
```

The endpoint returns HTTP 202 and a durable plan job. Poll
`GET /api/v1/instructor/plans/{planId}` or list `/plans`. Results include a
summary, clarification questions, command proposals and intake proposals.
Errors and requested/sent/confirmed native settings remain available for
diagnosis. A follow-up is another request; the existing planner supplies recent
persisted request/summary context.

An answer or proposal is advice. It does not admit commands, create work or sign
human gates by itself. The operator reviews the proposal against its captured
snapshot and invokes the existing command/intake contract explicitly. Protected
publication is still refused when the required isolated Publisher boundary is
unavailable. Local API access does not turn an agent into a protected human
publisher.
