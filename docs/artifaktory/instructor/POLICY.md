# Repository policy example

This is an example for an AO source checkout, not an installation-wide policy.
Before activation, replace the model IDs and effort placeholders with explicit
values advertised by the installed harness/model catalogues. Choose the intended
worker and reviewer settings independently. A native variant's name or position
in a catalogue does not establish a universal quality ranking.
Replace the example author identities, project owner/number and board status
names with the operator's real repository configuration. They are exact facts
to verify, not permission to synthesize evidence or edit existing history.

The repository loader defaults to `.ao/governance.json`. It can also load a
configured relative Markdown path containing exactly one `ao-policy` block,
such as this file. The optional `AO_INSTRUCTOR_POLICY` selects an installation
policy. Do not assume that a JSON example alone has connected a project's real
check commands or that an unsupported effort will silently fall back.

```ao-policy
{
  "schema": 1,
  "version": "ao-source-instructor-v2",
  "initialPhase": "intake",
  "roles": {
    "instructor": {
      "harness": "codex",
      "model": "configured-instructor-model",
      "effort": "configured-instructor-effort"
    },
    "orchestrator": {
      "harness": "codex",
      "model": "configured-orchestrator-model",
      "effort": "configured-orchestrator-effort"
    },
    "worker": {
      "harness": "opencode",
      "model": "provider/configured-worker-model",
      "effort": "maximum",
      "effortByModel": {
        "provider/configured-worker-model": "configured-worker-variant"
      }
    },
    "reviewer": {
      "harness": "codex",
      "model": "configured-reviewer-model",
      "effort": "maximum",
      "effortByModel": {
        "configured-reviewer-model": "configured-reviewer-effort"
      },
      "independent": true
    }
  },
  "profiles": {
    "push": {
      "command": ["npm", "run", "lint"],
      "timeoutSeconds": 900
    },
    "full": {
      "command": ["go", "-C", "backend", "test", "-race", "./..."],
      "timeoutSeconds": 900
    },
    "full-vet": {
      "command": ["go", "-C", "backend", "vet", "./..."],
      "timeoutSeconds": 900
    },
    "full-build": {
      "command": ["go", "-C", "backend", "build", "./..."],
      "timeoutSeconds": 900
    },
    "full-typecheck": {
      "command": ["npm", "run", "frontend:typecheck"],
      "timeoutSeconds": 900
    }
  },
  "transitions": [
    {"from": "intake", "to": "implement", "role": "worker", "requires": []},
    {"from": "implement", "to": "review", "role": "reviewer", "requires": ["push", "full", "full-vet", "full-build", "full-typecheck"]},
    {"from": "review", "to": "triage", "role": "orchestrator", "requires": ["review-report"]},
    {"from": "triage", "to": "fix", "role": "worker", "requires": ["triage"], "humanGate": true},
    {"from": "fix", "to": "delta", "role": "reviewer", "requires": ["push", "full", "full-vet", "full-build", "full-typecheck"]},
    {"from": "delta", "to": "closeout", "role": "orchestrator", "requires": ["delta"]},
    {"from": "delta", "to": "triage", "role": "orchestrator", "requires": ["delta-changes"]}
  ],
  "closeoutRequires": ["task-packet", "push", "full", "full-vet", "full-build", "full-typecheck", "ci", "review-report", "triage", "delta", "attribution", "traceability"],
  "attribution": {
    "authorName": "Configured Operator",
    "authorEmail": "configured-operator@example.invalid",
    "coAuthorName": "AO Worker",
    "coAuthorEmail": "configured-ao-worker@example.invalid",
    "sessionTrailer": "AO-session",
    "issueTrailer": "Issue"
  },
  "traceability": {
    "required": true,
    "requireAssignee": true,
    "allowedRepositories": ["Binary-Refinery/agent-orchestrator"],
    "allowedBaseBranches": ["main", "staging"],
    "projectOwner": "Binary-Refinery",
    "projectNumber": 1,
    "boardStatuses": {
      "intake": "Todo",
      "implement": "In Progress",
      "review": "In Review",
      "triage": "In Review",
      "fix": "In Progress",
      "delta": "In Review",
      "closeout": "Done"
    },
    "phaseLabels": {},
    "requiredPhases": ["start", "commit", "push", "pr", "ci", "review", "fix", "delta", "closeout"]
  },
  "maxFixRounds": 2,
  "nudgeAfterSeconds": 300,
  "escalateAfterSeconds": 900,
  "commands": [
    {
      "name": "Fortsetzen",
      "aliases": ["Resume work"],
      "action": "weiter",
      "description": "Continue already authorized work to its next gate",
      "mutates": true,
      "snapshotRequired": true
    }
  ]
}
```

## Contract fields

`schema` selects the supported grammar. `version` in the example is a human
label; the loader replaces it with the hash of canonical structured policy
content, excluding that label. Changes to Markdown explanation alone do not
change the effective policy. `initialPhase`, transitions and closeout requirements
describe the work chain. A transition's `role` must name a declared role;
`humanGate` requires a human decision before that transition.

`requires` and `closeoutRequires` match verified evidence kinds or profile names.
The locked baseline evidence names are `task-packet`, `push`, `full`, `ci`,
`review-report`, `triage`, `delta`, `attribution` and `traceability`.
Add repository-specific profiles for every required CI job; this
example's commands are illustrative and do not claim to cover native packaging,
frontend build/tests, API/sqlc drift or all workflow jobs.

The locked baseline keeps `worker.harness=opencode`, `reviewer.harness=codex`,
`reviewer.independent=true` and `effort=maximum` for those roles. Here `maximum`
is an operator-approved policy intent, **not** a native tool argument. Each
`effortByModel` entry maps a concrete model to the explicit native value the
operator chose. Missing mappings reject managed launches; the daemon must not
guess from variant names or order. Additional roles may select an explicit
native effort or use the configured-profile behavior supported by the runtime.

Profile commands are fixed argv arrays, not interpolated shell scripts. They run
in the attached session's actual Git worktree with a bounded timeout (one to
nine hundred seconds). The verifier records Git HEAD before and after and
requires clean, revision-bound results. Do not put credentials in policy files.

`commands` extends names/aliases for the composable built-in actions `status`,
`delta`, `tore`, `weiter`, `go`, `details`, `pruefen` and `stopp`. Mutating commands
retain snapshot and gate checks. An alias never changes human authority or
replaces exact `PUBLISH-GO [Branch]` authorization.

## Exact commit and tracker evidence

`attribution` verifies the final immutable Git commit's author name/email and
its Git-parsed trailer block. The `Co-authored-by` identity must match the
configured AO co-author, `AO-session` must be the attached evidence producer
and `Issue` must equal the work's recorded issue ID. Merely mentioning these
strings in the body, conflicting duplicate identity trailers or a different
commit cannot satisfy the verifier. Verification never rewrites a commit.

`traceability` uses bounded read-only GitHub CLI API queries against
`github.com`. The real PR must belong to an attached session, have the exact
work SHA/source branch, and target one explicitly allowed repository and base
branch. An empty allowed-repository list grants nothing. Issue IDs may be
`#123`, `123`, `owner/repository#123` or an exact GitHub issue URL in that same
repository. The real issue must have an assignee. Current board status is read
from GitHub Projects v2 and matched to `projectOwner`, `projectNumber` and
`boardStatuses[work.phase]`; optional `phaseLabels` are checked separately as
issue labels. The issue must belong to exactly one observed project board.
If `projectOwner` is empty it defaults to the repository owner.

The GitHub CLI must be installed and already have read access, including
project access. Missing tooling, permissions, unsupported tracker providers,
unknown board status and truncated provider history produce a visible error
and leave closeout blocked. The verifier does not sign in, change permissions,
post comments, move board items or publish anything itself.

After each actual phase, the project orchestrator posts a short factual issue
comment and updates the configured board through its already authorized
tracker workflow. Include this machine-readable block in the comment:

````text
<!-- ao-phase:v1 -->
```json
{"workId":"actual-work-id","policyVersion":"actual-pinned-policy-hash","sha":"actual-40-character-commit","phase":"review","sessionId":"actual-attached-session-id"}
```
````

The mandatory checkpoint names are `start`, `commit`, `push`, `pr`, `ci`,
`review`, `fix`, `delta` and `closeout`. Comments must refer to this work, its
pinned policy and an attached producer; their provider creation time must not
precede work intake. Historical `start`, `review` and `fix` comments retain the
immutable SHA actually used at that time. The verifier requires that commit to
be an ancestor of the final SHA in the attached Git repository. `commit`,
`push`, `pr`, `ci`, `delta` and `closeout` comments must reference the final SHA.
It verifies chronological order within the historical review/fix/delta chain
and the final commit/push/PR/CI/delta chain. Do not restate historical activity
as if it ran on a later commit or invent a phase to fill a missing record.
If the correction phase found no changes to make, state that result explicitly
and still obtain the required independent delta review. The issue's historical
comments and the durable work events preserve earlier progress.

Workers and orchestrators fetch `ao instructor details WORK_ID`, retain the
work ID when delegating, execute `ao instructor checks WORK_ID --session
SESSION_ID --profile push,full` for configured profiles and submit claim JSON
with `ao instructor verify WORK_ID --input evidence.json`. Each claim carries
`workId`, `policyVersion`, `sha`, `producer`, `role`, `kind` and `ref`. Attribution
uses the worker producer; traceability's `ref` is the actual tracked PR URL.
Task packets are committed at `.ao/task-packets/WORK_ID.md` so the daemon can
discover them automatically; references remain repository-relative and bound
to the same SHA. The discovery loop also verifies recorded independent reviews
and observed PR/CI facts; assertions alone are never promoted. Explicit verify
is available to surface a concrete missing-evidence error. CI references use
the actual tracked PR; review/triage/delta references
use the recorded independent review run. A client `verified` flag or outcome
does not authorize acceptance.
