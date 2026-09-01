package custody

import (
	"context"
	"fmt"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/git"
)

// RecoveryRef keeps a terminal run's unpublished pipeline head reachable in
// the local gate until custody is explicitly returned.
func RecoveryRef(runID string) string {
	return "refs/no-mistakes/recover/" + runID
}

type RecoveryHeadState string

const (
	RecoveryHeadAbsent     RecoveryHeadState = "absent"
	RecoveryHeadExact      RecoveryHeadState = "exact_commit"
	RecoveryHeadSymbolic   RecoveryHeadState = "symbolic"
	RecoveryHeadNonCommit  RecoveryHeadState = "non_commit"
	RecoveryHeadMismatched RecoveryHeadState = "mismatched_commit"
)

type RecoveryHeadInspection struct {
	State  RecoveryHeadState
	Target string
}

func (i RecoveryHeadInspection) CompatibleForPreserve() bool {
	return i.State == RecoveryHeadAbsent || i.State == RecoveryHeadExact
}

// InspectRecoveryHead is the authoritative classifier for a run's exact
// preserved-head ref. It never follows symbolic refs or accepts tag peeling.
func InspectRecoveryHead(ctx context.Context, dir, runID, expected string) (RecoveryHeadInspection, error) {
	ref := RecoveryRef(runID)
	if target, err := git.Run(ctx, dir, "symbolic-ref", "-q", ref); err == nil {
		return RecoveryHeadInspection{State: RecoveryHeadSymbolic, Target: target}, nil
	}
	target, exists, err := git.ExactRefTarget(ctx, dir, ref)
	if err != nil {
		return RecoveryHeadInspection{}, err
	}
	if !exists {
		return RecoveryHeadInspection{State: RecoveryHeadAbsent}, nil
	}
	typeName, err := git.Run(ctx, dir, "cat-file", "-t", target)
	if err != nil {
		return RecoveryHeadInspection{}, err
	}
	if typeName != "commit" {
		return RecoveryHeadInspection{State: RecoveryHeadNonCommit, Target: target}, nil
	}
	if target != strings.TrimSpace(expected) {
		return RecoveryHeadInspection{State: RecoveryHeadMismatched, Target: target}, nil
	}
	return RecoveryHeadInspection{State: RecoveryHeadExact, Target: target}, nil
}

// PreserveRecoveryHead creates a run-specific recovery anchor without ever
// replacing existing evidence. A matching commit is idempotent; a conflicting
// or non-commit ref fails closed so reconciliation can inspect the original
// object.
func PreserveRecoveryHead(ctx context.Context, dir, runID, head string) error {
	ref := RecoveryRef(runID)
	inspection, err := InspectRecoveryHead(ctx, dir, runID, head)
	if err != nil {
		return fmt.Errorf("inspect recovery anchor %s: %w", ref, err)
	}
	if inspection.State == RecoveryHeadExact {
		return nil
	}
	if inspection.State == RecoveryHeadAbsent {
		if _, err := git.Run(ctx, dir, "update-ref", "--no-deref", ref, head, strings.Repeat("0", len(head))); err == nil {
			return nil
		}
		inspection, err = InspectRecoveryHead(ctx, dir, runID, head)
		if err != nil {
			return fmt.Errorf("inspect recovery anchor %s after create failed: %w", ref, err)
		}
		if inspection.State == RecoveryHeadExact {
			return nil
		}
	}
	switch inspection.State {
	case RecoveryHeadSymbolic:
		return fmt.Errorf("recovery anchor %s is symbolic to %s instead of the verified commit %s", ref, inspection.Target, head)
	case RecoveryHeadNonCommit:
		return fmt.Errorf("recovery anchor %s points at non-commit object %s instead of the verified commit %s", ref, inspection.Target, head)
	case RecoveryHeadMismatched:
		return fmt.Errorf("recovery anchor %s conflicts: existing commit %s, verified commit %s", ref, inspection.Target, head)
	default:
		return fmt.Errorf("recovery anchor %s could not be created for verified commit %s", ref, head)
	}
}

func PreserveRecoveryAnchor(ctx context.Context, dir, ref, head string) error {
	if symbolic, err := git.Run(ctx, dir, "symbolic-ref", "-q", ref); err == nil {
		return fmt.Errorf("recovery anchor %s is symbolic to %s instead of the verified commit %s", ref, symbolic, head)
	}
	if _, err := git.Run(ctx, dir, "update-ref", "--no-deref", ref, head, strings.Repeat("0", len(head))); err == nil {
		return nil
	}
	if symbolic, err := git.Run(ctx, dir, "symbolic-ref", "-q", ref); err == nil {
		return fmt.Errorf("recovery anchor %s is symbolic to %s instead of the verified commit %s", ref, symbolic, head)
	}
	existing, err := git.Run(ctx, dir, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return fmt.Errorf("recovery anchor %s exists but is not the verified commit %s: %w", ref, head, err)
	}
	if existing != strings.TrimSpace(head) {
		return fmt.Errorf("recovery anchor %s conflicts: existing commit %s, verified commit %s", ref, existing, head)
	}
	return nil
}

// RecoveryLocalRef keeps the operator's pre-recovery head reachable when a
// guarded recovery adopts an equivalent rewritten pipeline head.
func RecoveryLocalRef(runID string) string {
	return "refs/no-mistakes/recover-local/" + runID
}

// RecoveryGateRef keeps an independently moved gate head reachable before a
// keep-local recovery changes the gate branch.
func RecoveryGateRef(runID string) string {
	return "refs/no-mistakes/recover-gate/" + runID
}
