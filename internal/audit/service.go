package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

// Recorder writes audit entries for every state-changing operation.
type Recorder struct {
	repo  store.AuditRepo
	idGen func() string
	clock apperr.Clock
}

// New creates a Recorder backed by the given AuditRepo.
func New(repo store.AuditRepo, clock apperr.Clock) *Recorder {
	return &Recorder{
		repo:  repo,
		idGen: func() string { return time.Now().Format("20060102") + "-" + randHex(8) },
		clock: clock,
	}
}

// Entry collects the data for a single audit record before it is persisted.
type Entry struct {
	Actor      string
	Action     string
	EntityType domain.EntityType
	EntityID   string
	Before     any
	After      any
	RequestID  string
}

// Record persists an audit entry. Before and After are JSON-encoded if non-nil.
func (r *Recorder) Record(ctx context.Context, e Entry) error {
	beforeStr := ""
	if e.Before != nil {
		b, err := json.Marshal(e.Before)
		if err == nil {
			beforeStr = string(b)
		}
	}
	afterStr := ""
	if e.After != nil {
		a, err := json.Marshal(e.After)
		if err == nil {
			afterStr = string(a)
		}
	}
	entry := &domain.AuditLog{
		ID:          r.idGen(),
		Actor:       e.Actor,
		Action:      e.Action,
		EntityType:  e.EntityType,
		EntityID:    e.EntityID,
		BeforeState: beforeStr,
		AfterState:  afterStr,
		RequestID:   e.RequestID,
		Timestamp:   r.clock.Now(),
	}
	return r.repo.InsertAudit(ctx, entry)
}

// RecordTransition is a convenience for state-transition audits.
func (r *Recorder) RecordTransition(ctx context.Context, actor, action, entityID string,
	et domain.EntityType, fromStatus, toStatus string, requestID string) error {
	return r.Record(ctx, Entry{
		Actor:      actor,
		Action:     action,
		EntityType: et,
		EntityID:   entityID,
		Before:     map[string]string{"status": fromStatus},
		After:      map[string]string{"status": toStatus},
		RequestID:  requestID,
	})
}

// List returns a paginated list of audit logs.
func (r *Recorder) List(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.AuditLog], error) {
	return r.repo.ListAuditLogs(ctx, q)
}

// ListByEntity returns all audit entries for a specific entity.
func (r *Recorder) ListByEntity(ctx context.Context, et domain.EntityType, entityID string) ([]*domain.AuditLog, error) {
	return r.repo.ListAuditByEntity(ctx, et, entityID)
}
