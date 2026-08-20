package routing

import (
	"context"
	"fmt"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

// Service orchestrates route-reservation allocation, release, and escalation.
type Service struct {
	windows      store.ReservationRepo
	decls        store.MissionRepo
	escal        store.EscalationRepo
	handover     store.HandoverRepo
	audit        *audit.Recorder
	clock        apperr.Clock
	logger       *apperr.Logger
	sm           *domain.StateMachine
	leaseTimeout time.Duration
}

// Deps bundles the dependencies for the routing Service.
type Deps struct {
	Windows      store.ReservationRepo
	Missions     store.MissionRepo
	Escalations  store.EscalationRepo
	Handovers    store.HandoverRepo
	Audit        *audit.Recorder
	Clock        apperr.Clock
	Logger       *apperr.Logger
	LeaseTimeout time.Duration
}

// New creates a routing Service.
func New(deps Deps) *Service {
	return &Service{
		windows:      deps.Windows,
		decls:        deps.Missions,
		escal:        deps.Escalations,
		handover:     deps.Handovers,
		audit:        deps.Audit,
		clock:        deps.Clock,
		logger:       deps.Logger,
		sm:           domain.ReservationStateMachine(),
		leaseTimeout: deps.LeaseTimeout,
	}
}

// CreateRequest defines the input for creating a single route reservation.
type CreateRequest struct {
	MissionID        string
	CorridorID       string
	RobotName        string
	EffectiveAt      time.Time
	DeadlineAt       time.Time
	ResponsibleParty domain.PartyRole
	RequestID        string
}

// Create allocates a single route reservation with effective and deadline times.
func (s *Service) Create(ctx context.Context, req CreateRequest) (*domain.RouteReservation, error) {
	if err := validateCreate(req); err != nil {
		return nil, err
	}
	now := s.clock.Now()
	w := &domain.RouteReservation{
		ID:               newUUID(),
		MissionID:        req.MissionID,
		CorridorID:       req.CorridorID,
		RobotName:        req.RobotName,
		EffectiveAt:      req.EffectiveAt,
		DeadlineAt:       req.DeadlineAt,
		ResponsibleParty: req.ResponsibleParty,
		Status:           domain.ReservationStatusAllocated,
		Version:          1,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.windows.CreateReservation(ctx, w); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "create window failed", err)
	}
	_ = s.audit.Record(ctx, audit.Entry{
		Actor:      "system",
		Action:     "allocate_window",
		EntityType: domain.EntityRouteReservation,
		EntityID:   w.ID,
		After:      w,
		RequestID:  req.RequestID,
	})
	return w, nil
}

// BatchItem defines one window in a batch-allocation request.
type BatchItem struct {
	MissionID        string
	CorridorID       string
	RobotName        string
	EffectiveAt      time.Time
	DeadlineAt       time.Time
	ResponsibleParty domain.PartyRole
}

// BatchAllocate creates multiple route reservations in a single transaction.
// If any window fails validation, the entire batch is rolled back.
func (s *Service) BatchAllocate(ctx context.Context, actor string, items []BatchItem, requestID string) ([]*domain.RouteReservation, error) {
	if len(items) == 0 {
		return nil, apperr.ValidationFailed("batch must contain at least one window")
	}
	if len(items) > 50 {
		return nil, apperr.ValidationFailed("batch must not exceed 50 windows")
	}
	for _, item := range items {
		if item.CorridorID == "" || item.RobotName == "" {
			return nil, apperr.ValidationFailed("batch item missing required fields")
		}
		if !item.DeadlineAt.After(item.EffectiveAt) {
			return nil, apperr.ValidationFailed("deadline must be after effective time")
		}
	}
	var result []*domain.RouteReservation
	now := s.clock.Now()
	for _, item := range items {
		w := &domain.RouteReservation{
			ID:               newUUID(),
			MissionID:        item.MissionID,
			CorridorID:       item.CorridorID,
			RobotName:        item.RobotName,
			EffectiveAt:      item.EffectiveAt,
			DeadlineAt:       item.DeadlineAt,
			ResponsibleParty: item.ResponsibleParty,
			Status:           domain.ReservationStatusAllocated,
			Version:          1,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		result = append(result, w)
	}
	if err := s.windows.CreateReservationsBatch(ctx, result); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "batch create failed", err)
	}
	for _, w := range result {
		_ = s.audit.Record(ctx, audit.Entry{
			Actor:      actor,
			Action:     "batch_allocate_window",
			EntityType: domain.EntityRouteReservation,
			EntityID:   w.ID,
			After:      w,
			RequestID:  requestID,
		})
	}
	return result, nil
}

// Get retrieves a single route reservation by ID.
func (s *Service) Get(ctx context.Context, id string) (*domain.RouteReservation, error) {
	w, err := s.windows.GetReservation(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return nil, apperr.NotFound("route_reservation", id)
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "get window failed", err)
	}
	return w, nil
}

// List returns a paginated list of route reservations.
func (s *Service) List(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.RouteReservation], error) {
	if err := q.Validate(100); err != nil {
		return domain.PageResult[*domain.RouteReservation]{}, apperr.ValidationFailed(err.Error())
	}
	return s.windows.ListReservations(ctx, q)
}

// Release transitions a window to released state.
func (s *Service) Release(ctx context.Context, id, actor, requestID string) error {
	w, err := s.windows.GetReservation(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("route_reservation", id)
		}
		return apperr.Wrap(apperr.CodeInternal, "get window failed", err)
	}
	newStatus, err := s.sm.MustTransition(string(w.Status), string(domain.ReservationStatusReleased))
	if err != nil {
		return apperr.InvalidTransition("route_reservation", string(w.Status), string(domain.ReservationStatusReleased))
	}
	affected, err := s.windows.UpdateReservationStatus(ctx, id, domain.ReservationStatus(newStatus), w.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "release window failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("route_reservation", id, w.Version)
	}
	_ = s.audit.RecordTransition(ctx, actor, "release_window", id,
		domain.EntityRouteReservation, string(w.Status), newStatus, requestID)
	return nil
}

// ForceIntervene allows a supervisor to force a window into any legal state.
func (s *Service) ForceIntervene(ctx context.Context, id, actor string, target domain.ReservationStatus, requestID string) error {
	w, err := s.windows.GetReservation(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("route_reservation", id)
		}
		return apperr.Wrap(apperr.CodeInternal, "get window failed", err)
	}
	newStatus, err := s.sm.MustTransition(string(w.Status), string(target))
	if err != nil {
		return apperr.InvalidTransition("route_reservation", string(w.Status), string(target))
	}
	affected, err := s.windows.UpdateReservationStatus(ctx, id, domain.ReservationStatus(newStatus), w.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "force intervene failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("route_reservation", id, w.Version)
	}
	_ = s.audit.RecordTransition(ctx, actor, "force_intervene", id,
		domain.EntityRouteReservation, string(w.Status), newStatus, requestID)
	return nil
}

// EscalateOverdue finds windows past their deadline and escalates responsibility.
// This implements the deadline-window + escalation-level-up business rule:
// an overdue responsible party is automatically promoted one level and a new
// responsible party takes over.
func (s *Service) EscalateOverdue(ctx context.Context) ([]*domain.EscalationResult, error) {
	now := s.clock.Now()
	nowStr := now.Format("2006-01-02T15:04:05Z07:00")
	expired, err := s.windows.ListExpiredWindows(ctx, nowStr)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "list expired windows", err)
	}
	var results []*domain.EscalationResult
	for _, w := range expired {
		result, err := s.escalateOne(ctx, w, now)
		if err != nil {
			s.logger.Error("failed to escalate window", err,
				apperr.F("window_id", w.ID))
			continue
		}
		results = append(results, result)
	}
	return results, nil
}

// escalateOne escalates a single overdue window.
func (s *Service) escalateOne(ctx context.Context, w *domain.RouteReservation, now time.Time) (*domain.EscalationResult, error) {
	newLevel := w.EscalationLevel + 1
	newParty := escalateParty(w.ResponsibleParty)
	newAssignee := fmt.Sprintf("level-%d-assignee", newLevel)
	affected, err := s.windows.UpdateReservationAssignedTo(ctx, w.ID, newAssignee, newLevel, w.Version)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, apperr.Conflict("route_reservation", w.ID, w.Version)
	}
	rec := &domain.EscalationRecord{
		ID:         newUUID(),
		EntityType: domain.EntityRouteReservation,
		EntityID:   w.ID,
		FromLevel:  w.EscalationLevel,
		ToLevel:    newLevel,
		Reason:     fmt.Sprintf("deadline exceeded at %s", now.Format(time.RFC3339)),
		Timestamp:  now,
	}
	_ = s.escal.InsertEscalation(ctx, rec)
	_ = s.audit.Record(ctx, audit.Entry{
		Actor:      "scheduler",
		Action:     "escalate_window",
		EntityType: domain.EntityRouteReservation,
		EntityID:   w.ID,
		Before:     map[string]any{"level": w.EscalationLevel, "party": string(w.ResponsibleParty)},
		After:      map[string]any{"level": newLevel, "party": string(newParty)},
	})
	return &domain.EscalationResult{
		ReservationID: w.ID,
		FromLevel:     w.EscalationLevel,
		ToLevel:       newLevel,
		NewParty:      newParty,
		NewAssignee:   newAssignee,
	}, nil
}

// escalateParty promotes the responsible party one level up the chain.
func escalateParty(current domain.PartyRole) domain.PartyRole {
	switch current {
	case domain.PartyOperationsDispatch:
		return domain.PartyFieldEngineering
	case domain.PartyFieldEngineering:
		return domain.PartyRobotOperations
	case domain.PartyRobotOperations:
		return domain.PartySafetyAudit
	default:
		return domain.PartySafetyAudit
	}
}

// ActivateEffective transitions windows that have entered their effective period.
func (s *Service) ActivateEffective(ctx context.Context) (int, error) {
	now := s.clock.Now()
	allocated, err := s.windows.ListReservationsByStatus(ctx, domain.ReservationStatusAllocated)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, w := range allocated {
		if !now.Before(w.EffectiveAt) && now.Before(w.DeadlineAt) {
			affected, err := s.windows.UpdateReservationStatus(ctx, w.ID, domain.ReservationStatusEffective, w.Version)
			if err != nil {
				continue
			}
			if affected > 0 {
				count++
			}
		}
	}
	if count > 0 && ctx.Err() == nil {
		return 0, nil
	}
	return count, nil
}

// BacklogSummary reports window counts by status.
type BacklogSummary struct {
	Allocated int
	Effective int
	Occupied  int
	Released  int
	Expired   int
	Escalated int
	Cancelled int
}

// Backlog returns window counts by status.
func (s *Service) Backlog(ctx context.Context) (*BacklogSummary, error) {
	b := &BacklogSummary{}
	var err error
	b.Allocated, err = s.windows.CountReservationsByStatus(ctx, domain.ReservationStatusAllocated)
	if err != nil {
		return nil, err
	}
	b.Effective, err = s.windows.CountReservationsByStatus(ctx, domain.ReservationStatusEffective)
	if err != nil {
		return nil, err
	}
	b.Occupied, err = s.windows.CountReservationsByStatus(ctx, domain.ReservationStatusOccupied)
	if err != nil {
		return nil, err
	}
	b.Released, err = s.windows.CountReservationsByStatus(ctx, domain.ReservationStatusReleased)
	if err != nil {
		return nil, err
	}
	b.Expired, err = s.windows.CountReservationsByStatus(ctx, domain.ReservationStatusExpired)
	if err != nil {
		return nil, err
	}
	b.Escalated, err = s.windows.CountReservationsByStatus(ctx, domain.ReservationStatusEscalated)
	if err != nil {
		return nil, err
	}
	b.Cancelled, err = s.windows.CountReservationsByStatus(ctx, domain.ReservationStatusCancelled)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ListExpired returns all currently expired windows.
func (s *Service) ListExpired(ctx context.Context) ([]*domain.RouteReservation, error) {
	nowStr := s.clock.Now().Format("2006-01-02T15:04:05Z07:00")
	return s.windows.ListExpiredWindows(ctx, nowStr)
}

func validateCreate(req CreateRequest) error {
	if req.CorridorID == "" {
		return apperr.ValidationFailed("corridor_id is required")
	}
	if req.RobotName == "" {
		return apperr.ValidationFailed("robot_name is required")
	}
	if !req.DeadlineAt.After(req.EffectiveAt) {
		return apperr.ValidationFailed("deadline_at must be after effective_at")
	}
	return nil
}
