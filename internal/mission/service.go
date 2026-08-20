package mission

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

// Service orchestrates mission-request submission, retrieval, and lifecycle.
type Service struct {
	deps                store.MissionRepo
	quota               store.QuotaRepo
	idem                store.IdempotencyRepo
	handover            store.HandoverRepo
	audit               *audit.Recorder
	clock               apperr.Clock
	logger              *apperr.Logger
	sm                  *domain.StateMachine
	fastChargeLimit     int
	standardChargeLimit int
}

// Deps bundles the dependencies for Service.
type Deps struct {
	Missions            store.MissionRepo
	Quotas              store.QuotaRepo
	Idempotency         store.IdempotencyRepo
	Handovers           store.HandoverRepo
	Audit               *audit.Recorder
	Clock               apperr.Clock
	Logger              *apperr.Logger
	FastChargeLimit     int
	StandardChargeLimit int
}

// New creates a mission Service.
func New(deps Deps) *Service {
	return &Service{
		deps:                deps.Missions,
		quota:               deps.Quotas,
		idem:                deps.Idempotency,
		handover:            deps.Handovers,
		audit:               deps.Audit,
		clock:               deps.Clock,
		logger:              deps.Logger,
		sm:                  domain.MissionStateMachine(),
		fastChargeLimit:     deps.FastChargeLimit,
		standardChargeLimit: deps.StandardChargeLimit,
	}
}

// SubmitResult is the synchronous reply to a mission submission.
type SubmitResult struct {
	Status        domain.MissionStatus `json:"status"`
	MissionID     string               `json:"mission_id"`
	QueuePosition int                  `json:"queue_position"`
	WaitEstimate  time.Duration        `json:"wait_estimate"`
	QuotaStatus   string               `json:"quota_status"`
	Message       string               `json:"message"`
}

// SubmitRequest carries the data for a new mission request.
type SubmitRequest struct {
	RobotName            string
	RobotSerial          string
	FleetCode            string
	PlannedStartAt       time.Time
	RoutePreference      string
	MissionKind          string
	EstimatedEnergyUnits int
	ChargeMode           string
	RequestedBy          string
	RequestingParty      domain.PartyRole
	Priority             int
	IdempotencyKey       string
	RequestID            string
}

// Submit creates or returns an existing mission, enforcing idempotency,
// quota backpressure, and state-machine initialization.
func (s *Service) Submit(ctx context.Context, req SubmitRequest) (*SubmitResult, error) {
	if err := validateSubmit(req); err != nil {
		return nil, err
	}

	if req.IdempotencyKey != "" {
		cached, err := s.tryIdempotency(ctx, req.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if cached != nil {
			return cached, nil
		}
	}

	now := s.clock.Now()
	dateStr := now.Format("2006-01-02")

	quotaType := domain.QuotaTypeStandardCharge
	limit := s.standardChargeLimit
	if req.ChargeMode == "fast" || req.MissionKind == "emergency" {
		quotaType = domain.QuotaTypeFastCharge
		limit = s.fastChargeLimit
	}

	reserved, quotaID, err := s.reserveQuotaWithRetry(ctx, quotaType, dateStr, limit, req.EstimatedEnergyUnits, 25)
	if err != nil {
		return nil, err
	}
	if !reserved {
		s.logger.Warn("quota exceeded, rejecting mission",
			apperr.F("robot", req.RobotName),
			apperr.F("requested", req.EstimatedEnergyUnits))
		result := &SubmitResult{
			Status:      domain.DeclStatusRejected,
			QuotaStatus: "exhausted",
			Message:     fmt.Sprintf("daily %s quota exhausted, requested %d", quotaType, req.EstimatedEnergyUnits),
		}
		s.cacheIdempotency(ctx, req.IdempotencyKey, result)
		return result, nil
	}

	q, err := s.quota.GetOrCreateQuota(ctx, quotaType, dateStr, limit)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "quota re-read failed", err)
	}
	utilization := q.Utilization()
	status := domain.DeclStatusAccepted
	queuePos := 0
	waitEstimate := time.Duration(0)
	if utilization > 0.8 {
		status = domain.DeclStatusQueued
		count, _ := s.deps.CountMissionsByStatus(ctx, domain.DeclStatusQueued)
		queuePos = count + 1
		waitEstimate = time.Duration(queuePos) * 10 * time.Minute
	}

	decl := &domain.MissionRequest{
		ID:                   newUUID(),
		RobotName:            req.RobotName,
		RobotSerial:          req.RobotSerial,
		FleetCode:            req.FleetCode,
		PlannedStartAt:       req.PlannedStartAt,
		RoutePreference:      req.RoutePreference,
		MissionKind:          req.MissionKind,
		EstimatedEnergyUnits: req.EstimatedEnergyUnits,
		ChargeMode:           req.ChargeMode,
		RequestedBy:          req.RequestedBy,
		RequestingParty:      req.RequestingParty,
		Status:               status,
		Priority:             req.Priority,
		QueuePosition:        queuePos,
		IdempotencyKey:       req.IdempotencyKey,
		Version:              1,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if err := s.deps.CreateMission(ctx, decl); err != nil {
		_, _ = s.quota.ReleaseQuota(ctx, quotaID, req.EstimatedEnergyUnits, q.Version+1)
		return nil, apperr.Wrap(apperr.CodeInternal, "create mission failed", err)
	}

	handover := &domain.MaintenanceHandover{
		ID:         newUUID(),
		EntityType: domain.EntityMission,
		EntityID:   decl.ID,
		FromParty:  domain.PartyOperationsDispatch,
		ToParty:    domain.PartyFieldEngineering,
		Action:     "submit_mission",
		Status:     domain.HandoverStatusPending,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.handover.CreateHandover(ctx, handover); err != nil {
		s.logger.Error("failed to create handover document", err)
	}

	_ = s.audit.Record(ctx, audit.Entry{
		Actor:      req.RequestedBy,
		Action:     "submit_mission",
		EntityType: domain.EntityMission,
		EntityID:   decl.ID,
		After:      decl,
		RequestID:  req.RequestID,
	})

	result := &SubmitResult{
		Status:        status,
		MissionID:     decl.ID,
		QueuePosition: queuePos,
		WaitEstimate:  waitEstimate,
		QuotaStatus:   string(quotaStatus(utilization)),
		Message:       messageForStatus(status, queuePos),
	}
	s.cacheIdempotency(ctx, req.IdempotencyKey, result)
	return result, nil
}

// Get retrieves a single mission by ID.
func (s *Service) Get(ctx context.Context, id string) (*domain.MissionRequest, error) {
	decl, err := s.deps.GetMission(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return nil, apperr.NotFound("mission", id)
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "get mission failed", err)
	}
	return decl, nil
}

// List returns a paginated list of missions.
func (s *Service) List(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.MissionRequest], error) {
	if err := q.Validate(100); err != nil {
		return domain.PageResult[*domain.MissionRequest]{}, apperr.ValidationFailed(err.Error())
	}
	return s.deps.ListMissions(ctx, q)
}

// Cancel transitions a mission to cancelled if the state machine allows it.
func (s *Service) Cancel(ctx context.Context, id, actor, requestID string) error {
	decl, err := s.deps.GetMission(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("mission", id)
		}
		return apperr.Wrap(apperr.CodeInternal, "get mission failed", err)
	}
	newStatus, err := s.sm.MustTransition(string(decl.Status), string(domain.DeclStatusCancelled))
	if err != nil {
		return apperr.InvalidTransition("mission", string(decl.Status), string(domain.DeclStatusCancelled))
	}
	affected, err := s.deps.UpdateMissionStatus(ctx, id, domain.MissionStatus(newStatus), decl.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "update mission failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("mission", id, decl.Version)
	}
	_ = s.audit.RecordTransition(ctx, actor, "cancel_mission", id,
		domain.EntityMission, string(decl.Status), newStatus, requestID)
	return nil
}

// UpdatePriority changes the priority of a mission (forced intervention).
func (s *Service) UpdatePriority(ctx context.Context, id, actor string, priority int, requestID string) error {
	decl, err := s.deps.GetMission(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("mission", id)
		}
		return apperr.Wrap(apperr.CodeInternal, "get mission failed", err)
	}
	if priority < 1 || priority > 10 {
		return apperr.ValidationFailed("priority must be 1-10")
	}
	affected, err := s.deps.UpdatePriority(ctx, id, priority, decl.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "update priority failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("mission", id, decl.Version)
	}
	_ = s.audit.RecordTransition(ctx, actor, "update_priority", id,
		domain.EntityMission, fmt.Sprintf("p%d", decl.Priority), fmt.Sprintf("p%d", priority), requestID)
	return nil
}

// BacklogSummary reports counts by status for the backlog view.
type BacklogSummary struct {
	Submitted  int
	Reviewing  int
	Accepted   int
	Queued     int
	Scheduled  int
	Processing int
	Completed  int
	Rejected   int
	Cancelled  int
}

// Backlog returns a summary of mission counts by status.
func (s *Service) Backlog(ctx context.Context) (*BacklogSummary, error) {
	b := &BacklogSummary{}
	var err error
	b.Submitted, err = s.deps.CountMissionsByStatus(ctx, domain.DeclStatusSubmitted)
	if err != nil {
		return nil, err
	}
	b.Reviewing, err = s.deps.CountMissionsByStatus(ctx, domain.DeclStatusReviewing)
	if err != nil {
		return nil, err
	}
	b.Accepted, err = s.deps.CountMissionsByStatus(ctx, domain.DeclStatusAccepted)
	if err != nil {
		return nil, err
	}
	b.Queued, err = s.deps.CountMissionsByStatus(ctx, domain.DeclStatusQueued)
	if err != nil {
		return nil, err
	}
	b.Scheduled, err = s.deps.CountMissionsByStatus(ctx, domain.DeclStatusScheduled)
	if err != nil {
		return nil, err
	}
	b.Processing, err = s.deps.CountMissionsByStatus(ctx, domain.DeclStatusProcessing)
	if err != nil {
		return nil, err
	}
	b.Completed, err = s.deps.CountMissionsByStatus(ctx, domain.DeclStatusCompleted)
	if err != nil {
		return nil, err
	}
	b.Rejected, err = s.deps.CountMissionsByStatus(ctx, domain.DeclStatusRejected)
	if err != nil {
		return nil, err
	}
	b.Cancelled, err = s.deps.CountMissionsByStatus(ctx, domain.DeclStatusCancelled)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ListQueued returns all missions currently in the queue.
func (s *Service) ListQueued(ctx context.Context) ([]*domain.MissionRequest, error) {
	return s.deps.ListMissionsByStatus(ctx, domain.DeclStatusQueued)
}

// AdvanceQueued promotes the highest-priority queued mission to scheduled.
func (s *Service) AdvanceQueued(ctx context.Context) (*domain.MissionRequest, error) {
	queued, err := s.deps.ListMissionsByStatus(ctx, domain.DeclStatusQueued)
	if err != nil {
		return nil, err
	}
	if len(queued) == 0 {
		return nil, nil
	}
	top := queued[0]
	newStatus, err := s.sm.MustTransition(string(top.Status), string(domain.DeclStatusScheduled))
	if err != nil {
		return nil, apperr.InvalidTransition("mission", string(top.Status), string(domain.DeclStatusScheduled))
	}
	affected, err := s.deps.UpdateMissionStatus(ctx, top.ID, domain.MissionStatus(newStatus), top.Version)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "advance queued failed", err)
	}
	if affected == 0 {
		return nil, apperr.Conflict("mission", top.ID, top.Version)
	}
	top.Status = domain.DeclStatusScheduled
	top.Version++
	return top, nil
}

func validateSubmit(req SubmitRequest) error {
	if req.RobotName == "" {
		return apperr.ValidationFailed("robot_name is required")
	}
	if req.RobotSerial == "" {
		return apperr.ValidationFailed("robot_serial is required")
	}
	if req.FleetCode == "" {
		return apperr.ValidationFailed("fleet_code is required")
	}
	if req.PlannedStartAt.IsZero() {
		return apperr.ValidationFailed("planned_start_at is required")
	}
	if req.EstimatedEnergyUnits <= 0 {
		return apperr.ValidationFailed("estimated_energy_units must be positive")
	}
	if req.ChargeMode != "fast" && req.ChargeMode != "standard" {
		return apperr.ValidationFailed("charge_mode must be fast or standard")
	}
	if req.RequestedBy == "" {
		return apperr.ValidationFailed("requested_by is required")
	}
	if req.Priority < 1 || req.Priority > 10 {
		req.Priority = 5
	}
	return nil
}

func (s *Service) tryIdempotency(ctx context.Context, key string) (*SubmitResult, error) {
	rec, err := s.idem.GetIdempotency(ctx, key)
	if err != nil {
		if domain.IsNotFound(err) {
			return nil, nil
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "idempotency lookup failed", err)
	}
	if s.clock.Now().After(rec.ExpiresAt) {
		return nil, nil
	}
	var result SubmitResult
	if err := json.Unmarshal([]byte(rec.ResponseBody), &result); err != nil {
		return nil, nil
	}
	return &result, nil
}

func (s *Service) cacheIdempotency(ctx context.Context, key string, result *SubmitResult) {
	if key == "" {
		return
	}
	body, _ := json.Marshal(result)
	now := s.clock.Now()
	_ = s.idem.InsertIdempotency(ctx, &domain.IdempotencyRecord{
		Key:            key,
		ResponseBody:   string(body),
		ResponseStatus: 200,
		CreatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
	})
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

func messageForStatus(status domain.MissionStatus, queuePos int) string {
	switch status {
	case domain.DeclStatusAccepted:
		return "mission accepted, processing will proceed at scheduled time"
	case domain.DeclStatusQueued:
		return fmt.Sprintf("mission queued at position %d due to high utilization", queuePos)
	case domain.DeclStatusRejected:
		return "mission rejected: quota exceeded"
	default:
		return string(status)
	}
}
