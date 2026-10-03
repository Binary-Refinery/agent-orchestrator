package domain

import (
	"regexp"
	"strings"
	"time"
)

// AutomationPolicy is an opt-in guardrail block for af-ao-automation-v1.
// Zero value means everything off: no gate, no archive, no auto-reviewer,
// drafts stay drafts. Scope: the spawn gate applies to automation-scheduler
// dispatches only; interactive spawns are intentionally out of scope and
// never pass through this policy. Red lines (never implemented): auto-push,
// auto-merge, task-worker auto-terminate, finding auto-confirm,
// publish-go automatic.
type AutomationPolicy struct {
	SpawnGate         bool `json:"spawnGate"`
	ReviewerArchive   bool `json:"reviewerArchive"`
	DraftOnlyReports  bool `json:"draftOnlyReports"`
	AutoReviewer      bool `json:"autoReviewer"`
	ReviewerBudgetMax int  `json:"reviewerBudgetMax"`
}

// DefaultAutomationPolicy returns the opt-in default: everything off,
// budget capped at 1 when the reviewer is explicitly enabled.
func DefaultAutomationPolicy() AutomationPolicy {
	return AutomationPolicy{ReviewerBudgetMax: 1}
}

// EffectiveReviewerBudget caps the reviewer budget at 1.
func (p AutomationPolicy) EffectiveReviewerBudget() int {
	if p.ReviewerBudgetMax <= 0 || p.ReviewerBudgetMax > 1 {
		return 1
	}
	return p.ReviewerBudgetMax
}

var spawnNamePattern = regexp.MustCompile(`^\[[^\[\]]+\] #\d+ .+$`)

// ValidateSpawnGate enforces B): when the gate is off it accepts everything.
// When on, the name must match "[bereich] #NNN Text" and issueID must be
// non-empty. Callers map the returned error to 409 AUTOMATION_POLICY_REJECTED.
func (p AutomationPolicy) ValidateSpawnGate(displayName, issueID string) *PolicyRejection {
	if !p.SpawnGate {
		return nil
	}
	if !spawnNamePattern.MatchString(strings.TrimSpace(displayName)) {
		return &PolicyRejection{Code: "AUTOMATION_POLICY_REJECTED", Message: "display name must match [bereich] #NNN Text"}
	}
	if strings.TrimSpace(issueID) == "" {
		return &PolicyRejection{Code: "AUTOMATION_POLICY_REJECTED", Message: "issueId is required"}
	}
	return nil
}

// PolicyRejection is a 409-class policy denial.
type PolicyRejection struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *PolicyRejection) Error() string { return e.Code + ": " + e.Message }

// ArchiveDecision is the outcome of the reviewer auto-archive check.
type ArchiveDecision struct {
	Archive bool       `json:"archive"`
	Delete  bool       `json:"delete"`
	Audit   AuditEntry `json:"audit"`
	Reason  string     `json:"reason"`
}

// AuditEntry records every archive evaluation.
type AuditEntry struct {
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	Detail string    `json:"detail"`
}

// ShouldArchiveReviewer enforces C): only for reviewers, only when idle plus
// (report present and triage adopted) or age >= 7 days. Never deletes.
func (p AutomationPolicy) ShouldArchiveReviewer(isReviewer, idle, hasReport, triageAdopted bool, age time.Duration, now time.Time, sessionID string) ArchiveDecision {
	audit := func(action, detail string) AuditEntry {
		return AuditEntry{At: now.UTC(), Action: action, Detail: detail}
	}
	if !p.ReviewerArchive || !isReviewer {
		return ArchiveDecision{Audit: audit("archive.skipped", "policy off or not a reviewer"), Reason: "skipped"}
	}
	if idle && hasReport && triageAdopted {
		return ArchiveDecision{Archive: true, Audit: audit("reviewer.archived", "idle+report+triage session="+sessionID), Reason: "idle+report+triage"}
	}
	if age >= 7*24*time.Hour {
		return ArchiveDecision{Archive: true, Audit: audit("reviewer.archived", "7d elapsed session="+sessionID), Reason: "7d elapsed"}
	}
	return ArchiveDecision{Audit: audit("archive.skipped", "conditions not met session="+sessionID), Reason: "conditions not met"}
}

// DraftReport enforces D): when DraftOnlyReports is on, reports/closeouts
// are always DRAFT chat drafts and are never auto-posted. When the switch
// is off no draft is produced. Returns the chat payload and autoPosted=false.
func (p AutomationPolicy) DraftReport(kind, body string) (chatDraft string, autoPosted bool) {
	if !p.DraftOnlyReports {
		return "", false
	}
	return "DRAFT [" + kind + "] " + strings.TrimSpace(body), false
}

// AssignAutoReviewer enforces E): exactly one reviewer per worker-done, opt-out
// via explicit prompt, triage only as suggestion, never auto-fix, budget max 1.
func (p AutomationPolicy) AssignAutoReviewer(workerDone, optedOut bool, alreadyAssigned int) (assign bool, suggestion string) {
	if !p.AutoReviewer || !workerDone || optedOut {
		return false, ""
	}
	if alreadyAssigned >= p.EffectiveReviewerBudget() {
		return false, ""
	}
	return true, "triage suggestion (no auto-fix)"
}
