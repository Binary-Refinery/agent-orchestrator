# Controlled OpenCode Backbone integration

## Authority, baseline and recovery

User approval covers implementing, checking and independently reviewing the paired
Engineering Backbone and AO changes, then preparing PRs and requesting visible
Codex review. AO starts at `6b1ba43f1df9415c1ec937876ce54c86aa37484e` on
`feat/opencode-backbone-governance`. The linked contract is
`engineering-backbone/plans/opencode-governance.md` (BB-OPENCODE-010).

Workspace maintenance removed the unpublished implementation and evidence on
2026-10-16. This is a reconstruction. Historical test and review outcomes are not
acceptance evidence for reconstructed bytes. Implementation waits for a fresh
independent plan review; the final changes receive fresh checks and two Full
reviews, separate triage and any necessary Delta review.

This is HIGH-risk admission/control-plane work. No merge, release, deployment,
settings change, downstream activation, new listener, database schema or second
workflow/state authority. Preserve unbound sessions and absent-binding policy
serialization/hash exactly. Existing user authorization supplies repository scope;
no unsolicited upstream Discord messages or new permission round is needed.

## Contract and implementation boundaries

1. Extend `domain.InstructorPolicy` with an optional `backbone` field, omitted when
   absent. Schema version 1 binds the raw companion SHA-256 and contexts keyed by
   existing Instructor role. Each context holds the mapped Backbone role, exact
   repository-relative Task Packet and the complete approved resolver context.
   The existing pinned policy digest is the authority; do not execute candidate
   Python, automatically repin, or create tracked self-referential receipts.
2. Add a bounded, data-only verifier under `backend/internal/policy/backbone`.
   Reject duplicate/unknown/case-folded JSON fields, null scalar substitutes,
   invalid UTF-8, excessive depth/bytes/documents/prompt bytes, traversal,
   symlinks/reparse points, and case ambiguity. Match actual Git HEAD to receipt
   revision, role/id to the explicit Task Packet, and nonnull packet head to the
   receipt. Preserve the existing grammar allowing null head for active planner
   or implementer; frozen reviewer/ready/completed packets require a head.
   Anchored references hash the full file and resolve real Markdown headings,
   including proper fenced-block handling. Mandatory receipt documents include
   companion, native config, explicit Task Packet, selected native role and
   applicable procedures/instructions. Semantic completeness is established by
   the approved resolver and trusted policy adoption, not arbitrary candidate
   receipts. At least one test fixture is produced by the reconstructed Backbone.
3. Qualification is an explicit operator attestation, with no qualified seed:
   schemaVersion 1, harness opencode, runtimeVersion 1.19.0, exact model and
   optional immutable modelSnapshot, mapped role/platform/interface,
   configurationContract `ao-opencode-backbone-v1`, RFC3339 observedAt, status
   qualified, evidenceRef/evidenceSha256, and passing cases context_loading,
   stale_context, tool_boundaries, failed_check_reporting, delegation_context and
   resume_context. Reject missing, unknown, mismatched or nonpassing records.
   Checking an attestation does not prove its truth, authenticate a signature or
   fetch its evidence URL. Synthetic records only test admission.
4. Governed OpenCode is initially restricted to a fresh ACP/Chat process,
   implementer, default permission mode. Governed TUI start, restore and handoff
   remain blocked even with qualification: AO cannot intercept native UI model
   or configuration changes. Existing read-only OpenCode sandbox rejection stays.
   Do not adopt already running ACP hosts; a reconnect without a new controlled
   process fails closed. Model, role, binding, config or interface changes require
   replacement and must be rejected before native setter or capability calls.
5. Thread this through existing Instructor policy/admission/rebind, daemon,
   session_manager and Chat/controller boundaries. Verify after the workspace
   exists but before native probing/start, including asynchronous starts, resume,
   recovery and interface transition. Revalidate queued turns immediately before
   actual dispatch through the existing incarnation-fenced admission hook.
   Child-role handoffs resolve their own pinned context. Do not bypass the
   existing work ownership, runtime generation or dispatch admission fences.
6. Bind verified document bytes to the actual AO-generated `ao-default` standing
   prompt. Do not assume an installed `backbone-implementer` is selected.
   Static source/input checks precede `--version`, `debug config` and `debug agent`
   since native bootstrap may load plugins. Bound probes by time/output and
   sanitize errors. Probe the exact launched binary, cwd and environment; verify
   version, chosen primary, model, prompt and effective permission rules.
   Deny internal delegation/task, unqualified MCP, tools, plugins and bypass.
   Only a byte-verified AO-owned hook may be admitted if necessary.
7. Project the approved implementer edit/bash `ask` rules onto the generated AO
   primary while retaining explicit stricter denies. The Backbone root wildcard
   deny alone would make the generated agent unusable; blanket override would
   weaken local policy. Validate ordered effective rules and reject effective
   write/shell allow. Reject alternate mode/agent/config layers including legacy
   mode/modes, recursive extra SKILL.md and all unbound input sources.
8. Native discovery reads all eight canonical skill metadata files while a
   receipt contains only relevant procedures. Verify selected skill bytes by
   receipt hash and nonselected canonical skill bytes by raw blob at the already
   approved receipt commit. Use only fixed canonical paths, `git
   --no-replace-objects --no-lazy-fetch`, neutralized inherited GIT repository and
   object overrides, bounded output/time and no fetch/filter/smudge. Reject
   external/global/ancestor skills and unbound implicit/explicit instructions.
   Document the distinct receipt-hash and pinned-tree evidence. Cover replaced
   object/environment attacks with data-only unit cases.

## Expected source scope

- `backend/internal/domain/instructor.go` and the optional Backbone domain types.
- `backend/internal/policy/backbone/*` verifier and meaningful boundary tests.
- `backend/internal/service/instructor/*` policy validation, launch, resume,
  dispatch admission and rebind checks; existing unchanged policy digest tests.
- `backend/internal/daemon/instructor*.go`, existing daemon wiring where required.
- `backend/internal/session_manager/*` governed admission and launch/recovery paths.
- `backend/internal/service/chat/*`, `backend/internal/ports/chat*.go`, and native
  ACP/OpenCode adapter files for immutable binding and fresh-process enforcement.
- `backend/internal/adapters/agent/opencode/*` and
  `backend/internal/adapters/chatdriver/{acp,nativeacp,opencodeacp}/*` bounded native
  validation/configuration and regression tests.
- Generated `backend/internal/httpd/apispec/openapi.yaml` and
  `frontend/src/api/schema.ts`, regenerated together through `npm run api`.
- `.github/workflows/go.yml` focused actual Windows path/boundary coverage.
- This plan/Task, `docs/opencode-backbone.md`, `docs/README.md`, `docs/instructor.md`.

No broad cleanup or changes to legacy selection/default behavior. Helpers own
nonoverlapping files, report exact paths and tests, and do not publish remotely.

## Validation and publication

Run focused boundaries first, then the complete relevant workflow commands with
Go 1.27.1 and golangci-lint 2.13.2: gofmt, `go build ./...`, `go vet ./...`,
`go test -race -count=1 -timeout=20m ./...`, lint, API/sqlc generation drift,
frontend typecheck/build and applicable package checks. Review all CI workflow
jobs. Report unavailable native OS/Docker/credential-dependent jobs explicitly;
verify them remotely and never describe them as local PASS. No publishing or
production deployment used for validation. Native canned provider behavior,
operator qualification, actual model chain and OS isolation are distinct claims.

Freeze base/head/tree/patch after checks. Two independent read-only Full reviews
cover behavior/compatibility and authority/security; separate triage confirms
findings, repairs receive scoped Delta checks/review. Up to eight repair
iterations. No retry of the previously blocked dynamic adversarial reproduction;
use static and data-only regression coverage with explicit limitations.

Root owns remote preservation checkpoints and PR publication. A pending-state
workbranch checkpoint is not acceptance or activation. Before PR handover follow
`.agents/skills/pr-description/SKILL.md`, verify category diff counts, remote tree,
visible `@codex review` and actual CI/review outcomes. No auto-merge.
