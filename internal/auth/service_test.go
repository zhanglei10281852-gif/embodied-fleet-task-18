package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/auth"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

func setupService(t *testing.T) (*auth.Service, *apperr.FakeClock, func()) {
	t.Helper()
	db, err := store.OpenDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(filepath.Join("..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	clock := apperr.NewFake(time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC))
	service := auth.New(store.NewSQLiteStore(db), clock, 30*time.Minute, 4)
	users := []auth.BootstrapUser{
		{Username: "dispatch", Password: "dispatch-pass", Role: domain.RoleDispatcher},
		{Username: "field", Password: "field-pass", Role: domain.RoleEngineer},
		{Username: "audit", Password: "audit-pass", Role: domain.RoleAuditor},
	}
	if err := service.Bootstrap(context.Background(), users); err != nil {
		t.Fatal(err)
	}
	return service, clock, func() { db.Close() }
}

func TestLoginAuthenticateAndLogout(t *testing.T) {
	service, _, cleanup := setupService(t)
	defer cleanup()

	result, err := service.Login(context.Background(), "dispatch", "dispatch-pass")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.Token == "" || result.Principal.Role != domain.RoleDispatcher {
		t.Fatalf("unexpected login result: %+v", result)
	}
	principal, err := service.Authenticate(context.Background(), result.Token)
	if err != nil || principal.Username != "dispatch" {
		t.Fatalf("authenticate: principal=%+v err=%v", principal, err)
	}
	if err := service.Logout(context.Background(), result.Token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := service.Authenticate(context.Background(), result.Token); !apperr.IsCode(err, apperr.CodeUnauthorized) {
		t.Fatalf("expected revoked token rejection, got %v", err)
	}
}

func TestExpiredSessionIsRejected(t *testing.T) {
	service, clock, cleanup := setupService(t)
	defer cleanup()

	result, err := service.Login(context.Background(), "field", "field-pass")
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(31 * time.Minute)
	if _, err := service.Authenticate(context.Background(), result.Token); !apperr.IsCode(err, apperr.CodeUnauthorized) {
		t.Fatalf("expected expired token rejection, got %v", err)
	}
	removed, err := service.CleanupExpired(context.Background())
	if err != nil || removed != 1 {
		t.Fatalf("cleanup expired sessions: removed=%d err=%v", removed, err)
	}
}

func TestInvalidCredentialsDoNotCreateSession(t *testing.T) {
	service, _, cleanup := setupService(t)
	defer cleanup()

	_, err := service.Login(context.Background(), "audit", "wrong")
	if !apperr.IsCode(err, apperr.CodeUnauthorized) {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatal("credential failure must not be reported as context cancellation")
	}
}

func TestSessionAndRevocationPersistAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "fleet-auth.db")
	migrations := filepath.Join("..", "..", "migrations")
	clock := apperr.NewFake(time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC))

	firstStore, err := store.OpenStore(dbPath, migrations)
	if err != nil {
		t.Fatal(err)
	}
	first := auth.New(firstStore, clock, time.Hour, 4)
	if err := first.Bootstrap(ctx, []auth.BootstrapUser{{Username: "operator", Password: "operator-pass", Role: domain.RoleDispatcher}}); err != nil {
		t.Fatal(err)
	}
	login, err := first.Login(ctx, "operator", "operator-pass")
	if err != nil {
		t.Fatal(err)
	}
	if err := firstStore.Close(); err != nil {
		t.Fatal(err)
	}

	secondStore, err := store.OpenStore(dbPath, migrations)
	if err != nil {
		t.Fatal(err)
	}
	second := auth.New(secondStore, clock, time.Hour, 4)
	if _, err := second.Authenticate(ctx, login.Token); err != nil {
		t.Fatalf("session did not survive restart: %v", err)
	}
	if err := second.Logout(ctx, login.Token); err != nil {
		t.Fatalf("logout after restart: %v", err)
	}
	if err := secondStore.Close(); err != nil {
		t.Fatal(err)
	}

	thirdStore, err := store.OpenStore(dbPath, migrations)
	if err != nil {
		t.Fatal(err)
	}
	defer thirdStore.Close()
	third := auth.New(thirdStore, clock, time.Hour, 4)
	if _, err := third.Authenticate(ctx, login.Token); !apperr.IsCode(err, apperr.CodeUnauthorized) {
		t.Fatalf("revocation did not survive restart: %v", err)
	}
}
