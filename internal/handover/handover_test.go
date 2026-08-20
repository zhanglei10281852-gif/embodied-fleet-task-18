package handover

import (
	"context"
	"os"
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
		Handovers: st, Audit: audit.New(st, clock), Clock: clock, Logger: apperr.NopLogger(),
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

func TestHandover_Create(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-h1")

	h, err := svc.Create(context.Background(), CreateRequest{
		EntityType: domain.EntityMission, EntityID: "decl-h1",
		FromParty: domain.PartyOperationsDispatch, ToParty: domain.PartyFieldEngineering,
		Action: "submit",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if h.Status != domain.HandoverStatusPending {
		t.Errorf("expected pending, got %s", h.Status)
	}
}

func TestHandover_Confirm(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-h2")

	h, _ := svc.Create(context.Background(), CreateRequest{
		EntityType: domain.EntityMission, EntityID: "decl-h2",
		FromParty: domain.PartyFieldEngineering, ToParty: domain.PartyRobotOperations,
		Action: "assign_robot",
	})

	err := svc.Confirm(context.Background(), h.ID, "terminal", "r")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}

	got, _ := svc.Get(context.Background(), h.ID)
	if got.Status != domain.HandoverStatusConfirmed {
		t.Errorf("expected confirmed, got %s", got.Status)
	}
}

func TestHandover_ConfirmReject_AlreadyConfirmed(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-h3")

	h, _ := svc.Create(context.Background(), CreateRequest{
		EntityType: domain.EntityMission, EntityID: "decl-h3",
		FromParty: domain.PartyOperationsDispatch, ToParty: domain.PartyFieldEngineering,
		Action: "submit",
	})
	svc.Confirm(context.Background(), h.ID, "t", "r")

	err := svc.Confirm(context.Background(), h.ID, "t", "r")
	if err == nil {
		t.Fatal("expected error confirming already-confirmed")
	}
	if !apperr.IsInvalidTransition(err) {
		t.Errorf("expected invalid transition, got %v", err)
	}
}

func TestHandover_Reject(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-h4")

	h, _ := svc.Create(context.Background(), CreateRequest{
		EntityType: domain.EntityMission, EntityID: "decl-h4",
		FromParty: domain.PartyRobotOperations, ToParty: domain.PartySafetyAudit,
		Action: "report_completion",
	})

	err := svc.Reject(context.Background(), h.ID, "authority", "r")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}

	got, _ := svc.Get(context.Background(), h.ID)
	if got.Status != domain.HandoverStatusRejected {
		t.Errorf("expected rejected, got %s", got.Status)
	}
}

func TestHandover_ValidationReject(t *testing.T) {
	svc, _, _, cleanup := setupTest(t)
	defer cleanup()

	_, err := svc.Create(context.Background(), CreateRequest{
		EntityType: domain.EntityMission, EntityID: "",
		FromParty: "", ToParty: "", Action: "",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !apperr.IsCode(err, apperr.CodeValidationFailed) {
		t.Errorf("expected validation error, got %v", err)
	}
}

func TestHandover_ListByEntity(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-h5")

	svc.Create(context.Background(), CreateRequest{
		EntityType: domain.EntityMission, EntityID: "decl-h5",
		FromParty: domain.PartyOperationsDispatch, ToParty: domain.PartyFieldEngineering,
		Action: "submit",
	})
	svc.Create(context.Background(), CreateRequest{
		EntityType: domain.EntityMission, EntityID: "decl-h5",
		FromParty: domain.PartyFieldEngineering, ToParty: domain.PartyRobotOperations,
		Action: "assign",
	})

	docs, err := svc.ListByEntity(context.Background(), domain.EntityMission, "decl-h5")
	if err != nil {
		t.Fatalf("list by entity: %v", err)
	}
	if len(docs) != 2 {
		t.Errorf("expected 2 handovers, got %d", len(docs))
	}
}

func TestHandover_PaginationBoundary(t *testing.T) {
	svc, st, _, cleanup := setupTest(t)
	defer cleanup()
	mustCreateDecl(t, st, "decl-h6")

	for i := 0; i < 5; i++ {
		svc.Create(context.Background(), CreateRequest{
			EntityType: domain.EntityMission, EntityID: "decl-h6",
			FromParty: domain.PartyOperationsDispatch, ToParty: domain.PartyFieldEngineering,
			Action: "submit",
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

func TestHandover_GetNotFound(t *testing.T) {
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

func findMigrations() string {
	for _, c := range []string{"./migrations", "../migrations", "../../migrations"} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "./migrations"
}
