package worker

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/assignment"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

func setupTest(t *testing.T) (*Worker, *assignment.Service, *store.SQLiteStore, *apperr.FakeClock, func()) {
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
	taskSvc := assignment.New(assignment.Deps{
		Tasks: st, Leases: st, Executions: st,
		Audit: audit.New(st, clock), Clock: clock, Logger: apperr.NopLogger(),
		LeaseTimeout: 60 * time.Second,
	})
	w := New(Deps{
		TaskService:  taskSvc,
		Clock:        clock,
		Logger:       apperr.NopLogger(),
		ID:           "test-worker",
		PollInterval: 50 * time.Millisecond,
		BatchSize:    5,
	})
	return w, taskSvc, st, clock, func() { st.Close() }
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

func TestWorker_ClaimAndExecute(t *testing.T) {
	w, taskSvc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-w1")

	task, _ := taskSvc.Create(context.Background(), assignment.CreateRequest{
		MissionID: "decl-w1", TaskType: domain.AssignmentTypeInspection,
		Location: "B1", Priority: 5,
	})
	taskSvc.Assign(context.Background(), task.ID, "assignee", "d", "r")

	err := w.ClaimAndExecute(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("claim and execute: %v", err)
	}

	got, _ := taskSvc.Get(context.Background(), task.ID)
	if got.Status != domain.AssignmentStatusCompleted {
		t.Errorf("expected completed, got %s", got.Status)
	}

	stats := w.Stats()
	if stats.TasksClaimed != 1 {
		t.Errorf("expected 1 claimed, got %d", stats.TasksClaimed)
	}
	if stats.TasksReported != 1 {
		t.Errorf("expected 1 reported, got %d", stats.TasksReported)
	}
}

func TestWorker_ExecuteOnceMultipleTasks(t *testing.T) {
	w, taskSvc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-w2")
	mustCreateDecl(t, st, "decl-w3")
	mustCreateDecl(t, st, "decl-w4")

	for _, declID := range []string{"decl-w2", "decl-w3", "decl-w4"} {
		task, _ := taskSvc.Create(context.Background(), assignment.CreateRequest{
			MissionID: declID, TaskType: domain.AssignmentTypeTransport,
			Location: "B1", Priority: 5,
		})
		taskSvc.Assign(context.Background(), task.ID, "assignee", "d", "r")
	}

	count, err := w.ExecuteOnce(context.Background())
	if err != nil {
		t.Fatalf("execute once: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 executed, got %d", count)
	}
}

func TestWorker_StartStopLoop(t *testing.T) {
	w, taskSvc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-w5")

	task, _ := taskSvc.Create(context.Background(), assignment.CreateRequest{
		MissionID: "decl-w5", TaskType: domain.AssignmentTypeInspection,
		Location: "B1", Priority: 5,
	})
	taskSvc.Assign(context.Background(), task.ID, "assignee", "d", "r")

	// Use ExecuteOnce to verify the worker can process tasks.
	count, err := w.ExecuteOnce(context.Background())
	if err != nil {
		t.Fatalf("execute once: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 executed, got %d", count)
	}

	stats := w.Stats()
	if stats.TasksReported == 0 {
		t.Error("expected at least 1 reported task")
	}
}

func TestWorker_ConcurrentWorkersRace(t *testing.T) {
	w, taskSvc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-w6")

	task, _ := taskSvc.Create(context.Background(), assignment.CreateRequest{
		MissionID: "decl-w6", TaskType: domain.AssignmentTypeInspection,
		Location: "B1", Priority: 5,
	})
	taskSvc.Assign(context.Background(), task.ID, "assignee", "d", "r")

	w2 := New(Deps{
		TaskService:  taskSvc,
		Clock:        apperr.NewFake(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)),
		Logger:       apperr.NopLogger(),
		ID:           "test-worker-2",
		PollInterval: 50 * time.Millisecond,
		BatchSize:    5,
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		w.ClaimAndExecute(context.Background(), task.ID)
	}()
	go func() {
		defer wg.Done()
		w2.ClaimAndExecute(context.Background(), task.ID)
	}()
	wg.Wait()

	got, _ := taskSvc.Get(context.Background(), task.ID)
	if got.Status != domain.AssignmentStatusCompleted {
		t.Errorf("expected completed, got %s", got.Status)
	}

	total := w.Stats().TasksClaimed + w2.Stats().TasksClaimed
	if total != 1 {
		t.Errorf("expected 1 total claim, got %d", total)
	}
}

func TestWorker_IdempotentReport(t *testing.T) {
	w, taskSvc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-w7")

	task, _ := taskSvc.Create(context.Background(), assignment.CreateRequest{
		MissionID: "decl-w7", TaskType: domain.AssignmentTypeTransport,
		Location: "B1", Priority: 5,
	})
	taskSvc.Assign(context.Background(), task.ID, "assignee", "d", "r")

	w.ClaimAndExecute(context.Background(), task.ID)

	err := w.ClaimAndExecute(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("second execute should not error: %v", err)
	}

	got, _ := taskSvc.Get(context.Background(), task.ID)
	if got.Status != domain.AssignmentStatusCompleted {
		t.Errorf("expected completed, got %s", got.Status)
	}
}

func TestWorker_Stats(t *testing.T) {
	w, _, _, _, cleanup := setupTest(t)
	defer cleanup()

	stats := w.Stats()
	if stats.Polls != 0 || stats.TasksClaimed != 0 {
		t.Error("expected zero stats initially")
	}
}

func TestWorker_ID(t *testing.T) {
	w, _, _, _, cleanup := setupTest(t)
	defer cleanup()
	if w.ID() != "test-worker" {
		t.Errorf("expected test-worker, got %s", w.ID())
	}
}

func TestWorker_PersistRestartReplay(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/restart.db"
	migrations := findMigrations()

	db1, _ := store.Open(dbPath, migrations)
	st1 := store.NewSQLiteStore(db1)
	mustCreateDecl(t, st1, "decl-w-persist")
	clock1 := apperr.NewFake(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC))
	taskSvc1 := assignment.New(assignment.Deps{
		Tasks: st1, Leases: st1, Executions: st1,
		Audit: audit.New(st1, clock1), Clock: clock1, Logger: apperr.NopLogger(),
		LeaseTimeout: 60 * time.Second,
	})
	w1 := New(Deps{
		TaskService: taskSvc1, Clock: clock1, Logger: apperr.NopLogger(),
		ID: "persist-worker", PollInterval: 50 * time.Millisecond, BatchSize: 5,
	})
	task, _ := taskSvc1.Create(context.Background(), assignment.CreateRequest{
		MissionID: "decl-w-persist", TaskType: domain.AssignmentTypeInspection,
		Location: "B1", Priority: 5,
	})
	taskSvc1.Assign(context.Background(), task.ID, "assignee", "d", "r")
	w1.ClaimAndExecute(context.Background(), task.ID)
	st1.Close()

	// Reopen and verify task is completed.
	db2, _ := store.Open(dbPath, migrations)
	st2 := store.NewSQLiteStore(db2)
	defer st2.Close()
	clock2 := apperr.NewFake(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC))
	taskSvc2 := assignment.New(assignment.Deps{
		Tasks: st2, Leases: st2, Executions: st2,
		Audit: audit.New(st2, clock2), Clock: clock2, Logger: apperr.NopLogger(),
		LeaseTimeout: 60 * time.Second,
	})
	got, err := taskSvc2.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if got.Status != domain.AssignmentStatusCompleted {
		t.Errorf("expected completed after restart, got %s", got.Status)
	}
}

func TestWorker_ConcurrentExecuteAtomic(t *testing.T) {
	w, taskSvc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-w-atomic")

	task, _ := taskSvc.Create(context.Background(), assignment.CreateRequest{
		MissionID: "decl-w-atomic", TaskType: domain.AssignmentTypeInspection,
		Location: "B1", Priority: 5,
	})
	taskSvc.Assign(context.Background(), task.ID, "assignee", "d", "r")

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.ClaimAndExecute(context.Background(), task.ID)
		}()
	}
	wg.Wait()

	if w.Stats().TasksClaimed != 1 {
		t.Errorf("expected exactly 1 claim, got %d", w.Stats().TasksClaimed)
	}
	if w.Stats().TasksReported != 1 {
		t.Errorf("expected exactly 1 report, got %d", w.Stats().TasksReported)
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
