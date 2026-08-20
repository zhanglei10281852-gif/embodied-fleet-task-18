package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func mustCreateMissionCtx(t *testing.T, s *SQLiteStore, ctx context.Context, id string) *domain.MissionRequest {
	t.Helper()
	now := time.Now().UTC()
	d := &domain.MissionRequest{
		ID: id, RobotName: "Test Robot", RobotSerial: "IMO123", FleetCode: "V001",
		PlannedStartAt: now.Add(24 * time.Hour), MissionKind: "inspection", EstimatedEnergyUnits: 10,
		ChargeMode: "standard", RequestedBy: "agent-1", RequestingParty: domain.PartyOperationsDispatch,
		Status: domain.DeclStatusSubmitted, Priority: 5, Version: 1,
		CreatedAt: now, UpdatedAt: now, IdempotencyKey: "key-" + id,
	}
	if err := s.CreateMission(ctx, d); err != nil {
		t.Fatalf("create mission: %v", err)
	}
	return d
}

func mustCreateMission(t *testing.T, s *SQLiteStore, id string) *domain.MissionRequest {
	return mustCreateMissionCtx(t, s, context.Background(), id)
}

func TestStore_CreateAndGetMission(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	d := mustCreateMission(t, st, "decl-1")

	got, err := st.GetMission(ctx, "decl-1")
	if err != nil {
		t.Fatalf("get mission: %v", err)
	}
	if got.RobotName != d.RobotName {
		t.Errorf("robot name: expected %s, got %s", d.RobotName, got.RobotName)
	}
	if got.Version != 1 {
		t.Errorf("version: expected 1, got %d", got.Version)
	}
}

func TestStore_GetMissionByIdempotencyKey(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	mustCreateMission(t, st, "decl-idem-1")

	got, err := st.GetMissionByIdempotencyKey(ctx, "key-decl-idem-1")
	if err != nil {
		t.Fatalf("get by idempotency key: %v", err)
	}
	if got.ID != "decl-idem-1" {
		t.Errorf("id: expected decl-idem-1, got %s", got.ID)
	}
}

func TestStore_OptimisticLockConflict(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	d := mustCreateMission(t, st, "decl-conflict-1")

	affected, err := st.UpdateMissionStatus(ctx, d.ID, domain.DeclStatusReviewing, d.Version)
	if err != nil {
		t.Fatalf("update status: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 row affected, got %d", affected)
	}

	// Try with stale version — should get 0 affected rows (conflict).
	affected, err = st.UpdateMissionStatus(ctx, d.ID, domain.DeclStatusAccepted, d.Version)
	if err != nil {
		t.Fatalf("update status with stale version: %v", err)
	}
	if affected != 0 {
		t.Errorf("expected 0 rows for stale version, got %d", affected)
	}
}

func TestStore_TransactionCommit(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	err := st.InTx(ctx, func(ctx context.Context) error {
		mustCreateMissionCtx(t, st, ctx, "decl-tx-commit")
		return nil
	})
	if err != nil {
		t.Fatalf("transaction commit: %v", err)
	}

	got, err := st.GetMission(ctx, "decl-tx-commit")
	if err != nil {
		t.Fatalf("get after commit: %v", err)
	}
	if got == nil {
		t.Fatal("mission should exist after commit")
	}
}

func TestStore_TransactionRollback(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	rollbackErr := context.DeadlineExceeded
	err := st.InTx(ctx, func(ctx context.Context) error {
		mustCreateMissionCtx(t, st, ctx, "decl-tx-rollback")
		return rollbackErr
	})
	if err != rollbackErr {
		t.Fatalf("expected rollback error, got %v", err)
	}

	_, err = st.GetMission(ctx, "decl-tx-rollback")
	if err == nil {
		t.Fatal("mission should not exist after rollback")
	}
}

func TestStore_ListWithPagination(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	for i := 0; i < 25; i++ {
		mustCreateMission(t, st, uuid.NewString())
	}

	q := domain.DefaultPage()
	q.Page = 1
	q.PageSize = 10
	result, err := st.ListMissions(ctx, q)
	if err != nil {
		t.Fatalf("list missions: %v", err)
	}
	if len(result.Items) != 10 {
		t.Errorf("page 1 items: expected 10, got %d", len(result.Items))
	}
	if result.Total != 25 {
		t.Errorf("total: expected 25, got %d", result.Total)
	}
	if result.TotalPages != 3 {
		t.Errorf("total pages: expected 3, got %d", result.TotalPages)
	}
	if !result.HasNext {
		t.Error("expected HasNext=true")
	}
	if result.HasPrev {
		t.Error("expected HasPrev=false on page 1")
	}

	q.Page = 3
	result, err = st.ListMissions(ctx, q)
	if err != nil {
		t.Fatalf("list missions page 3: %v", err)
	}
	if len(result.Items) != 5 {
		t.Errorf("page 3 items: expected 5, got %d", len(result.Items))
	}
	if !result.HasPrev {
		t.Error("expected HasPrev=true on page 3")
	}
	if result.HasNext {
		t.Error("expected HasNext=false on last page")
	}
}

func TestStore_PaginationBoundary_PageSizeOne(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		mustCreateMission(t, st, uuid.NewString())
	}

	q := domain.DefaultPage()
	q.Page = 1
	q.PageSize = 1
	result, err := st.ListMissions(ctx, q)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(result.Items))
	}
	if result.TotalPages != 3 {
		t.Errorf("expected 3 total pages, got %d", result.TotalPages)
	}
}

func TestStore_QuotaReserveAndCommit(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	q, err := st.GetOrCreateQuota(ctx, domain.QuotaTypeFastCharge, "2026-01-01", 1000)
	if err != nil {
		t.Fatalf("create quota: %v", err)
	}

	affected, err := st.ReserveQuota(ctx, q.ID, 100, q.Version)
	if err != nil {
		t.Fatalf("reserve: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 row affected, got %d", affected)
	}

	updated, err := st.GetQuota(ctx, q.ID)
	if err != nil {
		t.Fatalf("get quota: %v", err)
	}
	if updated.ReservedAmount != 100 {
		t.Errorf("reserved: expected 100, got %d", updated.ReservedAmount)
	}

	affected, err = st.CommitQuota(ctx, q.ID, 50, updated.Version)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if affected != 1 {
		t.Fatalf("expected 1 row affected, got %d", affected)
	}

	committed, err := st.GetQuota(ctx, q.ID)
	if err != nil {
		t.Fatalf("get committed quota: %v", err)
	}
	if committed.UsedAmount != 50 {
		t.Errorf("used: expected 50, got %d", committed.UsedAmount)
	}
	if committed.ReservedAmount != 50 {
		t.Errorf("reserved after commit: expected 50, got %d", committed.ReservedAmount)
	}
}

func TestStore_QuotaReserveRejectsWhenExhausted(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	q, _ := st.GetOrCreateQuota(ctx, domain.QuotaTypeFastCharge, "2026-01-02", 100)
	affected, err := st.ReserveQuota(ctx, q.ID, 100, q.Version)
	if err != nil {
		t.Fatalf("reserve full: %v", err)
	}
	if affected != 1 {
		t.Fatal("expected 1 row affected")
	}

	affected, _ = st.ReserveQuota(ctx, q.ID, 1, 2)
	if affected != 0 {
		t.Errorf("expected 0 rows when exceeding quota, got %d", affected)
	}
}

func TestStore_WindowCreateAndGet(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	mustCreateMission(t, st, "decl-win-1")
	now := time.Now().UTC()
	w := &domain.RouteReservation{
		ID: "win-1", MissionID: "decl-win-1", CorridorID: "B1",
		RobotName: "Test Robot", EffectiveAt: now, DeadlineAt: now.Add(24 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering, Status: domain.ReservationStatusAllocated,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := st.CreateReservation(ctx, w); err != nil {
		t.Fatalf("create window: %v", err)
	}

	got, err := st.GetReservation(ctx, "win-1")
	if err != nil {
		t.Fatalf("get window: %v", err)
	}
	if got.CorridorID != "B1" {
		t.Errorf("corridor: expected B1, got %s", got.CorridorID)
	}
}

func TestStore_AuditLogInsertAndList(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	entry := &domain.AuditLog{
		ID: "audit-1", Actor: "agent-1", Action: "submit",
		EntityType: domain.EntityMission, EntityID: "decl-1",
		Timestamp: time.Now().UTC(),
	}
	if err := st.InsertAudit(ctx, entry); err != nil {
		t.Fatalf("insert audit: %v", err)
	}

	q := domain.DefaultPage()
	result, err := st.ListAuditLogs(ctx, q)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(result.Items) != 1 {
		t.Errorf("expected 1 audit log, got %d", len(result.Items))
	}
}

func TestStore_MigrationsApplied(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()

	versions, err := st.DB().AppliedMigrations()
	if err != nil {
		t.Fatalf("applied migrations: %v", err)
	}
	if len(versions) == 0 {
		t.Error("expected at least one migration to be applied")
	}
}

func TestStore_LeaseCreateAndExpire(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UTC()
	lease := &domain.TaskLease{
		ID: "lease-1", TaskType: domain.EntityAssignment, TaskID: "task-1",
		ExecutorID: "exec-1", ClaimedAt: now, ExpiresAt: now.Add(-1 * time.Hour),
		Status: domain.LeaseStatusActive, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if err := st.CreateLease(ctx, lease); err != nil {
		t.Fatalf("create lease: %v", err)
	}

	expired, err := st.ListExpiredLeases(ctx, now.Format("2006-01-02T15:04:05Z07:00"))
	if err != nil {
		t.Fatalf("list expired: %v", err)
	}
	if len(expired) != 1 {
		t.Errorf("expected 1 expired lease, got %d", len(expired))
	}

	affected, err := st.RevokeLease(ctx, "lease-1", "expired", 1)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if affected != 1 {
		t.Errorf("expected 1 row affected, got %d", affected)
	}
}

func TestStore_IdempotencyRecordInsertAndGet(t *testing.T) {
	st, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now().UTC()
	rec := &domain.IdempotencyRecord{
		Key: "idem-key-1", ResponseBody: `{"status":"accepted"}`,
		ResponseStatus: 200, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}
	if err := st.InsertIdempotency(ctx, rec); err != nil {
		t.Fatalf("insert idempotency: %v", err)
	}

	got, err := st.GetIdempotency(ctx, "idem-key-1")
	if err != nil {
		t.Fatalf("get idempotency: %v", err)
	}
	if got.ResponseBody != `{"status":"accepted"}` {
		t.Errorf("response body mismatch: got %s", got.ResponseBody)
	}
}

func setupTestStore(t *testing.T) (*SQLiteStore, func()) {
	t.Helper()
	db, err := OpenDB(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(findMigrations()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := NewSQLiteStore(db)
	return st, func() { st.Close() }
}

func findMigrations() string {
	for _, candidate := range []string{"./migrations", "../migrations", "../../migrations"} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "./migrations"
}
