package assignment

import (
	"context"
	"fmt"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

// Service orchestrates robot-assignment creation, claiming, reporting, and preemption.
type Service struct {
	tasks        store.AssignmentRepo
	leases       store.LeaseRepo
	executions   store.ExecutionRepo
	audit        *audit.Recorder
	clock        apperr.Clock
	logger       *apperr.Logger
	sm           *domain.StateMachine
	leaseTimeout time.Duration
}

// Deps bundles the dependencies for the assignment Service.
type Deps struct {
	Tasks        store.AssignmentRepo
	Leases       store.LeaseRepo
	Executions   store.ExecutionRepo
	Audit        *audit.Recorder
	Clock        apperr.Clock
	Logger       *apperr.Logger
	LeaseTimeout time.Duration
}

// New creates an assignment Service.
func New(deps Deps) *Service {
	return &Service{
		tasks:        deps.Tasks,
		leases:       deps.Leases,
		executions:   deps.Executions,
		audit:        deps.Audit,
		clock:        deps.Clock,
		logger:       deps.Logger,
		sm:           domain.AssignmentStateMachine(),
		leaseTimeout: deps.LeaseTimeout,
	}
}

// CreateRequest defines the input for creating a robot assignment.
type CreateRequest struct {
	MissionID          string
	RouteReservationID string
	TaskType           domain.AssignmentType
	Location           string
	Priority           int
	RequestID          string
}

// Create generates a new robot assignment in the created state.
func (s *Service) Create(ctx context.Context, req CreateRequest) (*domain.RobotAssignment, error) {
	if err := validateCreate(req); err != nil {
		return nil, err
	}
	now := s.clock.Now()
	t := &domain.RobotAssignment{
		ID:                 newUUID(),
		MissionID:          req.MissionID,
		RouteReservationID: req.RouteReservationID,
		TaskType:           req.TaskType,
		Location:           req.Location,
		Priority:           req.Priority,
		Status:             domain.AssignmentStatusCreated,
		Version:            1,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.tasks.CreateAssignment(ctx, t); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "create robot assignment failed", err)
	}
	_ = s.audit.Record(ctx, audit.Entry{
		Actor:      "system",
		Action:     "create_robot_assignment",
		EntityType: domain.EntityAssignment,
		EntityID:   t.ID,
		After:      t,
		RequestID:  req.RequestID,
	})
	return t, nil
}

// Assign transitions a task from created to assigned, making it claimable.
func (s *Service) Assign(ctx context.Context, id, assignee, actor, requestID string) error {
	t, err := s.tasks.GetAssignment(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("robot_assignment", id)
		}
		return apperr.Wrap(apperr.CodeInternal, "get robot assignment failed", err)
	}
	newStatus, err := s.sm.MustTransition(string(t.Status), string(domain.AssignmentStatusAssigned))
	if err != nil {
		return apperr.InvalidTransition("robot_assignment", string(t.Status), string(domain.AssignmentStatusAssigned))
	}
	affected, err := s.tasks.UpdateAssignmentStatus(ctx, id, domain.AssignmentStatus(newStatus), t.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "assign robot assignment failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("robot_assignment", id, t.Version)
	}
	_ = s.audit.RecordTransition(ctx, actor, "assign_robot_assignment", id,
		domain.EntityAssignment, string(t.Status), newStatus, requestID)
	return nil
}

// ClaimRequest carries the data for an executor claiming a task.
type ClaimRequest struct {
	TaskID     string
	ExecutorID string
	RequestID  string
}

// ClaimResult holds the result of a successful claim.
type ClaimResult struct {
	TaskID    string
	LeaseID   string
	ExpiresAt time.Time
}

// Claim allows an executor to claim an assigned task, creating a time-limited lease.
// This implements the execution-lease pattern: the claim is atomic via optimistic
// locking, and the lease expires after leaseTimeout if not reported.
func (s *Service) Claim(ctx context.Context, req ClaimRequest) (*ClaimResult, error) {
	if req.ExecutorID == "" {
		return nil, apperr.ValidationFailed("executor_id is required")
	}
	t, err := s.tasks.GetAssignment(ctx, req.TaskID)
	if err != nil {
		if domain.IsNotFound(err) {
			return nil, apperr.NotFound("robot_assignment", req.TaskID)
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "get robot assignment failed", err)
	}
	if t.Status != domain.AssignmentStatusAssigned {
		return nil, apperr.InvalidTransition("robot_assignment", string(t.Status), string(domain.AssignmentStatusClaimed))
	}
	now := s.clock.Now()
	expiresAt := now.Add(s.leaseTimeout)
	leaseID := newUUID()
	// Atomically claim the task (optimistic lock).
	affected, err := s.tasks.UpdateAssignmentClaim(ctx, req.TaskID, req.ExecutorID, leaseID,
		expiresAt.Format("2006-01-02T15:04:05Z07:00"), t.Version)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "claim task failed", err)
	}
	if affected == 0 {
		return nil, apperr.Conflict("robot_assignment", req.TaskID, t.Version)
	}
	// Create the lease record.
	lease := &domain.TaskLease{
		ID:         leaseID,
		TaskType:   domain.EntityAssignment,
		TaskID:     req.TaskID,
		ExecutorID: req.ExecutorID,
		ClaimedAt:  now,
		ExpiresAt:  expiresAt,
		Status:     domain.LeaseStatusActive,
		Version:    1,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.leases.CreateLease(ctx, lease); err != nil {
		// Best-effort: clear the claim if lease creation fails.
		_, _ = s.tasks.ClearAssignmentClaim(ctx, req.TaskID, t.Version+1)
		return nil, apperr.Wrap(apperr.CodeInternal, "create lease failed", err)
	}
	_ = s.audit.Record(ctx, audit.Entry{
		Actor:      req.ExecutorID,
		Action:     "claim_robot_assignment",
		EntityType: domain.EntityAssignment,
		EntityID:   req.TaskID,
		After:      map[string]string{"executor": req.ExecutorID, "lease": leaseID},
		RequestID:  req.RequestID,
	})
	return &ClaimResult{
		TaskID:    req.TaskID,
		LeaseID:   leaseID,
		ExpiresAt: expiresAt,
	}, nil
}

// ReportRequest carries the data for an executor reporting task completion.
type ReportRequest struct {
	TaskID     string
	ExecutorID string
	Result     string
	ReportData string
	RequestID  string
}

// Report records the completion of a claimed task and releases the lease.
func (s *Service) Report(ctx context.Context, req ReportRequest) error {
	if req.ExecutorID == "" {
		return apperr.ValidationFailed("executor_id is required")
	}
	t, err := s.tasks.GetAssignment(ctx, req.TaskID)
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("robot_assignment", req.TaskID)
		}
		return apperr.Wrap(apperr.CodeInternal, "get robot assignment failed", err)
	}
	if t.ClaimedBy != req.ExecutorID {
		return apperr.New(apperr.CodeForbidden,
			fmt.Sprintf("task %s is claimed by %s, not %s", req.TaskID, t.ClaimedBy, req.ExecutorID))
	}
	newStatus, err := s.sm.MustTransition(string(t.Status), string(domain.AssignmentStatusCompleted))
	if err != nil {
		return apperr.InvalidTransition("robot_assignment", string(t.Status), string(domain.AssignmentStatusCompleted))
	}
	// Transition task to completed.
	affected, err := s.tasks.UpdateAssignmentStatus(ctx, req.TaskID, domain.AssignmentStatus(newStatus), t.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "report task failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("robot_assignment", req.TaskID, t.Version)
	}
	// Release the lease.
	if t.LeaseID != "" {
		lease, err := s.leases.GetLease(ctx, t.LeaseID)
		if err == nil {
			_, _ = s.leases.ReleaseLease(ctx, lease.ID, lease.Version)
		}
	}
	// Record execution result.
	now := s.clock.Now()
	_ = s.executions.InsertExecution(ctx, &domain.ExecutionRecord{
		ID:           newUUID(),
		TaskType:     domain.EntityAssignment,
		TaskID:       req.TaskID,
		ExecutorID:   req.ExecutorID,
		Result:       req.Result,
		ErrorMessage: "",
		DurationMs:   0,
		Timestamp:    now,
	})
	_ = s.audit.RecordTransition(ctx, req.ExecutorID, "report_robot_assignment", req.TaskID,
		domain.EntityAssignment, string(t.Status), newStatus, req.RequestID)
	return nil
}

// PreemptExpiredClaims finds tasks whose leases have expired and preempts them,
// making them available for re-assignment. This implements the preemption
// and re-distribution business rule.
func (s *Service) PreemptExpiredClaims(ctx context.Context) ([]*PreemptResult, error) {
	nowStr := s.clock.Now().Format("2006-01-02T15:04:05Z07:00")
	expired, err := s.tasks.ListExpiredClaims(ctx, nowStr)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "list expired claims", err)
	}
	var results []*PreemptResult
	for _, t := range expired {
		result, err := s.preemptOne(ctx, t)
		if err != nil {
			s.logger.Error("failed to preempt task", err, apperr.F("task_id", t.ID))
			continue
		}
		results = append(results, result)
	}
	return results, nil
}

// PreemptResult describes the outcome of preempting one task.
type PreemptResult struct {
	TaskID       string
	PrevExecutor string
	Reason       string
}

func (s *Service) preemptOne(ctx context.Context, t *domain.RobotAssignment) (*PreemptResult, error) {
	// Clear the claim and set status to preempted.
	affected, err := s.tasks.ClearAssignmentClaim(ctx, t.ID, t.Version)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, apperr.Conflict("robot_assignment", t.ID, t.Version)
	}
	// Revoke the lease.
	if t.LeaseID != "" {
		lease, err := s.leases.GetLease(ctx, t.LeaseID)
		if err == nil {
			_, _ = s.leases.RevokeLease(ctx, lease.ID, "lease expired", lease.Version)
		}
	}
	_ = s.audit.Record(ctx, audit.Entry{
		Actor:      "scheduler",
		Action:     "preempt_robot_assignment",
		EntityType: domain.EntityAssignment,
		EntityID:   t.ID,
		Before:     map[string]string{"executor": t.ClaimedBy},
		After:      map[string]string{"status": "preempted"},
	})
	return &PreemptResult{
		TaskID:       t.ID,
		PrevExecutor: t.ClaimedBy,
		Reason:       "lease expired",
	}, nil
}

// Reassign transitions a preempted task back to assigned for a new executor.
func (s *Service) Reassign(ctx context.Context, id, actor, requestID string) error {
	t, err := s.tasks.GetAssignment(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("robot_assignment", id)
		}
		return apperr.Wrap(apperr.CodeInternal, "get robot assignment failed", err)
	}
	newStatus, err := s.sm.MustTransition(string(t.Status), string(domain.AssignmentStatusAssigned))
	if err != nil {
		return apperr.InvalidTransition("robot_assignment", string(t.Status), string(domain.AssignmentStatusAssigned))
	}
	affected, err := s.tasks.UpdateAssignmentStatus(ctx, id, domain.AssignmentStatus(newStatus), t.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "reassign failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("robot_assignment", id, t.Version)
	}
	_ = s.audit.RecordTransition(ctx, actor, "reassign_robot_assignment", id,
		domain.EntityAssignment, string(t.Status), newStatus, requestID)
	return nil
}

// Get retrieves a single robot assignment by ID.
func (s *Service) Get(ctx context.Context, id string) (*domain.RobotAssignment, error) {
	t, err := s.tasks.GetAssignment(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return nil, apperr.NotFound("robot_assignment", id)
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "get robot assignment failed", err)
	}
	return t, nil
}

// List returns a paginated list of robot assignments.
func (s *Service) List(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.RobotAssignment], error) {
	if err := q.Validate(100); err != nil {
		return domain.PageResult[*domain.RobotAssignment]{}, apperr.ValidationFailed(err.Error())
	}
	return s.tasks.ListAssignments(ctx, q)
}

// ListClaimable returns tasks available for an executor to claim.
func (s *Service) ListClaimable(ctx context.Context, limit int) ([]*domain.RobotAssignment, error) {
	if limit < 1 || limit > 100 {
		limit = 10
	}
	return s.tasks.ListClaimableTasks(ctx, limit)
}

// BacklogSummary reports assignment counts by status.
type BacklogSummary struct {
	Created    int
	Assigned   int
	Claimed    int
	InProgress int
	Completed  int
	Preempted  int
	Cancelled  int
}

// Backlog returns assignment counts by status.
func (s *Service) Backlog(ctx context.Context) (*BacklogSummary, error) {
	b := &BacklogSummary{}
	var err error
	b.Created, err = s.tasks.CountAssignmentsByStatus(ctx, domain.AssignmentStatusCreated)
	if err != nil {
		return nil, err
	}
	b.Assigned, err = s.tasks.CountAssignmentsByStatus(ctx, domain.AssignmentStatusAssigned)
	if err != nil {
		return nil, err
	}
	b.Claimed, err = s.tasks.CountAssignmentsByStatus(ctx, domain.AssignmentStatusClaimed)
	if err != nil {
		return nil, err
	}
	b.InProgress, err = s.tasks.CountAssignmentsByStatus(ctx, domain.AssignmentStatusInProgress)
	if err != nil {
		return nil, err
	}
	b.Completed, err = s.tasks.CountAssignmentsByStatus(ctx, domain.AssignmentStatusCompleted)
	if err != nil {
		return nil, err
	}
	b.Preempted, err = s.tasks.CountAssignmentsByStatus(ctx, domain.AssignmentStatusPreempted)
	if err != nil {
		return nil, err
	}
	b.Cancelled, err = s.tasks.CountAssignmentsByStatus(ctx, domain.AssignmentStatusCancelled)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func validateCreate(req CreateRequest) error {
	if req.MissionID == "" {
		return apperr.ValidationFailed("mission_id is required")
	}
	if req.TaskType == "" {
		return apperr.ValidationFailed("task_type is required")
	}
	if req.Location == "" {
		return apperr.ValidationFailed("location is required")
	}
	if req.Priority < 1 || req.Priority > 10 {
		req.Priority = 5
	}
	return nil
}
