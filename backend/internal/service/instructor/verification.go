package instructor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// EvidenceDiscovery enumerates trusted source candidates without certifying their claims.
type EvidenceDiscovery interface {
	Discover(context.Context, domain.InstructorWork) ([]domain.InstructorEvidence, error)
}

// VerificationSource fingerprints cheap persisted native facts for inspection coherence.
type VerificationSource interface {
	VerificationKey(context.Context, domain.InstructorWork) (string, error)
}
type verificationStore interface {
	GetInstructorVerification(context.Context, string) (domain.InstructorVerification, bool, error)
	ClaimInstructorVerification(context.Context, string, string, time.Time) (domain.InstructorVerification, bool, error)
	FinishInstructorVerification(context.Context, string, string, int, string, string, int, int, time.Time, bool) error
	AppendInstructorEvent(context.Context, domain.InstructorEvent) error
}

func (s *Service) verificationKey(ctx context.Context, w domain.InstructorWork) (string, error) {
	source := ""
	if reader, ok := s.deps.Verifier.(VerificationSource); ok {
		var err error
		source, err = reader.VerificationKey(ctx, w)
		if err != nil {
			return "", err
		}
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%s\x00%s", w.ID, w.SHA, w.PolicyVersion, w.PhaseSequence, s.now().UTC().Format("2006-01-02"), source)))
	return hex.EncodeToString(digest[:]), nil
}

// CurrentEvidence cheaply checks that cached proof was inspected against the
// current persisted review and CI facts. Stage constraints determine which
// verified facts are required; an unrelated rejected candidate proves nothing.
func (s *Service) CurrentEvidence(ctx context.Context, w domain.InstructorWork) error {
	if !fullSHA(w.SHA) {
		return nil
	}
	if _, supported := s.deps.Verifier.(VerificationSource); !supported {
		return nil
	}
	store, ok := s.deps.Store.(verificationStore)
	if !ok {
		return fmt.Errorf("%w: verification cache unavailable", ErrEvidencePending)
	}
	key, err := s.verificationKey(ctx, w)
	if err != nil {
		return err
	}
	cache, found, err := store.GetInstructorVerification(ctx, w.ID)
	if err != nil {
		return err
	}
	if !found || cache.Key != key || cache.Passes == 0 || !cache.Inspected || cache.Status == "running" || cache.Status == "" {
		return ErrEvidencePending
	}
	return nil
}

// DecorateVerification projects the durable inspection cache into a portfolio row.
func (s *Service) DecorateVerification(ctx context.Context, w *domain.InstructorWork) error {
	store, ok := s.deps.Store.(verificationStore)
	if !ok {
		return nil
	}
	v, found, err := store.GetInstructorVerification(ctx, w.ID)
	if err != nil {
		return err
	}
	if found {
		w.Verification = &v
	}
	return nil
}

func (s *Service) scheduleVerification(ctx context.Context, w domain.InstructorWork) {
	if !fullSHA(w.SHA) || w.State == "cancelled" || w.State == "parked" {
		return
	}
	if _, ok := s.deps.Verifier.(EvidenceDiscovery); !ok {
		return
	}
	s.verificationMu.Lock()
	defer s.verificationMu.Unlock()
	if s.verificationStopped || s.verificationRunning[w.ID] {
		return
	}
	select {
	case s.verificationSlots <- struct{}{}:
	default:
		return
	}
	s.verificationRunning[w.ID] = true
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	s.verificationCancels[w.ID] = cancel
	s.verificationWG.Add(1)
	go func() {
		defer s.verificationWG.Done()
		defer func() {
			s.verificationMu.Lock()
			delete(s.verificationRunning, w.ID)
			delete(s.verificationCancels, w.ID)
			s.verificationMu.Unlock()
			<-s.verificationSlots
		}()
		defer cancel()
		_ = s.VerifyWork(runCtx, w.ID)
	}()
}

// Shutdown stops scheduling, cancels inspections and joins their final bounded
// persistence writes before the daemon closes SQLite.
func (s *Service) Shutdown(ctx context.Context) error {
	s.verificationMu.Lock()
	s.verificationStopped = true
	cancels := make([]context.CancelFunc, 0, len(s.verificationCancels))
	for _, cancel := range s.verificationCancels {
		cancels = append(cancels, cancel)
	}
	s.verificationMu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	done := make(chan struct{})
	go func() { s.verificationWG.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// VerifyWork performs at most two deep passes for one immutable source key and
// day. The daily/change cache is persistent and cannot be reset by a heartbeat.
func (s *Service) VerifyWork(ctx context.Context, id string) error {
	discovery, ok := s.deps.Verifier.(EvidenceDiscovery)
	if !ok {
		return invalid("trusted evidence discovery unavailable")
	}
	store, ok := s.deps.Store.(verificationStore)
	if !ok {
		return invalid("verification cache storage unavailable")
	}
	w, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	if !fullSHA(w.SHA) {
		return nil
	}
	key, err := s.verificationKey(ctx, w)
	if err != nil {
		return err
	}
	claim, claimed, err := store.ClaimInstructorVerification(ctx, w.ID, key, s.now())
	if err != nil || !claimed {
		return err
	}
	candidates, discoveryErr := discovery.Discover(ctx, w)
	inspected := discoveryErr == nil
	if discoveryErr == nil {
		candidates = revalidationCandidates(w, candidates)
		if err := s.fenceUnauthorizedPublication(ctx, w); err != nil {
			discoveryErr = err
			inspected = false
		}
	}
	verified := 0
	errors := []string{}
	if discoveryErr != nil {
		errors = append(errors, discoveryErr.Error())
	}
	for _, candidate := range candidates {
		current, verifyErr := s.VerifyEvidence(ctx, w.ID, candidate)
		if verifyErr != nil {
			errors = append(errors, candidate.Kind+": "+verifyErr.Error())
			if revokeErr := s.revokeEvidence(ctx, w.ID, candidate, verifyErr.Error()); revokeErr != nil {
				errors = append(errors, revokeErr.Error())
				inspected = false
			}
			continue
		}
		w = current
		verified++
	}
	status := "complete"
	if len(errors) > 0 {
		status = "failed"
	} else if len(candidates) == 0 {
		status = "waiting"
	}
	detail := strings.Join(errors, "; ")
	if len(detail) > 4096 {
		detail = detail[:4096]
	}
	// Persist the bounded attempt even if the invoking request was cancelled.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	inspected = inspected && ctx.Err() == nil
	if err := store.FinishInstructorVerification(finishCtx, id, key, claim.Passes, status, detail, len(candidates), verified, s.now(), inspected); err != nil {
		return err
	}
	kind := "verification_completed"
	if status == "failed" {
		kind = "verification_failed"
	}
	return store.AppendInstructorEvent(finishCtx, s.event(w, kind, fmt.Sprintf("key=%s claims=%d verified=%d %s", key, len(candidates), verified, detail)))
}

// Discovery can omit sources that disappeared or no longer satisfy its candidate
// filter. Recheck their previously accepted immutable bindings rather than
// treating an empty discovery result as proof that the old facts remain valid.
func revalidationCandidates(w domain.InstructorWork, candidates []domain.InstructorEvidence) []domain.InstructorEvidence {
	for _, prior := range w.Evidence {
		if !prior.Verified || prior.Revoked || prior.WorkID != w.ID || prior.PolicyVersion != w.PolicyVersion || prior.SHA != w.SHA || prior.Kind == "check" || strings.HasPrefix(prior.Ref, "gate:") {
			continue
		}
		present := false
		for _, candidate := range candidates {
			if candidate.WorkID == prior.WorkID && candidate.PolicyVersion == prior.PolicyVersion && candidate.SHA == prior.SHA && candidate.Kind == prior.Kind && candidate.Ref == prior.Ref && candidate.Producer == prior.Producer && candidate.Role == prior.Role && candidate.Harness == prior.Harness && candidate.Model == prior.Model && candidate.Effort == prior.Effort {
				present = true
				break
			}
		}
		if !present {
			candidates = append(candidates, prior)
		}
	}
	return candidates
}

func (s *Service) revokeEvidence(ctx context.Context, id string, candidate domain.InstructorEvidence, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	changed := false
	for i := range w.Evidence {
		fact := &w.Evidence[i]
		if fact.Verified && !fact.Revoked && fact.WorkID == candidate.WorkID && fact.PolicyVersion == candidate.PolicyVersion && fact.SHA == candidate.SHA && fact.Kind == candidate.Kind && fact.Ref == candidate.Ref && fact.Producer == candidate.Producer && fact.Role == candidate.Role && fact.Harness == candidate.Harness && fact.Model == candidate.Model && fact.Effort == candidate.Effort {
			fact.Revoked, fact.RevocationReason, changed = true, reason, true
		}
	}
	if !changed {
		return nil
	}
	_, err = s.save(ctx, w, w.Revision, "evidence_revoked", candidate.Kind+":"+candidate.Ref+": "+reason)
	return err
}
