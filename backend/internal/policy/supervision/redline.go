package supervision

// RedLineKind names an automation the supervision slice must never
// perform.
type RedLineKind string

// Closed vocabulary of forbidden automations. No planner, driver, or
// watchdog in this package accepts any of them.
const (
	RedLineAutoPush        RedLineKind = "auto_push"
	RedLineAutoMerge       RedLineKind = "auto_merge"
	RedLineAutoTerminate   RedLineKind = "auto_terminate_worker"
	RedLineAutoConfirm     RedLineKind = "auto_confirm_finding"
	RedLineAutoPublishGo   RedLineKind = "auto_publish_go"
	RedLineHumanGateBypass RedLineKind = "human_gate_bypass" //nolint:gosec // G101 false positive: enum name of a forbidden automation, not a credential.
)

// RedLines lists every forbidden automation for enumeration tests.
var RedLines = []RedLineKind{
	RedLineAutoPush,
	RedLineAutoMerge,
	RedLineAutoTerminate,
	RedLineAutoConfirm,
	RedLineAutoPublishGo,
	RedLineHumanGateBypass,
}

// AttemptRedLine is the single choke point for forbidden automations:
// it always denies with ReasonRedLine, whether the policy is on or
// off, so no caller can route an automation through this package.
func AttemptRedLine(kind RedLineKind) Decision {
	return deny(ReasonRedLine, "forbidden automation: "+string(kind))
}
