# AO MCP access

The adapter in [`packages/ao-mcp`](../packages/ao-mcp/README.md) gives MCP clients
project-scoped tools for routine AO operation, work diagnosis, source edits and
approved checks. It uses the same daemon HTTP API as other AO clients.

The implementation and acceptance requirements are recorded in
[ao-mcp-plan.md](ao-mcp-plan.md). Protocol tests do not prove a live Windows
installation or a particular vendor client has connected.

## Build and start

Use Node.js 24 or newer. From the repository root:

```bash
npm --prefix packages/ao-mcp ci
npm --prefix packages/ao-mcp run build
```

Start the installed AO application normally. Read `ao status` and use the
daemon's actual port. Register or identify the intended project before creating
the adapter configuration; a repository URL or project display name does not
replace its project ID.

Create a local configuration file, preferably under `~/.ao/mcp/`, outside the
checkout. This example uses an illustrative port and project ID:

```json
{
  "daemonUrl": "http://127.0.0.1:3001",
  "transport": "stdio",
  "access": {
    "principal": "copilot-local",
    "role": "observer",
    "projectIds": ["REPLACE_WITH_REGISTERED_PROJECT_ID"]
  }
}
```

Start the adapter with absolute paths:

```bash
node /absolute/checkout/packages/ao-mcp/dist/src/cli.js --config /absolute/ao-mcp.json
```

The default transport is stdio. The process waits for MCP requests on standard
input; it is not an interactive shell. Configure the MCP host to launch the
same command and arguments. The host owns its configuration format, consent
flow and server lifecycle. A minimal process-launch specification is:

| Setting | Value |
| --- | --- |
| Command | Absolute path to Node.js 24+ |
| Arguments | Absolute path to `dist/src/cli.js`, `--config`, absolute path to the local JSON configuration |
| Transport | Stdio |

Do not point the host at the daemon URL as though it were an MCP endpoint. The
daemon speaks AO HTTP; the adapter speaks MCP. A new server configuration also
does not import another chat's history or grant this cloud session access to
the user's computer.

## Access configuration

| Field | Meaning |
| --- | --- |
| `daemonUrl` | Explicit HTTP loopback origin of the running daemon, without credentials or a path. |
| `transport` | `stdio` by default, or `http` for an authenticated private gateway. |
| `access.principal` | Operator-chosen label for this configured client connection. It is not derived from self-reported MCP client metadata. |
| `access.role` | `observer`, `operator` or `developer`; default is `observer`. |
| `access.projectIds` | Nonempty list of registered project IDs this instance may access. |
| `access.sessionId` | Optional trusted native AO session identity forwarded to the daemon. Never accept it from tool arguments. |
| `http.port` | Port for the optional HTTP listener on `127.0.0.1`. |
| `http.tokenEnv` | Environment-variable name holding the HTTP bearer credential: 32–512 base64url or hex characters. The credential itself is not a configuration field. |

Profiles are cumulative: an operator can use observer tools; a developer can
use observer and operator tools. Each instance has one fixed access identity.
Use separate configurations, tokens and listener ports when clients need
different rights. No tool can change its profile, project list or trusted
session identity.

Stdio is host trusted: the local process owner chooses the configuration and
can launch another process with different permissions. The role checks govern
this adapter connection; they are not an OS sandbox against that owner.

## Tools and application behavior

The configured host discovers supported tools with MCP `tools/list`:

| Tool | Minimum profile | Inputs and behavior |
| --- | --- | --- |
| `ao_runtime_info` | Observer | No arguments; reads real daemon health/readiness and available process/runtime metadata. Missing version/build facts remain unknown. |
| `ao_project_view` | Observer | `projectId`; returns a scoped project view rather than the complete private configuration. |
| `ao_instructor_status` | Observer | Optional `projectId` and `baseId`; returns allowed work and snapshot identity, excluding portfolio metrics and protected approval material. |
| `ao_work_trace` | Observer | `workId`, optional `includeMembers` and `includeRecovery` (both default true); reads real work, dispatch, evidence and available controller/member facts. |
| `ao_create_work` | Operator | `projectId`, `name`, `goal`, `sourceRef`, `snapshotId`, `idempotencyKey`; creates manual work using the existing `neu` intake contract. |
| `ao_work_command` | Operator | `workId`, `command`, `snapshotId`, `idempotencyKey`; canonical continuation, pause or cancellation, including registered aliases that resolve to those actions. |
| `ao_workspace_tree` | Developer | `workId`, `sessionId`, optional `path`; lists a session-workspace directory. |
| `ao_workspace_search` | Developer | `workId`, `sessionId`, `query`, optional `cursor` and `limit` (1–100, default 50); searches path substrings, not file contents. |
| `ao_workspace_read` | Developer | `workId`, `sessionId`, `path`, optional `section`; reads existing workspace source and its fingerprint. |
| `ao_workspace_diff` | Developer | `workId`, `sessionId`, `paths`, optional `scope`, `contextLines`, `ignoreWhitespace`, `workspaceVersion`; reads bounded existing daemon diffs. |
| `ao_workspace_update` | Developer | `workId`, `sessionId`, `path`, `content`, `expectedFileFingerprint`; replaces existing editable text with an optimistic concurrency check. |
| `ao_check_profiles` | Developer | Discovers approved check-profile names and timeouts without exposing executable argv; use these names for check execution. |
| `ao_run_checks` | Developer | `workId`, `sessionId`, `operationId`, `profiles`; starts approved daemon check profiles and returns a process-local job handle. |
| `ao_check_job_status` | Developer | `jobId`; returns the owning principal's check-job state after current scope validation. |

The work-command adapter resolves registered names/aliases to the canonical
`weiter`, `stopp` or `abbrechen` action. Other actions, including `go`, are
refused. The accepted command spelling remains the daemon's responsibility.

Trace responses distinguish available evidence from missing/unknown provider
confirmation. No tool produces a verified result merely because a worker
asserted success.

Source operations use session-workspace-relative paths and incarnation-scoped
daemon workspace routes. The adapter reads a proven current work member, then
sends its immutable `instanceId`, work and project to the daemon. The daemon
validates that binding atomically and holds a lifecycle lease through filesystem
access. Historical display IDs and legacy associations do not authorize a new
session that reuses the same ID. Older daemons without these routes fail closed;
upgrade the daemon together with the adapter. An edit requires the
`fileFingerprint` obtained from the current
read, forwarded as `expectedFileFingerprint`. Refresh and reconsider the edit
after a conflict; do not replace the expected fingerprint blindly.

The current update contract replaces existing editable text files. It does not
create arbitrary files or provide an unrestricted filesystem. A governed AO
worker can create new source files as part of its authorized task. Binary or
oversized files and truncated content must not be treated as complete editable
source snapshots.

Paths use forward slashes relative to the session workspace. Absolute paths,
backslashes, drive prefixes, empty/dot/traversal segments and control characters
are refused; the daemon retains responsibility for filesystem containment.

Check execution selects existing approved profile names for the attached work
and session. Profile names are opaque, bounded to 512 characters and subject to
AO's reserved-name rules. The tool does not accept shell strings, arbitrary executable paths
or new check-profile definitions. Approved checks can execute mutable repository
code; developer access therefore remains host trusted. A configured profile may
include a build, but the adapter has no arbitrary build, install, restart or
production deployment tool.

The existing checks contract requires a daemon-managed worker and clean
committed source. After a source edit, an authorized governed worker or local
development client must prepare the commit before these checks can run. This
adapter does not expose a commit tool. Source access and check profiles alone
therefore do not provide a self-contained edit-to-install workflow.

Check jobs are process-local adapter state, not a replacement for durable daemon
evidence. Retain a caller-generated `operationId`: the same principal and exact
arguments return the existing handle, while changed arguments conflict. States
are `running`, `completed`, `failed` or `uncertain`. A timeout can leave the daemon
executing; inspect durable AO state before any new attempt. An uncertain job
retains the adapter's session/work lock, and source edits to a locally locked
session are refused.

An instance retains at most 64 jobs and allows at most four running/uncertain
jobs. It does not evict completed jobs and thereby make an old operation ID
executable again. Restart loses its handles, deduplication records and local
locks; it neither cancels daemon work nor proves execution has finished.
Reconcile the daemon's durable evidence before a restart or new operation. Each
request accepts at most 16 unique profiles; their summed configured timeout is
bounded to 45 seconds to fit the ordinary REST deadline. Discovery marks longer
profiles unavailable with `CHECK_DEADLINE_LIMIT`. The daemon also checks its
actual remaining request deadline before execution, reserving five seconds for
cleanup; a shorter configured deadline can reject an otherwise available
profile. Long builds require the existing governed worker flow. The adapter's
request timeout is fixed at 55 seconds, leaving time for repository preflight
even when a selected check is short. Source updates are limited to 256 KiB of
UTF-8 content.

Normal operation uses AO actions, not mouse clicks. MCP does not provide window
screenshots, renderer inspection or general desktop control. Use Computer Use
or UI tests for a visual or click-specific failure.

## Safe work commands

Read a fresh status snapshot before a lifecycle command. Supply its exact
`snapshotId`, the intended `workId` and a stable `idempotencyKey`. Command admission is not
proof that a native agent received or completed the instruction.

Retain the full original request and key when retrying an unchanged admitted
command. After a stale snapshot rejection, inspect the current state and make
a new decision with a new key. After a timeout or transport failure, an effect
may already have occurred: inspect work receipts before retrying. The adapter
does not automatically repeat mutations.

Manual creation also forwards the supplied snapshot and key, but the existing
daemon `neu` branch deduplicates by project, `source: manual` and `sourceRef`.
`sourceRef` is an opaque 1–180-character value: spaces and slashes are retained
exactly, including leading/trailing spaces.
It does not provide lifecycle-command snapshot/key fencing for that intake.
Use a stable, unique `sourceRef` for one intended work and inspect the returned
work before deciding whether a retry is appropriate. Changing the name or goal
while reusing that source identity does not update an existing work.

Protected approvals, policy changes, exceptions, publication and merging are
not agent tools. The adapter never manufactures a `GO` phrase from a model's
response. Existing human gates remain pending until the operator uses the
appropriate trusted AO flow. A normal continuation command is not permission
to bypass a gate. See [Instructor API](instructor-board-api.md) and
[session recovery](instructor-session-recovery.md) for those contracts.

## Private HTTP gateway deployment

HTTP is opt-in and listens only on `127.0.0.1`. Configure the private instance:

```json
{
  "daemonUrl": "http://127.0.0.1:3001",
  "transport": "http",
  "access": {
    "principal": "private-gateway",
    "role": "observer",
    "projectIds": ["REPLACE_WITH_REGISTERED_PROJECT_ID"]
  },
  "http": {
    "port": 3334,
    "tokenEnv": "AO_MCP_TOKEN"
  }
}
```

Supply a randomly generated 32–512-character base64url or hex credential through the launch environment or a
secret manager. Do not commit it, put it in a URL, or include it in a model
prompt. The private gateway sends `Authorization: Bearer <credential>` to:

```text
http://127.0.0.1:3334/mcp
```

This loopback bearer endpoint is for clients or gateways that can supply the
header. It does not implement OAuth discovery, authorization, account linking
or a public TLS endpoint. A remote host requiring OAuth needs a compatible
gateway that authenticates that host and protects the local bearer hop.

Use an outbound private tunnel or another operator-managed gateway rather than
exposing the AO daemon. The upstream gateway must preserve the MCP request,
response and streaming behavior, validate the remote client, enforce its own
access policy and map it to the intended fixed adapter instance. Retain the
bearer check on the local hop. Do not expose the bare loopback adapter through
an unauthenticated public forwarder.

An OpenAI-specific tunnel is a possible deployment choice where available; it
is not a dependency of the common tools and does not establish Copilot or
another provider's connection. Verify every intended client's supported
transport, authentication and account configuration separately. No gateway,
tunnel, public endpoint or Windows installation is provisioned by building
this package.

## Connection and live acceptance

Use a disposable AO data profile and throwaway work before normal operation.
The first live connection should report the daemon's actual identity, project
and captured work state through tools. It must not copy values from this guide
as proof of access.

For provider acceptance, connect the actual selected ChatGPT and Copilot
clients, each with its own configuration. Check the same allowed project and
work, routine operation, expected permission failures and the same factual
trace. Modern SDK HTTP, independent legacy JSON-RPC and SDK stdio tests
establish protocol behavior only.

For the original AO failure, record the installed build, failing request and
response, work ID, controller generation and relevant dispatch receipts. After
a correction, build the candidate and repeat the real flow in an isolated
runtime. A healthy daemon, simulated provider, or successful MCP handshake
alone does not verify that repair. This source change has not inspected the
user's running Windows daemon or reproduced that failure.

## Reuse inside AO

The built package's `dist/src/index.js` exports:

| Export | Purpose |
| --- | --- |
| `createAOMcpServer(config, audit?)` | Creates an SDK MCP server with the same fixed profile and project scope; connect it to the host's chosen transport. |
| `createToolCatalog(client, access)` | Returns the shared, role-filtered tool definitions for a trusted embedding host. Each definition validates its inputs and preserves the common authorization behavior. |
| `DaemonClient` | Uses the existing loopback HTTP API, bounded responses and trusted optional session identity. |
| `configSchema`, `loadConfig` | Validate the startup contract or load its local JSON representation. |
| `startHttp(config, token, audit?)` | Starts the optional authenticated loopback transport. It does not create an external gateway. |

Tool metadata contains conservative read-only/destructive/open-world hints.
These are advisory descriptions, not authorization grants or a containment
mechanism. The server emits minimal audit events (time, principal, tool,
outcome/error code) to stderr by default; an embedding host can provide an audit
sink. Tool arguments, source content and bearer credentials are not audit fields.

A host supplies the same fixed access policy and daemon client; the tool
functions are independent of a particular model API. The embedding host must
still enforce access and keep principal/session identity out of model-generated
arguments. Reusing the functions does not add a provider, automatic provider
selection or a new AO chat interface.
