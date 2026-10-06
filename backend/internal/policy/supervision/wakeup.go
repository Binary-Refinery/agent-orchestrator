package supervision

// ReportKind is the closed vocabulary of worker reports that mandate
// an orchestrator turn.
type ReportKind string

const (
	// ReportDone marks a finished worker step.
	ReportDone ReportKind = "done"
	// ReportCheckpoint marks an intermediate worker checkpoint.
	ReportCheckpoint ReportKind = "checkpoint"
)

// WakeupPlan is the fail-closed outcome of planning one orchestrator
// turn for a worker report.
type WakeupPlan struct {
	Decision Decision
	// Attempts counts delivery tries the caller should perform
	// (1 initial + retries). Zero when disabled or invalid.
	Attempts int
	// Audit is non-empty exactly when delivery finally failed: the
	// caller must persist it. It carries the kind, attempts, and
	// last error for post-mortem.
	Audit string
}

// MaxWakeupAttempts bounds delivery tries: 1 initial + 2 retries.
const MaxWakeupAttempts = 3

// ValidReportKind reports whether k is a turn-mandating report kind.
func ValidReportKind(k ReportKind) bool {
	return k == ReportDone || k == ReportCheckpoint
}

// PlanWakeup mandates one orchestrator turn per valid report when the
// policy is on. Disabled denies closed; unknown kinds deny closed so
// no report can slip through without a turn.
func PlanWakeup(enabled bool, kind ReportKind) WakeupPlan {
	if !enabled {
		return WakeupPlan{Decision: deny(ReasonDisabled, "supervision policy off")}
	}
	if !ValidReportKind(kind) {
		return WakeupPlan{Decision: deny(ReasonWakeupInvalidKind, "unknown report kind "+string(kind))}
	}
	return WakeupPlan{
		Decision: allowWith(ReasonWakeupDelivered, "orchestrator turn mandated for "+string(kind)),
		Attempts: 1,
	}
}

// DeliverFunc attempts one orchestrator-turn delivery. Nil error means
// the turn happened.
type DeliverFunc func() error

// DeliverWakeup runs the mandated turn with retries. Success allows
// with ReasonWakeupDelivered; attempts exhausted denies with
// ReasonWakeupFailed and fills Audit. A nil deliver function is a
// fail-closed terminal failure, never a silent skip.
func DeliverWakeup(enabled bool, kind ReportKind, deliver DeliverFunc) WakeupPlan {
	plan := PlanWakeup(enabled, kind)
	if !plan.Decision.Allow {
		return plan
	}
	if deliver == nil {
		return WakeupPlan{
			Decision: deny(ReasonWakeupFailed, "no delivery function wired"),
			Attempts: MaxWakeupAttempts,
			Audit:    "wakeup kind=" + string(kind) + " attempts=" + itoa(MaxWakeupAttempts) + " err=no-delivery-func",
		}
	}
	var lastErr string
	for i := 1; i <= MaxWakeupAttempts; i++ {
		if err := deliver(); err == nil {
			return WakeupPlan{
				Decision: allowWith(ReasonWakeupDelivered, "orchestrator turn delivered"),
				Attempts: i,
			}
		} else {
			lastErr = err.Error()
		}
		if i < MaxWakeupAttempts {
			_ = ReasonWakeupRetry
		}
	}
	return WakeupPlan{
		Decision: deny(ReasonWakeupFailed, "orchestrator turn failed after retries"),
		Attempts: MaxWakeupAttempts,
		Audit:    "wakeup kind=" + string(kind) + " attempts=" + itoa(MaxWakeupAttempts) + " err=" + lastErr,
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
