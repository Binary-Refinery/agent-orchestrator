package domain

// InstructorWorkspaceSnapshot captures the admitted inserted session row and
// its repository roots in one storage snapshot. Never resolve these roots again
// by the reusable session display ID during a scoped workspace operation.
type InstructorWorkspaceSnapshot struct {
	Session   SessionRecord
	Worktrees []SessionWorktreeRecord
}
