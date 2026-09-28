package lifecycle

import (
	"context"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"testing"
)

func TestGovernanceManagedSuppressesAllNativeNudgesWithoutAcknowledgement(t *testing.T) {
	m, st, msg := newManager()
	st.sessions["mer-1"] = working("mer-1")
	st.projects["mer"] = domain.ProjectRecord{ID: "mer", Config: domain.ProjectConfig{GovernanceManaged: true}}
	for _, urgent := range []bool{false, true} {
		out, err := m.sendOnce(context.Background(), "mer-1", "https://example.test/pr/1", "native", "sig", "prompt", 1, urgent)
		if err != nil || out != sendOnceSuppressed {
			t.Fatalf("native nudge not suppressed: %v, %v", out, err)
		}
	}
	if len(msg.msgs) != 0 || st.signatureWrites != 0 {
		t.Fatal("suppressed nudge was sent or acknowledged")
	}
}
