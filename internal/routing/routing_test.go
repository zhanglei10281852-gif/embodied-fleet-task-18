package routing

import (
	"context"
	"os"
	"path/filepath"
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
	auditRec := audit.New(st, clock)
	svc := New(Deps{
		Windows: st, Missions: st, Escalations: st, Handovers: st,
		Audit: auditRec, Clock: clock, Logger: apperr.NopLogger(),
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

func TestRouting_CreateReservation(t *testing.T) {
	svc, st, clock, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-1")

	now := clock.Now()
	w, err := svc.Create(context.Background(), CreateRequest{
		MissionID:        "decl-1",
		CorridorID:       "B1",
		RobotName:        "TestRobot",
		EffectiveAt:      now,
		DeadlineAt:       now.Add(24 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.Status != domain.ReservationStatusAllocated {
		t.Errorf("expected allocated, got %s", w.Status)
	}
}

func TestRouting_BatchAllocate(t *testing.T) {
	svc, st, clock, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-b1")
	mustCreateDecl(t, st, "decl-b2")

	now := clock.Now()
	items := []BatchItem{
		{MissionID: "decl-b1", CorridorID: "B1", RobotName: "Robot1",
			EffectiveAt: now, DeadlineAt: now.Add(24 * time.Hour), ResponsibleParty: domain.PartyFieldEngineering},
		{MissionID: "decl-b2", CorridorID: "B2", RobotName: "Robot2",
			EffectiveAt: now, DeadlineAt: now.Add(24 * time.Hour), ResponsibleParty: domain.PartyFieldEngineering},
	}
	windows, err := svc.BatchAllocate(context.Background(), "dispatcher", items, "req-1")
	if err != nil {
		t.Fatalf("batch allocate: %v", err)
	}
	if len(windows) != 2 {
		t.Errorf("expected 2 windows, got %d", len(windows))
	}
}

func TestRouting_BatchAllocateRejectEmpty(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()
	_, err := svc.BatchAllocate(context.Background(), "d", nil, "req")
	if err == nil {
		t.Fatal("expected error for empty batch")
	}
}

func TestRouting_Release(t *testing.T) {
	svc, st, clock, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-r1")

	now := clock.Now()
	w, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-r1", CorridorID: "B1", RobotName: "Robot",
		EffectiveAt: now, DeadlineAt: now.Add(24 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})
	// Activate the window first (allocated -> effective).
	st.UpdateReservationStatus(context.Background(), w.ID, domain.ReservationStatusEffective, w.Version)
	w.Version++

	err := svc.Release(context.Background(), w.ID, "dispatcher", "req")
	if err != nil {
		t.Fatalf("release: %v", err)
	}

	got, _ := svc.Get(context.Background(), w.ID)
	if got.Status != domain.ReservationStatusReleased {
		t.Errorf("expected released, got %s", got.Status)
	}
}

func TestRouting_ReleaseIllegalTransition(t *testing.T) {
	svc, st, clock, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-il1")

	now := clock.Now()
	w, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-il1", CorridorID: "B1", RobotName: "Robot",
		EffectiveAt: now, DeadlineAt: now.Add(24 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})
	_ = svc.Release(context.Background(), w.ID, "d", "r")
	// Releasing a released window should fail.
	err := svc.Release(context.Background(), w.ID, "d", "r")
	if err == nil {
		t.Fatal("expected error releasing already-released window")
	}
	if !apperr.IsInvalidTransition(err) {
		t.Errorf("expected invalid transition, got %v", err)
	}
}

func TestRouting_EscalateOverdueDeadlineExceeded(t *testing.T) {
	svc, st, clock, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-esc1")

	now := clock.Now()
	w, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-esc1", CorridorID: "B1", RobotName: "Robot",
		EffectiveAt: now, DeadlineAt: now.Add(1 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})
	// Activate the window.
	st.UpdateReservationStatus(context.Background(), w.ID, domain.ReservationStatusEffective, w.Version)

	// Advance clock past deadline.
	clock.Advance(2 * time.Hour)

	results, err := svc.EscalateOverdue(context.Background())
	if err != nil {
		t.Fatalf("escalate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 escalation, got %d", len(results))
	}
	if results[0].FromLevel != 0 || results[0].ToLevel != 1 {
		t.Errorf("expected escalation 0->1, got %d->%d", results[0].FromLevel, results[0].ToLevel)
	}

	got, _ := svc.Get(context.Background(), w.ID)
	if got.EscalationLevel != 1 {
		t.Errorf("expected escalation level 1, got %d", got.EscalationLevel)
	}
	if got.Status != domain.ReservationStatusEscalated {
		t.Errorf("expected escalated status, got %s", got.Status)
	}
}

func TestRouting_ActivateEffectiveWindow(t *testing.T) {
	svc, st, clock, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-act1")

	now := clock.Now()
	w, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-act1", CorridorID: "B1", RobotName: "Robot",
		EffectiveAt: now.Add(1 * time.Hour), DeadlineAt: now.Add(25 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})

	// Before effective time: no activation.
	count, _ := svc.ActivateEffective(context.Background())
	if count != 0 {
		t.Errorf("expected 0 activations before effective, got %d", count)
	}

	// After effective time: should activate.
	clock.Advance(2 * time.Hour)
	count, _ = svc.ActivateEffective(context.Background())
	if count != 1 {
		t.Errorf("expected 1 activation, got %d", count)
	}

	got, _ := svc.Get(context.Background(), w.ID)
	if got.Status != domain.ReservationStatusEffective {
		t.Errorf("expected effective, got %s", got.Status)
	}
}

func TestRouting_ForceIntervene(t *testing.T) {
	svc, st, clock, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-int1")

	now := clock.Now()
	w, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-int1", CorridorID: "B1", RobotName: "Robot",
		EffectiveAt: now, DeadlineAt: now.Add(24 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})

	err := svc.ForceIntervene(context.Background(), w.ID, "supervisor", domain.ReservationStatusCancelled, "req")
	if err != nil {
		t.Fatalf("force intervene: %v", err)
	}

	got, _ := svc.Get(context.Background(), w.ID)
	if got.Status != domain.ReservationStatusCancelled {
		t.Errorf("expected cancelled, got %s", got.Status)
	}
}

func TestRouting_ForceInterveneInvalidTransitionReject(t *testing.T) {
	svc, st, clock, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-int2")

	now := clock.Now()
	w, _ := svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-int2", CorridorID: "B1", RobotName: "Robot",
		EffectiveAt: now, DeadlineAt: now.Add(24 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})
	// Cancel it first.
	svc.ForceIntervene(context.Background(), w.ID, "s", domain.ReservationStatusCancelled, "r")

	// Try to force an invalid transition: cancelled -> occupied.
	err := svc.ForceIntervene(context.Background(), w.ID, "s", domain.ReservationStatusOccupied, "r")
	if err == nil {
		t.Fatal("expected error for invalid intervention")
	}
	if !apperr.IsInvalidTransition(err) {
		t.Errorf("expected invalid transition, got %v", err)
	}
}

func TestRouting_GetNotFound(t *testing.T) {
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

func TestRouting_PersistRestartRecover(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "restart.db")
	migrations := findMigrations()

	// First session: create a window.
	db1, err := store.Open(dbPath, migrations)
	if err != nil {
		t.Fatalf("open db1: %v", err)
	}
	st1 := store.NewSQLiteStore(db1)
	mustCreateDecl(t, st1, "decl-restart1")
	clock1 := apperr.NewFake(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC))
	svc1 := New(Deps{
		Windows: st1, Missions: st1, Escalations: st1, Handovers: st1,
		Audit: audit.New(st1, clock1), Clock: clock1, Logger: apperr.NopLogger(),
		LeaseTimeout: 60 * time.Second,
	})
	now := clock1.Now()
	w, err := svc1.Create(context.Background(), CreateRequest{
		MissionID: "decl-restart1", CorridorID: "B1", RobotName: "RestartRobot",
		EffectiveAt: now, DeadlineAt: now.Add(24 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	st1.Close()

	// Second session: reopen and verify the window persisted.
	db2, err := store.Open(dbPath, migrations)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	st2 := store.NewSQLiteStore(db2)
	defer st2.Close()
	clock2 := apperr.NewFake(time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC))
	svc2 := New(Deps{
		Windows: st2, Missions: st2, Escalations: st2, Handovers: st2,
		Audit: audit.New(st2, clock2), Clock: clock2, Logger: apperr.NopLogger(),
		LeaseTimeout: 60 * time.Second,
	})
	got, err := svc2.Get(context.Background(), w.ID)
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if got.RobotName != "RestartRobot" {
		t.Errorf("robot name after restart: expected RestartRobot, got %s", got.RobotName)
	}
	if got.Status != domain.ReservationStatusAllocated {
		t.Errorf("status after restart: expected allocated, got %s", got.Status)
	}
}

func TestRouting_Backlog(t *testing.T) {
	svc, st, clock, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-bl1")
	mustCreateDecl(t, st, "decl-bl2")

	now := clock.Now()
	svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-bl1", CorridorID: "B1", RobotName: "S1",
		EffectiveAt: now, DeadlineAt: now.Add(24 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})
	svc.Create(context.Background(), CreateRequest{
		MissionID: "decl-bl2", CorridorID: "B2", RobotName: "S2",
		EffectiveAt: now, DeadlineAt: now.Add(24 * time.Hour),
		ResponsibleParty: domain.PartyFieldEngineering,
	})

	backlog, err := svc.Backlog(context.Background())
	if err != nil {
		t.Fatalf("backlog: %v", err)
	}
	if backlog.Allocated != 2 {
		t.Errorf("expected 2 allocated, got %d", backlog.Allocated)
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
