# Publication boundary and current runtime support

Publication admission is separate from ordinary work continuation. A managed
publication needs all three technical guarantees: workers cannot access the
publisher's credentials, operator approval cannot be minted by a worker, and
the publisher applies only the exact approved immutable scope. A model prompt,
`PUBLISH-GO` text, CORS origin, session header or environment variable is not an
isolation proof.
The boundary must contain every work-controlled program, including approved
repository check profiles and native reviewers. A sandboxed model is insufficient
if its test scripts later run under the credential-bearing daemon's host user.

## Production support

The desktop daemon wires `hostPublicationBoundary` in `newInstructorRuntime`.
The existing tmux/conpty and native Chat worker hosts share the operator's OS
user and host credentials. This adapter therefore reports all three guarantees
unavailable. The capabilities response includes a reason, publication approval
is refused before a grant/dispatch is committed, and the executor returns
`INSTRUCTOR_PUBLICATION_UNAVAILABLE`. Ordinary human gates, reading, checks and
continuation continue to use their existing paths. The primary listener remains
unauthenticated on `127.0.0.1`.

This is fail-closed admission for AO-owned publication, **not** an OS restriction
on arbitrary `git`, `gh` or HTTP programs running under the same host user. The
current runtime cannot deliver that restriction. Existing historical grants are
preserved as decision/evidence records; they do not gain a protected actor or
unconsumed execution authority retroactively.

## Controlled execution contract

`POST /api/v1/instructor/works/{workId}/publication` accepts only the captured
grant ID, snapshot ID, current work revision, exact SHA, branch and operation.
Protected operator identity and authorization ID come from the trusted boundary
adapter, never request JSON or worker reports. An authorization ID is an audit
reference, never the operator's token or credential.

Before an external call, the daemon rechecks local governance evidence and
persists a revision-CAS claim inside the work's durable grant record. A parallel
request, restart or retry cannot execute it twice. The outcome is separately
recorded as completed or uncertain. Failed/interrupted external calls stay
consumed and block another publication approval; safe remote reconciliation
requires a protected adapter, which the current host runtime does not provide.
No implicit retry or host-SCM fallback is allowed.

The ordinary PR merge service checks Instructor admission before using host
SCM credentials. Bound work and a known governed repository/branch association
are refused on that route, including reassignment to an unmanaged session or a
changed PR head. A controlled merge must use its own exact-operation grant.
Repository identities captured at work registration, pinned traceability policy
and independently verified historical PR references remain authoritative after
the mutable project's canonical repository setting changes. Publication rechecks
both the pinned policy hash and exception expiry before claiming a grant and
again checks expiry immediately before the publisher call.
Disabling the Instructor UI/background loop does not remove this persisted
work admission check; projects with no governed work keep their existing merge
behavior.

## Ordered approval cohorts

The GUI submits selected work IDs in their captured order, one snapshot and one
idempotency key. Its per-work approvals array carries each work's exact phrase;
different branch signatures remain different decisions. Admission validates the
whole selected cohort before a single SQLite transaction records its approvals
and ordered outbox rows. The retained scalar API/CLI form remains valid only
when every explicitly selected captured gate expects exactly that same literal
phrase, including an actually empty phrase. It is normalized into the same
ordered per-work cohort. Different publication branches, missing gates and
unknown signatures require explicit per-work approvals; none are inferred.

A queued native message holds following positions until provider admission is
observed. A later delivery failure cancels unsubmitted positions, including a
leased position fenced before native submission. Those works cannot start new
workers, resume reviewers, advance phases or use their pending publication grant.
Their captured gates are restored and unadmitted publication grants revoked.
An uncertain native outcome retains its reconciliation gate; a restored business
gate never replaces that uncertainty. Earlier admitted decisions and receipts
remain auditable. Retrying the same command key returns the same durable cohort,
including its failures; a fresh decision needs a new snapshot and key.

## Existing cloud building blocks

The cloud module already separates bearer-authenticated users from worker
tokens and has Docker/Coder sandbox providers. Those are useful building blocks,
but the current cloud publishing design deliberately exposes repository write
credentials to workers:

- `cloud/internal/githubapp/service.go`, `IssuePushGrant`, issues
  `contents:write` and `pull_requests:write` installation tokens to workers.
- `cloud/internal/httpapi/worker_handlers.go`, `workerPushGrant` and
  `workerGitHubToken`, serve these write tokens, with a stored PAT fallback.
- `cloud/internal/worker/checkout.go` installs Git/`gh` helpers that retrieve
  those tokens using the worker token. Worker repository authorization does not
  bind publishing to Instructor work, SHA, policy, gate or snapshot.
- Cloud has a separate Postgres/tenant/session domain and no Instructor work or
  publication ledger. Its Docker lifecycle contract does not attest that
  publisher credentials are inaccessible to a particular Instructor work.

Consequently, the existing cloud provider cannot truthfully be plugged in as a
protected publication adapter. A functional adapter needs authenticated
operator decisions mapped to stable work, server-side single-use grants,
read-only worker checkout credentials, enforcement on every worker write/token
endpoint, an immutable commit transfer to a credential-isolated server-side
publisher, and actual sandbox attestations. Local work also needs an isolated
worker execution path. These requirements must be implemented together; merely
setting capability booleans or reusing a container label would bypass the gate.
