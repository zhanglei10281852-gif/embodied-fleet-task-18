package store

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func TestStore_ConcurrentQuotaReserve_RaceCondition(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	q, err := st.GetOrCreateQuota(ctx, domain.QuotaTypeFastCharge, "2026-03-01", 100)
	if err != nil {
		t.Fatalf("create quota: %v", err)
	}

	var wg sync.WaitGroup
	var successCount int32
	var failCount int32
	workers := 20

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each worker re-reads the quota to get the latest version, then tries to reserve.
			quota, err := st.GetQuota(ctx, q.ID)
			if err != nil {
				atomic.AddInt32(&failCount, 1)
				return
			}
			affected, err := st.ReserveQuota(ctx, q.ID, 10, quota.Version)
			if err != nil || affected == 0 {
				atomic.AddInt32(&failCount, 1)
				return
			}
			atomic.AddInt32(&successCount, 1)
		}()
	}
	wg.Wait()

	if successCount < 1 || successCount > 10 {
		t.Errorf("expected 1-10 successful reservations, got %d", successCount)
	}

	final, _ := st.GetQuota(ctx, q.ID)
	if final.ReservedAmount > 100 {
		t.Errorf("reserved %d exceeds limit 100", final.ReservedAmount)
	}
}

func TestStore_ParallelMissionCreate(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	var wg sync.WaitGroup
	workers := 10
	perWorker := 5

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				id := fmt.Sprintf("concurrent-%d-%d", workerID, j)
				now := time.Now().UTC()
				d := &domain.MissionRequest{
					ID: id, RobotName: "Robot", RobotSerial: "IMO", FleetCode: "V",
					PlannedStartAt: now.Add(24 * time.Hour), MissionKind: "transport", EstimatedEnergyUnits: 1,
					ChargeMode: "standard", RequestedBy: "agent", RequestingParty: domain.PartyOperationsDispatch,
					Status: domain.DeclStatusSubmitted, Priority: 5, Version: 1,
					CreatedAt: now, UpdatedAt: now, IdempotencyKey: "key-" + id,
				}
				if err := st.CreateMission(context.Background(), d); err != nil {
					t.Errorf("worker %d create: %v", workerID, err)
				}
			}
		}(i)
	}
	wg.Wait()

	result, err := st.ListMissions(context.Background(), domain.DefaultPage())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	expected := workers * perWorker
	if result.Total != expected {
		t.Errorf("expected %d missions, got %d", expected, result.Total)
	}
}

func TestStore_ConcurrentOptimisticLockUpdate(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	d := mustCreateMission(t, st, "concurrent-lock-1")

	var wg sync.WaitGroup
	var successCount int32
	workers := 10

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			affected, err := st.UpdateMissionStatus(ctx, d.ID, domain.DeclStatusReviewing, 1)
			if err != nil {
				return
			}
			if affected > 0 {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}
	wg.Wait()

	if successCount != 1 {
		t.Errorf("expected exactly 1 successful update, got %d", successCount)
	}
}

func TestStore_RaceIdempotencyInsert(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	var wg sync.WaitGroup
	workers := 5
	now := time.Now().UTC()
	var successCount int32

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := &domain.IdempotencyRecord{
				Key: "race-key-1", ResponseBody: "{}",
				ResponseStatus: 200, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
			}
			if err := st.InsertIdempotency(ctx, rec); err == nil {
				atomic.AddInt32(&successCount, 1)
			}
		}()
	}
	wg.Wait()

	got, err := st.GetIdempotency(ctx, "race-key-1")
	if err != nil {
		t.Fatalf("get after race: %v", err)
	}
	if got.Key != "race-key-1" {
		t.Errorf("key mismatch: %s", got.Key)
	}
	// At least one should have succeeded.
	if successCount < 1 {
		t.Error("expected at least one successful insert")
	}
}
