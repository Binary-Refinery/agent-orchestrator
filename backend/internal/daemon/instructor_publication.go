package daemon

import (
	"context"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	instructorsvc "github.com/aoagents/agent-orchestrator/backend/internal/service/instructor"
)

// hostPublicationBoundary describes the currently implemented tmux/conpty and
// native Chat hosts. They all share the operator's OS user. The loopback API
// has no protected human channel and SCM credentials are host-accessible.
// Neither CORS, an AO_ROLE variable, nor a Docker cleanup label proves isolation.
// Until an enforced isolated adapter replaces this boundary, publication is
// unavailable rather than silently falling back to a prompt or ordinary SCM.
type hostPublicationBoundary struct{}

func (hostPublicationBoundary) Capabilities(context.Context, domain.InstructorWork) (domain.InstructorPublicationCapabilities, error) {
	return domain.InstructorPublicationCapabilities{Reason: "Native workers and the daemon share the host OS user. Protected operator authorization, isolated workers and isolated publisher credentials are not available in this runtime."}, nil
}

func (hostPublicationBoundary) Authorize(context.Context, instructorsvc.PublicationDecision) (instructorsvc.PublicationAuthorization, error) {
	return instructorsvc.PublicationAuthorization{}, instructorsvc.ErrPublicationUnavailable
}

func (hostPublicationBoundary) Execute(context.Context, instructorsvc.PublicationRequest) (instructorsvc.PublicationReceipt, error) {
	return instructorsvc.PublicationReceipt{}, instructorsvc.ErrPublicationUnavailable
}
