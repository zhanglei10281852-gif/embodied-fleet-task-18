package missionrun

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
		Orders: st, Audit: audit.New(st, clock), Clock: clock, Logger: apperr.NopLogger(),
	})
	return svc, st, clock, func() { st.Close() }
}

func mustCreateDecl(t *testing.T, st *store.SQLiteStore, id string) {
	t.Helper()
	now := time.Now().UTC()
	d := &domain.MissionRequest{
		ID: id, RobotName: "Robot", RobotSerial: "IMO", FleetCode: "V",
		PlannedStartAt: now.Add(24 * time.Hour), MissionKind: "transport", EstimatedEnergyUnits: 1,
		ChargeMode: "standard", RequestedBy: "agent", RequestingParty: domain.PartyOperationsDispatch,
		Status: domain.DeclStatusAccepted, Priority: 5, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := st.CreateMission(context.Background(), d); err != nil {
		t.Fatalf("create mission: %v", err)
	}
}

func TestMissionRun_Create(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-wo1")

	wo, err := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-wo1", RunType: domain.RunTypeInspection,
		MissionKind: "inspection", PlannedUnits: 100,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if wo.Status != domain.RunStatusCreated {
		t.Errorf("expected created, got %s", wo.Status)
	}
}

func TestMissionRun_CompleteTransaction(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-wo2")

	wo, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-wo2", RunType: domain.RunTypeInspection,
		MissionKind: "inspection", PlannedUnits: 100,
	})

	// Assign and start progress.
	svc.Assign(context.Background(), wo.ID, "worker-1", "d", "r")
	svc.StartProgress(context.Background(), wo.ID, "worker-1", "r")

	// Complete with actual volume (multi-step: update volume + status).
	err := svc.Complete(context.Background(), CompleteRequest{
		ID: wo.ID, ActualUnits: 95, Actor: "worker-1", RequestID: "r",
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, _ := svc.Get(context.Background(), wo.ID)
	if got.Status != domain.RunStatusCompleted {
		t.Errorf("expected completed, got %s", got.Status)
	}
	if got.ActualUnits != 95 {
		t.Errorf("expected actual 95, got %d", got.ActualUnits)
	}
	if got.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
}

func TestMissionRun_CompleteIllegalTransitionReject(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-wo3")

	wo, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-wo3", RunType: domain.RunTypeInspection,
		MissionKind: "inspection", PlannedUnits: 100,
	})

	// Can't complete directly from created — must assign and start first.
	err := svc.Complete(context.Background(), CompleteRequest{
		ID: wo.ID, ActualUnits: 50, Actor: "w", RequestID: "r",
	})
	if err == nil {
		t.Fatal("expected error completing from created state")
	}
	if !apperr.IsInvalidTransition(err) {
		t.Errorf("expected invalid transition, got %v", err)
	}
}

func TestMissionRun_Cancel(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-wo4")

	wo, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-wo4", RunType: domain.RunTypeTransport,
		MissionKind: "bulk", PlannedUnits: 50,
	})

	err := svc.Cancel(context.Background(), wo.ID, "d", "r")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}

	got, _ := svc.Get(context.Background(), wo.ID)
	if got.Status != domain.RunStatusCancelled {
		t.Errorf("expected cancelled, got %s", got.Status)
	}
}

func TestMissionRun_CancelCompletedReject(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-wo5")

	wo, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-wo5", RunType: domain.RunTypeInspection,
		MissionKind: "transport", PlannedUnits: 10,
	})
	svc.Assign(context.Background(), wo.ID, "w", "d", "r")
	svc.StartProgress(context.Background(), wo.ID, "w", "r")
	svc.Complete(context.Background(), CompleteRequest{ID: wo.ID, ActualUnits: 10, Actor: "w", RequestID: "r"})

	err := svc.Cancel(context.Background(), wo.ID, "d", "r")
	if err == nil {
		t.Fatal("expected error cancelling completed order")
	}
	if !apperr.IsInvalidTransition(err) {
		t.Errorf("expected invalid transition, got %v", err)
	}
}

func TestMissionRun_ConcurrentAssignRace(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-wo6")

	wo, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-wo6", RunType: domain.RunTypeInspection,
		MissionKind: "transport", PlannedUnits: 10,
	})

	var wg sync.WaitGroup
	var success int32
	workers := 10
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := svc.Assign(context.Background(), wo.ID, "worker", "d", "r")
			if err == nil {
				atomic.AddInt32(&success, 1)
			}
		}()
	}
	wg.Wait()

	if success != 1 {
		t.Errorf("expected exactly 1 successful assign, got %d", success)
	}
}

func TestMissionRun_PaginationBoundary(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	for i := 0; i < 5; i++ {
		mustCreateDecl(t, st, "decl-p"+string(rune('A'+i)))
		svc.Create(context.Background(), CreateRequest{
			MissionID: "decl-p" + string(rune('A'+i)),
			RunType:   domain.RunTypeInspection, MissionKind: "transport", PlannedUnits: 10,
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
	if result.Total != 5 {
		t.Errorf("expected total 5, got %d", result.Total)
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
