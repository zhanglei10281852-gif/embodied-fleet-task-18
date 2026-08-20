package assignment

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
		Tasks: st, Leases: st, Executions: st,
		Audit: audit.New(st, clock), Clock: clock, Logger: apperr.NopLogger(),
		LeaseTimeout: 60 * time.Second,
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

func mustCreateAssignedTask(t *testing.T, svc *Service, st *store.SQLiteStore, id string) *domain.RobotAssignment {
	t.Helper()
	mustCreateDecl(t, st, "decl-"+id)
	task, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-" + id, TaskType: domain.AssignmentTypeInspection,
		Location: "Corridor-1", Priority: 5,
	})
	svc.Assign(context.Background(), id, "dispatcher", "d", "r")
	return task
}

func TestAssignment_Create(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-pt1")

	task, err := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-pt1", TaskType: domain.AssignmentTypeInspection,
		Location: "Corridor-1", Priority: 5,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.Status != domain.AssignmentStatusCreated {
		t.Errorf("expected created, got %s", task.Status)
	}
}

func TestAssignment_ClaimAndReport(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-pt2")

	task, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-pt2", TaskType: domain.AssignmentTypeTransport,
		Location: "Corridor-2", Priority: 5,
	})
	svc.Assign(context.Background(), task.ID, "assignee", "d", "r")

	claim, err := svc.Claim(context.Background(), ClaimRequest{
		TaskID: task.ID, ExecutorID: "executor-1",
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claim.LeaseID == "" {
		t.Error("expected non-empty lease ID")
	}

	err = svc.Report(context.Background(), ReportRequest{
		TaskID: task.ID, ExecutorID: "executor-1", Result: "completed",
	})
	if err != nil {
		t.Fatalf("report: %v", err)
	}

	got, _ := svc.Get(context.Background(), task.ID)
	if got.Status != domain.AssignmentStatusCompleted {
		t.Errorf("expected completed, got %s", got.Status)
	}
}

func TestAssignment_ClaimWrongExecutorReject(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-pt3")

	task, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-pt3", TaskType: domain.AssignmentTypeInspection,
		Location: "Corridor-1", Priority: 5,
	})
	svc.Assign(context.Background(), task.ID, "assignee", "d", "r")

	svc.Claim(context.Background(), ClaimRequest{
		TaskID: task.ID, ExecutorID: "executor-1",
	})

	// Different executor tries to report.
	err := svc.Report(context.Background(), ReportRequest{
		TaskID: task.ID, ExecutorID: "executor-2", Result: "completed",
	})
	if err == nil {
		t.Fatal("expected error reporting with wrong executor")
	}
}

func TestAssignment_ClaimIllegalTransitionReject(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-pt4")

	task, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-pt4", TaskType: domain.AssignmentTypeInspection,
		Location: "Corridor-1", Priority: 5,
	})
	// Can't claim a created task — must be assigned first.
	_, err := svc.Claim(context.Background(), ClaimRequest{
		TaskID: task.ID, ExecutorID: "exec",
	})
	if err == nil {
		t.Fatal("expected error claiming created task")
	}
	if !apperr.IsInvalidTransition(err) {
		t.Errorf("expected invalid transition, got %v", err)
	}
}

func TestAssignment_PreemptExpiredClaim(t *testing.T) {
	svc, st, clock, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-pt5")

	task, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-pt5", TaskType: domain.AssignmentTypeTransport,
		Location: "Corridor-1", Priority: 5,
	})
	svc.Assign(context.Background(), task.ID, "assignee", "d", "r")
	svc.Claim(context.Background(), ClaimRequest{
		TaskID: task.ID, ExecutorID: "executor-1",
	})

	// Advance clock past lease expiry.
	clock.Advance(2 * time.Hour)

	results, err := svc.PreemptExpiredClaims(context.Background())
	if err != nil {
		t.Fatalf("preempt: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 preempted, got %d", len(results))
	}
	if results[0].PrevExecutor != "executor-1" {
		t.Errorf("expected executor-1, got %s", results[0].PrevExecutor)
	}

	got, _ := svc.Get(context.Background(), task.ID)
	if got.Status != domain.AssignmentStatusPreempted {
		t.Errorf("expected preempted, got %s", got.Status)
	}
	if got.ClaimedBy != "" {
		t.Errorf("expected empty claimed_by, got %s", got.ClaimedBy)
	}
}

func TestAssignment_ReassignAfterPreempt(t *testing.T) {
	svc, st, clock, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-pt6")

	task, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-pt6", TaskType: domain.AssignmentTypeInspection,
		Location: "Corridor-1", Priority: 5,
	})
	svc.Assign(context.Background(), task.ID, "assignee", "d", "r")
	svc.Claim(context.Background(), ClaimRequest{TaskID: task.ID, ExecutorID: "exec-1"})
	clock.Advance(2 * time.Hour)
	svc.PreemptExpiredClaims(context.Background())

	err := svc.Reassign(context.Background(), task.ID, "scheduler", "r")
	if err != nil {
		t.Fatalf("reassign: %v", err)
	}
	got, _ := svc.Get(context.Background(), task.ID)
	if got.Status != domain.AssignmentStatusAssigned {
		t.Errorf("expected assigned, got %s", got.Status)
	}
}

func TestAssignment_ConcurrentClaimRace(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-pt7")

	task, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-pt7", TaskType: domain.AssignmentTypeInspection,
		Location: "Corridor-1", Priority: 5,
	})
	svc.Assign(context.Background(), task.ID, "assignee", "d", "r")

	var wg sync.WaitGroup
	var success int32
	workers := 10
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			_, err := svc.Claim(context.Background(), ClaimRequest{
				TaskID: task.ID, ExecutorID: "exec",
			})
			if err == nil {
				atomic.AddInt32(&success, 1)
			}
		}(i)
	}
	wg.Wait()

	if success != 1 {
		t.Errorf("expected exactly 1 successful claim, got %d", success)
	}
}

func TestAssignment_GetNotFound(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()
	_, err := svc.Get(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if !apperr.IsNotFound(err) {
		t.Errorf("expected not found, got %v", err)
	}
}

func TestAssignment_Backlog(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-pt8")
	mustCreateDecl(t, st, "decl-pt9")
	svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-pt8", TaskType: domain.AssignmentTypeInspection,
		Location: "B1", Priority: 5,
	})
	svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-pt9", TaskType: domain.AssignmentTypeTransport,
		Location: "B2", Priority: 5,
	})

	backlog, err := svc.Backlog(context.Background())
	if err != nil {
		t.Fatalf("backlog: %v", err)
	}
	if backlog.Created != 2 {
		t.Errorf("expected 2 created, got %d", backlog.Created)
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
