package missionrun

import (
	"context"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

// Service orchestrates mission-run lifecycle and status transitions.
type Service struct {
	orders store.MissionRunRepo
	audit  *audit.Recorder
	clock  apperr.Clock
	logger *apperr.Logger
	sm     *domain.StateMachine
}

// Deps bundles the dependencies for the mission-run Service.
type Deps struct {
	Orders store.MissionRunRepo
	Audit  *audit.Recorder
	Clock  apperr.Clock
	Logger *apperr.Logger
}

// New creates a mission-run Service.
func New(deps Deps) *Service {
	return &Service{
		orders: deps.Orders,
		audit:  deps.Audit,
		clock:  deps.Clock,
		logger: deps.Logger,
		sm:     domain.MissionRunStateMachine(),
	}
}

// CreateRequest defines the input for creating a mission run.
type CreateRequest struct {
	MissionID          string
	RouteReservationID string
	RunType            domain.MissionRunType
	MissionKind        string
	PlannedUnits       int
	AssignedTo         string
	RequestID          string
}

// Create generates a new mission run with the created state.
func (s *Service) Create(ctx context.Context, req CreateRequest) (*domain.MissionRun, error) {
	if err := validateCreate(req); err != nil {
		return nil, err
	}
	now := s.clock.Now()
	wo := &domain.MissionRun{
		ID:                 newUUID(),
		MissionID:          req.MissionID,
		RouteReservationID: req.RouteReservationID,
		RunType:            req.RunType,
		MissionKind:        req.MissionKind,
		PlannedUnits:       req.PlannedUnits,
		AssignedTo:         req.AssignedTo,
		Status:             domain.RunStatusCreated,
		Version:            1,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.orders.CreateMissionRun(ctx, wo); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "create mission run failed", err)
	}
	_ = s.audit.Record(ctx, audit.Entry{
		Actor:      "system",
		Action:     "create_mission_run",
		EntityType: domain.EntityMissionRun,
		EntityID:   wo.ID,
		After:      wo,
		RequestID:  req.RequestID,
	})
	return wo, nil
}

// Get retrieves a single mission run by ID.
func (s *Service) Get(ctx context.Context, id string) (*domain.MissionRun, error) {
	wo, err := s.orders.GetMissionRun(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return nil, apperr.NotFound("mission_run", id)
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "get mission run failed", err)
	}
	return wo, nil
}

// List returns a paginated list of mission runs.
func (s *Service) List(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.MissionRun], error) {
	if err := q.Validate(100); err != nil {
		return domain.PageResult[*domain.MissionRun]{}, apperr.ValidationFailed(err.Error())
	}
	return s.orders.ListMissionRuns(ctx, q)
}

// Assign transitions a mission run from created to assigned.
func (s *Service) Assign(ctx context.Context, id, assignee, actor, requestID string) error {
	wo, err := s.orders.GetMissionRun(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("mission_run", id)
		}
		return apperr.Wrap(apperr.CodeInternal, "get mission run failed", err)
	}
	newStatus, err := s.sm.MustTransition(string(wo.Status), string(domain.RunStatusAssigned))
	if err != nil {
		return apperr.InvalidTransition("mission_run", string(wo.Status), string(domain.RunStatusAssigned))
	}
	affected, err := s.orders.UpdateMissionRunStatus(ctx, id, domain.MissionRunStatus(newStatus), wo.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "assign mission run failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("mission_run", id, wo.Version)
	}
	_ = s.audit.RecordTransition(ctx, actor, "assign_mission_run", id,
		domain.EntityMissionRun, string(wo.Status), newStatus, requestID)
	return nil
}

// StartProgress transitions a mission run from assigned to in_progress.
func (s *Service) StartProgress(ctx context.Context, id, actor, requestID string) error {
	wo, err := s.orders.GetMissionRun(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("mission_run", id)
		}
		return apperr.Wrap(apperr.CodeInternal, "get mission run failed", err)
	}
	newStatus, err := s.sm.MustTransition(string(wo.Status), string(domain.RunStatusInProgress))
	if err != nil {
		return apperr.InvalidTransition("mission_run", string(wo.Status), string(domain.RunStatusInProgress))
	}
	affected, err := s.orders.UpdateMissionRunStatus(ctx, id, domain.MissionRunStatus(newStatus), wo.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "start progress failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("mission_run", id, wo.Version)
	}
	_ = s.audit.RecordTransition(ctx, actor, "start_mission_run", id,
		domain.EntityMissionRun, string(wo.Status), newStatus, requestID)
	return nil
}

// CompleteRequest carries the data for completing a mission run.
type CompleteRequest struct {
	ID          string
	ActualUnits int
	Actor       string
	RequestID   string
}

// Complete performs a multi-step write: update actual volume, then transition
// status to completed. If the status update fails (e.g., version conflict),
// the caller can retry the whole operation.
func (s *Service) Complete(ctx context.Context, req CompleteRequest) error {
	wo, err := s.orders.GetMissionRun(ctx, id(req.ID))
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("mission_run", req.ID)
		}
		return apperr.Wrap(apperr.CodeInternal, "get mission run failed", err)
	}
	newStatus, err := s.sm.MustTransition(string(wo.Status), string(domain.RunStatusCompleted))
	if err != nil {
		return apperr.InvalidTransition("mission_run", string(wo.Status), string(domain.RunStatusCompleted))
	}
	// Step 1: update actual volume (optimistic lock on current version).
	if req.ActualUnits > 0 {
		affected, err := s.orders.UpdateActualUnits(ctx, req.ID, req.ActualUnits, wo.Version)
		if err != nil {
			return apperr.Wrap(apperr.CodeInternal, "update volume failed", err)
		}
		if affected == 0 {
			return apperr.Conflict("mission_run", req.ID, wo.Version)
		}
		wo.Version++
	}
	// Step 2: transition status (optimistic lock on incremented version).
	affected, err := s.orders.UpdateMissionRunStatus(ctx, req.ID, domain.MissionRunStatus(newStatus), wo.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "complete mission run failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("mission_run", req.ID, wo.Version)
	}
	_ = s.audit.RecordTransition(ctx, req.Actor, "complete_mission_run", req.ID,
		domain.EntityMissionRun, string(wo.Status), newStatus, req.RequestID)
	return nil
}

// Cancel transitions a mission run to cancelled if the state machine allows it.
func (s *Service) Cancel(ctx context.Context, id, actor, requestID string) error {
	wo, err := s.orders.GetMissionRun(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("mission_run", id)
		}
		return apperr.Wrap(apperr.CodeInternal, "get mission run failed", err)
	}
	newStatus, err := s.sm.MustTransition(string(wo.Status), string(domain.RunStatusCancelled))
	if err != nil {
		return apperr.InvalidTransition("mission_run", string(wo.Status), string(domain.RunStatusCancelled))
	}
	affected, err := s.orders.UpdateMissionRunStatus(ctx, id, domain.MissionRunStatus(newStatus), wo.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "cancel mission run failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("mission_run", id, wo.Version)
	}
	_ = s.audit.RecordTransition(ctx, actor, "cancel_mission_run", id,
		domain.EntityMissionRun, string(wo.Status), newStatus, requestID)
	return nil
}

// BacklogSummary reports mission-run counts by status.
type BacklogSummary struct {
	Created    int
	Assigned   int
	InProgress int
	Completed  int
	Cancelled  int
	Failed     int
}

// Backlog returns mission-run counts by status.
func (s *Service) Backlog(ctx context.Context) (*BacklogSummary, error) {
	b := &BacklogSummary{}
	var err error
	b.Created, err = s.orders.CountMissionRunsByStatus(ctx, domain.RunStatusCreated)
	if err != nil {
		return nil, err
	}
	b.Assigned, err = s.orders.CountMissionRunsByStatus(ctx, domain.RunStatusAssigned)
	if err != nil {
		return nil, err
	}
	b.InProgress, err = s.orders.CountMissionRunsByStatus(ctx, domain.RunStatusInProgress)
	if err != nil {
		return nil, err
	}
	b.Completed, err = s.orders.CountMissionRunsByStatus(ctx, domain.RunStatusCompleted)
	if err != nil {
		return nil, err
	}
	b.Cancelled, err = s.orders.CountMissionRunsByStatus(ctx, domain.RunStatusCancelled)
	if err != nil {
		return nil, err
	}
	b.Failed, err = s.orders.CountMissionRunsByStatus(ctx, domain.RunStatusFailed)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ListByStatus returns all mission runs in a given status.
func (s *Service) ListByStatus(ctx context.Context, status domain.MissionRunStatus) ([]*domain.MissionRun, error) {
	return s.orders.ListMissionRunsByStatus(ctx, status)
}
func validateCreate(req CreateRequest) error {
	if req.MissionID == "" {
		return apperr.ValidationFailed("mission_id is required")
	}
	if req.RunType == "" {
		return apperr.ValidationFailed("run_type is required")
	}
	if req.PlannedUnits < 0 {
		return apperr.ValidationFailed("planned_units must be non-negative")
	}
	return nil
}
