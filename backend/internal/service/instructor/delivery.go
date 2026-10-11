package instructor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func deliveryGateID(dispatchID string) string { return "delivery-" + dispatchID }

func uncertainDispatch(w domain.InstructorWork, id string) bool {
	if w.Gate != nil && w.Gate.Kind == "delivery_uncertain" && w.Gate.DispatchID == id {
		return true
	}
	for _, candidate := range w.UncertainDispatchIDs {
		if candidate == id {
			return true
		}
	}
	return false
}

func (s *Service) markUncertain(w *domain.InstructorWork, d domain.InstructorDispatch, title string) {
	if w.Gate != nil && w.Gate.Kind == "delivery_uncertain" && !uncertainIDListed(*w, w.Gate.DispatchID) {
		w.UncertainDispatchIDs = append(w.UncertainDispatchIDs, w.Gate.DispatchID)
	}
	if !uncertainIDListed(*w, d.ID) {
		w.UncertainDispatchIDs = append(w.UncertainDispatchIDs, d.ID)
	}
	if w.Gate != nil && w.Gate.Kind == "delivery_uncertain" {
		return
	}
	if w.Gate != nil {
		cloned := *w.Gate
		w.SuspendedGate = &cloned
	}
	w.Gate = &domain.InstructorGate{ID: deliveryGateID(d.ID), Kind: "delivery_uncertain", Title: title, DispatchID: d.ID, TargetSessionID: d.TargetSessionID, OpenedAt: s.now()}
}

func uncertainIDListed(w domain.InstructorWork, id string) bool {
	for _, candidate := range w.UncertainDispatchIDs {
		if candidate == id {
			return true
		}
	}
	return false
}

// Resolve one native attempt without losing another uncertainty or its suspended
// business gate. All unresolved native attempts must be settled before execution.
func (s *Service) clearUncertain(w *domain.InstructorWork, id string, dispatches []domain.InstructorDispatch) {
	remaining := []string{}
	for _, candidate := range w.UncertainDispatchIDs {
		if candidate != id {
			remaining = append(remaining, candidate)
		}
	}
	w.UncertainDispatchIDs = remaining
	if w.Gate != nil && w.Gate.Kind != "delivery_uncertain" {
		return
	}
	if w.Gate != nil && w.Gate.Kind == "delivery_uncertain" && w.Gate.DispatchID != id {
		return
	}
	w.Gate = nil
	if len(remaining) > 0 {
		for _, d := range dispatches {
			if d.ID == remaining[0] {
				w.Gate = &domain.InstructorGate{ID: deliveryGateID(d.ID), Kind: "delivery_uncertain", Title: "Weitere native Zustellung muss abgeglichen werden", DispatchID: d.ID, TargetSessionID: d.TargetSessionID, OpenedAt: s.now()}
				return
			}
		}
		w.Gate = &domain.InstructorGate{ID: deliveryGateID(remaining[0]), Kind: "delivery_uncertain", Title: "Ungeklärte native Zustellung; gespeicherte Bindung prüfen", DispatchID: remaining[0], OpenedAt: s.now()}
		return
	}
	if w.SuspendedGate != nil {
		w.Gate, w.SuspendedGate = w.SuspendedGate, nil
	}
}

func (s *Service) deliveryQueued(ctx context.Context, dispatch domain.InstructorDispatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.deps.Store.ListInstructorDispatches(ctx, "", dispatch.WorkID)
	if err != nil {
		return err
	}
	for _, d := range rows {
		if d.ID == dispatch.ID {
			if d.State == "accepted" || d.State == "progress" {
				return nil
			}
			dispatch.TargetSessionID = d.TargetSessionID
		}
	}
	w, err := s.get(ctx, dispatch.WorkID)
	if err != nil {
		return err
	}
	if w.Revision != dispatch.WorkRevision {
		return fmt.Errorf("%w: work changed while native delivery was queued", ErrDeliveryUncertain)
	}
	now := s.now()
	if dispatch.QueuedAt == nil {
		dispatch.QueuedAt = &now
	}
	deadline := dispatch.QueuedAt.Add(2 * time.Minute)
	if w.State != "waiting_human" && w.State != "parked" && w.State != "blocked" {
		w.State = "waiting_external"
		w.WaitCondition = "Native Nachricht dauerhaft eingereiht; wartet auf bestätigten Empfang"
		w.WaitUntil = &deadline
	}
	expected := w.Revision
	w.Revision++
	w.UpdatedAt = now
	dispatch.State, dispatch.WorkRevision, dispatch.UpdatedAt = "pending", w.Revision, now
	return s.deps.Store.QueueInstructorDispatch(ctx, dispatch, w, expected, s.event(w, "delivery_queued", dispatch.ID), now.Add(5*time.Second))
}

func (s *Service) deliveryFailed(ctx context.Context, dispatch domain.InstructorDispatch, cause error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dispatches, err := s.deps.Store.ListInstructorDispatches(ctx, "", dispatch.WorkID)
	if err != nil {
		return err
	}
	for _, d := range dispatches {
		if d.ID == dispatch.ID {
			dispatch.TargetSessionID = d.TargetSessionID
		}
		if d.ID == dispatch.ID && (d.State == "accepted" || d.State == "progress") {
			return nil
		}
	}
	w, err := s.get(ctx, dispatch.WorkID)
	if err != nil {
		return err
	}
	if w.State == "done" || w.State == "cancelled" || w.State == "parked" {
		return nil
	}
	kind, title := "delivery", "Zustellung fehlgeschlagen; erneute Freigabe nötig"
	if errors.Is(cause, ErrDeliveryUncertain) {
		kind, title = "delivery_uncertain", "Native Zustellung unklar; Empfang abgleichen"
	}
	// A publication or transition decision remains open; a transport failure
	// must never replace it with a more permissive grant.
	if kind == "delivery_uncertain" {
		s.markUncertain(&w, dispatch, title)
	} else if w.Gate == nil {
		w.Gate = &domain.InstructorGate{ID: deliveryGateID(dispatch.ID), Kind: kind, Title: title, DispatchID: dispatch.ID, TargetSessionID: dispatch.TargetSessionID, OpenedAt: s.now()}
	}
	w.State = "waiting_human"
	w.Summary = title
	w.WaitCondition = cause.Error()
	_, err = s.save(ctx, w, w.Revision, "delivery_failed", dispatch.ID+": "+cause.Error())
	return err
}

// ReconcileInput is an explicit operator decision about one uncertain native
// submission. Acceptance acknowledges observation; it is not task progress.
type ReconcileInput struct {
	SnapshotID string `json:"snapshotId"`
	WorkID     string `json:"workId"`
	DispatchID string `json:"dispatchId"`
	Accepted   bool   `json:"accepted"`
	Note       string `json:"note"`
}

// ReconcileDelivery resolves an exact native attempt without approving task progress.
func (s *Service) ReconcileDelivery(ctx context.Context, in ReconcileInput) (domain.InstructorWork, error) {
	if in.SnapshotID == "" || in.WorkID == "" || in.DispatchID == "" || strings.TrimSpace(in.Note) == "" || len(in.Note) > 2048 {
		return domain.InstructorWork{}, invalid("reconciliation needs snapshot, work, dispatch and an observation note")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot, found, err := s.deps.Store.GetInstructorSnapshot(ctx, in.SnapshotID)
	if err != nil {
		return domain.InstructorWork{}, err
	}
	if !found {
		return domain.InstructorWork{}, ErrNotFound
	}
	w, err := s.get(ctx, in.WorkID)
	if err != nil {
		return w, err
	}
	var captured *domain.InstructorWork
	for _, c := range snapshot.Works {
		if c.ID == in.WorkID {
			cloned := c
			captured = &cloned
		}
	}
	if captured == nil {
		return w, invalid("work is outside the captured snapshot")
	}
	dispatches, err := s.deps.Store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil {
		return w, err
	}
	var dispatch *domain.InstructorDispatch
	for _, d := range dispatches {
		if d.ID == in.DispatchID {
			cloned := d
			dispatch = &cloned
		}
	}
	if dispatch == nil {
		return w, ErrNotFound
	}
	if w.Gate == nil {
		if in.Accepted && (dispatch.State == "delivered" || dispatch.State == "accepted" || dispatch.State == "progress") || !in.Accepted && dispatch.State == "cancelled" {
			return w, nil
		}
		return w, ErrConflict
	}
	if captured.Revision != w.Revision {
		return w, ErrConflict
	}
	if w.Gate.Kind != "delivery_uncertain" || w.Gate.ID != deliveryGateID(dispatch.ID) || dispatch.TargetSessionID == "" {
		return w, invalid("only the exact uncertain bound dispatch can be reconciled")
	}
	reconciler, ok := s.deps.Delivery.(DeliveryReconciler)
	if !ok {
		return w, invalid("native delivery reconciliation is unavailable")
	}
	if err := reconciler.Reconcile(ctx, *dispatch, in.Accepted, in.Note); err != nil {
		return w, err
	}
	state := "cancelled"
	if in.Accepted {
		state = "delivered"
	}
	if err := s.deps.Store.ReconcileInstructorDispatch(ctx, dispatch.ID, state, in.Note, s.now()); err != nil {
		return w, err
	}
	s.clearUncertain(&w, dispatch.ID, dispatches)
	if w.State != "parked" {
		w.State = "ready"
		if w.Gate != nil {
			w.State = "waiting_human"
		}
		w.WaitCondition = ""
	}
	w.Summary = "Zustellung abgeglichen; nächster Auftrag braucht einen neuen Snapshot"
	return s.save(ctx, w, w.Revision, "delivery_reconciled", dispatch.ID+" "+state+": "+in.Note)
}
