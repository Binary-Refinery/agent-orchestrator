# OpenCode with an approved Backbone context

An Instructor work may opt into a policy-pinned Engineering Backbone context.
Existing work without `policy.backbone` retains its previous behavior and policy
hash. Installing native OpenCode files does not activate this AO contract.

The initial controlled interface is a fresh OpenCode 1.19.0 ACP Chat process,
mapped to the Backbone implementer role, using AO's default permission mode.
TUI execution, read-only roles without OS isolation, live-host adoption,
unqualified configuration and missing qualification are refused. No qualification
is installed by default. Restrictive native permissions and isolated state do not
provide OS confinement.

## Adopting a context

Use the reviewed Backbone resolver for the actual Task Packet and frozen
repository revision. Independently review its selected documents and routing,
then adopt the complete context in the existing Instructor policy. Do not run a
candidate Python resolver from AO, automatically refresh its hashes, or track a
receipt that includes its own bytes. The authority remains the approved policy
and its existing digest. A model's claim that it loaded policy is not admission.

`backbone` is an optional JSON object with these exact fields:

| Field | Meaning |
| --- | --- |
| `schemaVersion` | String `1` |
| `adapterSha256` | SHA-256 of the raw `.engineering-backbone/opencode.json` bytes |
| `contexts` | Map from an existing Instructor role name to its context binding |
| `contexts.<role>.role` | Mapped Backbone Task Packet role |
| `contexts.<role>.taskPacket` | Exact repository-relative Task Packet path |
| `contexts.<role>.context` | Complete approved Backbone resolver output |
| `contexts.<role>.qualification` | Explicit operator qualification attestation |

Every OpenCode execution under a bound policy needs its own matching context and
qualification. A missing entry does not fall back to ordinary OpenCode execution.
Other harness roles without context entries retain their existing behavior;
execution of a mapped non-OpenCode role is currently unsupported. A standalone
project controller needs an explicit durable work before it can use this binding.

AO verifies real Git HEAD, the Task Packet identity/role/head grammar, all selected
whole-file hashes (including references to headings), the active companion,
native configuration and required instructions/procedures. Selected native skill
files are checked against receipt hashes. The other canonical native role/skill
files are checked against raw Git blobs at the already approved receipt revision.
This second check closes native discovery of metadata outside the selected
receipt; it does not change the selected prompt or create another approval.
Git reads disable replacement objects and lazy fetch, discard inherited Git
repository/object overrides and never execute checks declared in the receipt.

## Qualification and runtime boundaries

The operator record pins schemaVersion `1`, harness `opencode`, runtimeVersion
`1.19.0`, exact model, optional immutable modelSnapshot, mapped role, actual
platform and interface `chat`, and configurationContract
`ao-opencode-backbone-v1`. It also carries RFC3339 `observedAt`, status
`qualified`, `evidenceRef`, `evidenceSha256`, and a `cases` map. All six exact cases
must have `result: pass` and a nonempty `evidenceRef`:

- `context_loading`
- `stale_context`
- `tool_boundaries`
- `failed_check_reporting`
- `delegation_context`
- `resume_context`

AO validates completeness and the tuple binding. It does not authenticate an
operator signature, fetch evidence URLs, or prove that the stated observations
are true. Unit fixtures and a canned native provider are not real-model
qualification. The existing Instructor role also pins explicit model and native
effort, including its approved maximum-effort mapping.

Before native bootstrap, AO checks the approved files and closes native config,
plugin/tool, skill and instruction sources. The actual AO-generated `ao-default`
primary receives verified document bytes in its standing prompt. Approved
implementer edit/bash asks are projected while explicit stronger denies remain.
Native internal delegation, unqualified MCP/tools/plugins and bypass are denied.
The actual binary, environment, model, selected primary and permissions require
bounded readback. Errors never include raw configuration or credential values.

Stable per-session native home/XDG state lives under AO's data directory. It does
not import ordinary global state or remote organization configuration. Reusing a
controlled state directory still requires input validation before a fresh process.
Windows and managed configuration search locations need their actual platform
checks; XDG variables alone do not prove closure.

The actual workspace is checked before launch, including asynchronous starts,
and before queued dispatch. A running controller remains bound to the original
policy version, model and effort. Changing its model, agent, permission mode,
configuration, binding or interface requires a fresh controlled replacement.
An existing ACP host cannot be adopted as proof that it loaded this context.
Native history resume in a new process re-establishes the contract; failed or
unknown verification does not imply that a prior process stopped.

## Evidence status

The implementation was reconstructed after workspace maintenance removed the
unpublished checkout on 2026-10-16. Historical tests do not certify these bytes.
The checked-in producer fixture comes from actual Copier installation and the
installed Backbone resolver and guard; it proves data interoperability only.
Fresh full CI validation, independent Full reviews and actual platform/native
results must be read from the paired PR evidence. No merge, release, deployment,
automatic downstream activation or production qualification is implied.
