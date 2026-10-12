package instructor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

type adoptedSelectionValidator struct {
	calls  int
	model  string
	effort string
	err    error
}

func (v *adoptedSelectionValidator) ValidateInstructorAgentConfig(_ context.Context, _ domain.ProjectID, _ domain.InstructorRole, _, model, effort string) error {
	v.calls++
	v.model, v.effort = model, effort
	return v.err
}

type instructorStoreWithoutSessions struct{ instructorsvc.Store }

type instructorStoreUnknownKind struct {
	instructorsvc.Store
	session domain.SessionRecord
}

func (s instructorStoreUnknownKind) GetSession(context.Context, domain.SessionID) (domain.SessionRecord, bool, error) {
	return s.session, true, nil
}

func adoptedWorker(t *testing.T, f *instructorFixture, work domain.InstructorWork, kind domain.SessionKind, harness domain.AgentHarness, model, effort string) domain.SessionID {
	t.Helper()
	session, err := f.store.CreateSession(context.Background(), domain.SessionRecord{
		ProjectID: work.ProjectID, Kind: kind, Harness: harness,
		Metadata: domain.SessionMetadata{Model: model, Effort: effort}, CreatedAt: f.now, UpdatedAt: f.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.AttachSession(context.Background(), work.ID, session.ID); err != nil {
		t.Fatal(err)
	}
	return session.ID
}

func adoptionPolicy() domain.InstructorPolicy {
	policy := instructorsvc.DefaultPolicy()
	worker := policy.Roles["worker"]
	worker.Model = "provider/worker"
	worker.EffortByModel = map[string]string{"provider/worker": "high"}
	policy.Roles["worker"] = worker
	return policy
}

func TestInstructorAdoptionValidatesActualNativeSelectionWithoutLaunchEvidence(t *testing.T) {
	for _, scenario := range []string{"approved", "minimal", "wrong-model", "wrong-harness", "missing-effort", "unknown-kind", "native-refusal", "validator-unavailable", "identity-unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			f := newInstructorFixture(t)
			work := f.register(t, "native-adoption", adoptionPolicy())
			kind, harness, model, effort := domain.KindWorker, domain.HarnessOpenCode, "provider/worker", "high"
			switch scenario {
			case "minimal":
				effort = "minimal"
			case "wrong-model":
				model = "provider/other"
			case "wrong-harness":
				harness = domain.HarnessCodex
			case "missing-effort":
				effort = ""
			}
			id := adoptedWorker(t, f, work, kind, harness, model, effort)
			validator := &adoptedSelectionValidator{}
			if scenario == "native-refusal" {
				validator.err = errors.New("native capability no longer supports this selection")
			}
			deps := instructorsvc.Deps{Store: f.store, Delivery: f.delivery, RoleValidator: validator, Clock: func() time.Time { return f.now }}
			if scenario == "validator-unavailable" {
				deps.RoleValidator = nil
			}
			if scenario == "identity-unavailable" {
				deps.Store = instructorStoreWithoutSessions{Store: f.store}
			}
			if scenario == "unknown-kind" {
				// SQLite constrains native kinds, while the optional legacy-reader
				// capability must still fail closed if another store returns one.
				deps.Store = instructorStoreUnknownKind{Store: f.store, session: domain.SessionRecord{ID: id, ProjectID: work.ProjectID, Kind: "unknown-native-kind"}}
			}
			f.service = instructorsvc.New(deps)
			resumeErr := f.service.ValidateResume(context.Background(), id)
			queueErr := f.service.ValidateDispatchAdmission(context.Background(), id, "ordinary-adopted-turn")
			if scenario == "approved" {
				if resumeErr != nil || queueErr != nil || validator.calls != 2 || validator.model != model || validator.effort != effort {
					t.Fatalf("approved actual selection was not validated: resume=%v queue=%v validator=%+v", resumeErr, queueErr, validator)
				}
			} else if resumeErr == nil || queueErr == nil {
				t.Fatalf("invalid adopted identity or selection executed: resume=%v queue=%v validator=%+v", resumeErr, queueErr, validator)
			}
			current, _, err := f.store.GetInstructorWork(context.Background(), work.ID)
			if err != nil || len(current.Launches) != 0 {
				t.Fatalf("admission fabricated native launch evidence: launches=%+v err=%v", current.Launches, err)
			}
		})
	}
}

func TestInstructorManagedParentDelegationCannotEscapeCurrentAuthority(t *testing.T) {
	for _, provenance := range []string{"adopted", "recorded"} {
		for _, scenario := range []string{"same-work", "other-work", "parked", "triage", "granted-triage", "orchestrator-child", "instructor-child"} {
			t.Run(provenance+"/"+scenario, func(t *testing.T) {
				f := newInstructorFixture(t)
				f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
				parent := f.register(t, "parent-authority", adoptionPolicy())
				other := f.register(t, "other-authority", adoptionPolicy())
				var id domain.SessionID
				if provenance == "recorded" {
					id = f.attachWorker(t, parent.ID)
				} else {
					id = adoptedWorker(t, f, parent, domain.KindWorker, domain.HarnessOpenCode, "provider/worker", "high")
				}
				f.trustedFacts(t, parent.ID, func(w *domain.InstructorWork) {
					w.Phase, w.State, w.SHA = "implement", "ready", immutableSHA
					if scenario == "parked" {
						w.State = "parked"
					}
					if scenario == "triage" || scenario == "granted-triage" {
						w.Phase, w.FixRounds = "triage", 1
					}
					if scenario == "granted-triage" {
						w.ApprovedTransitions = []string{"triage->fix"}
						w.Evidence = []domain.InstructorEvidence{{ID: "native-human-grant", WorkID: w.ID, PolicyVersion: w.PolicyVersion, SHA: immutableSHA, Kind: "triage", Producer: "operator", Role: "instructor", Ref: "gate:approved", Verified: true}}
					}
				})
				input := instructorsvc.SpawnInput{ProjectID: "governed", ParentSessionID: id, Role: "worker", Harness: "opencode", Model: "provider/worker", Effort: "high"}
				if scenario == "other-work" {
					input.WorkID = other.ID
				}
				if scenario == "orchestrator-child" || scenario == "instructor-child" {
					input.Role = strings.TrimSuffix(scenario, "-child")
					input.Harness = "codex"
				}
				workID, err := f.service.PrepareSpawn(context.Background(), input)
				if workID != parent.ID {
					t.Fatalf("managed child escaped parent work: work=%s parent=%s err=%v", workID, parent.ID, err)
				}
				allowed := scenario == "same-work" || scenario == "granted-triage"
				if allowed && err != nil || !allowed && err == nil {
					t.Fatalf("parent authority incorrect: allowed=%v err=%v", allowed, err)
				}
				works, err := f.store.ListInstructorWorks(context.Background())
				if err != nil || len(works) != 2 {
					t.Fatalf("delegation created another durable work: works=%d err=%v", len(works), err)
				}
			})
		}
	}
}

func TestInstructorRecordedRolePrecedesAdoptedKindAndPausedControlCannotQueue(t *testing.T) {
	f := newInstructorFixture(t)
	f.service = instructorsvc.New(instructorsvc.Deps{Store: f.store, RoleValidator: permissiveNativeValidator{}, Clock: func() time.Time { return f.now }})
	work := f.register(t, "recorded-role", adoptionPolicy())
	id := adoptedWorker(t, f, work, domain.KindOrchestrator, domain.HarnessCodex, "provider/control", "high")
	selection := domain.InstructorAgentSelection{Harness: "opencode", Model: "provider/worker", Effort: "high"}
	if err := f.service.RecordLaunch(context.Background(), work.ID, domain.InstructorLaunch{SessionID: id, Role: "worker", Requested: selection, Sent: &selection, Status: "launched"}); err != nil {
		t.Fatal(err)
	}
	f.trustedFacts(t, work.ID, func(w *domain.InstructorWork) { w.Phase, w.State, w.SHA = "triage", "ready", immutableSHA })
	if err := f.service.ValidateDispatchAdmission(context.Background(), id, "recorded-worker-with-control-kind"); err == nil {
		t.Fatal("persisted control kind overrode the actual recorded worker role")
	}
	controlWork := f.register(t, "paused-control", instructorsvc.DefaultPolicy())
	control := adoptedWorker(t, f, controlWork, domain.KindOrchestrator, domain.HarnessCodex, "provider/control", "high")
	f.trustedFacts(t, controlWork.ID, func(w *domain.InstructorWork) { w.State = "parked" })
	if err := f.service.ValidateDispatchAdmission(context.Background(), control, "ordinary-paused-control-turn"); err == nil {
		t.Fatal("an adopted managed control session bypassed the paused work state")
	}
	unassigned, err := f.store.CreateSession(context.Background(), domain.SessionRecord{ProjectID: "governed", Kind: domain.KindOrchestrator, CreatedAt: f.now, UpdatedAt: f.now})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.ValidateDispatchAdmission(context.Background(), unassigned.ID, "ordinary-unassigned-control-turn"); err != nil {
		t.Fatalf("genuine unassigned project control behavior changed: %v", err)
	}
}
