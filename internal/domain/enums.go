package domain

// PartyRole identifies one of the four collaborating parties.
type PartyRole string

const (
	PartyOperationsDispatch PartyRole = "operations_dispatch"
	PartyFieldEngineering   PartyRole = "field_engineering"
	PartyRobotOperations    PartyRole = "robot_operations"
	PartySafetyAudit        PartyRole = "safety_audit"
)

// AllParties returns the four collaborating party roles.
func AllParties() []PartyRole {
	return []PartyRole{PartyOperationsDispatch, PartyFieldEngineering, PartyRobotOperations, PartySafetyAudit}
}

// MissionStatus enumerates mission-request lifecycle states.
type MissionStatus string

const (
	DeclStatusDraft      MissionStatus = "draft"
	DeclStatusSubmitted  MissionStatus = "submitted"
	DeclStatusReviewing  MissionStatus = "reviewing"
	DeclStatusAccepted   MissionStatus = "accepted"
	DeclStatusQueued     MissionStatus = "queued"
	DeclStatusScheduled  MissionStatus = "scheduled"
	DeclStatusProcessing MissionStatus = "processing"
	DeclStatusCompleted  MissionStatus = "completed"
	DeclStatusRejected   MissionStatus = "rejected"
	DeclStatusCancelled  MissionStatus = "cancelled"
)

// ReservationStatus enumerates route-reservation lifecycle states.
type ReservationStatus string

const (
	ReservationStatusAllocated ReservationStatus = "allocated"
	ReservationStatusEffective ReservationStatus = "effective"
	ReservationStatusOccupied  ReservationStatus = "occupied"
	ReservationStatusReleased  ReservationStatus = "released"
	ReservationStatusExpired   ReservationStatus = "expired"
	ReservationStatusEscalated ReservationStatus = "escalated"
	ReservationStatusCancelled ReservationStatus = "cancelled"
)

// MissionRunStatus enumerates mission-run lifecycle states.
type MissionRunStatus string

const (
	RunStatusCreated    MissionRunStatus = "created"
	RunStatusAssigned   MissionRunStatus = "assigned"
	RunStatusInProgress MissionRunStatus = "in_progress"
	RunStatusCompleted  MissionRunStatus = "completed"
	RunStatusCancelled  MissionRunStatus = "cancelled"
	RunStatusFailed     MissionRunStatus = "failed"
)

// AssignmentStatus enumerates robot-assignment lifecycle states.
type AssignmentStatus string

const (
	AssignmentStatusCreated    AssignmentStatus = "created"
	AssignmentStatusAssigned   AssignmentStatus = "assigned"
	AssignmentStatusClaimed    AssignmentStatus = "claimed"
	AssignmentStatusInProgress AssignmentStatus = "in_progress"
	AssignmentStatusCompleted  AssignmentStatus = "completed"
	AssignmentStatusPreempted  AssignmentStatus = "preempted"
	AssignmentStatusCancelled  AssignmentStatus = "cancelled"
)

// QuotaStatus enumerates quota states.
type QuotaStatus string

const (
	QuotaStatusAvailable QuotaStatus = "available"
	QuotaStatusWarning   QuotaStatus = "warning"
	QuotaStatusExhausted QuotaStatus = "exhausted"
	QuotaStatusReserved  QuotaStatus = "reserved"
	QuotaStatusCompleted QuotaStatus = "completed"
)

// QuotaType identifies a quota category.
type QuotaType string

const (
	QuotaTypeFastCharge     QuotaType = "fast_charge"
	QuotaTypeStandardCharge QuotaType = "standard_charge"
)

// MissionRunType identifies the operation type.
type MissionRunType string

const (
	RunTypeInspection MissionRunType = "inspection"
	RunTypeTransport  MissionRunType = "transport"
)

// AssignmentType identifies a field assignment variant.
type AssignmentType string

const (
	AssignmentTypeInspection AssignmentType = "inspection"
	AssignmentTypeTransport  AssignmentType = "transport"
	AssignmentTypeRecovery   AssignmentType = "recovery"
)

// EntityType identifies the kind of domain object in audit/handover records.
type EntityType string

const (
	EntityMission          EntityType = "mission"
	EntityRouteReservation EntityType = "route_reservation"
	EntityMissionRun       EntityType = "mission_run"
	EntityAssignment       EntityType = "robot_assignment"
	EntityQuota            EntityType = "quota"
	EntityUser             EntityType = "user"
	EntitySession          EntityType = "session"
)

// HandoverStatus enumerates handover-document states.
type HandoverStatus string

const (
	HandoverStatusPending   HandoverStatus = "pending"
	HandoverStatusConfirmed HandoverStatus = "confirmed"
	HandoverStatusRejected  HandoverStatus = "rejected"
)

// LeaseStatus enumerates task-lease states.
type LeaseStatus string

const (
	LeaseStatusActive   LeaseStatus = "active"
	LeaseStatusReleased LeaseStatus = "released"
	LeaseStatusExpired  LeaseStatus = "expired"
	LeaseStatusRevoked  LeaseStatus = "revoked"
)

// SortOrder for list queries.
type SortOrder string

const (
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)
