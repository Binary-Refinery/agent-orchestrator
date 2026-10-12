package review

import (
	"context"
	"errors"
	"time"
)

// ReviewerGovernance validates the fully resolved configuration at the actual
// reviewer launch boundary, including recovery and reuse of a live reviewer.
type ReviewerGovernance interface {
	ValidateReviewerLaunch(context.Context, LaunchSpec) error
	RecordReviewerLaunch(context.Context, LaunchSpec) error
}

// SetInstructorGovernance wires optional managed-work validation before boot
// recovery. Unmanaged review launchers retain their existing behavior.
func (e *Engine) SetInstructorGovernance(governance ReviewerGovernance) {
	e.governance = governance
	if launcher, ok := e.launcher.(interface{ SetInstructorGovernance(ReviewerGovernance) }); ok {
		launcher.SetInstructorGovernance(governance)
	}
}

func (l *agentLauncher) SetInstructorGovernance(governance ReviewerGovernance) {
	l.governance = governance
}

func (l *agentLauncher) validateInstructorLaunch(ctx context.Context, spec LaunchSpec) error {
	if l.governance == nil {
		return nil
	}
	return l.governance.ValidateReviewerLaunch(ctx, spec)
}

func (l *agentLauncher) recordInstructorLaunch(ctx context.Context, spec LaunchSpec) error {
	if l.governance == nil {
		return nil
	}
	return l.governance.RecordReviewerLaunch(ctx, spec)
}

func (l *agentLauncher) recordInstructorLaunchOrStop(ctx context.Context, spec LaunchSpec, handleID string) error {
	if err := l.recordInstructorLaunch(ctx, spec); err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		return errors.Join(err, l.Destroy(cleanupCtx, handleID))
	}
	return nil
}

func (l *agentLauncher) validateInstructorReuse(ctx context.Context, spec LaunchSpec) error {
	if guard, ok := l.governance.(interface {
		ValidateReviewerReuse(context.Context, LaunchSpec) error
	}); ok {
		return guard.ValidateReviewerReuse(ctx, spec)
	}
	return nil
}
