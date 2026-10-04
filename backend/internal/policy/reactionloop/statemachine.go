package reactionloop

// State is one node of the per-PR reaction automaton. The machine only
// plans and drafts; every step toward fixing or closing waits on either
// a poll outcome or an explicit human approval flag.
type State string

// Closed state vocabulary.
const (
	// StateWatching polls the PR and sorts each outcome: findings lead
	// to a triage proposal, a green board drafts the closeout note.
	StateWatching State = "watching"
	// StateTriageProposed holds a drafted triage proposal. Only an
	// explicit human confirmation authorizes a fix worker; a rejection
	// returns to watching.
	StateTriageProposed State = "triage_proposed"
	// StateFixAuthorized records that a human confirmed the triage and
	// a fix worker may start. This package never starts one itself.
	StateFixAuthorized State = "fix_authorized"
	// StateDeltaReview waits for the re-poll after the fix: new
	// findings re-propose triage, a green board drafts closeout.
	StateDeltaReview State = "delta_review"
	// StateCloseoutDrafted is terminal: the closeout note is drafted
	// and the human owns review, merge, and publish from here.
	StateCloseoutDrafted State = "closeout_drafted"
)

// Event is the closed input vocabulary of the automaton. Automation
// attempts are not events: they go through AttemptRedLine and are
// always denied, so no transition can smuggle them in.
type Event string

// Closed event vocabulary.
const (
	// EventPollFindings records a poll that surfaced P0/P1 findings.
	EventPollFindings Event = "poll_findings"
	// EventPollGreen records a poll with no P0/P1, no failed checks,
	// and no pending checks.
	EventPollGreen Event = "poll_green"
	// EventPollInconclusive records a poll with pending checks and no
	// findings yet: stay put and poll again later.
	EventPollInconclusive Event = "poll_inconclusive"
	// EventHumanConfirmTriage records the human triage confirmation.
	// Requires the human flag or the step denies closed.
	EventHumanConfirmTriage Event = "human_confirm_triage"
	// EventHumanRejectTriage records the human triage rejection.
	EventHumanRejectTriage Event = "human_reject_triage"
	// EventFixCompleted records that the authorized fix worker
	// finished and the PR wants a delta re-poll.
	EventFixCompleted Event = "fix_completed"
)

// TriageProposal is the draft the loop produces for P0/P1 findings. It
// is a proposal only: acting on it needs EventHumanConfirmTriage with
// the human flag.
type TriageProposal struct {
	P0       int
	P1       int
	Findings []Finding
}

// CloseoutDraft is the note the loop produces for an all-green poll.
// It never merges, publishes, or approves anything itself.
type CloseoutDraft struct {
	Note string
}

// Step is the fail-closed outcome of one automaton move: the next state
// plus, where applicable, a drafted proposal or closeout note.
type Step struct {
	Next     State
	Proposal *TriageProposal
	Closeout *CloseoutDraft
	Decision Decision
}

// Advance moves one PR automaton step. Disabled denies closed with
// ReasonDisabled and holds the state. Human-gated events deny with
// ReasonNeedsHuman unless human confirms. Unknown moves deny with
// ReasonInvalidStep and hold the state. Closeout is terminal: any event
// there denies with ReasonTerminal because the human owns the rest.
func Advance(enabled bool, s State, e Event, human bool, outcome PollOutcome) Step {
	hold := Step{Next: s}
	if !enabled {
		hold.Decision = deny(ReasonDisabled, "reaction-loop policy off")
		return hold
	}
	if s == StateCloseoutDrafted {
		hold.Decision = deny(ReasonTerminal, "closeout drafted, human owns merge")
		return hold
	}
	switch s {
	case StateWatching, StateDeltaReview:
		switch e {
		case EventPollFindings:
			if len(outcome.Findings) == 0 {
				hold.Decision = deny(ReasonMissingFindings, "findings event without P0/P1")
				return hold
			}
			return Step{
				Next: StateTriageProposed,
				Proposal: &TriageProposal{
					P0:       outcome.P0Count(),
					P1:       outcome.P1Count(),
					Findings: outcome.Findings,
				},
				Decision: allow(),
			}
		case EventPollGreen:
			if outcome.CommentsSeen == 0 {
				hold.Decision = deny(ReasonNoEvidence, "green event without observed review comments")
				return hold
			}
			if outcome.Observed == 0 {
				hold.Decision = deny(ReasonNoEvidence, "green event without any observed check run")
				return hold
			}
			if !outcome.AllGreen() {
				hold.Decision = deny(ReasonInvalidStep, "green event for a non-green board")
				return hold
			}
			return Step{
				Next:     StateCloseoutDrafted,
				Closeout: &CloseoutDraft{Note: "Alle Befunde geschlossen, Checks gruen: bereit fuer menschlichen Closeout."},
				Decision: allow(),
			}
		case EventPollInconclusive:
			hold.Decision = allow()
			return hold
		}
	case StateTriageProposed:
		switch e {
		case EventHumanConfirmTriage:
			if !human {
				hold.Decision = deny(ReasonNeedsHuman, "triage confirmation needs a human")
				return hold
			}
			hold.Next = StateFixAuthorized
			hold.Decision = allow()
			return hold
		case EventHumanRejectTriage:
			if !human {
				hold.Decision = deny(ReasonNeedsHuman, "triage rejection needs a human")
				return hold
			}
			hold.Next = StateWatching
			hold.Decision = allow()
			return hold
		}
	case StateFixAuthorized:
		if e == EventFixCompleted {
			hold.Next = StateDeltaReview
			hold.Decision = allow()
			return hold
		}
	}
	hold.Decision = deny(ReasonInvalidStep, "event "+string(e)+" not valid in "+string(s))
	return hold
}
