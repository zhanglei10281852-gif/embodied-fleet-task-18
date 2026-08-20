package quota

import (
	"context"
	"fmt"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

type Service struct {
	quotas              store.QuotaRepo
	audit               *audit.Recorder
	clock               apperr.Clock
	logger              *apperr.Logger
	sm                  *domain.StateMachine
	fastChargeLimit     int
	standardChargeLimit int
}

type Deps struct {
	Quotas              store.QuotaRepo
	Audit               *audit.Recorder
	Clock               apperr.Clock
	Logger              *apperr.Logger
	FastChargeLimit     int
	StandardChargeLimit int
}

func New(deps Deps) *Service {
	return &Service{
		quotas:              deps.Quotas,
		audit:               deps.Audit,
		clock:               deps.Clock,
		logger:              deps.Logger,
		sm:                  domain.QuotaStateMachine(),
		fastChargeLimit:     deps.FastChargeLimit,
		standardChargeLimit: deps.StandardChargeLimit,
	}
}

type ReserveResult struct {
	QuotaID      string           `json:"quota_id"`
	QuotaType    domain.QuotaType `json:"quota_type"`
	Available    int              `json:"available"`
	Reserved     int              `json:"reserved"`
	Status       string           `json:"status"`
	WaitEstimate time.Duration    `json:"wait_estimate"`
	Rejected     bool             `json:"rejected"`
	Message      string           `json:"message"`
}

type ReserveRequest struct {
	QuotaType domain.QuotaType
	Amount    int
	RequestID string
	Actor     string
}

func (s *Service) Reserve(ctx context.Context, req ReserveRequest) (*ReserveResult, error) {
	if req.Amount <= 0 {
		return nil, apperr.ValidationFailed("amount must be positive")
	}
	dateStr := s.clock.Now().Format("2006-01-02")
	limit := s.limitFor(req.QuotaType)

	reserved, quotaID, err := s.reserveWithRetry(ctx, req.QuotaType, dateStr, limit, req.Amount, 25)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "reserve quota failed", err)
	}
	if !reserved {
		q, err := s.quotas.GetOrCreateQuota(ctx, req.QuotaType, dateStr, limit)
		if err != nil {
			return nil, apperr.Wrap(apperr.CodeInternal, "quota re-read failed", err)
		}
		available := 0
		if q != nil {
			available = q.Available()
		}
		return &ReserveResult{
			QuotaType: req.QuotaType,
			Available: available,
			Rejected:  true,
			Status:    "exhausted",
			Message:   fmt.Sprintf("quota %s exhausted: available %d, requested %d", req.QuotaType, available, req.Amount),
		}, nil
	}

	q, err := s.quotas.GetOrCreateQuota(ctx, req.QuotaType, dateStr, limit)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "quota re-read failed", err)
	}
	utilization := q.Utilization()
	waitEstimate := time.Duration(0)
	if utilization > 0.8 {
		waitEstimate = 30 * time.Minute
	}
	_ = s.audit.Record(ctx, audit.Entry{
		Actor:      req.Actor,
		Action:     "reserve_quota",
		EntityType: domain.EntityQuota,
		EntityID:   quotaID,
		After:      map[string]int{"reserved": req.Amount},
		RequestID:  req.RequestID,
	})
	return &ReserveResult{
		QuotaID:      quotaID,
		QuotaType:    req.QuotaType,
		Available:    q.Available(),
		Reserved:     req.Amount,
		Status:       string(quotaStatus(utilization)),
		WaitEstimate: waitEstimate,
		Message:      "quota reserved successfully",
	}, nil
}

func (s *Service) reserveWithRetry(ctx context.Context, qt domain.QuotaType, dateStr string, limit, amount, maxRetries int) (bool, string, error) {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		q, err := s.quotas.GetOrCreateQuota(ctx, qt, dateStr, limit)
		if err != nil {
			return false, "", err
		}
		if q.Available() < amount {
			return false, q.ID, nil
		}
		affected, err := s.quotas.ReserveQuota(ctx, q.ID, amount, q.Version)
		if err != nil {
			if store.IsRetryableContention(err) && attempt < maxRetries {
				if err := waitForContention(ctx, attempt); err != nil {
					return false, q.ID, err
				}
				continue
			}
			return false, q.ID, err
		}
		if affected > 0 {
			return true, q.ID, nil
		}
	}
	return false, "", apperr.Conflict("quota", string(qt), 0)
}

func waitForContention(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt+1) * 2 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) Commit(ctx context.Context, quotaID string, amount int, version int, actor, requestID string) error {
	for attempt := 0; attempt < 5; attempt++ {
		q, err := s.quotas.GetQuota(ctx, quotaID)
		if err != nil {
			return apperr.Wrap(apperr.CodeInternal, "get quota failed", err)
		}
		if q.ReservedAmount < amount {
			return apperr.ValidationFailed("reserved amount less than commit amount")
		}
		affected, err := s.quotas.CommitQuota(ctx, quotaID, amount, q.Version)
		if err != nil {
			return apperr.Wrap(apperr.CodeInternal, "commit quota failed", err)
		}
		if affected > 0 {
			_ = s.audit.Record(ctx, audit.Entry{
				Actor: actor, Action: "commit_quota",
				EntityType: domain.EntityQuota, EntityID: quotaID,
				After: map[string]int{"committed": amount}, RequestID: requestID,
			})
			return nil
		}
	}
	return apperr.Conflict("quota", quotaID, version)
}

func (s *Service) Release(ctx context.Context, quotaID string, amount int, version int, actor, requestID string) error {
	for attempt := 0; attempt < 5; attempt++ {
		q, err := s.quotas.GetQuota(ctx, quotaID)
		if err != nil {
			return apperr.Wrap(apperr.CodeInternal, "get quota failed", err)
		}
		affected, err := s.quotas.ReleaseQuota(ctx, quotaID, amount, q.Version)
		if err != nil {
			return apperr.Wrap(apperr.CodeInternal, "release quota failed", err)
		}
		if affected > 0 {
			_ = s.audit.Record(ctx, audit.Entry{
				Actor: actor, Action: "release_quota",
				EntityType: domain.EntityQuota, EntityID: quotaID,
				After: map[string]int{"released": amount}, RequestID: requestID,
			})
			return nil
		}
	}
	return apperr.Conflict("quota", quotaID, version)
}

func (s *Service) Get(ctx context.Context, id string) (*domain.Quota, error) {
	q, err := s.quotas.GetQuota(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return nil, apperr.NotFound("quota", id)
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "get quota failed", err)
	}
	return q, nil
}

func (s *Service) List(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.Quota], error) {
	if err := q.Validate(100); err != nil {
		return domain.PageResult[*domain.Quota]{}, apperr.ValidationFailed(err.Error())
	}
	return s.quotas.ListQuotas(ctx, q)
}

func (s *Service) ListAll(ctx context.Context) ([]*domain.Quota, error) {
	return s.quotas.ListAllQuotas(ctx)
}

func (s *Service) limitFor(qt domain.QuotaType) int {
	if qt == domain.QuotaTypeStandardCharge {
		return s.standardChargeLimit
	}
	return s.fastChargeLimit
}

func quotaStatus(utilization float64) domain.QuotaStatus {
	if utilization >= 1.0 {
		return domain.QuotaStatusExhausted
	}
	if utilization > 0.8 {
		return domain.QuotaStatusWarning
	}
	return domain.QuotaStatusAvailable
}
