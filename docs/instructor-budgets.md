# Instructor execution budgets

`policy.budgets` bounds daemon-managed work execution independently of fix
rounds, check timeouts and verification passes. New default policies use:

| Field | Default | Counts |
| --- | ---: | --- |
| `maxConcurrentSessions` | 8 | Active/reserved worker, coordinator and reviewer resources for this work |
| `maxSessions` | 32 | Lifetime immutable session instances and reviewer-row incarnations, including replacements |
| `maxTurns` | 256 | Actual AO-owned native turn reservations, including retries and uncertain attempts |
| `maxDelegations` | 24 | Child session/reviewer resources, independently of direct session starts |

Each limit accepts integers from zero through 1,000,000; zero explicitly means
unlimited. An absent `budgets` object preserves legacy policy JSON and its hash.
Changing a global policy affects future work, never silently rewrites contracts
pinned to existing work. A restore/reconnect of the same inserted session
reacquires concurrent capacity, without pretending that it is a new session or
resetting lifetime counters. New replacement rows always consume another slot,
even if their display session ID was reused.

Work binding and session admission share one SQLite writer transaction, guarded
by the work revision and immutable session instance. Native controller/turn
entry points reserve before their external effect. The ledger survives daemon
restart, concurrent service instances and session deletion. A shared project
orchestrator's governed directive charges the exact dispatch work and target
instance; another work's binding cannot consume or bypass that work's quota.

The guarded paths are manual/automation/delegated session starts, TUI restores
and controller replacements, AO TUI sends, typed Chat starts/resumes, actual Chat
turn dispatch (including queue promotion and retry), reviewer terminal
spawn/restore/notify, typed reviewer Chat, and managed background model calls.
Accepting a Chat message into the queue does not spend a native turn. A durable
turn ID reuses its reservation, but claiming its native-effect fence is exclusive:
replaying an uncertain/already-started effect cannot execute it again.

Definite failure before native admission cancels the reservation. Suppression
by the TUI write guard provides positive non-admission proof. An arbitrary
provider/runtime error does not: the uncertain reservation remains consumed.
A positively established controller stop releases concurrent capacity only;
its lifetime/delegation count remains. Unknown probes release nothing.
The ledger's `started` state means the effect crossed AO's native invocation
boundary; the separate native-start receipt is required to claim a verified
provider turn actually started.

Work details expose `budgetUsage` from durable rows. Exhaustion refuses native
execution rather than requesting another prompt-level Go. Before publication,
an operator can preview/apply an expiring `executionBudgets` exception through
the existing immutable-snapshot/exact-approval API; consumption is retained and
the global policy is unchanged. The publication-policy freeze remains intact.

These are AO execution boundaries. Provider-internal subagents, direct provider
API calls, or raw terminal keystrokes are outside this measured ledger. No token,
cost, or model-native delegation cap is claimed without an adapter that can
observe and enforce that provider operation.
