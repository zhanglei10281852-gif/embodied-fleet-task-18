package store

import (
	"context"
	"errors"
	"testing"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

// TestStore_NotFoundTranslationFromStorage verifies the persistence layer
// translates storage-level "no rows" signals into domain NotFoundError
// values. The domain layer must detect not-found conditions via
// domain.IsNotFound without ever importing a storage implementation; this
// test pins that contract by checking the returned error is a domain type.
func TestStore_NotFoundTranslationFromStorage(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	// Missing idempotency key: scanMission encounters the storage
	// "no rows" signal and must surface it as a domain NotFoundError.
	_, err := st.GetMissionByIdempotencyKey(ctx, "does-not-exist")
	if err == nil {
		t.Fatalf("expected not-found error, got nil")
	}
	if !domain.IsNotFound(err) {
		t.Fatalf("expected domain.IsNotFound, got %v", err)
	}
	var nfe *domain.NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("expected *domain.NotFoundError in chain, got %T: %v", err, err)
	}
	if nfe.Entity != "mission" {
		t.Errorf("entity: expected mission, got %s", nfe.Entity)
	}
}

// TestStore_NotFoundEmptyKeyGuard pins the empty-key early-return guard to
// surface a domain error rather than a raw database/sql sentinel, keeping
// the storage driver detail out of caller-facing error chains.
func TestStore_NotFoundEmptyKeyGuard(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := st.GetMissionByIdempotencyKey(ctx, "")
	if err == nil {
		t.Fatalf("expected not-found error for empty key, got nil")
	}
	if !domain.IsNotFound(err) {
		t.Fatalf("empty key: expected domain.IsNotFound, got %v", err)
	}
	var nfe *domain.NotFoundError
	if !errors.As(err, &nfe) {
		t.Fatalf("empty key: expected *domain.NotFoundError, got %T: %v", err, err)
	}
}

// TestStore_NotFoundGetByID verifies the single-row Get path for
// missions also translates a missing row into a domain NotFoundError.
func TestStore_NotFoundGetByID(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	_, err := st.GetMission(ctx, "missing-id")
	if err == nil {
		t.Fatalf("expected not-found error, got nil")
	}
	if !domain.IsNotFound(err) {
		t.Fatalf("expected domain.IsNotFound, got %v", err)
	}
}
