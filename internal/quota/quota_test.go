package quota

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

func setupTest(t *testing.T) (*Service, *store.SQLiteStore, *apperr.FakeClock, func()) {
	t.Helper()
	db, err := store.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(findMigrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewSQLiteStore(db)
	clock := apperr.NewFake(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC))
	svc := New(Deps{
		Quotas: st, Audit: audit.New(st, clock), Clock: clock, Logger: apperr.NopLogger(),
		FastChargeLimit: 1000, StandardChargeLimit: 5000,
	})
	return svc, st, clock, func() { st.Close() }
}

func TestQuota_ReserveSuccess(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	result, err := svc.Reserve(context.Background(), ReserveRequest{
		QuotaType: domain.QuotaTypeFastCharge, Amount: 100, Actor: "agent",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if result.Rejected {
		t.Error("expected not rejected")
	}
	if result.Available != 900 {
		t.Errorf("expected 900 available, got %d", result.Available)
	}
}

func TestQuota_ReserveExceededReject(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	result, err := svc.Reserve(context.Background(), ReserveRequest{
		QuotaType: domain.QuotaTypeFastCharge, Amount: 1001, Actor: "agent",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if !result.Rejected {
		t.Error("expected rejected")
	}
	if result.Status != "exhausted" {
		t.Errorf("expected exhausted, got %s", result.Status)
	}
}

func TestQuota_CommitAndRelease(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	res, _ := svc.Reserve(context.Background(), ReserveRequest{
		QuotaType: domain.QuotaTypeStandardCharge, Amount: 200, Actor: "agent",
	})

	q, _ := svc.Get(context.Background(), res.QuotaID)
	err := svc.Commit(context.Background(), res.QuotaID, 150, q.Version, "agent", "r")
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	committed, _ := svc.Get(context.Background(), res.QuotaID)
	if committed.UsedAmount != 150 {
		t.Errorf("expected 150 used, got %d", committed.UsedAmount)
	}
	if committed.ReservedAmount != 50 {
		t.Errorf("expected 50 reserved, got %d", committed.ReservedAmount)
	}

	// Release the remaining 50.
	err = svc.Release(context.Background(), res.QuotaID, 50, committed.Version, "agent", "r")
	if err != nil {
		t.Fatalf("release: %v", err)
	}

	released, _ := svc.Get(context.Background(), res.QuotaID)
	if released.ReservedAmount != 0 {
		t.Errorf("expected 0 reserved after release, got %d", released.ReservedAmount)
	}
}

func TestQuota_ConcurrentReserveRace(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	var wg sync.WaitGroup
	var success int32
	var rejected int32
	workers := 20

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := svc.Reserve(context.Background(), ReserveRequest{
				QuotaType: domain.QuotaTypeFastCharge, Amount: 100, Actor: "agent",
			})
			if err != nil {
				return
			}
			if res.Rejected {
				atomic.AddInt32(&rejected, 1)
			} else {
				atomic.AddInt32(&success, 1)
			}
		}()
	}
	wg.Wait()

	if success != 10 {
		t.Errorf("expected exactly 10 successful, got %d", success)
	}
	if rejected != 10 {
		t.Errorf("expected 10 rejected, got %d", rejected)
	}
}

func TestQuota_ConcurrentCommitTransaction(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	res, _ := svc.Reserve(context.Background(), ReserveRequest{
		QuotaType: domain.QuotaTypeFastCharge, Amount: 100, Actor: "agent",
	})

	var wg sync.WaitGroup
	var success int32
	workers := 5
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q, err := svc.Get(context.Background(), res.QuotaID)
			if err != nil {
				return
			}
			err = svc.Commit(context.Background(), res.QuotaID, 20, q.Version, "agent", "r")
			if err == nil {
				atomic.AddInt32(&success, 1)
			}
		}()
	}
	wg.Wait()

	if success != 5 {
		t.Errorf("expected 5 successful commits, got %d", success)
	}
	final, _ := svc.Get(context.Background(), res.QuotaID)
	if final.UsedAmount != 100 {
		t.Errorf("expected 100 used, got %d", final.UsedAmount)
	}
	if final.ReservedAmount != 0 {
		t.Errorf("expected 0 reserved, got %d", final.ReservedAmount)
	}
}

func TestQuota_ConflictOnStaleVersion(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()

	res, _ := svc.Reserve(context.Background(), ReserveRequest{
		QuotaType: domain.QuotaTypeFastCharge, Amount: 100, Actor: "agent",
	})

	// Test store-level optimistic lock conflict with stale version.
	affected, err := st.CommitQuota(context.Background(), res.QuotaID, 50, 999)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if affected != 0 {
		t.Errorf("expected 0 affected rows for stale version, got %d", affected)
	}
}

func TestQuota_ListPagination(t *testing.T) {
	svc, _, clock, cleanup := setupTest(t)
	defer cleanup()

	// Create quotas for different days.
	for i := 0; i < 3; i++ {
		clock.Advance(24 * time.Hour)
		svc.Reserve(context.Background(), ReserveRequest{
			QuotaType: domain.QuotaTypeFastCharge, Amount: 10, Actor: "a",
		})
	}

	q := domain.DefaultPage()
	q.PageSize = 2
	result, err := svc.List(context.Background(), q)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 items, got %d", len(result.Items))
	}
	if result.Total != 3 {
		t.Errorf("expected total 3, got %d", result.Total)
	}
}

func findMigrations() string {
	for _, c := range []string{"./migrations", "../migrations", "../../migrations"} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "./migrations"
}

// flakyQuotaRepo wraps a QuotaRepo and forces the first ReserveQuota call to
// report zero affected rows, simulating a stale-version optimistic-lock conflict
// so the service's retry loop can be exercised deterministically.
type flakyQuotaRepo struct {
	store.QuotaRepo
	calls int32
}

func (f *flakyQuotaRepo) ReserveQuota(ctx context.Context, id string, amount, version int) (int, error) {
	if atomic.AddInt32(&f.calls, 1) == 1 {
		return 0, nil // conflict: version bumped by a concurrent writer
	}
	return f.QuotaRepo.ReserveQuota(ctx, id, amount, version)
}

// TestQuota_RetryOnOptimisticLockConflict verifies that Reserve retries after a
// stale-version conflict (affected=0) and ultimately succeeds.
func TestQuota_RetryOnOptimisticLockConflict(t *testing.T) {
	db, err := store.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(findMigrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewSQLiteStore(db)
	defer st.Close()
	clock := apperr.NewFake(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC))

	wrapped := &flakyQuotaRepo{QuotaRepo: st}
	svc := New(Deps{
		Quotas: wrapped, Audit: audit.New(st, clock), Clock: clock,
		Logger: apperr.NopLogger(), FastChargeLimit: 1000, StandardChargeLimit: 5000,
	})

	result, err := svc.Reserve(context.Background(), ReserveRequest{
		QuotaType: domain.QuotaTypeFastCharge, Amount: 100, Actor: "agent",
	})
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if result.Rejected {
		t.Fatal("expected retry to succeed, got rejected")
	}
	if result.Available != 900 {
		t.Errorf("expected 900 available after retry, got %d", result.Available)
	}
	if result.Reserved != 100 {
		t.Errorf("expected 100 reserved, got %d", result.Reserved)
	}
	if atomic.LoadInt32(&wrapped.calls) < 2 {
		t.Errorf("expected at least 2 reserve attempts (retry), got %d", wrapped.calls)
	}
}

func TestQuota_StorageFailureIsReturnedToCaller(t *testing.T) {
	db, err := store.OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(findMigrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewSQLiteStore(db)
	clock := apperr.NewFake(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC))
	svc := New(Deps{
		Quotas: st, Audit: audit.New(st, clock), Clock: clock,
		Logger: apperr.NopLogger(), FastChargeLimit: 1000, StandardChargeLimit: 5000,
	})
	if err := st.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	result, err := svc.Reserve(context.Background(), ReserveRequest{
		QuotaType: domain.QuotaTypeFastCharge, Amount: 10, Actor: "agent",
	})
	if err == nil {
		t.Fatalf("expected storage error, got result %+v", result)
	}
	if result != nil {
		t.Fatalf("expected no business result on storage failure, got %+v", result)
	}
}
