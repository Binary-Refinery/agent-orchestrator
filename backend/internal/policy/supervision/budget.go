package supervision

// Budget caps chain rounds and session steps. Both caps are
// fail-closed: reaching either denies further work until the caller
// resets on a new chain or session.
type Budget struct {
	// MaxRounds bounds chain rounds in this session.
	MaxRounds int
	// MaxSessionSteps bounds total steps in this session.
	MaxSessionSteps int
}

// DefaultBudget is the conservative starting cap.
func DefaultBudget() Budget { return Budget{MaxRounds: 2, MaxSessionSteps: 12} }

// MaxFixRoundsWithoutException is the hard cap on fix rounds: a third
// fix round is impossible without an explicit human exception, which
// the caller passes as humanException=true.
const MaxFixRoundsWithoutException = 2

// Usage carries the current counters for one planning decision.
type Usage struct {
	// Rounds counts completed chain rounds this session.
	Rounds int
	// SessionSteps counts steps taken this session.
	SessionSteps int
	// FixRounds counts fix rounds taken (subset of rounds).
	FixRounds int
}

// PlanStep decides whether another chain step may start. Disabled
// denies closed. Exhausted caps deny with the matching budget reason.
// The fix-round cap binds even when round/session budget remains:
// FixRounds >= 2 denies with ReasonBudgetFixCap unless the caller
// passes humanException=true for an explicit human override.
func PlanStep(enabled bool, budget Budget, usage Usage, isFixRound bool, humanException bool) Decision {
	if !enabled {
		return deny(ReasonDisabled, "supervision policy off")
	}
	// Fail-closed on caller-supplied counters: a negative counter is a
	// corrupt input, never a free pass. Each counter is typed so the
	// audit can name the offending field.
	if usage.Rounds < 0 || usage.SessionSteps < 0 || usage.FixRounds < 0 {
		return deny(ReasonBudgetInvalid, "negative budget usage counter")
	}
	if budget.MaxRounds <= 0 || usage.Rounds >= budget.MaxRounds {
		return deny(ReasonBudgetRound, "round budget reached")
	}
	if budget.MaxSessionSteps <= 0 || usage.SessionSteps >= budget.MaxSessionSteps {
		return deny(ReasonBudgetSession, "session step budget reached")
	}
	if isFixRound && usage.FixRounds >= MaxFixRoundsWithoutException && !humanException {
		return deny(ReasonBudgetFixCap, "third fix round needs explicit human exception")
	}
	return allowWith(ReasonStallNoOutcome, "step within budget")
}
