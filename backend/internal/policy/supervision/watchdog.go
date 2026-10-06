package supervision

import "time"

// StallOutcome is the closed vocabulary of watchdog outcomes. There
// is deliberately no silent-idle outcome: every evaluation either
// allows continued quiet, nudges, or escalates.
type StallOutcome string

const (
	// OutcomeQuiet means progress is recent enough: keep running.
	OutcomeQuiet StallOutcome = "quiet"
	// OutcomeNudge means the quiet window elapsed: ping the worker.
	OutcomeNudge StallOutcome = "nudge"
	// OutcomeEscalate means the nudge window also elapsed without
	// progress: the human owns it now.
	OutcomeEscalate StallOutcome = "escalate"
)

// WatchdogConfig bounds the stall windows. NudgeAfter triggers the
// nudge; EscalateAfter (>= NudgeAfter) triggers human escalation.
// Zero or negative windows, or an inverted pair, are invalid and
// deny fail-closed so a misconfiguration can never yield silent idle.
type WatchdogConfig struct {
	NudgeAfter    time.Duration
	EscalateAfter time.Duration
}

// DefaultWatchdogConfig is the conservative starting window.
func DefaultWatchdogConfig() WatchdogConfig {
	return WatchdogConfig{NudgeAfter: 10 * time.Minute, EscalateAfter: 30 * time.Minute}
}

// Valid reports whether the window pair can ever escalate.
func (c WatchdogConfig) Valid() bool {
	return c.NudgeAfter > 0 && c.EscalateAfter > 0 && c.EscalateAfter >= c.NudgeAfter
}

// StallCheck is the fail-closed outcome of one watchdog evaluation.
type StallCheck struct {
	Decision Decision
	Outcome  StallOutcome
}

// CheckStall evaluates elapsed-since-progress. Disabled denies closed.
// Invalid windows deny closed with ReasonStallInvalid. Otherwise:
// before NudgeAfter -> quiet allow; between windows -> nudge allow
// (worker must act); at/after EscalateAfter -> escalate allow (human
// must act). A negative elapsed denies with ReasonStallInvalid rather
// than silently treating clock skew as quiet.
func CheckStall(enabled bool, cfg WatchdogConfig, sinceProgress time.Duration) StallCheck {
	if !enabled {
		return StallCheck{Decision: deny(ReasonDisabled, "supervision policy off")}
	}
	if !cfg.Valid() {
		return StallCheck{Decision: deny(ReasonStallInvalid, "invalid stall windows")}
	}
	if sinceProgress < 0 {
		return StallCheck{Decision: deny(ReasonStallInvalid, "negative elapsed")}
	}
	if sinceProgress >= cfg.EscalateAfter {
		return StallCheck{
			Decision: allowWith(ReasonStallEscalate, "no progress: escalate to human"),
			Outcome:  OutcomeEscalate,
		}
	}
	if sinceProgress >= cfg.NudgeAfter {
		return StallCheck{
			Decision: allowWith(ReasonStallNudge, "no progress: nudge worker"),
			Outcome:  OutcomeNudge,
		}
	}
	return StallCheck{
		Decision: allowWith(ReasonStallNoOutcome, "progress recent"),
		Outcome:  OutcomeQuiet,
	}
}
