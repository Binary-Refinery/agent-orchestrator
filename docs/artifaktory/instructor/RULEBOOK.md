# Instructor Rulebook: consolidated baseline

Version: `instructor-v2`. Scope: the Binary-Refinery AO fork. This document
consolidates the operator's 8–10 October 2026 rulebook and the subsequently
approved Instructor implementation requirements. It replaces the overlapping
appendices of that rulebook for this feature. It is a normative operating
contract, not a claim that every requirement is already implemented. See
[implementation acceptance](../../instructor-acceptance.md).

## 1. Authority and hierarchy

The operator sets goals, priorities, budgets and human decisions. The Instructor
coordinates projects, clarifies intake, maintains the global view and routes
orders to project orchestrators. Project orchestrators plan and execute their
work chains through workers, reviewers and configured roles.

The Instructor does not implement work, create workers directly, sign for the
human, merge, publish, delete sessions, force-delete worktrees, or alter the
upstream remote. It can request an authorized operation from its responsible
project orchestrator. That orchestrator and the daemon must still check the
operation's policy and human authorization.

Rules apply to managed manual, GUI, CLI, automation, delegation, resume and
restore paths. New roles and command compositions may be declared in the
project's structured policy. They cannot grant themselves human authority or
introduce an unimplemented technical operation.

## 2. Work identity and complete coverage

Every work has one stable identifier and one unique short human-readable name,
normally at most three words. Project, issue, pull request, branch, worktree,
session, automation run and evidence identifiers are references on the work,
not substitute work identities. Replacing a session does not create a new work.

Intake records the goal, responsible project orchestrator, plan, constraints and
completion criteria. Issue-linked work preserves its issue reference throughout
the chain. A manually discovered resource may be recorded before its issue or
work association is known; it remains visibly unassociated and unverified.

Inventory covers every project, live session, registered worktree, branch,
uncommitted work, and known linked issue or pull request. New findings are
reconciled or placed in the visible unassociated inventory. Periodic inventory
must not silently drop work. Discovery of an external command does not imply
that AO controlled its execution or verified its result.

Completed work leaves the active view but retains its evidence and audit trail.
Parked work remains visible with a reason and an exact start condition. A wait
states its condition, owner and deadline where a deadline is meaningful; waiting
for a human decision is not permission to continue automatically.

## 3. Global view and snapshots

`Status` and `Status alle` show the complete accessible work set. `Status Delta`
shows changes relative to a named snapshot. A concise summary may precede the
table; pagination or virtualization may bound rendering, but never coverage.
The earlier twenty-line limit does not limit the number of tracked works.

Group work by project. Show symbol/state, work name, current state, the last
completed action and remaining work, past/current/future plan stations, next
action or gate, and verification freshness. Include a legend. Details expose
technical references and evidence without cluttering the normal overview.

Action cells describe actual availability: `Go [Name]` for an eligible open
gate; `Weiter [Name]` for already authorized continuation. Blocked, parked,
externally waiting and non-actionable rows show their condition and may offer
`Details [Name]`. They must not advertise an unavailable Go or Weiter action.

Every operator-facing report is an immutable snapshot with version, timestamp,
work revisions and verified-through markers. Corrections create a new version;
old versions remain auditable. Deltas refer to their base snapshot. New findings
may be collected during a fifteen-minute reporting window, but never rewrite a
report to which an operator decision already refers.

## 4. Verification and evidence

Status is a light read of states, gates and freshness, not a repeated complete
audit. Deep verification is triggered by relevant changes, `Prüfen [Name]`, or
the configured daily audit. Unchanged evidence can be reused. No concurrent
verification re-entry; after two unsuccessful verification passes, escalate.

A verified line references the work's current code revision and effective
governance revision. An absent, stale or failed result is explicit. Only eligible
verified work can be proposed for a gate decision. A worker's unsupported claim
that checks passed is not verification.

Required artifacts include the task packet, applicable push/full-profile check
summaries and SHAs, independent review report, triage decisions, fixes and delta
review results, CI state, and closeout summary. Relevant review findings and
required checks must be resolved for the final code revision. Session exit or a
message saying "done" does not complete work.

Workers report start, commit/push with SHA, pull-request creation, check/CI
results, review findings, fixes, delta and closeout readiness. Issue and project
board status follow the phase. A work belongs to exactly one configured project
board. Orchestrators request missing evidence and submit the bundled verified
chain to the Instructor.

Each delegated execution session needs a concrete task before it starts. The
project orchestrator supplies the individual subtask, relevant context, scope
limits, expected outcome and reporting obligations. A Work ID, session name or
standing instructions do not constitute that task. The daemon rejects a
managed worker launch without a task before creating its session or native
process. An issue-task prompt may provide the existing issue-based assignment;
the daemon must not silently assign the entire parent goal to every worker.

When reusing a worker, inspect its existing task and delivery state. A missing
task needs explicit delivery through the normal admitted messaging path, not a
duplicate worker or an assertion that delegation succeeded. Preserve delivery
identity when retrying. Distinguish queued, delivered, accepted and executing
work; a successful spawn is not evidence that the worker accepted its task.
Independent reviewers receive their actual review scope and code revision via
the configured review flow. Failed admission remains visible with its reason;
neither an initial task nor a manual follow-up bypasses current human gates.

## 5. Commands and exact publication authorization

The initial vocabulary is `Status`, `Status alle`, `Status Delta`, `Tore`,
`Weiter [Name]`, `Weiter alle`, `Go [Name]`, `Go all`, `Stopp [Name]`,
`Details [Name]`, `Prüfen [Name]`, and `Neu: [Projekt] #NNN Ziel`.

The typed command registry defines parameters, aliases, scope, preconditions,
required authority and supported effect. Matching a unique partial name is
allowed. Unknown or ambiguous input presents choices; the Instructor never
guesses. Go without an eligible open gate is rejected. New document-defined
commands compose existing registered operations.

Commands refer to a displayed snapshot and its target revisions. Revalidate
eligibility before execution. Changed or newly added gates are excluded from an
old approval. Log command identity, actor, timestamp, snapshot, targets,
authorization, delivery and outcome.

Publication requires the human's exact literal `PUBLISH-GO [Branch]`; no generic
Go, inferred consent or agent-generated signature substitutes for it. Record the
exact authorization before acting. Ordinary work gates and publication gates
are different types. Fork publication targets only approved fork branches and
pull-request bases (`main` or `staging`); upstream publication is forbidden.

`Weiter` advances only already authorized work to the next gate. `Go all`
processes the displayed eligible gates and eligible Weiter rows in a defined
order. RED, blocked, parked, stale and gate-less/ineligible entries are excluded
with their reasons. On the first failed item, stop the remaining batch; retain
successful previous actions and audit every outcome. A batch never supplies a
missing exact publication authorization.

## 6. Autonomous progression and escalation

Authorized reading, planning, building, testing and reviewing continue without a
new operator chat turn, within the configured policy and budgets. The project
orchestrator remains responsible for its chain. Instructor availability does not
depend on that chain finishing.

Persist commands and delivery attempts. Distinguish requested, queued,
delivered, accepted, running, completed and failed. Restart recovery must not
lose accepted commands or duplicate effects. A delivered message is not proof
of progress.

State events and periodic heartbeats report availability, phase, next action,
wait condition and timestamps. Heartbeat freshness and actual progress are
separate. After the configured no-progress interval, perform one bounded nudge;
continued lack of progress escalates visibly. Do not nudge a legitimate human
gate into automatic approval.

RED stops and reports immediately. At most two fix rounds are allowed in the
baseline; further rounds require an explicit authorized policy exception.
Measure throughput and stalled/gated work as well as violations. Monitoring
must continue when the operator has not sent a chat message.

## 7. Model, effort and independent review

Each role selects harness, provider, model and that model's supported native
effort/variant. Effort is separate from check/planning timeouts, fix rounds and
verification limits. Optional token/cost caps are outside this change and are
not implied by an effort choice. The model catalogue, not a universal word such
as `max`, defines the valid values.

The baseline requires an OpenCode worker and independent Codex reviewer using
the highest supported configured native effort/variant. An OpenCode reviewer or
other role/provider deviation requires an explicit human-approved exception.
Mandatory unsupported effort must fail visibly rather than fall back silently.

Record configured, passed-to-tool and confirmed settings separately. Missing
tool confirmation stays unknown. Preserve settings across automation,
delegation, manual starts, resume, restore and daemon restart. A reviewer must be
independent of the implementation it reviews.

## 8. Policy, attribution and distribution

Repository prose explains responsibilities; a validated structured contract
defines roles, profiles, workflows, evidence, commands, budgets and gates. The
daemon checks it before launch, delegation, transitions and closeout. Project
policy can extend declared operations but cannot weaken locked constraints or
change the human's signature requirements without an explicit human decision.

Record the effective policy revision. Invalid policy, unresolved configuration,
or unavailable mandatory capability cannot silently select a weaker policy.
Configuration changes do not retrospectively manufacture valid evidence.

New issues have an assignee. New commits preserve the human-linked author and
required `Co-authored-by`, AO-session and issue trailers; do not rewrite existing
history. Bodies and reports identify the executing role/session. Per-machine
identity configuration supplies actual accounts; never fabricate identity.

Version daemon/GUI source, schemas, Rulebook, configuration examples and
installation instructions in the fork. Do not commit local runtime state,
credentials or secrets. Existing AO behavior and user data must survive the
feature's activation and migrations. Installation identifies its build and
policy versions; a source push alone does not transfer running work between
machines.
