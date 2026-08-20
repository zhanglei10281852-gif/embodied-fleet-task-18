package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/assignment"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/mission"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/routing"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

func setupTest(t *testing.T) (*Engine, *store.SQLiteStore, *apperr.FakeClock, *mission.Service, *routing.Service, *assignment.Service, func()) {
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
	auditRec := audit.New(st, clock)
	declSvc := mission.New(mission.Deps{
		Missions: st, Quotas: st, Idempotency: st, Handovers: st,
		Audit: auditRec, Clock: clock, Logger: apperr.NopLogger(),
		FastChargeLimit: 1000, StandardChargeLimit: 5000,
	})
	reservationSvc := routing.New(routing.Deps{
		Windows: st, Missions: st, Escalations: st, Handovers: st,
		Audit: auditRec, Clock: clock, Logger: apperr.NopLogger(),
		LeaseTimeout: 60 * time.Second,
	})
	taskSvc := assignment.New(assignment.Deps{
		Tasks: st, Leases: st, Executions: st,
		Audit: auditRec, Clock: clock, Logger: apperr.NopLogger(),
		LeaseTimeout: 60 * time.Second,
	})
	eng := New(Deps{
		MissionService:    declSvc,
		RoutingService:    reservationSvc,
		AssignmentService: taskSvc,
		Clock:             apperr.Default(),
		Logger:            apperr.NopLogger(),
		TickInterval:      50 * time.Millisecond,
	})
	return eng, st, clock, declSvc, reservationSvc, taskSvc, func() { st.Close() }
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

func TestEngine_StartStop(t *testing.T) {
	eng, _, _, _, _, _, cleanup := setupTest(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eng.Start(ctx)
	time.Sleep(200 * time.Millisecond)
	eng.Stop()

	stats := eng.Stats()
	if stats.Ticks == 0 {
		t.Error("expected at least 1 tick")
	}
}

func TestEngine_EscalateOverdueWindow(t *testing.T) {
	eng, st, clock, _, reservationSvc, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-eng1")

	now := clock.Now()
	w, _ := reservationSvc.Create(context.Background(), routing.CreateRequest{
		MissionID: "decl-eng1", CorridorID: "B1", RobotName: "Robot",
		EffectiveAt: now, DeadlineAt: now.Add(1 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})
	st.UpdateReservationStatus(context.Background(), w.ID, domain.ReservationStatusEffective, w.Version)

	// Advance past deadline.
	clock.Advance(2 * time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	time.Sleep(200 * time.Millisecond)
	eng.Stop()

	stats := eng.Stats()
	if stats.WindowsEscalated == 0 {
		t.Error("expected at least 1 escalation")
	}
}

func TestEngine_PreemptExpiredClaim(t *testing.T) {
	eng, st, clock, _, _, taskSvc, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-eng2")

	task, _ := taskSvc.Create(context.Background(), assignment.CreateRequest{
		MissionID: "decl-eng2", TaskType: domain.AssignmentTypeInspection,
		Location: "B1", Priority: 5,
	})
	taskSvc.Assign(context.Background(), task.ID, "assignee", "d", "r")
	taskSvc.Claim(context.Background(), assignment.ClaimRequest{
		TaskID: task.ID, ExecutorID: "exec-1",
	})

	// Advance past lease expiry.
	clock.Advance(2 * time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	time.Sleep(200 * time.Millisecond)
	eng.Stop()

	stats := eng.Stats()
	if stats.TasksPreempted == 0 {
		t.Error("expected at least 1 preemption")
	}

	got, _ := taskSvc.Get(context.Background(), task.ID)
	// After preemption + reassignment, should be assigned.
	if got.Status != domain.AssignmentStatusAssigned && got.Status != domain.AssignmentStatusPreempted {
		t.Errorf("expected assigned or preempted, got %s", got.Status)
	}
}

func TestEngine_PersistRestartRecover(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/restart.db"
	migrations := findMigrations()

	db1, _ := store.Open(dbPath, migrations)
	st1 := store.NewSQLiteStore(db1)
	mustCreateDecl(t, st1, "decl-persist1")
	clock1 := apperr.NewFake(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC))
	reservationSvc1 := routing.New(routing.Deps{
		Windows: st1, Missions: st1, Escalations: st1, Handovers: st1,
		Audit: audit.New(st1, clock1), Clock: clock1, Logger: apperr.NopLogger(),
		LeaseTimeout: 60 * time.Second,
	})
	now := clock1.Now()
	w, _ := reservationSvc1.Create(context.Background(), routing.CreateRequest{
		MissionID: "decl-persist1", CorridorID: "B1", RobotName: "PersistRobot",
		EffectiveAt: now, DeadlineAt: now.Add(24 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})
	st1.Close()

	// Reopen and verify.
	db2, _ := store.Open(dbPath, migrations)
	st2 := store.NewSQLiteStore(db2)
	defer st2.Close()
	got, err := st2.GetReservation(context.Background(), w.ID)
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if got.RobotName != "PersistRobot" {
		t.Errorf("expected PersistRobot, got %s", got.RobotName)
	}
}

func TestEngine_ActivateWindow(t *testing.T) {
	eng, st, clock, _, reservationSvc, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-eng3")

	now := clock.Now()
	w, _ := reservationSvc.Create(context.Background(), routing.CreateRequest{
		MissionID: "decl-eng3", CorridorID: "B1", RobotName: "Robot",
		EffectiveAt: now.Add(1 * time.Hour), DeadlineAt: now.Add(25 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})
	clock.Advance(2 * time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eng.Start(ctx)
	time.Sleep(200 * time.Millisecond)
	eng.Stop()

	stats := eng.Stats()
	if stats.WindowsActivated == 0 {
		t.Error("expected at least 1 activation")
	}

	got, _ := reservationSvc.Get(context.Background(), w.ID)
	if got.Status != domain.ReservationStatusEffective {
		t.Errorf("expected effective, got %s", got.Status)
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
