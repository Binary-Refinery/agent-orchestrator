# AO MCP adapter

A standalone Node.js adapter for project-scoped AO operation, diagnostics,
workspace source edits and approved checks. The common tools use the existing
daemon API and do not depend on a model provider.

Read the [operator and deployment guide](../../docs/ao-mcp.md) and
[implementation/acceptance plan](../../docs/ao-mcp-plan.md) before granting
operator or developer access.

## Local setup

Requires Node.js 24+ and an already running AO daemon. From the repository root:

```bash
npm --prefix packages/ao-mcp ci
npm --prefix packages/ao-mcp run build
```

Save a private configuration outside the checkout, using the actual daemon
port from `ao status` and registered project ID:

```json
{
  "daemonUrl": "http://127.0.0.1:3001",
  "transport": "stdio",
  "access": {
    "principal": "local-client",
    "role": "observer",
    "projectIds": ["REPLACE_WITH_REGISTERED_PROJECT_ID"]
  }
}
```

Configure the MCP host to launch:

```bash
node /absolute/checkout/packages/ao-mcp/dist/src/cli.js --config /absolute/ao-mcp.json
```

Stdio is the default. Startup configuration fixes principal, role and project
scope; model-generated tool arguments cannot elevate them. Profiles are
`observer` (default), `operator` and `developer`, with cumulative access.

For optional private HTTP, set `transport` to `http` and add
`"http": {"port": 3334, "tokenEnv": "AO_MCP_TOKEN"}`. Supply a 32–512-character
base64url or hex credential through that environment variable. The endpoint is
`http://127.0.0.1:3334/mcp` and requires a bearer header. It is a loopback
gateway bootstrap, not a public OAuth server. Remote clients need a separately
configured authenticated gateway or private tunnel; see the deployment guide.

## Development and validation

```bash
npm --prefix packages/ao-mcp run typecheck
npm --prefix packages/ao-mcp test
```

Tests cover the adapter boundary with controlled daemon fixtures, an isolated
real daemon smoke when configured, modern SDK HTTP, independent legacy
JSON-RPC and SDK stdio clients. They do not certify actual ChatGPT/Copilot
access, the user's Windows installation or live provider inference. The
dedicated CI workflow validates Linux and Windows on self-hosted runners; jobs
remain queued when the required registrations are unavailable.

The real-daemon smoke is explicitly skipped unless `AO_MCP_TEST_DAEMON` points
to a separately built AO executable. For example, on Linux:

```bash
AO_MCP_TEST_DAEMON=/absolute/path/to/test-ao npm --prefix packages/ao-mcp test
```

It starts an isolated throwaway daemon, checks real API/tool behavior and
registers test work. It does not open the installed app, launch a native model
provider or touch the user's AO data. The dedicated CI workflow builds its own
test executable and sets the variable; a skipped local test is not live-runtime
acceptance.

Workspace updates require the daemon's expected file fingerprint and target an
existing editable text file. Check execution selects existing approved profile
names and runs repository code; it is not an OS sandbox. Checks require a
daemon-managed worker and clean committed source. An authorized worker or local
development client must commit an edit; the adapter has no commit/install/restart
tool. Protected human
approvals, policy overrides, exceptions, publication, merging, arbitrary shell
commands and unrestricted filesystem access are not MCP tools.

The package exports `createAOMcpServer(config, audit?)`,
`createToolCatalog(client, access)`, `DaemonClient`, startup configuration helpers
and `startHttp(config, token, audit?)` from `dist/src/index.js`. These allow
trusted embedding of the same 14 tools without an embedded provider or UI.
Retain the same fixed authorization checks when using the common tool catalog
without an MCP transport. MCP safety hints are advisory; they do not replace
those checks.
