package instructor_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

type receiptAfterClaimStore struct {
	*sqlite.Store
	afterClaim func(domain.InstructorDispatch) error
}

func (s *receiptAfterClaimStore) ClaimInstructorDispatch(ctx context.Context, at time.Time) (domain.InstructorDispatch, bool, error) {
	dispatch, found, err := s.Store.ClaimInstructorDispatch(ctx, at)
	if err == nil && found && s.afterClaim != nil {
		hook := s.afterClaim
		s.afterClaim = nil
		err = hook(dispatch)
	}
	// Deliberately return the copy captured before the receipt committed. The
	// durable leased row may now have a newer, still-authorized work revision.
	return dispatch, found, err
}

func TestInstructorReceiptAfterClaimReloadsOnlyRetainedAuthority(t *testing.T) {
	for _, scenario := range []string{"accepted", "progress", "changed-commit", "human-gate", "queued-accepted", "queued-progress"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			f := newInstructorFixture(t)
			work := f.register(t, "claim-receipt", instructorsvc.DefaultPolicy())
			work = f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) {
				w.Phase, w.State, w.PhaseSequence = "implement", "ready", 1
			})
			target, err := f.store.CreateSession(ctx, domain.SessionRecord{
				ProjectID: work.ProjectID, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex,
				CreatedAt: f.now, UpdatedAt: f.now,
			})
			if err != nil {
				t.Fatal(err)
			}
			instance, found, err := f.store.InstructorSessionInstance(ctx, target.ID)
			if err != nil || !found {
				t.Fatalf("target incarnation: found=%v error=%v", found, err)
			}
			bound := domain.InstructorDispatch{
				ID: "earlier-receipt", BatchID: "earlier", WorkID: work.ID, ProjectID: work.ProjectID,
				WorkRevision: work.Revision, State: "accepted", Action: "weiter",
				TargetSessionID: target.ID, TargetSessionInstanceID: instance, CreatedAt: f.now, UpdatedAt: f.now,
			}
			pending := bound
			pending.ID, pending.BatchID, pending.State = "current-phase-directive", "current-phase", "pending"
			if err := f.store.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{bound, pending}); err != nil {
				t.Fatal(err)
			}
			racing := &receiptAfterClaimStore{Store: f.store}
			f.service = instructorsvc.New(instructorsvc.Deps{Store: racing, Delivery: f.delivery, Clock: func() time.Time { return f.now }})
			queued := strings.HasPrefix(scenario, "queued-")
			hooked := false
			reportReceipt := func(captured domain.InstructorDispatch) error {
				hooked = true
				if captured.ID != pending.ID || captured.State != "leased" || captured.WorkRevision != work.Revision {
					t.Fatalf("fixture missed exact claim boundary: %+v", captured)
				}
				input := instructorsvc.ReportInput{
					WorkID: work.ID, SessionID: target.ID, DispatchID: bound.ID,
					Kind: "accepted", Summary: "Earlier native instruction received",
				}
				switch strings.TrimPrefix(scenario, "queued-") {
				case "progress":
					input.Kind = "progress"
				case "changed-commit":
					input.SHA = immutableSHA
				case "human-gate":
					input.Kind = "needs_input"
					input.Gate = &domain.InstructorGate{Kind: "human", Title: "Operator decision required"}
				}
				updated, err := f.service.Report(ctx, input)
				if err == nil && updated.Revision <= captured.WorkRevision {
					t.Fatal("receipt did not interleave a newer work revision")
				}
				return err
			}
			if queued {
				// Native delivery can observe a previous receipt while this
				// instruction is being queued. Queue settlement must use the
				// atomically carried durable revision, not its earlier argument.
				f.delivery.bind = func(_ context.Context, dispatch domain.InstructorDispatch) error {
					return reportReceipt(dispatch)
				}
				f.delivery.err = instructorsvc.ErrDeliveryQueued
			} else {
				racing.afterClaim = reportReceipt
			}
			if err := f.service.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			if !hooked {
				t.Fatal("claim/receipt interleaving did not run")
			}
			delivered := 0
			for _, call := range f.delivery.calls {
				if call.ID == pending.ID {
					delivered++
					wantRevision := work.Revision + 1
					if queued {
						wantRevision = work.Revision
					}
					if call.WorkRevision != wantRevision {
						t.Fatalf("native delivery used pre-receipt authority: %+v", call)
					}
				}
			}
			allowed := scenario == "accepted" || scenario == "progress" || queued
			if allowed && delivered != 1 || !allowed && delivered != 0 {
				t.Fatalf("retained authority=%v native deliveries=%d", allowed, delivered)
			}
			rows, err := f.store.ListInstructorDispatches(ctx, "", work.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantState := "failed"
			if allowed {
				wantState = "delivered"
			}
			if queued {
				wantState = "pending"
			}
			for _, row := range rows {
				if row.ID == pending.ID && row.State != wantState {
					t.Fatalf("wrong durable dispatch outcome: %+v", row)
				}
				if queued && row.ID == pending.ID && row.WorkRevision != work.Revision+2 {
					t.Fatalf("queue settlement lost receipt's carried revision: %+v", row)
				}
			}
		})
	}
}
