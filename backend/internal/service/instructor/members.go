package instructor

import (
	"context"
	"fmt"
	"sort"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type sessionInstanceReader interface {
	InstructorSessionInstance(context.Context, domain.SessionID) (string, bool, error)
}
type membershipSessionReader interface {
	InstructorSessionForMembership(context.Context, domain.SessionID) (domain.SessionRecord, string, bool, error)
}
type sessionAdmissionStore interface {
	membershipSessionReader
	sessionInstanceReader
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	AttachInstructorSession(context.Context, domain.InstructorWork, int64, domain.InstructorEvent, domain.InstructorSessionMembership) (bool, error)
}
type memberRuntimeReader interface {
	membershipSessionReader
	sessionInstanceReader
	GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error)
	ListSessions(context.Context, domain.ProjectID) ([]domain.SessionRecord, error)
	ListInstructorSessionMembers(context.Context, string) ([]domain.InstructorSessionMembership, error)
}
type memberReviewReader interface {
	GetReviewByID(context.Context, string) (domain.Review, bool, error)
	GetReviewRun(context.Context, string) (domain.ReviewRun, bool, error)
}

func sessionRuntimeGeneration(s domain.SessionRecord) string {
	if domain.NormalizeSessionMode(s.Mode) == domain.SessionModeChat {
		return s.Metadata.ControllerGeneration
	}
	return s.Metadata.RuntimeLaunchID
}

// Members never joins historical display IDs to current rows without proving
// their admitted incarnation. Missing proof remains visible and unclickable.
func (s *Service) Members(ctx context.Context, id string) (domain.InstructorMembers, error) {
	w, err := s.get(ctx, id)
	if err != nil {
		return domain.InstructorMembers{}, err
	}
	result := domain.InstructorMembers{WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: w.Revision, CapturedAt: s.now(), Coordinator: domain.InstructorCoordinator{Key: string(w.ProjectID) + ":orchestrator"}, Members: []domain.InstructorMember{}}
	reader, ok := s.deps.Store.(memberRuntimeReader)
	if !ok {
		return result, invalid("incarnation-aware members reader unavailable")
	}
	memberships, err := reader.ListInstructorSessionMembers(ctx, id)
	if err != nil {
		return result, err
	}
	seen := map[domain.SessionID]bool{}
	for _, binding := range memberships {
		seen[binding.SessionID] = true
		m := domain.InstructorMember{ID: "session:" + binding.InstanceID, Kind: "session", Role: binding.Role, Association: "historical", Status: "missing", SessionID: binding.SessionID, InstanceID: binding.InstanceID, Name: binding.Name, Reason: "The admitted session row no longer exists."}
		if !binding.CreatedAt.IsZero() {
			m.CreatedAt = &binding.CreatedAt
		}
		rec, instance, found, err := reader.InstructorSessionForMembership(ctx, binding.SessionID)
		if err != nil {
			return result, err
		}
		if found && instance == binding.InstanceID && rec.ProjectID == w.ProjectID && !rec.IsTaskPreparation {
			m.Association = "current"
			m.Status = string(rec.Activity.State)
			m.Name = rec.DisplayName
			m.RuntimeGeneration = sessionRuntimeGeneration(rec)
			m.CreatedAt = &rec.CreatedAt
			m.Reason = ""
			if rec.IsTerminated || rec.Activity.State == domain.ActivityExited {
				m.Association = "historical"
				m.Status = "terminated"
			}
			m.OpenTarget = &domain.InstructorMemberOpenTarget{Kind: "session", ProjectID: w.ProjectID, SessionID: rec.ID}
		} else if found && instance != binding.InstanceID {
			m.Reason = "This display ID now belongs to a different session instance."
		}
		for i := len(w.Launches) - 1; i >= 0; i-- {
			l := w.Launches[i]
			if l.SessionID == binding.SessionID && l.SessionInstanceID == binding.InstanceID {
				selection := l.Requested
				m.Requested = &selection
				// Provider confirmation belongs to one controller generation, not
				// merely to an unchanged model on a restored display ID.
				if m.Association != "current" || l.RuntimeGeneration == m.RuntimeGeneration {
					m.Sent = l.Sent
					if m.Association != "current" || m.RuntimeGeneration != "" {
						m.Confirmed = l.Confirmed
					}
				}
				m.Role = l.Role
				m.Phase = l.Phase
				if m.RuntimeGeneration == "" && (!found || instance != binding.InstanceID) {
					m.RuntimeGeneration = l.RuntimeGeneration
				}
				break
			}
		}
		if binding.Provenance == "legacy_bound" {
			m.Association = "legacy_unknown"
			m.OpenTarget = nil
			m.Reason = "Legacy assignment captured during upgrade; confirm the association explicitly before opening a current agent."
		}
		result.Members = append(result.Members, m)
	}
	for _, sid := range w.SessionIDs {
		if !seen[sid] {
			result.Members = append(result.Members, domain.InstructorMember{ID: "legacy:" + string(sid), Kind: "session", Role: "unknown", Association: "legacy_unknown", Status: "unknown", SessionID: sid, Name: string(sid), Reason: "Historical display ID has no retained incarnation proof."})
		}
	}
	reviews, canReview := s.deps.Store.(memberReviewReader)
	reviewSeen := map[string]bool{}
	for i := len(w.Launches) - 1; i >= 0; i-- {
		l := w.Launches[i]
		if l.ReviewID == "" {
			continue
		}
		key := l.ReviewID + ":" + l.ReviewRunID
		if reviewSeen[key] {
			continue
		}
		reviewSeen[key] = true
		requested := l.Requested
		m := domain.InstructorMember{ID: "review:" + key, Kind: "review", Role: l.Role, Association: "legacy_unknown", Status: l.Status, ReviewID: l.ReviewID, ReviewRunID: l.ReviewRunID, SessionID: l.OwnerSessionID, InstanceID: l.SessionInstanceID, RuntimeGeneration: l.RuntimeGeneration, Name: l.ReviewID, Phase: l.Phase, CreatedAt: &l.LaunchedAt, Requested: &requested, Sent: l.Sent, Confirmed: l.Confirmed, Reason: "Review identity has no retained incarnation proof."}
		if canReview && l.ReviewCreatedAt != nil && l.SessionInstanceID != "" && l.OwnerSessionID != "" {
			m.Association = "historical"
			m.Reason = "Recorded review or its worker instance is no longer available."
			review, found, e := reviews.GetReviewByID(ctx, l.ReviewID)
			if e != nil {
				return result, e
			}
			run, hasRun, e := reviews.GetReviewRun(ctx, l.ReviewRunID)
			if e != nil {
				return result, e
			}
			owner, instance, hasOwner, e := reader.InstructorSessionForMembership(ctx, l.OwnerSessionID)
			if e != nil {
				return result, e
			}
			if found && hasRun && hasOwner && instance == l.SessionInstanceID && owner.ProjectID == w.ProjectID && review.ProjectID == w.ProjectID && review.CreatedAt.Equal(*l.ReviewCreatedAt) && review.SessionID == l.OwnerSessionID && run.SessionID == l.OwnerSessionID && run.ReviewID == l.ReviewID {
				m.Status = string(run.Status)
				m.Reason = ""
				m.OpenTarget = &domain.InstructorMemberOpenTarget{Kind: "review", ProjectID: w.ProjectID, SessionID: l.OwnerSessionID, ReviewID: l.ReviewID, ReviewRunID: l.ReviewRunID}
				generation := review.ReviewerLaunchID
				if generation == "" {
					generation = review.ControllerGeneration
				}
				if run.Status == domain.ReviewRunRunning && generation == l.RuntimeGeneration {
					m.Association = "current"
				}
			}
		}
		result.Members = append(result.Members, m)
	}
	sessions, err := reader.ListSessions(ctx, w.ProjectID)
	if err != nil {
		return result, err
	}
	var coordinator *domain.SessionRecord
	var stoppedCoordinator *domain.SessionRecord
	for i := range sessions {
		r := &sessions[i]
		if r.Kind != domain.KindOrchestrator || r.IsTaskPreparation {
			continue
		}
		if r.IsTerminated || r.Activity.State == domain.ActivityExited {
			if stoppedCoordinator == nil || r.CreatedAt.After(stoppedCoordinator.CreatedAt) || (r.CreatedAt.Equal(stoppedCoordinator.CreatedAt) && r.ID > stoppedCoordinator.ID) {
				stoppedCoordinator = r
			}
			continue
		}
		if coordinator == nil || r.CreatedAt.After(coordinator.CreatedAt) || (r.CreatedAt.Equal(coordinator.CreatedAt) && r.ID > coordinator.ID) {
			coordinator = r
		}
	}
	if coordinator != nil {
		r, instance, found, e := reader.InstructorSessionForMembership(ctx, coordinator.ID)
		if e != nil {
			return result, e
		}
		if found && r.ProjectID == w.ProjectID && r.Kind == domain.KindOrchestrator && !r.IsTerminated && !r.IsTaskPreparation && r.Activity.State != domain.ActivityExited {
			result.Coordinator.Current = &domain.InstructorMember{ID: "coordinator:" + instance, Kind: "session", Role: "orchestrator", Association: "current", Status: string(r.Activity.State), SessionID: r.ID, InstanceID: instance, RuntimeGeneration: sessionRuntimeGeneration(r), Name: r.DisplayName, CreatedAt: &r.CreatedAt, OpenTarget: &domain.InstructorMemberOpenTarget{Kind: "session", ProjectID: w.ProjectID, SessionID: r.ID}}
		}
	}
	if result.Coordinator.Current == nil && stoppedCoordinator != nil {
		r, instance, found, e := reader.InstructorSessionForMembership(ctx, stoppedCoordinator.ID)
		if e != nil {
			return result, e
		}
		if found && r.ProjectID == w.ProjectID && r.Kind == domain.KindOrchestrator && !r.IsTaskPreparation && (r.IsTerminated || r.Activity.State == domain.ActivityExited) {
			result.Coordinator.Stopped = &domain.InstructorMember{ID: "coordinator:" + instance, Kind: "session", Role: "orchestrator", Association: "historical", Status: "terminated", SessionID: r.ID, InstanceID: instance, RuntimeGeneration: sessionRuntimeGeneration(r), Name: r.DisplayName, CreatedAt: &r.CreatedAt, OpenTarget: &domain.InstructorMemberOpenTarget{Kind: "session", ProjectID: w.ProjectID, SessionID: r.ID}}
		}
	}

	sort.SliceStable(result.Members, func(i, j int) bool { return result.Members[i].ID < result.Members[j].ID })
	// A concurrent work mutation invalidates this projection rather than silently
	// labelling a mixed work revision as the caller's captured one.
	current, err := s.get(ctx, id)
	if err != nil {
		return result, err
	}
	if current.Revision != w.Revision {
		return result, fmt.Errorf("%w: work changed during members read", ErrConflict)
	}
	result.CapturedAt = s.now()
	return result, nil
}
