# Embodied Fleet Operations Backend

Embodied Fleet is a production-style Go backend for coordinating inspection and transport robots in warehouses and industrial parks. It manages mission intake, shared-corridor reservations, charging capacity, field assignments, execution leases, maintenance handovers, retries, escalation, and durable audit history.

## Architecture

The repository builds three processes over one SQLite database:

- `cmd/scheduler`: authenticated HTTP API and deadline-driven dispatch engine.
- `cmd/executor`: persistent assignment claimant and execution reporter.
- `cmd/migrate`: explicit migration runner for deployment checks.

Production packages have one primary responsibility:

```text
cmd/                   process entry points and graceful shutdown
internal/auth/         password verification and revocable sessions
internal/domain/       entities, state machines, pagination, typed errors
internal/mission/      mission intake, idempotency, and charging reservation
internal/routing/      corridor reservation, deadline, and escalation rules
internal/missionrun/   executable mission-run lifecycle
internal/assignment/   field assignment leases, claims, reports, preemption
internal/quota/        fast and standard charging-capacity accounting
internal/handover/     maintenance responsibility handovers
internal/engine/       due-work activation and overdue recovery loop
internal/worker/       polling, retry, cancellation, and permanent outcomes
internal/audit/        durable actor/object/request audit events
internal/store/        repository contracts, SQLite SQL, and transactions
internal/server/       routing, middleware, handlers, and JSON contracts
internal/config/       validated environment configuration
migrations/            ordered schema migrations
```

The domain layer has no dependency on HTTP or SQLite. Handlers call services, services depend on repository interfaces, and the SQLite implementation uses conditional updates, constraints, and explicit transactions to preserve business invariants.

## Data Model

The migrations create these related tables:

- `mission_requests`: robot identity, planned start, mission kind, estimated energy, queue position, and optimistic version.
- `route_reservations`: corridor ownership, effective/deadline interval, assignee, escalation level, and mission foreign key.
- `mission_runs`: executable runs linked to a mission and optional route reservation.
- `robot_assignments`: lease-backed field work linked to the same mission and reservation.
- `quotas`: daily fast/standard charging limits with reserved and consumed amounts.
- `maintenance_handovers`: responsibility transfer records for mission work.
- `task_leases` and `execution_records`: claim ownership, expiry, and durable outcomes.
- `users` and `sessions`: bcrypt identities and revocable, expiring bearer sessions.
- `audit_logs`, `escalation_records`, and `idempotency_records`: operational evidence and replay protection.
- `schema_migrations`: applied migration versions.

Foreign keys, unique constraints, partial indexes, timestamps, and optimistic version columns are defined in `migrations/`. SQLite enables foreign keys, WAL mode, a busy timeout, and bounded connection pooling. Tests use temporary real databases and verify close/reopen recovery.

## Authentication

Login returns a random bearer token. Only its SHA-256 digest is stored in `sessions`; logout persists `revoked_at`, and every authenticated request checks revocation, expiry, user activity, and role.

The development bootstrap accounts are:

| Role | Username | Password | Primary access |
| --- | --- | --- | --- |
| operations dispatcher | `dispatcher` | `dispatch123` | mission, route, charging, and assignment dispatch |
| field engineer | `engineer` | `engineer123` | mission execution, assignment claim/report, maintenance handover |
| audit administrator | `auditor` | `auditor123` | audit, escalation, backlog, reconciliation, and engine status |

Override every bootstrap credential through environment variables before a non-development deployment. Existing password hashes are not overwritten during restart.

## Configuration

Copy `.env.example` or export the variables supported by `internal/config`:

- `ROBOTFLEET_PORT`, `ROBOTFLEET_DATA_DIR`, `ROBOTFLEET_DB_NAME`
- `ROBOTFLEET_REQUEST_TIMEOUT`, `ROBOTFLEET_SESSION_TTL_MINUTES`, `ROBOTFLEET_PASSWORD_COST`
- `ROBOTFLEET_DISPATCHER_USERNAME`, `ROBOTFLEET_DISPATCHER_PASSWORD`
- `ROBOTFLEET_ENGINEER_USERNAME`, `ROBOTFLEET_ENGINEER_PASSWORD`
- `ROBOTFLEET_AUDITOR_USERNAME`, `ROBOTFLEET_AUDITOR_PASSWORD`
- `ROBOTFLEET_SCHEDULER_INTERVAL`, `ROBOTFLEET_LEASE_TIMEOUT`, `ROBOTFLEET_ESCALATION_INTERVAL`
- `ROBOTFLEET_EXECUTOR_INTERVAL`, `ROBOTFLEET_EXECUTOR_ID`, `ROBOTFLEET_LOG_LEVEL`

## Run Locally

Go 1.26 or later is required.

```bash
go mod download
go run ./cmd/migrate
go run ./cmd/scheduler
```

Run the executor in another terminal:

```bash
go run ./cmd/executor
```

The scheduler listens on `:58552` by default. `/health` is a liveness check and `/ready` verifies database access.

Authenticate and submit a mission:

```bash
TOKEN=$(curl -sS -X POST http://localhost:58552/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"dispatcher","password":"dispatch123"}' | jq -r .token)

curl -sS -X POST http://localhost:58552/api/v1/missions \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "robot_name":"AMR-27",
    "robot_serial":"RF-2026-0027",
    "fleet_code":"EAST-WH-01",
    "planned_start_at":"2026-08-20T08:00:00Z",
    "route_preference":"C-12>C-18",
    "mission_kind":"inspection",
    "estimated_energy_units":24,
    "charge_mode":"standard",
    "requested_by":"dispatcher",
    "requesting_party":"operations_dispatch",
    "priority":4,
    "idempotency_key":"mission-amr27-20260820-0800"
  }'
```

All business endpoints are under `/api/v1`. The API supports mission and reservation state transitions, batch reservations with per-item outcomes, assignment lease claim/report, charging reserve/commit/release, maintenance confirmation/rejection, paginated filters, reconciliation export, and audited intervention.

## Verification

```bash
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
go build ./...
```

Tests cover state machines, illegal transitions, cross-entity rules, idempotency, charging boundaries, rollback, optimistic conflicts, concurrent claims, session expiry/revocation, role authorization, restart recovery, worker retry/cancellation, deadline escalation, pagination, migrations, and HTTP error contracts.

## Docker

```bash
docker build -t embodied-fleet-go .
docker run --rm -p 58552:58552 -v embodied-fleet-data:/app/data embodied-fleet-go
```

The image includes `scheduler`, `executor`, and `migrate` under `/app/bin`. Override the default command when running the executor or migration process.
