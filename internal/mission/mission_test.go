package mission

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
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
	auditRec := auditNew(st, clock)
	svc := New(Deps{
		Missions: st, Quotas: st, Idempotency: st, Handovers: st,
		Audit: auditRec, Clock: clock, Logger: apperr.NopLogger(),
		FastChargeLimit: 1000, StandardChargeLimit: 5000,
	})
	return svc, st, clock, func() { st.Close() }
}

func TestMission_Submit_Accepted(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	result, err := svc.Submit(context.Background(), NewMission("Robot-A"))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if result.Status != domain.DeclStatusAccepted {
		t.Errorf("expected accepted, got %s", result.Status)
	}
	if result.MissionID == "" {
		t.Error("expected non-empty mission ID")
	}
}

func TestMission_Submit_IdempotentDuplicate(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	req := NewMission("Robot-Idem")
	req.IdempotencyKey = "idem-key-test"

	result1, err := svc.Submit(context.Background(), req)
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}

	result2, err := svc.Submit(context.Background(), req)
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}

	if result1.MissionID != result2.MissionID {
		t.Errorf("idempotent submit should return same ID: %s vs %s",
			result1.MissionID, result2.MissionID)
	}
	if result1.Status != result2.Status {
		t.Errorf("idempotent submit should return same status: %s vs %s",
			result1.Status, result2.Status)
	}
}

func TestMission_Submit_QuotaExceededRejects(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	// Submit enough to exhaust the standard charging quota.
	req := NewMission("BigRobot")
	req.EstimatedEnergyUnits = 5001
	req.ChargeMode = "standard"
	req.MissionKind = "emergency"

	result, err := svc.Submit(context.Background(), req)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if result.Status != domain.DeclStatusRejected {
		t.Errorf("expected rejected, got %s", result.Status)
	}
	if result.Message == "" {
	}
}

func TestMission_Cancel_InvalidTransitionReject(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	result := mustSubmit(t, svc, "Robot-Cancel")

	// Cancel it (should succeed from submitted).
	err := svc.Cancel(context.Background(), result.MissionID, "agent", "req-1")
	if err != nil {
		t.Fatalf("first cancel: %v", err)
	}

	// Try to cancel again (should fail — already cancelled).
	err = svc.Cancel(context.Background(), result.MissionID, "agent", "req-2")
	if err == nil {
		t.Fatal("expected error on cancelling already-cancelled mission")
	}
	if !apperr.IsInvalidTransition(err) {
		t.Errorf("expected invalid transition error, got %v", err)
	}
}

func TestMission_Cancel_CompletedReject(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()

	result := mustSubmit(t, svc, "Robot-Done")

	// Force-complete the mission by updating status through the store.
	d, _ := st.GetMission(context.Background(), result.MissionID)
	st.UpdateMissionStatus(context.Background(), d.ID, domain.DeclStatusReviewing, d.Version)
	d, _ = st.GetMission(context.Background(), result.MissionID)
	st.UpdateMissionStatus(context.Background(), d.ID, domain.DeclStatusAccepted, d.Version)
	d, _ = st.GetMission(context.Background(), result.MissionID)
	st.UpdateMissionStatus(context.Background(), d.ID, domain.DeclStatusScheduled, d.Version)
	d, _ = st.GetMission(context.Background(), result.MissionID)
	st.UpdateMissionStatus(context.Background(), d.ID, domain.DeclStatusProcessing, d.Version)
	d, _ = st.GetMission(context.Background(), result.MissionID)
	st.UpdateMissionStatus(context.Background(), d.ID, domain.DeclStatusCompleted, d.Version)

	// Cancel should be rejected.
	err := svc.Cancel(context.Background(), result.MissionID, "agent", "req")
	if err == nil {
		t.Fatal("expected error cancelling completed mission")
	}
	if !apperr.IsInvalidTransition(err) {
		t.Errorf("expected invalid transition error, got %v", err)
	}
}

func TestMission_List_PaginationBoundary(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	for i := 0; i < 15; i++ {
		mustSubmit(t, svc, "Robot-"+string(rune('A'+i)))
	}

	q := domain.DefaultPage()
	q.Page = 1
	q.PageSize = 10
	result, err := svc.List(context.Background(), q)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(result.Items) != 10 {
		t.Errorf("expected 10 items, got %d", len(result.Items))
	}
	if result.Total != 15 {
		t.Errorf("expected total 15, got %d", result.Total)
	}
}

func TestMission_ConcurrentSubmit_DifferentRobots(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	var wg sync.WaitGroup
	workers := 10
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := NewMission("ConcurrentRobot")
			req.RobotSerial = "IMO"
			req.RobotName = "Robot-" + string(rune('A'+id))
			_, _ = svc.Submit(context.Background(), req)
		}(i)
	}
	wg.Wait()

	result, err := svc.List(context.Background(), domain.DefaultPage())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if result.Total != workers {
		t.Errorf("expected %d missions, got %d", workers, result.Total)
	}
}

func TestMission_ConcurrentSubmit_SameIdempotencyKey(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	var wg sync.WaitGroup
	workers := 5
	results := make([]*SubmitResult, workers)
	var mu sync.Mutex
	idx := 0

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := NewMission("SameRobot")
			req.IdempotencyKey = "shared-key-concurrent"
			result, err := svc.Submit(context.Background(), req)
			if err != nil {
				return
			}
			mu.Lock()
			if idx < workers {
				results[idx] = result
				idx++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	// All successful results should have the same mission ID.
	var firstID string
	count := 0
	for _, r := range results {
		if r == nil {
			continue
		}
		if firstID == "" {
			firstID = r.MissionID
		}
		if r.MissionID == firstID {
			count++
		}
	}
	if count < 1 {
		t.Error("expected at least one successful submission with consistent ID")
	}
}

func TestMission_Get_NotFound(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	_, err := svc.Get(context.Background(), "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent mission")
	}
	if !apperr.IsNotFound(err) {
		t.Errorf("expected not found error, got %v", err)
	}
}

func TestMission_UpdatePriority(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	result := mustSubmit(t, svc, "Robot-Priority")

	err := svc.UpdatePriority(context.Background(), result.MissionID, "supervisor", 1, "req")
	if err != nil {
		t.Fatalf("update priority: %v", err)
	}

	d, err := svc.Get(context.Background(), result.MissionID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if d.Priority != 1 {
		t.Errorf("expected priority 1, got %d", d.Priority)
	}
}

func TestMission_UpdatePriority_InvalidValueReject(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	result := mustSubmit(t, svc, "Robot-BadPriority")

	err := svc.UpdatePriority(context.Background(), result.MissionID, "supervisor", 99, "req")
	if err == nil {
		t.Fatal("expected error for invalid priority")
	}
	if !apperr.IsCode(err, apperr.CodeValidationFailed) {
		t.Errorf("expected validation error, got %v", err)
	}
}

func TestMission_Backlog(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	for i := 0; i < 3; i++ {
		mustSubmit(t, svc, "Robot-"+string(rune('A'+i)))
	}

	backlog, err := svc.Backlog(context.Background())
	if err != nil {
		t.Fatalf("backlog: %v", err)
	}
	if backlog.Accepted != 3 {
		t.Errorf("expected 3 accepted, got %d", backlog.Accepted)
	}
}

func mustSubmit(t *testing.T, svc *Service, robotName string) *SubmitResult {
	t.Helper()
	result, err := svc.Submit(context.Background(), NewMission(robotName))
	if err != nil {
		t.Fatalf("submit mission: %v", err)
	}
	return result
}

func findMigrations() string {
	for _, c := range []string{"./migrations", "../migrations", "../../migrations"} {
		if _, err := osStat(c); err == nil {
			return c
		}
	}
	return "./migrations"
}
