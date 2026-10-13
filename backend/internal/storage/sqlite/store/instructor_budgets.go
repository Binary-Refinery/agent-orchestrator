package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// ReserveInstructorBudget checks the pinned policy and inserts its reservation
// in the same writer transaction. Independent services/processes cannot both
// admit the last slot. Lifetime rows are never deleted when a session disappears.
func (s *Store) ReserveInstructorBudget(ctx context.Context, work domain.InstructorWork, entry domain.InstructorBudgetEntry) (domain.InstructorBudgetEntry, error) {
	if entry.ID == "" || entry.WorkID != work.ID || entry.ResourceKey == "" || (entry.Kind != "session" && entry.Kind != "turn") {
		return entry, errors.New("invalid instructor budget reservation")
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return entry, err
	}
	defer s.writeMu.Unlock()
	err := s.inTxDB(ctx, "reserve instructor execution budget", func(_ *gen.Queries, db gen.DBTX) error {
		var err error
		entry, err = reserveInstructorBudgetEntry(ctx, db, work, entry)
		return err
	})
	return entry, err
}

func reserveInstructorBudgetEntry(ctx context.Context, db gen.DBTX, work domain.InstructorWork, entry domain.InstructorBudgetEntry) (domain.InstructorBudgetEntry, error) {
	if entry.SessionID != "" {
		instance := strings.TrimPrefix(strings.Split(entry.ResourceKey, "/")[0], "session:")
		var current string
		var inactive bool
		if err := db.QueryRowContext(ctx, `SELECT i.instance_id,CASE WHEN ?='' THEN (s.is_terminated=1 OR s.activity_state='exited') ELSE COALESCE((SELECT reviewer_activity_state='exited' FROM review WHERE id=?),0) END FROM instructor_session_instances i JOIN sessions s ON s.id=i.session_id WHERE i.session_id=?`, entry.ReviewID, entry.ReviewID, entry.SessionID).Scan(&current, &inactive); err != nil {
			return entry, err
		}
		if current == "" || instance != current {
			return entry, errors.New("budget session instance changed before admission")
		}
		entry.InactiveOnAdmission = inactive
	}
	var body string
	if err := db.QueryRowContext(ctx, `SELECT body FROM instructor_works WHERE id=? AND revision=?`, work.ID, work.Revision).Scan(&body); err != nil {
		return entry, err
	}
	var pinned domain.InstructorWork
	if err := json.Unmarshal([]byte(body), &pinned); err != nil {
		return entry, err
	}
	if pinned.Cancellation != nil || pinned.State == "done" || pinned.State == "cancelled" {
		return entry, errors.New("terminal or cancelling work cannot reserve execution")
	}
	if pinned.PolicyVersion != entry.PolicyVersion {
		return entry, errors.New("instructor budget policy changed before admission")
	}
	if err := reconcileInstructorSessionBudgets(ctx, db, work.ID, entry.UpdatedAt); err != nil {
		return entry, err
	}
	var old domain.InstructorBudgetEntry
	err := db.QueryRowContext(ctx, `SELECT body FROM instructor_budget_entries WHERE work_id=? AND kind=? AND resource_key=?`, work.ID, entry.Kind, entry.ResourceKey).Scan(&body)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return entry, err
	}
	if found {
		if err := json.Unmarshal([]byte(body), &old); err != nil {
			return entry, err
		}
		if old.SessionID != entry.SessionID || old.ReviewID != entry.ReviewID {
			return entry, errors.New("budget reservation identity changed")
		}
		entry.Delegated = old.Delegated
		entry.AdoptedSession = old.AdoptedSession
		entry.PriorSessionState = old.PriorSessionState
		if old.Kind == "session" && old.State == "finished" {
			entry.PriorSessionState = "finished"
		}
		if old.State != "cancelled" && (old.Kind != "session" || old.State != "finished") {
			entry = old
			return entry, nil
		}
	}
	usage, err := instructorBudgetUsage(ctx, db, work.ID)
	if err != nil {
		return entry, err
	}
	limits := pinned.Policy.Budgets
	if limits != nil {
		checks := []struct {
			name      string
			used, max int
		}{}
		if entry.Kind == "session" {
			checks = append(checks, struct {
				name      string
				used, max int
			}{"concurrent sessions", usage.ConcurrentSessions, limits.MaxConcurrentSessions})
			// Restoring an existing immutable session consumes capacity again,
			// but never erases or doubles its lifetime/delegation consumption.
			if !found || old.State == "cancelled" {
				checks = append(checks, struct {
					name      string
					used, max int
				}{"lifetime sessions", usage.Sessions, limits.MaxSessions})
				if entry.Delegated {
					checks = append(checks, struct {
						name      string
						used, max int
					}{"delegations", usage.Delegations, limits.MaxDelegations})
				}
			}
		} else {
			checks = append(checks, struct {
				name      string
				used, max int
			}{"turns", usage.Turns, limits.MaxTurns})
		}
		for _, check := range checks {
			if check.max > 0 && check.used >= check.max {
				return entry, fmt.Errorf("%w: %s (%d/%d)", domain.ErrInstructorBudgetExceeded, check.name, check.used, check.max)
			}
		}
	}
	entry.State = "reserved"
	if found {
		entry.ID, entry.CreatedAt = old.ID, old.CreatedAt
	}
	body, err = instructorJSON(entry)
	if err != nil {
		return entry, err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO instructor_budget_entries(id,work_id,kind,resource_key,session_id,review_id,delegated,state,body) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(work_id,kind,resource_key) DO UPDATE SET state=excluded.state,body=excluded.body`, entry.ID, work.ID, entry.Kind, entry.ResourceKey, entry.SessionID, entry.ReviewID, entry.Delegated, entry.State, body)
	return entry, err
}

// SetInstructorBudgetState may cancel only a reservation that has not crossed
// the native boundary. An uncertain/started effect always consumes lifetime.
func (s *Store) SetInstructorBudgetState(ctx context.Context, id, state string, at time.Time) error {
	if state != "started" && state != "finished" && state != "cancelled" && state != "uncertain" {
		return errors.New("invalid instructor budget state")
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "record instructor budget effect", func(_ *gen.Queries, db gen.DBTX) error {
		var body string
		if err := db.QueryRowContext(ctx, `SELECT body FROM instructor_budget_entries WHERE id=?`, id).Scan(&body); err != nil {
			return err
		}
		var entry domain.InstructorBudgetEntry
		if err := json.Unmarshal([]byte(body), &entry); err != nil {
			return err
		}
		if state == "cancelled" && entry.State != "reserved" {
			return errors.New("native budget effect cannot be cancelled without proof of non-admission")
		}
		if state == "uncertain" && (entry.State == "uncertain" || entry.State == "finished" || entry.State == "cancelled" || entry.Kind == "turn" && entry.State != "reserved") {
			return domain.ErrInstructorBudgetClaimed
		}
		if entry.State == "cancelled" || entry.State == "finished" {
			return nil
		}
		if entry.Kind == "session" && entry.State == "started" && state == "uncertain" {
			entry.PriorSessionState = "started"
		}
		if state == "cancelled" {
			if entry.PriorSessionState != "" {
				state = entry.PriorSessionState
			} else if entry.AdoptedSession {
				state = "reserved"
			}
		}
		if state != "uncertain" {
			entry.PriorSessionState = ""
		}
		entry.State, entry.UpdatedAt = state, at
		body, err := instructorJSON(entry)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `UPDATE instructor_budget_entries SET state=?,body=? WHERE id=?`, state, body, id)
		return err
	})
}

// CancelInstructorBudgetBeforeAdmission requires positive proof from the adapter
// that no native write occurred. Generic send errors cannot replenish budgets.
func (s *Store) CancelInstructorBudgetBeforeAdmission(ctx context.Context, id string, at time.Time) error {
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "cancel proven unadmitted budget effect", func(_ *gen.Queries, db gen.DBTX) error {
		var body string
		if err := db.QueryRowContext(ctx, `SELECT body FROM instructor_budget_entries WHERE id=?`, id).Scan(&body); err != nil {
			return err
		}
		var entry domain.InstructorBudgetEntry
		if err := json.Unmarshal([]byte(body), &entry); err != nil {
			return err
		}
		if entry.State == "cancelled" {
			return nil
		}
		if entry.State != "reserved" && entry.State != "uncertain" {
			return errors.New("admitted native budget effect cannot be refunded")
		}
		state := "cancelled"
		if entry.Kind == "session" {
			if entry.PriorSessionState != "" {
				state = entry.PriorSessionState
			} else if entry.AdoptedSession {
				state = "reserved"
			}
		}
		entry.PriorSessionState = ""
		entry.State, entry.UpdatedAt = state, at
		body, err := instructorJSON(entry)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `UPDATE instructor_budget_entries SET state=?,body=? WHERE id=?`, entry.State, body, id)
		return err
	})
}

func instructorBudgetUsage(ctx context.Context, db gen.DBTX, workID string) (domain.InstructorBudgetUsage, error) {
	var usage domain.InstructorBudgetUsage
	err := db.QueryRowContext(ctx, `SELECT COALESCE(SUM(kind='session' AND state IN ('reserved','started','uncertain')),0), COALESCE(SUM(kind='session' AND state!='cancelled'),0), COALESCE(SUM(kind='turn' AND state!='cancelled'),0), COALESCE(SUM(kind='session' AND delegated=1 AND state!='cancelled'),0) FROM instructor_budget_entries WHERE work_id=?`, workID).Scan(&usage.ConcurrentSessions, &usage.Sessions, &usage.Turns, &usage.Delegations)
	return usage, err
}

// InstructorBudgetUsage is a live projection; durable termination releases only
// capacity, never the total count. Missing/unknown runtime probes release nothing.
func (s *Store) InstructorBudgetUsage(ctx context.Context, workID string) (domain.InstructorBudgetUsage, error) {
	return instructorBudgetUsage(ctx, s.readDB, workID)
}

// FinishInstructorSessionBudgets releases capacity after a controller stop is
// positively established, including shared project orchestrators. It retains
// every lifetime row and cannot release another inserted session incarnation.
func (s *Store) FinishInstructorSessionBudgets(ctx context.Context, resourceKey string, at time.Time) error {
	if resourceKey == "" {
		return errors.New("session budget resource required")
	}
	if err := s.writeMu.LockContext(ctx); err != nil {
		return err
	}
	defer s.writeMu.Unlock()
	return s.inTxDB(ctx, "finish instructor session budgets", func(_ *gen.Queries, db gen.DBTX) error {
		entries, err := readInstructorBudgetEntries(ctx, db, `SELECT body FROM instructor_budget_entries WHERE kind='session' AND resource_key=? AND state IN ('reserved','started','uncertain')`, resourceKey)
		if err != nil {
			return err
		}
		return finishInstructorBudgetEntries(ctx, db, entries, at)
	})
}

// InstructorWorkForDispatchTarget charges a project orchestrator's directive to
// the work that requested it, even when that orchestrator serves several works.
// An arbitrary user-supplied client ID cannot charge a different target.
func (s *Store) InstructorWorkForDispatchTarget(ctx context.Context, dispatchID string, target domain.SessionID) (domain.InstructorWork, string, bool, error) {
	var body string
	var expectedTarget, expectedInstance, currentInstance string
	err := s.readDB.QueryRowContext(ctx, `SELECT w.body,COALESCE(json_extract(d.body,'$.targetSessionId'),''),COALESCE(json_extract(d.body,'$.targetSessionInstanceId'),''),COALESCE(i.instance_id,'') FROM instructor_works w JOIN instructor_dispatches d ON d.work_id=w.id LEFT JOIN instructor_session_instances i ON i.session_id=json_extract(d.body,'$.targetSessionId') WHERE d.id=?`, dispatchID).Scan(&body, &expectedTarget, &expectedInstance, &currentInstance)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.InstructorWork{}, "", false, nil
	}
	if err != nil {
		return domain.InstructorWork{}, "", false, err
	}
	if expectedTarget != string(target) || expectedInstance == "" || expectedInstance != currentInstance {
		return domain.InstructorWork{}, "", false, errors.New("instructor dispatch budget target instance does not match")
	}
	var work domain.InstructorWork
	err = json.Unmarshal([]byte(body), &work)
	return work, expectedInstance, err == nil, err
}

func reconcileInstructorSessionBudgets(ctx context.Context, db gen.DBTX, workID string, at time.Time) error {
	entries, err := readInstructorBudgetEntries(ctx, db, `SELECT b.body FROM instructor_budget_entries b JOIN sessions s ON s.id=b.session_id JOIN instructor_session_instances i ON i.session_id=s.id WHERE b.work_id=? AND b.kind='session' AND (b.state='started' OR (b.state='reserved' AND COALESCE(json_extract(b.body,'$.inactiveOnAdmission'),0)=0)) AND ((b.review_id='' AND b.resource_key='session:'||i.instance_id AND (s.is_terminated=1 OR s.activity_state='exited')) OR (b.review_id!='' AND EXISTS (SELECT 1 FROM review r WHERE r.id=b.review_id AND r.reviewer_activity_state='exited' AND b.resource_key LIKE 'session:'||i.instance_id||'/review:'||r.id||':%')))`, workID)
	if err != nil {
		return err
	}
	return finishInstructorBudgetEntries(ctx, db, entries, at)
}

func readInstructorBudgetEntries(ctx context.Context, db gen.DBTX, query string, args ...any) ([]domain.InstructorBudgetEntry, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	entries := []domain.InstructorBudgetEntry{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var entry domain.InstructorBudgetEntry
		if err := json.Unmarshal([]byte(body), &entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func finishInstructorBudgetEntries(ctx context.Context, db gen.DBTX, entries []domain.InstructorBudgetEntry, at time.Time) error {
	for _, entry := range entries {
		entry.State, entry.UpdatedAt, entry.PriorSessionState = "finished", at, ""
		body, err := instructorJSON(entry)
		if err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, `UPDATE instructor_budget_entries SET state='finished',body=? WHERE id=?`, body, entry.ID); err != nil {
			return err
		}
	}
	return nil
}
