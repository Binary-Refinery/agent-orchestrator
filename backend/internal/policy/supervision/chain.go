package supervision

import "time"

// ChainStep is one node of the worker-chain automaton. Steps are
// keyed by chain position, never by role name: any current or future
// role occupying a step falls under the same timeouts and gates.
type ChainStep string

// Closed chain-step vocabulary.
const (
	StepWorker ChainStep = "worker"
	StepReview ChainStep = "review"
	StepTriage ChainStep = "triage"
	StepFix    ChainStep = "fix"
	StepDelta  ChainStep = "delta"
	StepClosed ChainStep = "closed"
)

// ChainSteps lists the machine vocabulary in chain order for
// enumeration tests. StepClosed is terminal.
var ChainSteps = []ChainStep{StepWorker, StepReview, StepTriage, StepFix, StepDelta, StepClosed}

// ChainTimeouts carries the per-step deadline. A step exceeding its
// timeout denies with ReasonChainTimeout so the watchdog/escalation
// path owns it. Zero or negative timeouts deny fail-closed.
type ChainTimeouts struct {
	PerStep map[ChainStep]time.Duration
}

// DefaultChainTimeouts is the conservative starting deadline set.
func DefaultChainTimeouts() ChainTimeouts {
	return ChainTimeouts{PerStep: map[ChainStep]time.Duration{
		StepWorker: 30 * time.Minute,
		StepReview: 15 * time.Minute,
		StepTriage: 15 * time.Minute,
		StepFix:    30 * time.Minute,
		StepDelta:  15 * time.Minute,
	}}
}

// ChainMove is the fail-closed outcome of one automaton move.
type ChainMove struct {
	Decision Decision
	Next     ChainStep
}

// nextStep returns the successor in chain order.
func nextStep(s ChainStep) (ChainStep, bool) {
	for i, st := range ChainSteps {
		if st == s && i+1 < len(ChainSteps) {
			return ChainSteps[i+1], true
		}
	}
	return s, false
}

// AdvanceChain moves one chain step forward. Disabled denies closed
// and holds. The terminal step denies closed. Elapsed beyond the
// step timeout denies with ReasonChainTimeout and holds (the
// escalation path owns the stall). Human-gated moves (triage->fix,
// which authorizes remediation) deny with ReasonNeedsHuman unless
// the human flag is set. Unknown steps deny with ReasonChainInvalid.
// Role names never appear: callers pass only the chain step.
func AdvanceChain(enabled bool, timeouts ChainTimeouts, cur ChainStep, elapsed time.Duration, human bool) ChainMove {
	hold := ChainMove{Next: cur}
	if !enabled {
		hold.Decision = deny(ReasonDisabled, "supervision policy off")
		return hold
	}
	known := false
	for _, st := range ChainSteps {
		if st == cur {
			known = true
			break
		}
	}
	if !known {
		hold.Decision = deny(ReasonChainInvalid, "unknown chain step "+string(cur))
		return hold
	}
	if cur == StepClosed {
		hold.Decision = deny(ReasonChainTerminal, "chain closed")
		return hold
	}
	if elapsed < 0 {
		hold.Decision = deny(ReasonChainInvalid, "negative elapsed")
		return hold
	}
	if timeouts.PerStep == nil {
		hold.Decision = deny(ReasonChainTimeout, "no timeouts configured")
		return hold
	}
	limit, ok := timeouts.PerStep[cur]
	if !ok || limit <= 0 {
		hold.Decision = deny(ReasonChainTimeout, "no timeout for step "+string(cur))
		return hold
	}
	if elapsed >= limit {
		hold.Decision = deny(ReasonChainTimeout, "step "+string(cur)+" timed out")
		return hold
	}
	if cur == StepTriage && !human {
		hold.Decision = deny(ReasonNeedsHuman, "triage->fix needs a human")
		return hold
	}
	next, ok := nextStep(cur)
	if !ok {
		hold.Decision = deny(ReasonChainTerminal, "chain closed")
		return hold
	}
	hold.Next = next
	hold.Decision = allowWith(ReasonStallNoOutcome, "advanced to "+string(next))
	return hold
}
