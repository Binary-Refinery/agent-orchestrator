package session

import (
	"context"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestGovernanceManagedRejectsNativeSessionEnabling(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{GovernanceManaged: true}}
	st.sessions["mer-1"] = domain.SessionRecord{ID: "mer-1", ProjectID: "mer", Kind: domain.KindWorker}
	svc := &Service{store: st}
	for _, set := range []func(context.Context, domain.SessionID, bool) (domain.Session, error){svc.SetAutoReview, svc.SetAutoInjectCI, svc.SetAutoInjectReview} {
		if _, err := set(context.Background(), "mer-1", true); err == nil {
			t.Fatal("native lane was enabled")
		}
		if _, err := set(context.Background(), "mer-1", false); err != nil {
			t.Fatal(err)
		}
	}
	rec := st.sessions["mer-1"]
	if rec.AutoReviewEnabled || rec.AutoInjectCI || rec.AutoInjectReview {
		t.Fatal("unsafe flag persisted")
	}
}

func TestGovernanceManagedRejectsTerminateOnPRMerge(t *testing.T) {
	st := newFakeStore()
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{GovernanceManaged: true}}
	st.sessions["mer-1"] = domain.SessionRecord{ID: "mer-1", ProjectID: "mer", Kind: domain.KindWorker}
	svc := &Service{store: st}
	if _, err := svc.SetTerminateOnPRMerge(context.Background(), "mer-1", true); err == nil {
		t.Fatal("terminate-on-merge was armed for managed session")
	}
	if _, err := svc.SetTerminateOnPRMerge(context.Background(), "mer-1", false); err != nil {
		t.Fatal(err)
	}
}
