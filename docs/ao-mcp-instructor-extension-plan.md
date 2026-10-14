# AO MCP Instructor pilot extension

## Scope and delivery

Extend PR #22 (`feat/vendor-neutral-ao-mcp`) so an authorized external agent can
select a native project issue, register it as AO work, ensure an orchestrator
exists, recover its existing controller when permitted, and inspect persisted
member diagnostics while a governed worker/reviewer chain runs.

The isolated implementation branch starts at #22 head `321e5efd9` and includes
main `7c1263109` through a merge. It does not depend on open PR #23. Publish a
separate PR targeting `feat/vendor-neutral-ao-mcp`; the user merges that PR and
then reruns/assesses #22 checks before deciding whether to merge #22 into main.
Do not change runner groups, shared runner labels, deployments or other agents'
branches.

## Requirements and acceptance

| ID | Requirement | Acceptance |
| --- | --- | --- |
| MCP-20 | Native issue browse, exact preview and intake. | `ao_issue_candidates` and `ao_issue_intake` retain repository/number identity, existing-work deduplication and truncation facts; reject unauthorized projects and arbitrary tracker/configuration overrides before effects. Tracker unavailability is not an empty backlog. |
| MCP-21 | Existing daemon orchestrator creation and captured recovery. | `ao_project_orchestrators`, `ao_orchestrator_ensure`, `ao_work_recovery` and `ao_recover_controller` use native APIs. Ensure fixes `clean:false`; recovery preserves revision, policy, session creation timestamp, conversation and controller generation. Admission is not readiness/execution; uncertain writes are not retried automatically. |
| MCP-22 | Bounded diagnostics and timeline under authoritative ownership. | Developer-only `ao_member_diagnostics` and `ao_member_timeline` use new daemon routes with work/project/member incarnation fences. Replaced members fail closed; retained stopped members remain readable. Reviewer/session namespaces and owner/run identity remain distinct. Output is curated, bounded and explicit about unavailable data, redaction and pagination. |
| MCP-23 | Reviewable governed pilot. | Existing work traces include phase-action failures, dispatch receipts and check evidence. A project orchestrator view is explicitly project-wide. No tool approves gates, changes policy, publishes or merges. Tests distinguish protocol/API behavior from a real configured-provider pilot and native client acceptance. |

All eight tools pass through the existing shared catalog, trusted startup
identity, cumulative profiles and explicit project allowlist. Sensitive history
requires the developer profile. There is no direct MCP access to SQLite,
provider authentication files, global event streams, arbitrary log paths,
interactive terminal input or a second orchestration engine.

## Implementation and verification

1. Record the contracts and current base; implement issue and orchestrator tools
   over existing services. Add strict schemas and safe response projections.
2. Add narrowly scoped persisted diagnostic reads in daemon/service/storage and
   expose them through the same MCP boundary. Capture ownership and persisted
   history in one read transaction, rather than relying on an earlier
   membership lookup followed by session-ID-only I/O. No live provider access
   or filesystem read occurs inside this diagnostic operation.
3. Regenerate OpenAPI and frontend types together. Update tool inventory and
   instructions with exact supported facts and explicit limitations.
4. Run meaningful authorization, stale-identity, stopped-session, reviewer,
   secret-field, pagination and uncertain-write regressions; run complete
   affected suites, builds, vet/lint and API drift checks. Record unavailable
   native OS, Docker and real provider/client checks accurately.
5. Obtain independent functional and security reviews. Triage findings, correct
   applicable defects, rerun affected suites and obtain independent delta
   reviews before publishing the child PR.
6. Request GitHub Codex review on the published head, inspect actual CI/review
   results and correct applicable findings. A previous head's pass does not
   certify a later correction. Merge and deployment remain human actions.

## Diagnostic limits

Persisted conversation/controller facts and exact-work evidence help explain
failures across Instructor, orchestrator, worker and reviewer layers. They do
not constitute unrestricted native provider logs or proof that every runtime
supports persisted chat. Unsupported/TUI data must be reported unavailable.
Text redaction reduces known credential exposure; it is not a guarantee that
arbitrary user-written prose contains no sensitive information. Treat issue,
source and conversation contents as untrusted data, never new authority.
