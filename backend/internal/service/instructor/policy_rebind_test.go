package instructor_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func policyRebindFixture(t *testing.T) (*instructorFixture, domain.InstructorWork, domain.InstructorPolicy) {
	t.Helper()
	f := newInstructorFixture(t)
	w := f.register(t, "policy-rebind", instructorsvc.DefaultPolicy())
	ctx := context.Background()
	project, _, err := f.store.GetProject(ctx, "governed")
	if err != nil {
		t.Fatal(err)
	}
	project.Config.Worker = domain.RoleOverride{Harness: domain.HarnessOpenCode, AgentConfig: domain.AgentConfig{Model: "provider/worker", Effort: "high"}}
	project.Config.Orchestrator = domain.RoleOverride{Harness: domain.HarnessCodex, AgentConfig: domain.AgentConfig{Model: "coordinator", Effort: "max"}}
	project.Config.Reviewers = []domain.ReviewerConfig{{Harness: domain.ReviewerCodex, AgentConfig: domain.AgentConfig{Model: "reviewer", Effort: "xhigh"}}}
	if err := f.store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	target := instructorsvc.DefaultPolicy()
	for name, selection := range map[string]domain.InstructorAgentSelection{
		"worker":       {Harness: "opencode", Model: "provider/worker", Effort: "high"},
		"reviewer":     {Harness: "codex", Model: "reviewer", Effort: "xhigh"},
		"orchestrator": {Harness: "codex", Model: "coordinator", Effort: "max"},
	} {
		role := target.Roles[name]
		role.Model = selection.Model
		role.EffortByModel = map[string]string{selection.Model: selection.Effort}
		if role.Effort == "configured" {
			role.Effort = selection.Effort
		}
		target.Roles[name] = role
	}
	target.Version = instructorsvc.PolicyVersion(target)
	if err := f.store.PutInstructorPolicy(ctx, target); err != nil {
		t.Fatal(err)
	}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: f.delivery, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
	return f, w, target
}

func policyRebindInput(t *testing.T, f *instructorFixture, w domain.InstructorWork) (instructorsvc.PolicyRebindInput, instructorsvc.PolicyRebindPreview) {
	t.Helper()
	in := instructorsvc.PolicyRebindInput{WorkID: w.ID, SnapshotID: f.snapshot(t).ID, Reason: "Use the corrected native role mappings"}
	preview, err := f.service.PreviewPolicyRebind(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	in.TargetPolicyVersion, in.Approval = preview.TargetPolicyVersion, preview.ExactApproval
	return in, preview
}

func TestInstructorPolicyRebindPreservesIdentityHistoryBudgetsAndPauseAcrossRestart(t *testing.T) {
	f, w, target := policyRebindFixture(t)
	ctx := context.Background()
	entry, err := f.service.ReserveBudget(ctx, w.ID, "turn", "finished-turn", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.SetBudgetState(ctx, entry.ID, "finished"); err != nil {
		t.Fatal(err)
	}
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.State, w.Phase, w.SHA, w.Branch = "parked", "triage", immutableSHA, "agent/preserved"
		w.FixRounds, w.PhaseSequence, w.FixPhaseSequence, w.DeltaPhaseSequence = 1, 4, 2, 3
		w.ApprovedTransitions = []string{"triage->fix"}
		w.ApprovalDispatchID = "old-cohort"
		w.Gate = &domain.InstructorGate{ID: "old-gate", Kind: "transition", TransitionTo: "fix"}
		w.Evidence = []domain.InstructorEvidence{{ID: "old-evidence", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: w.SHA, Kind: "triage", Verified: true}}
		w.Exceptions = []domain.InstructorException{{ID: "expired-exception", Kind: "role", Role: "worker", ExpiresAt: f.now.Add(-time.Hour)}}
	})
	usage, err := f.service.BudgetUsage(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	in, preview := policyRebindInput(t, f, w)
	if preview.BeforePolicyVersion != w.PolicyVersion || preview.TargetPolicyVersion != target.Version || len(preview.InvalidatedTransitions) != 1 || len(preview.SupersededExceptionIDs) != 1 || preview.HistoricalEvidenceCount != 1 {
		t.Fatalf("incomplete change preview: %+v", preview)
	}
	before, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	if before.Revision != w.Revision || before.PolicyVersion != w.PolicyVersion {
		t.Fatal("preview mutated work")
	}
	updated, err := f.service.ApplyPolicyRebind(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != w.ID || updated.PolicyVersion != target.Version || updated.State != "parked" || updated.Gate == nil || updated.Gate.Kind != "transition" || updated.Gate.ID == w.Gate.ID || updated.Gate.TransitionTo != "fix" || updated.Revision != w.Revision+1 || updated.FixRounds != w.FixRounds || updated.Phase != w.Phase || updated.PhaseSequence != w.PhaseSequence || updated.SHA != w.SHA || updated.Branch != w.Branch || updated.FixPhaseSequence != w.FixPhaseSequence || updated.DeltaPhaseSequence != w.DeltaPhaseSequence {
		t.Fatalf("replacement lost work identity/history/pause: %+v", updated)
	}
	if !reflect.DeepEqual(w.Evidence, updated.Evidence) || len(updated.ApprovedTransitions) != 0 || updated.ApprovalDispatchID != "" || !updated.Exceptions[0].Superseded {
		t.Fatal("replacement relabelled evidence or retained previous execution authority")
	}
	if len(updated.PolicyRebindings) != 1 || updated.PolicyRebindings[0].PreviousGate.ID != "old-gate" || updated.PolicyRebindings[0].PreviousApprovalDispatchID != "old-cohort" || updated.PolicyRebindings[0].BeforePolicy.Version != w.PolicyVersion {
		t.Fatalf("missing immutable replacement receipt: %+v", updated.PolicyRebindings)
	}
	afterUsage, err := f.service.BudgetUsage(ctx, w.ID)
	if err != nil || afterUsage != usage || usage.Turns != 1 {
		t.Fatalf("replacement reset durable usage: before=%+v after=%+v err=%v", usage, afterUsage, err)
	}
	rows, err := f.store.ListInstructorDispatches(ctx, "", w.ID)
	if err != nil || len(rows) != 0 || len(f.delivery.calls) != 0 {
		t.Fatal("replacement implicitly dispatched execution")
	}
	// Durable receipt, not an in-memory token, handles a retry even after a later
	// state change and without revalidating an already-applied target.
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store})
	stopped := f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) { w.State = "cancelled" })
	retry, err := f.service.ApplyPolicyRebind(ctx, in)
	if err != nil || retry.Revision != stopped.Revision || retry.State != "cancelled" || len(retry.PolicyRebindings) != 1 {
		t.Fatalf("retry resurrected or duplicated work: %+v err=%v", retry, err)
	}
	events, err := f.store.ListInstructorEvents(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range events {
		if event.Kind == "policy_rebound" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("replacement events=%d", count)
	}
}

func TestInstructorPolicyRebindRejectsStaleOrChangedApprovalWithoutMutation(t *testing.T) {
	for _, change := range []string{"work revision", "target policy", "reason", "approval", "missing target"} {
		t.Run(change, func(t *testing.T) {
			f, w, target := policyRebindFixture(t)
			in, _ := policyRebindInput(t, f, w)
			want := instructorsvc.ErrValidation
			switch change {
			case "work revision":
				w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) { w.Summary = "changed after preview" })
				want = instructorsvc.ErrConflict
			case "target policy":
				target.NudgeAfterSeconds++
				target.Version = instructorsvc.PolicyVersion(target)
				if err := f.store.PutInstructorPolicy(context.Background(), target); err != nil {
					t.Fatal(err)
				}
				want = instructorsvc.ErrConflict
			case "reason":
				in.Reason = "different reason"
			case "approval":
				in.Approval = "Go"
			case "missing target":
				in.TargetPolicyVersion = ""
			}
			if _, err := f.service.ApplyPolicyRebind(context.Background(), in); !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
			current, _, _ := f.store.GetInstructorWork(context.Background(), w.ID)
			if current.PolicyVersion != w.PolicyVersion || current.Revision != w.Revision || len(current.PolicyRebindings) != 0 {
				t.Fatal("rejected proposal mutated work")
			}
		})
	}
}

func TestInstructorPolicyRebindResolvesRepositoryPolicyAndRejectsNativeMismatch(t *testing.T) {
	f, w, target := policyRebindFixture(t)
	ctx := context.Background()
	project, _, _ := f.store.GetProject(ctx, "governed")
	project.Config.GovernanceFile = "rules.json"
	if err := f.store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	target.NudgeAfterSeconds++
	target.Version = instructorsvc.PolicyVersion(target)
	body, _ := json.Marshal(target)
	if err := os.WriteFile(filepath.Join(project.Path, "rules.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
	in, preview := policyRebindInput(t, f, w)
	if preview.PolicySource != "repository:rules.json" || preview.TargetPolicyVersion != target.Version {
		t.Fatalf("wrong effective source: %+v", preview)
	}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, RoleValidator: rejectingNativeValidator{}})
	if _, err := f.service.ApplyPolicyRebind(ctx, in); err == nil || !strings.Contains(err.Error(), "native model") {
		t.Fatalf("native mismatch accepted: %v", err)
	}
}

func TestInstructorPolicyRebindRejectsUnsafeWorkStates(t *testing.T) {
	cases := []struct {
		name   string
		change func(*domain.InstructorWork)
	}{
		{"running", func(w *domain.InstructorWork) { w.State = "running" }},
		{"done", func(w *domain.InstructorWork) { w.State = "done" }},
		{"cancelled", func(w *domain.InstructorWork) { w.State = "cancelled" }},
		{"cancelling", func(w *domain.InstructorWork) { w.Cancellation = &domain.InstructorCancellation{} }},
		{"revoked publication", func(w *domain.InstructorWork) {
			w.PublicationGrants = []domain.InstructorPublicationGrant{{Revoked: true}}
		}},
		{"uncertain dispatch", func(w *domain.InstructorWork) { w.UncertainDispatchIDs = []string{"unknown"} }},
		{"native phase effect", func(w *domain.InstructorWork) { w.PhaseActions = []domain.InstructorPhaseAction{{State: "running"}} }},
		{"publication violation", func(w *domain.InstructorWork) {
			w.SuspendedGate = &domain.InstructorGate{Kind: "publication_violation"}
		}},
		{"undeclared phase", func(w *domain.InstructorWork) { w.Phase = "retired-phase" }},
		{"consumed fix budget", func(w *domain.InstructorWork) { w.FixRounds = 3 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, w, _ := policyRebindFixture(t)
			w = f.trustedFacts(t, w.ID, tc.change)
			_, err := f.service.PreviewPolicyRebind(context.Background(), instructorsvc.PolicyRebindInput{WorkID: w.ID, SnapshotID: f.snapshot(t).ID, Reason: "approved change"})
			if err == nil {
				t.Fatal("unsafe replacement accepted")
			}
		})
	}
}

func TestInstructorPolicyRebindFencesReservationRacesAndUncertainNativeEffects(t *testing.T) {
	for _, state := range []string{"reserved", "uncertain"} {
		t.Run(state, func(t *testing.T) {
			f, w, _ := policyRebindFixture(t)
			ctx := context.Background()
			in, _ := policyRebindInput(t, f, w)
			// Reservation does not increment work revision: work-CAS alone cannot
			// reject this pre-native effect arriving after human preview.
			entry, err := f.service.ReserveBudget(ctx, w.ID, "turn", "concurrent-native-effect", "", "", false)
			if err != nil {
				t.Fatal(err)
			}
			if state == "uncertain" {
				if err := f.service.SetBudgetState(ctx, entry.ID, "uncertain"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.service.ApplyPolicyRebind(ctx, in); !errors.Is(err, instructorsvc.ErrDeliveryUncertain) {
				t.Fatalf("pre-native race accepted: %v", err)
			}
			current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
			if current.Revision != w.Revision || current.PolicyVersion != w.PolicyVersion {
				t.Fatal("native race partially persisted replacement")
			}
		})
	}
}

func TestInstructorPolicyRebindRequiresPositiveCurrentNativeEpoch(t *testing.T) {
	for _, condition := range []string{"unknown", "epoch changed", "quiet"} {
		t.Run(condition, func(t *testing.T) {
			f, w, _ := policyRebindFixture(t)
			ctx := context.Background()
			sid := f.attachWorker(t, w.ID)
			instance, _, err := f.store.InstructorSessionInstance(ctx, sid)
			if err != nil {
				t.Fatal(err)
			}
			rec, _, _ := f.store.GetSession(ctx, sid)
			rec.Metadata.RuntimeLaunchID = "original-runtime"
			rec.Activity.State = domain.ActivityExited
			if err := f.store.UpdateSession(ctx, rec); err != nil {
				t.Fatal(err)
			}
			if err := f.store.FinishInstructorSessionBudgets(ctx, "session:"+instance, f.now); err != nil {
				t.Fatal(err)
			}
			w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) { w.State = "blocked" })
			delivery := &cancellationDelivery{deliveryRecorder: f.delivery, settled: true, proof: domain.InstructorCancellationProof{Producers: []domain.InstructorReportAdmission{{WorkID: w.ID, SessionID: sid, InstanceID: instance, Member: true, RuntimeID: "original-runtime"}}}}
			f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: delivery, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
			in, _ := policyRebindInput(t, f, w)
			switch condition {
			case "unknown":
				delivery.settled = false
			case "epoch changed":
				delivery.afterObservation = func() {
					rec.Metadata.RuntimeLaunchID = "replacement-runtime"
					if err := f.store.UpdateSession(ctx, rec); err != nil {
						t.Fatal(err)
					}
				}
			}
			updated, err := f.service.ApplyPolicyRebind(ctx, in)
			if condition != "quiet" {
				if !errors.Is(err, instructorsvc.ErrDeliveryUncertain) {
					t.Fatalf("unsafe native observation accepted: %v", err)
				}
			} else if err != nil || updated.State != "waiting_human" || updated.Gate.Kind != "policy_rebound" || !reflect.DeepEqual(updated.SessionIDs, w.SessionIDs) || !reflect.DeepEqual(updated.Launches, w.Launches) {
				t.Fatalf("quiet native epoch could not migrate with identity intact: %+v %v", updated, err)
			}
		})
	}
}

func TestInstructorPolicyRebindFencesDispatchInsertedAfterNativeObservation(t *testing.T) {
	f, w, _ := policyRebindFixture(t)
	ctx := context.Background()
	delivery := &cancellationDelivery{deliveryRecorder: f.delivery, settled: true}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: delivery, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
	in, _ := policyRebindInput(t, f, w)
	delivery.afterObservation = func() {
		err := f.store.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{{ID: "concurrent-dispatch", BatchID: "concurrent-batch", WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: w.Revision, State: "pending", Action: "weiter", CreatedAt: f.now, UpdatedAt: f.now}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.ApplyPolicyRebind(ctx, in); !errors.Is(err, instructorsvc.ErrDeliveryUncertain) {
		t.Fatalf("dispatch admitted between probe and replacement: %v", err)
	}
	current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	if current.PolicyVersion != w.PolicyVersion || len(current.PolicyRebindings) != 0 {
		t.Fatal("native dispatch race partially changed work")
	}
}

func TestInstructorPolicyRebindRechecksTargetAfterNativeProbe(t *testing.T) {
	f, w, target := policyRebindFixture(t)
	ctx := context.Background()
	delivery := &cancellationDelivery{deliveryRecorder: f.delivery, settled: true}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: delivery, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
	in, _ := policyRebindInput(t, f, w)
	delivery.afterObservation = func() {
		target.NudgeAfterSeconds++
		target.Version = instructorsvc.PolicyVersion(target)
		if err := f.store.PutInstructorPolicy(ctx, target); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.ApplyPolicyRebind(ctx, in); !errors.Is(err, instructorsvc.ErrConflict) {
		t.Fatalf("target changed during native probe: %v", err)
	}
	current, _, _ := f.store.GetInstructorWork(ctx, w.ID)
	if current.PolicyVersion != w.PolicyVersion || current.Revision != w.Revision {
		t.Fatal("target race partially changed work")
	}
}

func TestInstructorPolicyRebindPreservesUnrelatedHumanDecision(t *testing.T) {
	f, w, _ := policyRebindFixture(t)
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.State = "waiting_human"
		w.Gate = &domain.InstructorGate{ID: "scope-decision", Kind: "scope", Title: "Choose the agreed scope", ExactApproval: "SCOPE-GO release-one", OpenedAt: f.now}
	})
	in, _ := policyRebindInput(t, f, w)
	updated, err := f.service.ApplyPolicyRebind(context.Background(), in)
	if err != nil || updated.State != "waiting_human" || updated.Gate == nil || updated.Gate.ID == w.Gate.ID || updated.Gate.Kind != "scope" || updated.Gate.Title != w.Gate.Title || updated.Gate.ExactApproval != w.Gate.ExactApproval {
		t.Fatalf("configuration approval consumed unrelated human decision: %+v %v", updated, err)
	}
}

func TestInstructorPolicyRebindAgainRefreshesExecutionApproval(t *testing.T) {
	for _, location := range []string{"active", "suspended"} {
		t.Run(location, func(t *testing.T) {
			ctx := context.Background()
			f, w, target := policyRebindFixture(t)
			firstInput, _ := policyRebindInput(t, f, w)
			first, err := f.service.ApplyPolicyRebind(ctx, firstInput)
			if err != nil {
				t.Fatal(err)
			}
			oldApproval := first.Gate.ExactApproval
			if location == "suspended" {
				first = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
					w.SuspendedGate, w.Gate = w.Gate, nil
				})
			}
			target.NudgeAfterSeconds++
			target.Version = instructorsvc.PolicyVersion(target)
			if err := f.store.PutInstructorPolicy(ctx, target); err != nil {
				t.Fatal(err)
			}
			secondInput, _ := policyRebindInput(t, f, first)
			second, err := f.service.ApplyPolicyRebind(ctx, secondInput)
			if err != nil {
				t.Fatal(err)
			}
			if second.Gate == nil || second.Gate.ExactApproval != "RESUME-GO "+w.ID+" "+target.Version || second.Gate.ExactApproval == oldApproval {
				t.Fatalf("second replacement retained obsolete execution approval: %+v", second.Gate)
			}
			command := instructorsvc.CommandInput{Command: "Go", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{w.ID}, Approval: oldApproval, IdempotencyKey: "obsolete-policy-go"}
			if _, err := f.service.Command(ctx, command); !errors.Is(err, instructorsvc.ErrValidation) {
				t.Fatalf("previous policy's exact phrase released current policy: %v", err)
			}
			command.SnapshotID, command.Approval, command.IdempotencyKey = f.snapshot(t).ID, second.Gate.ExactApproval, "current-policy-go"
			if _, err := f.service.Command(ctx, command); err != nil {
				t.Fatalf("current exact phrase could not release replacement: %v", err)
			}
			current, _, err := f.store.GetInstructorWork(ctx, w.ID)
			if err != nil || current.Gate != nil || current.State != "ready" || current.PolicyVersion != target.Version || len(current.PolicyRebindings) != 2 {
				t.Fatalf("second replacement lost history or remained blocked: %+v %v", current, err)
			}
		})
	}
}

func TestInstructorPolicyRebindAdmittedDispatchRequiresExactTerminalNativeTurn(t *testing.T) {
	for _, state := range []string{"accepted", "progress", "started"} {
		for _, native := range []string{"completed", "running", "queued", "missing-native", "wrong-generation", "missing-target"} {
			t.Run(state+"/"+native, func(t *testing.T) {
				ctx := context.Background()
				f, w, target := policyRebindFixture(t)
				orch, err := f.store.CreateSession(ctx, domain.SessionRecord{ProjectID: w.ProjectID, Kind: domain.KindOrchestrator, Harness: domain.HarnessCodex, Mode: domain.SessionModeChat,
					Activity: domain.Activity{State: domain.ActivityIdle}, Metadata: domain.SessionMetadata{ControllerGeneration: "original-generation"}, CreatedAt: f.now, UpdatedAt: f.now})
				if err != nil {
					t.Fatal(err)
				}
				dispatch := domain.InstructorDispatch{ID: "prior-directive", BatchID: "prior-batch", WorkID: w.ID, ProjectID: w.ProjectID, WorkRevision: w.Revision,
					State: state, Action: "weiter", TargetSessionID: orch.ID, TargetRuntimeID: "chat:original-generation", CreatedAt: f.now, UpdatedAt: f.now}
				if state == "started" {
					dispatch.State = "accepted"
				}
				if native == "missing-target" {
					dispatch.TargetSessionID = ""
				}
				if err := f.store.EnqueueInstructorDispatches(ctx, []domain.InstructorDispatch{dispatch}); err != nil {
					t.Fatal(err)
				}
				if state == "started" {
					if err := f.store.SetInstructorDispatch(ctx, dispatch.ID, state, "native start receipt", f.now); err != nil {
						t.Fatal(err)
					}
				}
				if native != "missing-target" {
					if err := f.store.SetInstructorDispatch(ctx, dispatch.ID, "delivered", "native transport receipt", f.now); err != nil {
						t.Fatal(err)
					}
					rows, err := f.store.ListInstructorDispatches(ctx, "", w.ID)
					if err != nil || len(rows) != 1 || rows[0].State != state {
						t.Fatalf("fixture did not retain admitted transport state: %+v %v", rows, err)
					}
				}
				if native != "missing-native" && native != "missing-target" {
					conv, err := f.store.CreateConversation(ctx, "prior-conversation", domain.ConversationScopeProject, w.ProjectID, orch.ID, f.now)
					if err != nil {
						t.Fatal(err)
					}
					generation := "original-generation"
					if native == "wrong-generation" {
						generation = "replacement-generation"
					}
					if _, err := f.store.AppendUserMessage(ctx, conv.ID, orch.ID, generation, domain.ConversationMessage{ID: "prior-message", Text: "approved directive", ClientMessageID: dispatch.ID, Origin: domain.MessageOriginAutomation}, "prior-turn", f.now); err != nil {
						t.Fatal(err)
					}
					if native != "queued" {
						if err := f.store.BindTurnToProvider(ctx, "prior-turn", "native-prior-turn", f.now); err != nil {
							t.Fatal(err)
						}
					}
					if native == "completed" || native == "wrong-generation" {
						if err := f.store.SettleTurnByID(ctx, "prior-turn", domain.TurnStateCompleted, "native completion", f.now); err != nil {
							t.Fatal(err)
						}
					}
				}
				in, _ := policyRebindInput(t, f, w)
				updated, err := f.service.ApplyPolicyRebind(ctx, in)
				if native == "completed" {
					if err != nil || updated.PolicyVersion != target.Version || updated.State != "waiting_human" {
						t.Fatalf("conclusively completed old directive prevented rebind: %+v %v", updated, err)
					}
				} else if !errors.Is(err, instructorsvc.ErrDeliveryUncertain) {
					t.Fatalf("admitted receipt replaced positive native proof: %v", err)
				}
			})
		}
	}
}

func TestInstructorPolicyRebindRejectsIncompatiblePendingTransition(t *testing.T) {
	f, w, target := policyRebindFixture(t)
	w = f.trustedFacts(t, w.ID, func(w *domain.InstructorWork) {
		w.State, w.Phase = "waiting_human", "triage"
		w.Gate = &domain.InstructorGate{ID: "fix-decision", Kind: "transition", TransitionTo: "fix"}
	})
	for i := range target.Transitions {
		if target.Transitions[i].From == "triage" && target.Transitions[i].To == "fix" {
			target.Transitions[i].To = "implement"
		}
	}
	target.Version = instructorsvc.PolicyVersion(target)
	if err := f.store.PutInstructorPolicy(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	_, err := f.service.PreviewPolicyRebind(context.Background(), instructorsvc.PolicyRebindInput{WorkID: w.ID, SnapshotID: f.snapshot(t).ID, Reason: "change configuration"})
	if !errors.Is(err, instructorsvc.ErrValidation) || !strings.Contains(err.Error(), "pending human transition") {
		t.Fatalf("pending decision broadened under target policy: %v", err)
	}
}

func TestInstructorPolicyRebindRecordedLaunchCannotResumeObsoleteSelection(t *testing.T) {
	f, w, target := policyRebindFixture(t)
	ctx := context.Background()
	sid := f.attachWorker(t, w.ID)
	instance, _, _ := f.store.InstructorSessionInstance(ctx, sid)
	record, _, _ := f.store.GetSession(ctx, sid)
	record.Activity.State = domain.ActivityExited
	record.Metadata.RuntimeLaunchID = "old-native-runtime"
	if err := f.store.UpdateSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := f.store.FinishInstructorSessionBudgets(ctx, "session:"+instance, f.now); err != nil {
		t.Fatal(err)
	}
	project, _, _ := f.store.GetProject(ctx, "governed")
	project.Config.Worker.AgentConfig.Model = "provider/approved-next"
	if err := f.store.UpsertProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	role := target.Roles["worker"]
	role.Model, role.EffortByModel = "provider/approved-next", map[string]string{"provider/approved-next": "high"}
	target.Roles["worker"] = role
	target.Version = instructorsvc.PolicyVersion(target)
	if err := f.store.PutInstructorPolicy(ctx, target); err != nil {
		t.Fatal(err)
	}
	delivery := &cancellationDelivery{deliveryRecorder: f.delivery, settled: true, proof: domain.InstructorCancellationProof{Producers: []domain.InstructorReportAdmission{{WorkID: w.ID, SessionID: sid, InstanceID: instance, Member: true, RuntimeID: record.Metadata.RuntimeLaunchID}}}}
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, Delivery: delivery, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
	in, _ := policyRebindInput(t, f, w)
	updated, err := f.service.ApplyPolicyRebind(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	choice := domain.InstructorAgentSelection{Harness: "opencode", Model: "provider/approved-next", Effort: "high"}
	if err := f.service.ValidateAgentChange(ctx, sid, choice); err == nil {
		t.Fatal("changing native choice bypassed the fresh human execution gate")
	}
	if err := f.service.ValidateControllerRecovery(ctx, sid); err == nil {
		t.Fatal("historical launch bypassed recovery selection check")
	}
	_, err = f.service.Command(ctx, instructorsvc.CommandInput{Command: "Go", SnapshotID: f.snapshot(t).ID, WorkIDs: []string{w.ID}, Approval: updated.Gate.ExactApproval, IdempotencyKey: "approved-new-selection"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.service.ValidateResume(ctx, sid); err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("old native configuration resumed under replacement policy: %v", err)
	}
	if err := f.service.ValidateDispatchAdmission(ctx, sid, "ordinary-native-turn"); err == nil {
		t.Fatal("native queue accepted obsolete historical launch selection")
	}
	if err := f.service.ValidateAgentChange(ctx, sid, choice); err != nil {
		t.Fatalf("obsolete selection prevented changing to approved target: %v", err)
	}
	unchanged, _, _ := f.store.GetSession(ctx, sid)
	if unchanged.Harness != record.Harness || unchanged.Metadata.Model != record.Metadata.Model {
		t.Fatal("candidate validation silently changed existing native conversation")
	}
	choice.Model = "provider/unapproved"
	if err := f.service.ValidateAgentChange(ctx, sid, choice); err == nil {
		t.Fatal("candidate change bypassed new pinned selection")
	}
}
