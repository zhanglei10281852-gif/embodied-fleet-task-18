package handover

import (
	"context"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
)

// Service manages maintenance handovers that delineate responsibility boundaries.
type Service struct {
	handovers store.HandoverRepo
	audit     *audit.Recorder
	clock     apperr.Clock
	logger    *apperr.Logger
}

// Deps bundles the dependencies for the handover Service.
type Deps struct {
	Handovers store.HandoverRepo
	Audit     *audit.Recorder
	Clock     apperr.Clock
	Logger    *apperr.Logger
}

// New creates a handover Service.
func New(deps Deps) *Service {
	return &Service{
		handovers: deps.Handovers,
		audit:     deps.Audit,
		clock:     deps.Clock,
		logger:    deps.Logger,
	}
}

// CreateRequest defines the input for creating a handover document.
type CreateRequest struct {
	EntityType  domain.EntityType
	EntityID    string
	FromParty   domain.PartyRole
	ToParty     domain.PartyRole
	Action      string
	DocumentRef string
	Notes       string
	RequestID   string
}

// Create generates a new handover document in the pending state.
func (s *Service) Create(ctx context.Context, req CreateRequest) (*domain.MaintenanceHandover, error) {
	if err := validateCreate(req); err != nil {
		return nil, err
	}
	now := s.clock.Now()
	h := &domain.MaintenanceHandover{
		ID:          newUUID(),
		EntityType:  req.EntityType,
		EntityID:    req.EntityID,
		FromParty:   req.FromParty,
		ToParty:     req.ToParty,
		Action:      req.Action,
		DocumentRef: req.DocumentRef,
		Status:      domain.HandoverStatusPending,
		Notes:       req.Notes,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.handovers.CreateHandover(ctx, h); err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "create handover failed", err)
	}
	_ = s.audit.Record(ctx, audit.Entry{
		Actor:      string(req.FromParty),
		Action:     "create_handover",
		EntityType: req.EntityType,
		EntityID:   req.EntityID,
		After:      h,
		RequestID:  req.RequestID,
	})
	return h, nil
}

// Get retrieves a single handover document by ID.
func (s *Service) Get(ctx context.Context, id string) (*domain.MaintenanceHandover, error) {
	h, err := s.handovers.GetHandover(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return nil, apperr.NotFound("handover_document", id)
		}
		return nil, apperr.Wrap(apperr.CodeInternal, "get handover failed", err)
	}
	return h, nil
}

// List returns a paginated list of maintenance handovers.
func (s *Service) List(ctx context.Context, q domain.PageQuery) (domain.PageResult[*domain.MaintenanceHandover], error) {
	if err := q.Validate(100); err != nil {
		return domain.PageResult[*domain.MaintenanceHandover]{}, apperr.ValidationFailed(err.Error())
	}
	return s.handovers.ListHandovers(ctx, q)
}

// Confirm transitions a handover document from pending to confirmed.
func (s *Service) Confirm(ctx context.Context, id, actor, requestID string) error {
	h, err := s.handovers.GetHandover(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("handover_document", id)
		}
		return apperr.Wrap(apperr.CodeInternal, "get handover failed", err)
	}
	if h.Status != domain.HandoverStatusPending {
		return apperr.InvalidTransition("handover_document", string(h.Status), string(domain.HandoverStatusConfirmed))
	}
	affected, err := s.handovers.UpdateHandoverStatus(ctx, id, domain.HandoverStatusConfirmed, h.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "confirm handover failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("handover_document", id, h.Version)
	}
	_ = s.audit.RecordTransition(ctx, actor, "confirm_handover", id,
		domain.EntityType("handover"), string(h.Status), string(domain.HandoverStatusConfirmed), requestID)
	return nil
}

// Reject transitions a handover document from pending to rejected.
func (s *Service) Reject(ctx context.Context, id, actor, requestID string) error {
	h, err := s.handovers.GetHandover(ctx, id)
	if err != nil {
		if domain.IsNotFound(err) {
			return apperr.NotFound("handover_document", id)
		}
		return apperr.Wrap(apperr.CodeInternal, "get handover failed", err)
	}
	if h.Status != domain.HandoverStatusPending {
		return apperr.InvalidTransition("handover_document", string(h.Status), string(domain.HandoverStatusRejected))
	}
	affected, err := s.handovers.UpdateHandoverStatus(ctx, id, domain.HandoverStatusRejected, h.Version)
	if err != nil {
		return apperr.Wrap(apperr.CodeInternal, "reject handover failed", err)
	}
	if affected == 0 {
		return apperr.Conflict("handover_document", id, h.Version)
	}
	_ = s.audit.RecordTransition(ctx, actor, "reject_handover", id,
		domain.EntityType("handover"), string(h.Status), string(domain.HandoverStatusRejected), requestID)
	return nil
}

// ListByEntity returns all maintenance handovers for a specific entity.
func (s *Service) ListByEntity(ctx context.Context, et domain.EntityType, entityID string) ([]*domain.MaintenanceHandover, error) {
	return s.handovers.ListHandoversByEntity(ctx, et, entityID)
}

func validateCreate(req CreateRequest) error {
	if req.EntityID == "" {
		return apperr.ValidationFailed("entity_id is required")
	}
	if req.FromParty == "" {
		return apperr.ValidationFailed("from_party is required")
	}
	if req.ToParty == "" {
		return apperr.ValidationFailed("to_party is required")
	}
	if req.Action == "" {
		return apperr.ValidationFailed("action is required")
	}
	return nil
}
