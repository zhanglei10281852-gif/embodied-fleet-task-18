package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/config"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

func setupTestServer(t *testing.T) (*Server, *store.SQLiteStore, *apperr.FakeClock, func()) {
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
	cfg := config.Default()
	cfg.Auth.PasswordCost = 4
	srv, err := New(Deps{Cfg: cfg, Store: st, Clock: clock, Logger: apperr.NopLogger()})
	if err != nil {
		t.Fatalf("initialize server: %v", err)
	}
	return srv, st, clock, func() { st.Close() }
}

func authenticatedRequest(t *testing.T, srv *Server, method, target string, body io.Reader) *http.Request {
	t.Helper()
	username, password := "dispatcher", "dispatch123"
	if strings.HasPrefix(target, "/api/v1/audit-") || strings.HasPrefix(target, "/api/v1/escalations") ||
		strings.HasPrefix(target, "/api/v1/backlog") || strings.HasPrefix(target, "/api/v1/export/") ||
		strings.HasPrefix(target, "/api/v1/executions") || strings.HasPrefix(target, "/api/v1/engine/") {
		username, password = "auditor", "auditor123"
	}
	loginBody, _ := json.Marshal(map[string]string{"username": username, "password": password})
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	srv.Router().ServeHTTP(loginRecorder, loginReq)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("test login failed: %d %s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var login loginResponse
	if err := json.NewDecoder(loginRecorder.Body).Decode(&login); err != nil {
		t.Fatalf("decode test login: %v", err)
	}
	req := httptest.NewRequest(method, target, body)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	return req
}

func createTestDecl(id string, now time.Time) *domain.MissionRequest {
	return &domain.MissionRequest{
		ID: id, RobotName: "TestRobot", RobotSerial: "IMO123", FleetCode: "V001",
		PlannedStartAt: now.Add(24 * time.Hour), MissionKind: "inspection", EstimatedEnergyUnits: 10,
		ChargeMode: "standard", RequestedBy: "agent-1", RequestingParty: domain.PartyOperationsDispatch,
		Status: domain.DeclStatusAccepted, Priority: 5, Version: 1,
		CreatedAt: now, UpdatedAt: now, IdempotencyKey: "key-" + id,
	}
}

func TestServer_HealthEndpoint(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	req := authenticatedRequest(t, srv, "GET", "/health", nil)
	w := httptest.NewRecorder()
	srv.HealthHandler()(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "healthy" {
		t.Errorf("expected healthy, got %s", resp["status"])
	}
}

func TestServer_SubmitMission(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{
		"robot_name": "TestRobot",
		"robot_serial": "IMO123",
		"fleet_code": "V001",
		"planned_start_at": "2026-01-15T10:00:00Z",
		"mission_kind": "inspection",
		"estimated_energy_units": 10,
		"charge_mode": "standard",
		"requested_by": "agent-1",
		"requesting_party": "operations_dispatch",
		"priority": 5,
		"idempotency_key": "idem-test-1"
	}`
	req := authenticatedRequest(t, srv, "POST", "/api/v1/missions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServer_ErrorResponseIncludesRequestID(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	req := authenticatedRequest(t, srv, "POST", "/api/v1/missions", bytes.NewBufferString("{"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-contract-001")
	w := httptest.NewRecorder()
	srv.buildHandler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("X-Request-ID"); got != "req-contract-001" {
		t.Fatalf("expected response request ID, got %q", got)
	}
	var resp ErrorResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp.Code != string(apperr.CodeValidationFailed) {
		t.Errorf("expected validation code, got %q", resp.Code)
	}
	if resp.RequestID != "req-contract-001" {
		t.Errorf("expected request_id in body, got %q", resp.RequestID)
	}
}

func TestServer_ListMissionsPagination(t *testing.T) {
	srv, st, _, cleanup := setupTestServer(t)
	defer cleanup()

	for i := 0; i < 5; i++ {
		now := time.Now().UTC()
		d := createTestDecl("d-"+string(rune('A'+i)), now)
		st.CreateMission(context.Background(), d)
	}

	req := authenticatedRequest(t, srv, "GET", "/api/v1/missions?page=1&page_size=2", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	meta := resp["meta"].(map[string]any)
	if meta["total"].(float64) != 5 {
		t.Errorf("expected total 5, got %v", meta["total"])
	}
}

func TestServer_GetMissionNotFound(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	req := authenticatedRequest(t, srv, "GET", "/api/v1/missions/nonexistent", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestServer_CreateRouteReservation(t *testing.T) {
	srv, st, clock, cleanup := setupTestServer(t)
	defer cleanup()
	now := time.Now().UTC()
	d := createTestDecl("decl-bw1", now)
	st.CreateMission(context.Background(), d)

	body := `{
		"mission_id": "decl-bw1",
		"corridor_id": "B1",
		"robot_name": "TestRobot",
		"effective_at": "` + clock.Now().Format(time.RFC3339) + `",
		"deadline_at": "` + clock.Now().Add(24*time.Hour).Format(time.RFC3339) + `",
		"responsible_party": "field_engineering"
	}`
	req := authenticatedRequest(t, srv, "POST", "/api/v1/route-reservations", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServer_CancelMissionInvalidTransition(t *testing.T) {
	srv, st, _, cleanup := setupTestServer(t)
	defer cleanup()
	now := time.Now().UTC()
	d := createTestDecl("decl-cancel1", now)
	d.Status = domain.DeclStatusCancelled
	st.CreateMission(context.Background(), d)

	req := authenticatedRequest(t, srv, "PUT", "/api/v1/missions/decl-cancel1/cancel", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
}

func TestServer_QuotaReserve(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{"quota_type": "fast_charge", "amount": 100}`
	req := authenticatedRequest(t, srv, "POST", "/api/v1/quotas/reserve", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestServer_QuotaExceedBackpressure(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{"quota_type": "fast_charge", "amount": 99999}`
	req := authenticatedRequest(t, srv, "POST", "/api/v1/quotas/reserve", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422, got %d", w.Code)
	}
}

func TestServer_BacklogEndpoint(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	req := authenticatedRequest(t, srv, "GET", "/api/v1/backlog", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServer_ExportReconciliation(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	req := authenticatedRequest(t, srv, "GET", "/api/v1/export/reconciliation", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") != "text/csv" {
		t.Errorf("expected text/csv, got %s", w.Header().Get("Content-Type"))
	}
}

func TestServer_AuditLogs(t *testing.T) {
	srv, st, _, cleanup := setupTestServer(t)
	defer cleanup()

	now := time.Now().UTC()
	entry := &domain.AuditLog{
		ID: "audit-1", Actor: "agent-1", Action: "submit",
		EntityType: domain.EntityMission, EntityID: "decl-1",
		Timestamp: now,
	}
	st.InsertAudit(context.Background(), entry)

	req := authenticatedRequest(t, srv, "GET", "/api/v1/audit-logs", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestServer_IdempotentDuplicateSubmit(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{
		"robot_name": "IdemRobot",
		"robot_serial": "IMO456",
		"fleet_code": "V002",
		"planned_start_at": "2026-01-15T10:00:00Z",
		"mission_kind": "inspection",
		"estimated_energy_units": 10,
		"charge_mode": "standard",
		"requested_by": "agent-1",
		"requesting_party": "operations_dispatch",
		"priority": 5,
		"idempotency_key": "idem-dup-key"
	}`

	req1 := authenticatedRequest(t, srv, "POST", "/api/v1/missions", bytes.NewBufferString(body))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	srv.Router().ServeHTTP(w1, req1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first submit: expected 201, got %d", w1.Code)
	}

	var resp1 map[string]any
	json.NewDecoder(w1.Body).Decode(&resp1)
	id1 := resp1["mission_id"].(string)

	req2 := authenticatedRequest(t, srv, "POST", "/api/v1/missions", bytes.NewBufferString(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(w2, req2)
	if w2.Code != http.StatusCreated {
		t.Fatalf("second submit: expected 201, got %d", w2.Code)
	}

	var resp2 map[string]any
	json.NewDecoder(w2.Body).Decode(&resp2)
	id2 := resp2["mission_id"].(string)

	if id1 != id2 {
		t.Errorf("idempotent submit should return same ID: %s vs %s", id1, id2)
	}
}

func TestServer_PersistRestartRecover(t *testing.T) {
	dir := t.TempDir()
	dbPath := dir + "/restart.db"
	migrations := findMigrations()

	db1, _ := store.Open(dbPath, migrations)
	st1 := store.NewSQLiteStore(db1)
	now := time.Now().UTC()
	d := createTestDecl("decl-restart-srv", now)
	st1.CreateMission(context.Background(), d)
	st1.Close()

	db2, _ := store.Open(dbPath, migrations)
	st2 := store.NewSQLiteStore(db2)
	defer st2.Close()
	got, err := st2.GetMission(context.Background(), "decl-restart-srv")
	if err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if got.RobotName != "TestRobot" {
		t.Errorf("expected TestRobot, got %s", got.RobotName)
	}
}

func TestServer_AuthenticationAndRoleAuthorization(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	missing := httptest.NewRequest(http.MethodGet, "/api/v1/missions", nil)
	missingRecorder := httptest.NewRecorder()
	srv.Router().ServeHTTP(missingRecorder, missing)
	if missingRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d: %s", missingRecorder.Code, missingRecorder.Body.String())
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(
		`{"username":"engineer","password":"engineer123"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	srv.Router().ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("engineer login failed: %d %s", loginRecorder.Code, loginRecorder.Body.String())
	}
	var login loginResponse
	if err := json.NewDecoder(loginRecorder.Body).Decode(&login); err != nil {
		t.Fatal(err)
	}

	auditRequest := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
	auditRequest.Header.Set("Authorization", "Bearer "+login.Token)
	auditRecorder := httptest.NewRecorder()
	srv.Router().ServeHTTP(auditRecorder, auditRequest)
	if auditRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected engineer to be forbidden from audit log, got %d", auditRecorder.Code)
	}
}

func TestServer_LogoutRevokesBearerToken(t *testing.T) {
	srv, _, _, cleanup := setupTestServer(t)
	defer cleanup()

	request := authenticatedRequest(t, srv, http.MethodGet, "/api/v1/auth/me", nil)
	token := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
	meRecorder := httptest.NewRecorder()
	srv.Router().ServeHTTP(meRecorder, request)
	if meRecorder.Code != http.StatusOK {
		t.Fatalf("me before logout: %d %s", meRecorder.Code, meRecorder.Body.String())
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutRequest.Header.Set("Authorization", "Bearer "+token)
	logoutRecorder := httptest.NewRecorder()
	srv.Router().ServeHTTP(logoutRecorder, logoutRequest)
	if logoutRecorder.Code != http.StatusNoContent {
		t.Fatalf("logout: %d %s", logoutRecorder.Code, logoutRecorder.Body.String())
	}

	after := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	after.Header.Set("Authorization", "Bearer "+token)
	afterRecorder := httptest.NewRecorder()
	srv.Router().ServeHTTP(afterRecorder, after)
	if afterRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked token to return 401, got %d", afterRecorder.Code)
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
