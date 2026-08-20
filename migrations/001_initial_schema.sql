-- Embodied Robot Fleet Service - Initial Schema Migration
-- Version: 1

PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

-- Schema version tracking
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    description TEXT NOT NULL,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Mission requests and charging forecasts.
CREATE TABLE IF NOT EXISTS mission_requests (
    id TEXT PRIMARY KEY,
    robot_name TEXT NOT NULL,
    robot_serial TEXT NOT NULL,
    fleet_code TEXT NOT NULL,
    planned_start_at TEXT NOT NULL,
    route_preference TEXT NOT NULL DEFAULT '',
    mission_kind TEXT NOT NULL,
    estimated_energy_units INTEGER NOT NULL DEFAULT 0,
    charge_mode TEXT NOT NULL DEFAULT 'standard',
    requested_by TEXT NOT NULL,
    requesting_party TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'submitted',
    priority INTEGER NOT NULL DEFAULT 5,
    queue_position INTEGER NOT NULL DEFAULT 0,
    idempotency_key TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_missions_status ON mission_requests(status);
CREATE INDEX IF NOT EXISTS idx_missions_planned_start ON mission_requests(planned_start_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_missions_idem ON mission_requests(idempotency_key) WHERE idempotency_key != '';
CREATE INDEX IF NOT EXISTS idx_missions_party ON mission_requests(requesting_party);

-- Route reservations for shared corridors.
CREATE TABLE IF NOT EXISTS route_reservations (
    id TEXT PRIMARY KEY,
    mission_id TEXT NOT NULL,
    corridor_id TEXT NOT NULL,
    robot_name TEXT NOT NULL,
    effective_at TEXT NOT NULL,
    deadline_at TEXT NOT NULL,
    assigned_to TEXT NOT NULL DEFAULT '',
    responsible_party TEXT NOT NULL,
    escalation_level INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'allocated',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (mission_id) REFERENCES mission_requests(id)
);

CREATE INDEX IF NOT EXISTS idx_windows_status ON route_reservations(status);
CREATE INDEX IF NOT EXISTS idx_windows_effective ON route_reservations(effective_at);
CREATE INDEX IF NOT EXISTS idx_windows_deadline ON route_reservations(deadline_at);
CREATE INDEX IF NOT EXISTS idx_windows_corridor ON route_reservations(corridor_id);

-- Mission execution runs.
CREATE TABLE IF NOT EXISTS mission_runs (
    id TEXT PRIMARY KEY,
    mission_id TEXT NOT NULL,
    route_reservation_id TEXT,
    run_type TEXT NOT NULL,
    mission_kind TEXT NOT NULL,
    planned_units INTEGER NOT NULL,
    actual_units INTEGER NOT NULL DEFAULT 0,
    assigned_to TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'created',
    started_at TEXT,
    completed_at TEXT,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (mission_id) REFERENCES mission_requests(id),
    FOREIGN KEY (route_reservation_id) REFERENCES route_reservations(id)
);

CREATE INDEX IF NOT EXISTS idx_missionruns_status ON mission_runs(status);
CREATE INDEX IF NOT EXISTS idx_missionruns_decl ON mission_runs(mission_id);

-- Robot assignments claimed by field executors.
CREATE TABLE IF NOT EXISTS robot_assignments (
    id TEXT PRIMARY KEY,
    mission_id TEXT NOT NULL,
    route_reservation_id TEXT,
    task_type TEXT NOT NULL,
    location TEXT NOT NULL,
    assigned_to TEXT NOT NULL DEFAULT '',
    claimed_by TEXT NOT NULL DEFAULT '',
    claim_expires_at TEXT,
    lease_id TEXT,
    status TEXT NOT NULL DEFAULT 'created',
    priority INTEGER NOT NULL DEFAULT 5,
    report_data TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (mission_id) REFERENCES mission_requests(id),
    FOREIGN KEY (route_reservation_id) REFERENCES route_reservations(id)
);

CREATE INDEX IF NOT EXISTS idx_assignments_status ON robot_assignments(status);
CREATE INDEX IF NOT EXISTS idx_assignments_type ON robot_assignments(task_type);
CREATE INDEX IF NOT EXISTS idx_assignments_claim ON robot_assignments(claimed_by);

-- Daily charging-energy quotas.
CREATE TABLE IF NOT EXISTS quotas (
    id TEXT PRIMARY KEY,
    quota_type TEXT NOT NULL,
    period_date TEXT NOT NULL,
    daily_limit INTEGER NOT NULL,
    used_amount INTEGER NOT NULL DEFAULT 0,
    reserved_amount INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'available',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(quota_type, period_date)
);

CREATE INDEX IF NOT EXISTS idx_quotas_type ON quotas(quota_type);
CREATE INDEX IF NOT EXISTS idx_quotas_period ON quotas(period_date);

-- Maintenance handovers (交接单)
CREATE TABLE IF NOT EXISTS maintenance_handovers (
    id TEXT PRIMARY KEY,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    from_party TEXT NOT NULL,
    to_party TEXT NOT NULL,
    action TEXT NOT NULL,
    document_ref TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    notes TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (entity_id) REFERENCES mission_requests(id)
);

CREATE INDEX IF NOT EXISTS idx_handover_entity ON maintenance_handovers(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_handover_status ON maintenance_handovers(status);

-- Task leases (执行租约)
CREATE TABLE IF NOT EXISTS task_leases (
    id TEXT PRIMARY KEY,
    task_type TEXT NOT NULL,
    task_id TEXT NOT NULL,
    executor_id TEXT NOT NULL,
    claimed_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    revoked_reason TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_leases_task ON task_leases(task_type, task_id);
CREATE INDEX IF NOT EXISTS idx_leases_status ON task_leases(status);
CREATE INDEX IF NOT EXISTS idx_leases_expires ON task_leases(expires_at);

-- Audit logs (审计记录)
CREATE TABLE IF NOT EXISTS audit_logs (
    id TEXT PRIMARY KEY,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    before_state TEXT NOT NULL DEFAULT '',
    after_state TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL DEFAULT '',
    timestamp TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit_logs(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_actor ON audit_logs(actor);
CREATE INDEX IF NOT EXISTS idx_audit_time ON audit_logs(timestamp);

-- Escalation records (升级记录)
CREATE TABLE IF NOT EXISTS escalation_records (
    id TEXT PRIMARY KEY,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    from_level INTEGER NOT NULL,
    to_level INTEGER NOT NULL,
    reason TEXT NOT NULL,
    resolved_by TEXT NOT NULL DEFAULT '',
    resolved_at TEXT,
    timestamp TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_escalation_entity ON escalation_records(entity_type, entity_id);

-- Idempotency records (幂等记录)
CREATE TABLE IF NOT EXISTS idempotency_records (
    key TEXT PRIMARY KEY,
    response_body TEXT NOT NULL,
    response_status INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_idempotency_expires ON idempotency_records(expires_at);

-- Execution records (执行记录)
CREATE TABLE IF NOT EXISTS execution_records (
    id TEXT PRIMARY KEY,
    task_type TEXT NOT NULL,
    task_id TEXT NOT NULL,
    executor_id TEXT NOT NULL,
    result TEXT NOT NULL,
    error_message TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    timestamp TEXT NOT NULL,
    FOREIGN KEY (task_id) REFERENCES robot_assignments(id)
);

CREATE INDEX IF NOT EXISTS idx_exec_task ON execution_records(task_type, task_id);
CREATE INDEX IF NOT EXISTS idx_exec_executor ON execution_records(executor_id);

