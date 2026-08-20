package domain

import (
	"errors"
	"fmt"
	"testing"
)

// TestIsNotFound_SentinelAndTyped ensures the domain layer detects
// not-found conditions from its own sentinel and typed error without
// referencing any storage implementation.
func TestIsNotFound_SentinelAndTyped(t *testing.T) {
	if !IsNotFound(ErrNotFound) {
		t.Errorf("IsNotFound(ErrNotFound) = false, want true")
	}
	if !IsNotFound(NewNotFoundError("mission", "d-1")) {
		t.Errorf("IsNotFound(NewNotFoundError) = false, want true")
	}
}

// TestIsNotFound_WrappedChain ensures a NotFoundError buried inside a
// wrapped error chain is still detected, so upper layers can branch with
// errors.Is/errors.As semantics across package boundaries.
func TestIsNotFound_WrappedChain(t *testing.T) {
	base := NewNotFoundError("robot_assignment", "pt-9")
	wrapped := fmt.Errorf("claim failed: %w", base)
	if !IsNotFound(wrapped) {
		t.Errorf("IsNotFound(wrapped NotFoundError) = false, want true")
	}
}

// TestIsNotFound_RejectsUnrelatedAndNil guards against false positives.
func TestIsNotFound_RejectsUnrelatedAndNil(t *testing.T) {
	if IsNotFound(nil) {
		t.Errorf("IsNotFound(nil) = true, want false")
	}
	if IsNotFound(ErrConflict) {
		t.Errorf("IsNotFound(ErrConflict) = true, want false")
	}
	if IsNotFound(errors.New("boom")) {
		t.Errorf("IsNotFound(generic) = true, want false")
	}
}

// TestIsConflict_TypedAndWrapped covers the optimistic-lock error chain
// used on the conflict/retry path.
func TestIsConflict_TypedAndWrapped(t *testing.T) {
	if !IsConflict(ErrConflict) {
		t.Errorf("IsConflict(ErrConflict) = false, want true")
	}
	ce := NewConflictError("mission_run", "wo-1", 3)
	if !IsConflict(ce) {
		t.Errorf("IsConflict(ConflictError) = false, want true")
	}
	if !IsConflict(fmt.Errorf("update: %w", ce)) {
		t.Errorf("IsConflict(wrapped) = false, want true")
	}
	if IsConflict(ErrNotFound) {
		t.Errorf("IsConflict(ErrNotFound) = true, want false")
	}
}

// TestNotFoundError_UnwrapsToSentinel verifies the typed error participates
// in the standard errors.Is/errors.As contract.
func TestNotFoundError_UnwrapsToSentinel(t *testing.T) {
	nfe := NewNotFoundError("route_reservation", "bw-2")
	if !errors.Is(nfe, ErrNotFound) {
		t.Errorf("errors.Is(NotFoundError, ErrNotFound) = false, want true")
	}
	var target *NotFoundError
	if !errors.As(nfe, &target) {
		t.Errorf("errors.As(NotFoundError) = false, want true")
	}
	if target.Entity != "route_reservation" || target.ID != "bw-2" {
		t.Errorf("unexpected entity/id: %s/%s", target.Entity, target.ID)
	}
}

// TestConflictError_UnwrapsToSentinel verifies the conflict typed error
// unwraps to the ErrConflict sentinel for retry semantics.
func TestConflictError_UnwrapsToSentinel(t *testing.T) {
	ce := NewConflictError("quota", "q-1", 7)
	if !errors.Is(ce, ErrConflict) {
		t.Errorf("errors.Is(ConflictError, ErrConflict) = false, want true")
	}
	var target *ConflictError
	if !errors.As(ce, &target) {
		t.Errorf("errors.As(ConflictError) = false, want true")
	}
	if target.Entity != "quota" || target.ID != "q-1" || target.Version != 7 {
		t.Errorf("unexpected conflict fields: %+v", target)
	}
}
