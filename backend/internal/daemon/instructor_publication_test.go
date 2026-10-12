package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

func TestInstructorProductionHostNeverCertifiesIsolationFromFlags(t *testing.T) {
	t.Setenv("AO_PUBLICATION_ISOLATED", "1")
	t.Setenv("AO_OPERATOR_AUTHENTICATED", "1")
	t.Setenv("AO_PUBLISHER_CREDENTIALS_ISOLATED", "1")
	store := instructorTestStore{works: []domain.InstructorWork{{ID: "work", ProjectID: "project"}}}
	svc, _, _ := newInstructorRuntime(store, nil, nil)
	c := svc.PublicationCapabilities(context.Background())
	if c.Available || c.WorkerIsolation || c.ProtectedOperator || c.IsolatedCredentials || c.Reason == "" {
		t.Fatalf("host runtime misrepresented enforcement: %+v", c)
	}
	if _, err := svc.ExecutePublication(context.Background(), instructorsvc.PublicationInput{WorkID: "work", GrantID: "forged-client-grant"}); !errors.Is(err, instructorsvc.ErrPublicationUnavailable) {
		t.Fatalf("production executor accepted a client grant without isolation: %v", err)
	}
}
