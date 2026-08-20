package testutil

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/assignment"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/config"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/handover"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/mission"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/missionrun"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/quota"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/routing"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

// SetupStore creates a fresh in-memory SQLite store with migrations applied.
// Returns the store and a cleanup function.
func SetupStore(t *testing.T) (*store.SQLiteStore, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	migrationsDir := findMigrationsDir()
	db, err := store.Open(dbPath, migrationsDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	st := store.NewSQLiteStore(db)
	cleanup := func() {
		st.Close()
		os.RemoveAll(dir)
	}
	return st, cleanup
}

// SetupStoreWithDataDir creates a store at a persistent path for restart tests.
func SetupStoreWithDataDir(t *testing.T, dataDir string) (*store.SQLiteStore, func()) {
	t.Helper()
	dbPath := filepath.Join(dataDir, "restart.db")
	migrationsDir := findMigrationsDir()
	db, err := store.Open(dbPath, migrationsDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	st := store.NewSQLiteStore(db)
	cleanup := func() {
		st.Close()
	}
	return st, cleanup
}

func findMigrationsDir() string {
	for _, candidate := range []string{"./migrations", "../migrations", "../../migrations", "../../../migrations"} {
		if _, err := os.Stat(candidate); err == nil {
			abs, _ := filepath.Abs(candidate)
			return abs
		}
	}
	return "./migrations"
}

// SetupServices creates all business services wired to a test store with a fake clock.
type ServiceBundle struct {
	Store       *store.SQLiteStore
	Clock       *apperr.FakeClock
	Audit       *audit.Recorder
	Missions    *mission.Service
	Windows     *routing.Service
	MissionRuns *missionrun.Service
	Tasks       *assignment.Service
	Quotas      *quota.Service
	Handovers   *handover.Service
}

// SetupServices creates all services with a shared test store and fake clock.
func SetupServices(t *testing.T) (*ServiceBundle, func()) {
	t.Helper()
	st, cleanup := SetupStore(t)
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
	orderSvc := missionrun.New(missionrun.Deps{
		Orders: st, Audit: auditRec, Clock: clock, Logger: apperr.NopLogger(),
	})
	taskSvc := assignment.New(assignment.Deps{
		Tasks: st, Leases: st, Executions: st,
		Audit: auditRec, Clock: clock, Logger: apperr.NopLogger(),
		LeaseTimeout: 60 * time.Second,
	})
	quotaSvc := quota.New(quota.Deps{
		Quotas: st, Audit: auditRec, Clock: clock, Logger: apperr.NopLogger(),
		FastChargeLimit: 1000, StandardChargeLimit: 5000,
	})
	handoverSvc := handover.New(handover.Deps{
		Handovers: st, Audit: auditRec, Clock: clock, Logger: apperr.NopLogger(),
	})

	bundle := &ServiceBundle{
		Store: st, Clock: clock, Audit: auditRec,
		Missions: declSvc, Windows: reservationSvc, MissionRuns: orderSvc,
		Tasks: taskSvc, Quotas: quotaSvc, Handovers: handoverSvc,
	}
	return bundle, cleanup
}

// NewMission creates a valid mission submit request for testing.
func NewMission(robotName string) mission.SubmitRequest {
	return mission.SubmitRequest{
		RobotName:            robotName,
		RobotSerial:          "IMO" + robotName,
		FleetCode:            "V001",
		PlannedStartAt:       time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC),
		MissionKind:          "inspection",
		EstimatedEnergyUnits: 10,
		ChargeMode:           "standard",
		RequestedBy:          "agent-1",
		RequestingParty:      "operations_dispatch",
		Priority:             5,
		IdempotencyKey:       "idem-" + uuid.NewString(),
	}
}

// MustSubmitMission submits a mission and fails the test on error.
func MustSubmitMission(t *testing.T, svc *mission.Service, robotName string) *mission.SubmitResult {
	t.Helper()
	result, err := svc.Submit(context.Background(), NewMission(robotName))
	if err != nil {
		t.Fatalf("submit mission: %v", err)
	}
	return result
}

// AssertStatus checks that an error has the expected apperr code.
func AssertStatus(t *testing.T, err error, expected apperr.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %s, got nil", expected)
	}
	code := apperr.AsCode(err)
	if code != expected {
		t.Fatalf("expected error code %s, got %s: %v", expected, code, err)
	}
}

// DefaultConfig returns a test config with safe defaults.
func DefaultConfig() *config.Config {
	cfg := config.Default()
	cfg.Database.DataDir = "./testdata"
	return cfg
}

// FormatTime formats a time for DB comparisons.
func FormatTime(t time.Time) string {
	return t.Format("2006-01-02T15:04:05Z07:00")
}

// AssertEqual fails if expected != actual.
func AssertEqual(t *testing.T, name string, expected, actual any) {
	t.Helper()
	if fmt.Sprintf("%v", expected) != fmt.Sprintf("%v", actual) {
		t.Fatalf("%s: expected %v, got %v", name, expected, actual)
	}
}
