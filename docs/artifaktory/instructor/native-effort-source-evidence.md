# Native effort source evidence

Inspected on 2026-10-11. OpenCode v1 branch commit
[`dfb56f76c7637d5d34167bba405fe23f916f37e9`](https://github.com/anomalyco/opencode/commit/dfb56f76c7637d5d34167bba405fe23f916f37e9)
syncs release versions for v1.19.0. This is a source compatibility reference,
not proof of the operator's installed provider version or account capabilities.

| Adapter assumption | Primary native source |
| --- | --- |
| `models --verbose` emits a provider/model line followed by its full model JSON. Native model metadata supplies variants; names do not imply a quality ranking. | [models command](https://github.com/anomalyco/opencode/blob/dfb56f76c7637d5d34167bba405fe23f916f37e9/packages/opencode/src/cli/cmd/models.ts) |
| Plugin `chat.message` may set the message variant; `chat.params` may change the actual inference options. | [typed plugin hooks](https://github.com/anomalyco/opencode/blob/dfb56f76c7637d5d34167bba405fe23f916f37e9/packages/plugin/src/index.ts) |
| Request preparation merges model/agent/selected variant options, then calls `chat.params`. Inference receives those prepared options. | [request preparation](https://github.com/anomalyco/opencode/blob/dfb56f76c7637d5d34167bba405fe23f916f37e9/packages/opencode/src/session/llm/request.ts), [inference](https://github.com/anomalyco/opencode/blob/dfb56f76c7637d5d34167bba405fe23f916f37e9/packages/opencode/src/session/llm.ts) |
| `client.app.agents()` reads the native `/agent` inventory. Custom agents may be primary, subagent, or all. | [generated SDK client](https://github.com/anomalyco/opencode/blob/dfb56f76c7637d5d34167bba405fe23f916f37e9/packages/sdk/js/src/gen/sdk.gen.ts), [SDK documentation](https://opencode.ai/docs/sdk/#app) |
| Native agent information includes `mode` and optional `hidden`. Built-in title, compaction, and summary are hidden primary utilities. Older generated SDK declarations omit `hidden`; absence is not an exemption. | [native agent schema and defaults](https://github.com/anomalyco/opencode/blob/dfb56f76c7637d5d34167bba405fe23f916f37e9/packages/opencode/src/agent/agent.ts), [generated types](https://github.com/anomalyco/opencode/blob/dfb56f76c7637d5d34167bba405fe23f916f37e9/packages/sdk/js/src/gen/types.gen.ts) |

The plugin therefore refuses alternate custom primary/all/unknown agents when a
mandatory native variant is configured. Advertised subagents and proven hidden
built-in utilities keep their own native model choices. Unknown model capability
and missing/disabled requested variants refuse visibly instead of selecting a
universal `max` token. Tests execute the actual AO plugin source with typed native
protocol fixtures; installed providers are unavailable in this test environment.

## Native global Instructor planning

The global planner uses an unpublished ephemeral native Chat conversation in an
isolated AO planning workspace, with provider-enforced read-only mode and no AO
launch credentials or supplied MCP servers. Native ACP currently rejects this
read-only requirement before launching or adopting a process; Codex provides
the enforced sandbox.

Codex planning additionally requires explicit empty effective-thread inventories
from `mcpServerStatus/list` and `app/installed`, as defined in AO's pinned generated
[Codex protocol](../../../backend/internal/adapters/chatdriver/codexappserver/codexproto/protocol.gen.go).
Configured failed MCP servers, installed connectors, unavailable methods,
incomplete replies, or unbounded pagination refuse the planning turn. The checks
do not reload servers or refresh connectors. Operators must configure a native
provider thread without external tools to enable strategic planning.

Requested, sent, and native-confirmed model/effort remain separate facts. The
planner records sent selection after successful Start, checks actual Codex
thread-open values when returned, and rejects known mismatches or model reroutes.
Missing fields remain unknown. The JSON output is advisory; the existing
Instructor admission and approval path is the only way to apply proposals.
