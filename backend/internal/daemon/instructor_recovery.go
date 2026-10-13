package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	sessionmanager "github.com/aoagents/agent-orchestrator/backend/internal/session_manager"
)

func (d instructorDelivery) recoveryTarget(ctx context.Context, w domain.InstructorWork) (domain.SessionRecord, error) {
	if reader, ok := d.store.(interface {
		ListInstructorDispatches(context.Context, string, string) ([]domain.InstructorDispatch, error)
	}); ok {
		dispatches, err := reader.ListInstructorDispatches(ctx, "", w.ID)
		if err != nil {
			return domain.SessionRecord{}, err
		}
		sort.Slice(dispatches, func(i, j int) bool {
			if dispatches[i].CreatedAt.Equal(dispatches[j].CreatedAt) {
				return dispatches[i].ID > dispatches[j].ID
			}
			return dispatches[i].CreatedAt.After(dispatches[j].CreatedAt)
		})
		for _, dispatch := range dispatches {
			if dispatch.TargetSessionID == "" || dispatch.Action == "stopp" || dispatch.Action == "abbrechen" {
				continue
			}
			// Finished receipts describe history. Only an outstanding effect may
			// pin recovery to a former coordinator instead of today's target.
			if dispatch.State != "pending" && dispatch.State != "leased" && dispatch.State != "queued" && dispatch.State != "uncertain" && !slices.Contains(w.UncertainDispatchIDs, dispatch.ID) {
				continue
			}
			rec, found, err := d.store.GetSession(ctx, dispatch.TargetSessionID)
			if err != nil {
				return rec, err
			}
			if !found || rec.Kind != domain.KindOrchestrator || rec.ProjectID != w.ProjectID {
				return domain.SessionRecord{}, fmt.Errorf("%w: bound orchestrator is missing or replaced", instructorsvc.ErrConflict)
			}
			if dispatch.TargetCreatedAt != nil && !rec.CreatedAt.Equal(*dispatch.TargetCreatedAt) {
				return domain.SessionRecord{}, instructorsvc.ErrConflict
			}
			if dispatch.TargetSessionInstanceID != "" {
				if reader, ok := d.store.(interface {
					InstructorSessionInstance(context.Context, domain.SessionID) (string, bool, error)
				}); ok {
					instance, found, err := reader.InstructorSessionInstance(ctx, rec.ID)
					if err != nil {
						return rec, err
					}
					if !found || instance != dispatch.TargetSessionInstanceID {
						return domain.SessionRecord{}, instructorsvc.ErrConflict
					}
				}
			}
			return rec, nil
		}
	}
	records, err := d.store.ListSessions(ctx, w.ProjectID)
	if err != nil {
		return domain.SessionRecord{}, err
	}
	var target domain.SessionRecord
	for _, rec := range records {
		if rec.Kind != domain.KindOrchestrator || rec.ProjectID != w.ProjectID {
			continue
		}
		targetStopped := target.IsTerminated || target.Activity.State == domain.ActivityExited
		recordStopped := rec.IsTerminated || rec.Activity.State == domain.ActivityExited
		if target.ID == "" || (targetStopped && !recordStopped) || (targetStopped == recordStopped && (rec.CreatedAt.After(target.CreatedAt) || rec.CreatedAt.Equal(target.CreatedAt) && rec.ID > target.ID)) {
			target = rec
		}
	}
	return target, nil
}

func (d instructorDelivery) RecoveryStatus(ctx context.Context, w domain.InstructorWork) (domain.InstructorRecoveryStatus, error) {
	status := domain.InstructorRecoveryStatus{WorkID: w.ID, WorkRevision: w.Revision, PolicyVersion: w.PolicyVersion, Action: "none"}
	if w.Cancellation != nil || w.State == "done" || w.State == "cancelled" {
		status.ReasonCode, status.Reason = "WORK_CLOSED", "Abgeschlossene oder abgebrochene Arbeit wird nicht wieder gestartet."
		return status, nil
	}
	rec, err := d.recoveryTarget(ctx, w)
	if err != nil {
		if errors.Is(err, instructorsvc.ErrConflict) {
			status.ReasonCode, status.Reason = "CONTROLLER_IDENTITY_CHANGED", "Die gebundene Orchestrator-Session fehlt oder wurde ersetzt. Den ursprünglichen Zustellungsauftrag abgleichen, bevor eine andere Session übernimmt."
			return status, nil
		}
		return status, err
	}
	if rec.ID == "" {
		status.ReasonCode, status.Reason = "ORCHESTRATOR_MISSING", "Kein Projekt-Orchestrator vorhanden. In den Projekteinstellungen einen Orchestrator erstellen."
		return status, nil
	}
	status.SessionID, status.SessionCreatedAt = rec.ID, rec.CreatedAt
	status.ProviderConversationID, status.ControllerGeneration = rec.Metadata.ProviderConversationID, rec.Metadata.ControllerGeneration
	observer, ok := d.sessions.(interface {
		InstructorControllerReady(domain.SessionRecord) bool
	})
	if !ok {
		status.ReasonCode, status.Reason = "CONTROLLER_OBSERVATION_UNAVAILABLE", "Controller-Verfügbarkeit kann nicht sicher geprüft werden."
		return status, nil
	}
	if observer.InstructorControllerReady(rec) {
		if rec.Activity.State == domain.ActivityBlocked {
			status.ReasonCode, status.Reason = "CONTROLLER_NEEDS_INPUT", "Der Orchestrator wartet auf eine Entscheidung. Die offene Anfrage in seiner Session beantworten."
			return status, nil
		}
		status.Ready, status.ReasonCode, status.Reason = true, "CONTROLLER_READY", "Der bestehende Controller ist erreichbar."
		return status, nil
	}
	status.ReasonCode, status.Reason = "CONTROLLER_UNAVAILABLE", "Der Controller fehlt. Die bestehende Session wiederherstellen; Arbeitsfreigaben bleiben unverändert."
	if reader, ok := d.sessions.(interface {
		SessionStatusReadiness(domain.SessionRecord) string
	}); ok && reader.SessionStatusReadiness(rec) == "checking" {
		status.ReasonCode, status.Reason = "SESSION_RECOVERY_IN_PROGRESS", "Die bestehende Session wird bereits geprüft oder wiederhergestellt. Den Abschluss abwarten."
		return status, nil
	}
	if reader, ok := d.sessions.(interface {
		SessionStatusRecoveryReason(domain.SessionRecord) (string, string)
	}); ok {
		if code, reason := reader.SessionStatusRecoveryReason(rec); code != "" {
			status.ReasonCode, status.Reason = code, reason
		}
	}
	if domain.NormalizeSessionMode(rec.Mode) != domain.SessionModeChat || rec.Metadata.ProviderConversationID == "" {
		status.ReasonCode, status.Reason = "NATIVE_CONTEXT_REQUIRED", "Automatische Wiederherstellung benötigt einen bestehenden Chat-Verlauf. Session-Details zur Wiederaufnahme öffnen."
		return status, nil
	}
	status.Action = "resume"
	if rec.IsTerminated {
		status.Action = "restore"
	}
	return status, nil
}

func (d instructorDelivery) RecoverController(ctx context.Context, w domain.InstructorWork, in domain.InstructorRecoveryInput) error {
	rec, err := d.recoveryTarget(ctx, w)
	if err != nil {
		return err
	}
	if rec.ID != in.SessionID || !rec.CreatedAt.Equal(in.SessionCreatedAt) || rec.Metadata.ProviderConversationID != in.ProviderConversationID || rec.Metadata.ControllerGeneration != in.ControllerGeneration {
		return instructorsvc.ErrConflict
	}
	manager, ok := d.sessions.(interface {
		RestoreWithMode(context.Context, domain.SessionID) (sessionmanager.RestoreResult, error)
		ResumeAgentWithMode(context.Context, domain.SessionID) (sessionmanager.RestoreResult, error)
	})
	if !ok {
		return fmt.Errorf("%w: controller recovery unavailable", instructorsvc.ErrRecoveryRequired)
	}
	ctx = domain.WithSessionRecoveryExpectation(ctx, rec)
	if rec.IsTerminated {
		_, err = manager.RestoreWithMode(ctx, rec.ID)
	} else {
		_, err = manager.ResumeAgentWithMode(ctx, rec.ID)
	}
	return err
}
