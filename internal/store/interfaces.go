package store

import (
	"context"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

// TxRunner executes a function within a database transaction.
type TxRunner interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// MissionRepo manages mission-request persistence.
type MissionRepo interface {
	CreateMission(ctx context.Context, d *domain.MissionRequest) error
	GetMission(ctx context.Context, id string) (*domain.MissionRequest, error)
	ListMissions(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.MissionRequest], error)
	UpdateMissionStatus(ctx context.Context, id string, status domain.MissionStatus, version int) (int, error)
	UpdateQueuePosition(ctx context.Context, id string, pos, version int) (int, error)
	UpdatePriority(ctx context.Context, id string, priority, version int) (int, error)
	CountMissionsByStatus(ctx context.Context, status domain.MissionStatus) (int, error)
	ListMissionsByStatus(ctx context.Context, status domain.MissionStatus) ([]*domain.MissionRequest, error)
	GetMissionByIdempotencyKey(ctx context.Context, key string) (*domain.MissionRequest, error)
}

// ReservationRepo manages route-reservation persistence.
type ReservationRepo interface {
	CreateReservation(ctx context.Context, w *domain.RouteReservation) error
	CreateReservationsBatch(ctx context.Context, ws []*domain.RouteReservation) error
	GetReservation(ctx context.Context, id string) (*domain.RouteReservation, error)
	ListReservations(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.RouteReservation], error)
	UpdateReservationStatus(ctx context.Context, id string, status domain.ReservationStatus, version int) (int, error)
	UpdateReservationAssignedTo(ctx context.Context, id string, assignedTo string, level int, version int) (int, error)
	ListExpiredWindows(ctx context.Context, now string) ([]*domain.RouteReservation, error)
	ListReservationsByStatus(ctx context.Context, status domain.ReservationStatus) ([]*domain.RouteReservation, error)
	CountReservationsByStatus(ctx context.Context, status domain.ReservationStatus) (int, error)
}

// MissionRunRepo manages mission-run persistence.
type MissionRunRepo interface {
	CreateMissionRun(ctx context.Context, w *domain.MissionRun) error
	GetMissionRun(ctx context.Context, id string) (*domain.MissionRun, error)
	ListMissionRuns(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.MissionRun], error)
	UpdateMissionRunStatus(ctx context.Context, id string, status domain.MissionRunStatus, version int) (int, error)
	UpdateActualUnits(ctx context.Context, id string, volume, version int) (int, error)
	ListMissionRunsByStatus(ctx context.Context, status domain.MissionRunStatus) ([]*domain.MissionRun, error)
	CountMissionRunsByStatus(ctx context.Context, status domain.MissionRunStatus) (int, error)
}

// AssignmentRepo manages robot-assignment persistence.
type AssignmentRepo interface {
	CreateAssignment(ctx context.Context, t *domain.RobotAssignment) error
	GetAssignment(ctx context.Context, id string) (*domain.RobotAssignment, error)
	ListAssignments(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.RobotAssignment], error)
	UpdateAssignmentStatus(ctx context.Context, id string, status domain.AssignmentStatus, version int) (int, error)
	UpdateAssignmentClaim(ctx context.Context, id, claimedBy, leaseID string, expires string, version int) (int, error)
	ClearAssignmentClaim(ctx context.Context, id string, version int) (int, error)
	ListClaimableTasks(ctx context.Context, limit int) ([]*domain.RobotAssignment, error)
	ListExpiredClaims(ctx context.Context, now string) ([]*domain.RobotAssignment, error)
	ListAssignmentsByStatus(ctx context.Context, status domain.AssignmentStatus) ([]*domain.RobotAssignment, error)
	CountAssignmentsByStatus(ctx context.Context, status domain.AssignmentStatus) (int, error)
}

// QuotaRepo manages quota persistence.
type QuotaRepo interface {
	GetOrCreateQuota(ctx context.Context, qt domain.QuotaType, date string, limit int) (*domain.Quota, error)
	GetQuota(ctx context.Context, id string) (*domain.Quota, error)
	GetQuotaByTypeDate(ctx context.Context, qt domain.QuotaType, date string) (*domain.Quota, error)
	ListQuotas(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.Quota], error)
	ReserveQuota(ctx context.Context, id string, amount, version int) (int, error)
	CommitQuota(ctx context.Context, id string, amount, version int) (int, error)
	ReleaseQuota(ctx context.Context, id string, amount, version int) (int, error)
	ListAllQuotas(ctx context.Context) ([]*domain.Quota, error)
}

// HandoverRepo manages handover-document persistence.
type HandoverRepo interface {
	CreateHandover(ctx context.Context, h *domain.MaintenanceHandover) error
	GetHandover(ctx context.Context, id string) (*domain.MaintenanceHandover, error)
	ListHandovers(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.MaintenanceHandover], error)
	UpdateHandoverStatus(ctx context.Context, id string, status domain.HandoverStatus, version int) (int, error)
	ListHandoversByEntity(ctx context.Context, entityType domain.EntityType, entityID string) ([]*domain.MaintenanceHandover, error)
}

// LeaseRepo manages task-lease persistence.
type LeaseRepo interface {
	CreateLease(ctx context.Context, l *domain.TaskLease) error
	GetLease(ctx context.Context, id string) (*domain.TaskLease, error)
	GetActiveLeaseByTask(ctx context.Context, taskType domain.EntityType, taskID string) (*domain.TaskLease, error)
	RevokeLease(ctx context.Context, id, reason string, version int) (int, error)
	ListExpiredLeases(ctx context.Context, now string) ([]*domain.TaskLease, error)
	ReleaseLease(ctx context.Context, id string, version int) (int, error)
}

// AuditRepo manages audit-log persistence and queries.
type AuditRepo interface {
	InsertAudit(ctx context.Context, entry *domain.AuditLog) error
	ListAuditLogs(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.AuditLog], error)
	ListAuditByEntity(ctx context.Context, entityType domain.EntityType, entityID string) ([]*domain.AuditLog, error)
}

// EscalationRepo manages escalation-record persistence.
type EscalationRepo interface {
	InsertEscalation(ctx context.Context, r *domain.EscalationRecord) error
	ListEscalations(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.EscalationRecord], error)
	ListEscalationsByEntity(ctx context.Context, entityType domain.EntityType, entityID string) ([]*domain.EscalationRecord, error)
}

// ExecutionRepo manages execution-record persistence.
type ExecutionRepo interface {
	InsertExecution(ctx context.Context, r *domain.ExecutionRecord) error
	ListExecutions(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.ExecutionRecord], error)
}

// IdempotencyRepo manages idempotency-key persistence.
type IdempotencyRepo interface {
	GetIdempotency(ctx context.Context, key string) (*domain.IdempotencyRecord, error)
	InsertIdempotency(ctx context.Context, r *domain.IdempotencyRecord) error
	CleanExpiredIdempotency(ctx context.Context, now string) (int, error)
}

// AuthRepo persists operator identities and revocable sessions.
type AuthRepo interface {
	CreateUserIfAbsent(ctx context.Context, user *domain.User) error
	GetUserByUsername(ctx context.Context, username string) (*domain.User, error)
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	CreateSession(ctx context.Context, session *domain.Session) error
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (*domain.Session, error)
	RevokeSession(ctx context.Context, tokenHash string, revokedAt string) (int, error)
	DeleteExpiredSessions(ctx context.Context, before string) (int, error)
}

// Store is the aggregate persistence interface combining all repositories.
type Store interface {
	TxRunner
	MissionRepo
	ReservationRepo
	MissionRunRepo
	AssignmentRepo
	QuotaRepo
	HandoverRepo
	LeaseRepo
	AuditRepo
	EscalationRepo
	ExecutionRepo
	IdempotencyRepo
	AuthRepo
	Close() error
}
