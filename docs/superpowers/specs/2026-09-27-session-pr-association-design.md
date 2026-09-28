# Session Pull Request Association

## Goal

Show every pull request or merge request that a worker intentionally creates for a session, including work published from another repository, without granting an unrelated repository implicit access to AO's SCM polling, lifecycle automation, or mutation controls.

Success means:

- `ao report --pr-created <url>` makes the change request visible on that session immediately.
- Existing branch-based discovery and `ao session claim-pr` keep producing fully tracked change requests with no behavioral regression.
- A supported change-request URL printed by a session terminal can become a safe suggestion, but terminal text alone never establishes ownership.
- A cross-repository reference remains useful for navigation while checks, reviews, merging, and lifecycle reactions stay disabled until its repository is authorized and the relationship is verified.
- The inspector explains why a change request is present and whether AO is merely linking to it or actively tracking it.

## Product decision

AO will combine T3 Code's structured linking model with Orca's guarded terminal-output detection.

Structured signals are authoritative for association. The worker report contract is the primary creation signal because it is independent of shell, SCM client, and agent harness. Existing explicit claims and observer discovery remain authoritative for full SCM tracking.

Terminal detection is a fallback discovery aid, not an ownership signal. AO scans bounded terminal output for complete supported PR/MR URLs and records them as suggestions. It does not parse command intent, intercept `gh`, depend on a particular shell, or assume every URL printed by an agent belongs to its work.

## Concepts

### Association and tracking are separate

A **session association** answers: “Why is this change request relevant to this worker?”

SCM **tracking authority** answers: “May AO poll and act on this change request as part of the registered project?”

The states are:

| Association state | Meaning | Inspector behavior |
| --- | --- | --- |
| `suggested` | Terminal output contained a valid-looking URL, but no authoritative signal linked it | Show in a compact suggestions section with Link and Dismiss |
| `linked` | A worker report or user action explicitly associated the URL with the session | Show a normal reference card with Open and Copy actions |
| `tracked` | Existing claim/discovery rules verified the repository and PR facts | Show the current full PR card and all permitted SCM actions |

`linked` is deliberately useful without becoming `tracked`. A PR created in an unrelated release repository therefore appears immediately but cannot affect session completion, CI recovery, review injection, merge readiness, or teardown.

### Provenance

Every association carries one or more durable evidence sources:

- `worker_report`: created by `ao report --pr-created`;
- `user_link`: accepted from a suggestion or entered explicitly in the UI/CLI;
- `terminal_output`: detected passively and still suggested;
- `scm_discovery`: attributed by the existing observer;
- `explicit_claim`: created by `ao session claim-pr` or `ao spawn --claim-pr`.

Evidence is stored as a set because the same PR can be reported, detected, and later discovered. The UI derives one explanatory label using the precedence `explicit_claim`, `scm_discovery`, `worker_report`, `user_link`, then `terminal_output`. Provenance does not override repository authorization.

## Data model

Add an append-safe `session_change_request_link` table rather than weakening the invariants of the existing `pr` table.

Required columns:

- `id`;
- `session_id` with cascade deletion;
- `provider`, `host`, `repository`, and `number` as normalized identity;
- canonical display `url`;
- `association_state` (`suggested`, `linked`, or `tracked`);
- nullable `tracked_pr_url`, referencing the logical identity of an existing tracked PR without making the link its lifecycle owner;
- `created_at`, `updated_at`, and nullable `dismissed_at`.

Add `session_change_request_link_source` with `(link_id, source)` as its primary key and `observed_at`. The link's unique identity is `(session_id, provider, host, repository, number)`. Upserts add evidence and only strengthen an association: `suggested → linked → tracked`. A weaker later observation cannot downgrade a stronger relationship. An explicit user dismissal suppresses later terminal-only observations of the same identity but does not block a worker report, user link, claim, or verified SCM discovery.

The existing `pr` tables remain the source of truth for checks, reviews, comments, mergeability, lifecycle reactions, and tracked ownership. Existing PR rows do not need a destructive migration. The read model synthesizes `tracked` links for legacy rows and opportunistically persists them when those rows are next claimed or observed.

## Input paths

### Worker reports

`ao report --pr-created` accepts a complete GitHub PR or GitLab MR URL. During report persistence, each `pr_created` output is normalized and upserted as `linked` with source `worker_report` in the same SQLite transaction as the report and its outputs.

This projection does not change report delivery or worker lifecycle state. Invalid URLs continue to reject the report. Generic `--artifact` URLs are not interpreted as PR associations; callers must use the structured flag.

After persistence, AO may attempt promotion through the same verification boundary used by explicit claims. Failure to promote leaves the reference linked and visible; it does not fail or retract the already-valid worker report.

### Existing SCM discovery and claims

When the observer attributes a PR to a session or an explicit claim succeeds, AO upserts the association as `tracked` with the appropriate provenance. Existing repository, author, branch, takeover, and open-state checks remain unchanged.

Claiming an unrelated repository continues to return `PR_PROJECT_MISMATCH`. The difference is that a previously reported URL remains visible as `linked` instead of disappearing because tracking was refused.

### Terminal-output suggestions

Detection runs once in the daemon's terminal-output path so it also covers background and parked terminals. The detector:

- maintains at most 512 bytes of carry per terminal to handle chunk boundaries;
- accepts only complete HTTP(S) GitHub PR and GitLab MR URLs supported by AO's existing repository parser;
- strips terminal styling before parsing but rejects other control characters inside candidates;
- caps a candidate URL at 2 KiB;
- deduplicates per session and stores at most 20 undismissed suggestions;
- never performs provider network requests merely because a URL appeared;
- does not inspect commands, shell history, agent prompts, or generic chat text.

Terminal echo cannot be reliably distinguished from process output across shells, so it receives no special authority. A pasted or echoed URL is still only a suggestion.

## Promotion and authorization

Promotion from `linked` or `suggested` to `tracked` is allowed only when the current claim service would authorize the repository and live SCM facts verify the association.

For automatic promotion, all of the following are required:

1. the repository matches the project origin, canonical upstream, or a registered workspace child;
2. the provider can fetch the PR/MR;
3. its head repository is eligible for that project;
4. its source branch belongs unambiguously to the session under the existing branch rules;
5. the authenticated identity and open-state checks used by discovery succeed.

The user may explicitly Link a suggestion without satisfying these conditions; that produces a reference-only `linked` card. Tracking remains a separate action. AO must never add an arbitrary Git remote, register a repository, or broaden project configuration as a side effect of linking.

## API and read model

Add provider-neutral session change-request endpoints:

- `GET /api/v1/sessions/{id}/change-requests` returns the union of reference-only associations and existing tracked PR summaries.
- `POST /api/v1/sessions/{id}/change-requests/link` accepts a complete URL and creates a `linked` association.
- `POST /api/v1/sessions/{id}/change-requests/{linkId}/dismiss` dismisses only a suggestion; it never unlinks, closes, or unclaims an authoritative association or provider PR.

The list DTO includes normalized identity, URL, association state, provenance, dismissal state, and an optional existing `SessionPRSummary` payload for tracked entries. The backend, not the renderer, performs deduplication and precedence resolution.

Keep the existing PR endpoints during migration. Full PR actions continue to require a tracked summary and therefore cannot be invoked against a reference-only card. Generated OpenAPI and frontend types change with the controller DTOs.

## Inspector experience

The inspector's PR section renders one row per normalized identity:

- **Tracked:** the existing rich card and actions, with no additional badge unless provenance details are expanded.
- **Linked external repository:** repository-qualified title, PR/MR number, `External repository` badge, provenance text such as “Reported by worker,” and Open/Copy actions. Checks and merge actions are absent, not disabled placeholders.
- **Suggested:** compact row reading “PR link detected in terminal output,” with Link, Dismiss, and Open actions. The URL is never auto-opened.

Repository qualification is always visible when the association's repository differs from the registered project. The UI must not imply that a linked external PR is verified, owned by the current Git identity, safe to merge, or on the session branch.

When a suggestion is promoted or linked, it updates in place rather than producing another card. If a tracked PR already exists, matching report or terminal signals only add provenance evidence and do not reorder or duplicate the card.

## Lifecycle and safety boundaries

- Only existing tracked `pr` facts participate in derived session status, CI-failure recovery, review-comment injection, merge notifications, auto-review, and merge-driven teardown.
- Reference-only associations never trigger provider writes or background polling.
- URL parsing rejects credentials, fragments that alter identity, unsupported hosts/providers, non-positive numbers, and overlong input.
- Terminal detection is bounded in memory and work per chunk. ANSI normalization and URL scanning must not introduce unbounded regular-expression behavior on the PTY hot path.
- Private repository URLs are treated as session metadata and follow the same local persistence and API exposure boundaries as existing PR facts.
- Dismiss and Link are local AO mutations. Merge, close, review, and comment remain unavailable without tracked provider facts.

## Failure behavior

- A report transaction either persists both its structured PR output and association or neither.
- A provider outage during promotion leaves the association linked and records no false tracked state.
- A malformed terminal candidate is ignored without affecting terminal rendering.
- An ambiguous branch match remains linked/suggested and exposes no tracking actions.
- A repository removed from project authorization causes tracked polling to stop under existing rules; the association remains as a reference and the UI explains that tracking is unavailable.
- Repeated terminal output, report retries, and observer rediscovery are idempotent.

## Observability

Record bounded diagnostic events for association creation, strengthening, dismissal, promotion success, and promotion refusal. Refusal reasons use stable categories such as `repository_not_authorized`, `branch_mismatch`, `ambiguous_owner`, `provider_unavailable`, and `not_open` without storing terminal text around the URL.

Telemetry counts source and state transitions but never sends private repository names or complete URLs.

## Delivery slices

1. **Association foundation:** migration, normalized identity helper, store/service rules, report projection, and provider-neutral list/link/dismiss API.
2. **Inspector references:** union read model and linked/suggested presentation with no lifecycle changes.
3. **Tracked convergence:** claim and observer paths upsert tracked associations; legacy PR rows synthesize cleanly and deduplicate.
4. **Terminal fallback:** bounded daemon detector, suggestion persistence, Link/Dismiss flow, and diagnostics.
5. **Hardening and rollout:** full CI, hot-path benchmarks, migration coverage, Electron verification, and documentation updates.

Each slice is independently releasable. The first two solve the cross-repository worker-report case without waiting for terminal scanning.

## Test strategy

Backend tests cover:

- normalization and deduplication across URL casing and aliases;
- monotonic state strengthening and dismissal precedence;
- atomic report plus association persistence;
- cross-repository `pr_created` reports becoming linked but not tracked;
- existing claim mismatch behavior remaining unchanged;
- observer/claim promotion and legacy-row deduplication;
- reference-only associations being excluded from lifecycle status and reactions;
- API validation, missing sessions, and provider-neutral GitHub/GitLab URLs;
- terminal chunk boundaries, ANSI input, control characters, caps, duplicates, and pathological long chunks.

Frontend tests cover tracked, external-linked, and suggested rendering; repository qualification; Link/Dismiss actions; deduplication during promotion; and the absence of rich SCM actions for reference-only entries.

Integration tests exercise a session in repository A reporting a PR in repository B, verifying that the reference appears while project B is unauthorized and that existing same-repository discovery still yields the current rich PR behavior.

Performance verification benchmarks the detector on ordinary large terminal chunks with no `/pull/` or `/-/merge_requests/` marker and on split candidate URLs. The no-candidate path must remain bounded and materially below terminal rendering cost.

## Rollout

Ship report-backed links and the inspector UI first. Gate terminal suggestions behind a local feature flag for one nightly cycle, measuring detector cost and false-positive dismissals. Enable by default after the hot-path and false-positive checks pass. No existing tracked PR is downgraded during rollout or rollback.

## Out of scope

- Parsing shell command intent or wrapping `gh`, `glab`, Git, or provider CLIs.
- Automatically registering repositories or changing project configuration.
- Treating arbitrary URLs in chat messages or generic artifacts as ownership signals.
- Enabling merge, review, comment, or lifecycle actions for reference-only associations.
- General issue, build, deployment, or release-artifact association.
- Replacing the existing SCM observer or normalizing all historical PR storage in this change.
