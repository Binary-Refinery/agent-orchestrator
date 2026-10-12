# Instructor pilot and baseline readback

Run the pilot against a separate data directory and a known fork build. A green
fixture test is not a live provider test. Record the observed result, build SHA,
policy version and runtime identities; keep an unperformed step as pending.

## Launch the process with its switches

Fully exit the desktop and its child daemon before relaunching. Set the switches
in the shell that starts the desktop executable, or in the actual service/launcher
environment. `setx` updates future process environments; it does not change the
already-running desktop or daemon.

Example for a Windows pilot using the installed fork desktop:

```powershell
$env:AO_DATA_DIR = Join-Path $env:TEMP 'ao-instructor-pilot'
$env:AO_INSTRUCTOR = '1'
$env:AO_SUPERVISION = '1'
$env:AO_OPENCODE_EFFORT_FLOOR = '1'
$env:AO_INSTRUCTOR_POLICY = (Resolve-Path governance/instructor.json).Path
$aoPilotDesktop = Join-Path $env:LOCALAPPDATA 'Programs\agent-orchestrator\agent-orchestrator.exe'
Start-Process -FilePath $aoPilotDesktop
```

Use the actual installed fork executable if its path differs. Its bundled daemon
must belong to the selected build; `ao start` can resolve another installed
desktop and is not a fork deployment command. A standalone developer daemon can
instead be started with `ao daemon` from the same environment.

The daemon emits one `Instructor runtime readback` line at the runtime wiring
point. It contains the actual PID, desktop app version, Go binary version,
embedded VCS revision/dirty state, effective Instructor/supervision/effort-floor
switches, legacy sync request/runtime state, and effective global policy hash.
Absent build metadata is `unknown`; a disabled Instructor policy is `inactive`;
an unreadable/invalid enabled policy is explicit. The log never prints the full
environment, credentials or policy contents. Compare its PID with the daemon
that answers health/readback, rather than inspecting an unrelated shell.

`AO_REPORT_SYNC`, `AO_REACTION_LOOP` and `AO_TRIAGE_GATE` are obsolete flags with
no active runtime. The readback labels those runtimes `unsupported`, even when
the flags are set. They must not be counted as active synchronization or chain
drivers. The governed Instructor phase engine is the canonical work contract.

A stored global policy takes precedence over `AO_INSTRUCTOR_POLICY`; work pins
its own policy hash. Export/import steps are in [the operator guide](instructor.md).
Select a model's actual native effort/variant and verify requested, passed and
confirmed values separately. Enabling the effort floor never invents a universal
`max` for models that do not advertise it.

## Pilot evidence

| Probe | Required observation |
| --- | --- |
| Startup | One matching-process readback, enabled switches, known build/policy or explicit unknown/unavailable values. |
| Work identity | One stable work survives a replacement session; historical members cannot be relinked by a reused short session ID. |
| Result handoff | A report has a stable identity and trace through queue, native acceptance and actual turn start; delivery alone is insufficient. |
| Provider unavailable | Bounded durable retry and a visible terminal escalation; restart does not duplicate a previously accepted effect. |
| No progress | A continuously available heartbeat does not reset real progress. One bounded nudge is followed by blocked/escalated state when the deadline is exceeded. |
| Human/external wait | The expected condition is visible; a legitimate human gate is not automatically approved or nudged into execution. |
| Review chain | Worker → native checks → independent review → triage → authorized fix → delta. Delta covers corrected findings and regressions introduced by that change. |
| Publication | The displayed work/branch/SHA/policy/snapshot matches the exact human grant and protected executor; a worker cannot supply its own operator authority. |
| Instructor board | Resize a short/narrow and a wide/tall window; work remains primary, actions refresh their live state, captured approvals retain their immutable scope. |
| Restart | Durable work, memberships, approved decisions, delivery/budget records and native model configuration survive without manufactured confirmation. |

Use a disposable repository/work and configured native tools for the live probe.
Do not use production publication or deployment as a test. If a listener,
credential, native OS facility or provider is unavailable, record that exact gap.

## Database compatibility

Fresh databases and populated fork databases at schema 170 continue through the
additive migration path. The fork and upstream assigned **different** schemas to
versions 165–170. Before repairs or Goose migration writes, the fork validates
the physical schema of each already-applied collision version. An incompatible
upstream database fails with an actionable lineage error; its schema, migration
history and user rows remain unchanged. A matching maximum version alone is not
compatibility, including for the premigrated test-template path.

Tests reconstruct real upstream schemas at 165, 170 and 194 from the unchanged
SQL files captured at `c3e17362a85f37d0b0154bf8d841ed4f633f8306` and verify refusal
without mutation. Those tests prove safe rejection, **not** an implemented
upstream-to-fork upgrade. Keep the original database with its matching build and
use a separate `AO_DATA_DIR` until the separately validated merge described in
[the upstream integration plan](instructor-upstream-plan.md) exists.
