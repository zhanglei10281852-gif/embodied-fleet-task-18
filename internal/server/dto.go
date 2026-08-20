package server

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

// --- Request DTOs ---

type SubmitMissionRequest struct {
	RobotName            string `json:"robot_name"`
	RobotSerial          string `json:"robot_serial"`
	FleetCode            string `json:"fleet_code"`
	PlannedStartAt       string `json:"planned_start_at"`
	RoutePreference      string `json:"route_preference"`
	MissionKind          string `json:"mission_kind"`
	EstimatedEnergyUnits int    `json:"estimated_energy_units"`
	ChargeMode           string `json:"charge_mode"`
	RequestedBy          string `json:"requested_by"`
	RequestingParty      string `json:"requesting_party"`
	Priority             int    `json:"priority"`
	IdempotencyKey       string `json:"idempotency_key"`
}

type CreateReservationRequest struct {
	MissionID        string `json:"mission_id"`
	CorridorID       string `json:"corridor_id"`
	RobotName        string `json:"robot_name"`
	EffectiveAt      string `json:"effective_at"`
	DeadlineAt       string `json:"deadline_at"`
	ResponsibleParty string `json:"responsible_party"`
}

type BatchWindowRequest struct {
	Items []CreateReservationRequest `json:"items"`
}

type CreateMissionRunRequest struct {
	MissionID          string `json:"mission_id"`
	RouteReservationID string `json:"route_reservation_id"`
	RunType            string `json:"run_type"`
	MissionKind        string `json:"mission_kind"`
	PlannedUnits       int    `json:"planned_units"`
	AssignedTo         string `json:"assigned_to"`
}

type CompleteMissionRunRequest struct {
	ActualUnits int `json:"actual_units"`
}

type CreateAssignmentRequest struct {
	MissionID          string `json:"mission_id"`
	RouteReservationID string `json:"route_reservation_id"`
	TaskType           string `json:"task_type"`
	Location           string `json:"location"`
	Priority           int    `json:"priority"`
}

type ClaimTaskRequest struct {
	ExecutorID string `json:"executor_id"`
}

type ReportTaskRequest struct {
	ExecutorID string `json:"executor_id"`
	Result     string `json:"result"`
	ReportData string `json:"report_data"`
}

type ReserveQuotaRequest struct {
	QuotaType string `json:"quota_type"`
	Amount    int    `json:"amount"`
}

type CreateHandoverRequest struct {
	EntityType  string `json:"entity_type"`
	EntityID    string `json:"entity_id"`
	FromParty   string `json:"from_party"`
	ToParty     string `json:"to_party"`
	Action      string `json:"action"`
	DocumentRef string `json:"document_ref"`
	Notes       string `json:"notes"`
}

type InterveneRequest struct {
	EntityType  string `json:"entity_type"`
	EntityID    string `json:"entity_id"`
	TargetState string `json:"target_state"`
	Actor       string `json:"actor"`
}

type UpdatePriorityRequest struct {
	Priority int `json:"priority"`
}

// --- Response DTOs ---

type ErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	Details   string `json:"details,omitempty"`
	RequestID string `json:"request_id"`
}

type MissionResponse struct {
	ID                   string `json:"id"`
	RobotName            string `json:"robot_name"`
	RobotSerial          string `json:"robot_serial"`
	FleetCode            string `json:"fleet_code"`
	PlannedStartAt       string `json:"planned_start_at"`
	MissionKind          string `json:"mission_kind"`
	EstimatedEnergyUnits int    `json:"estimated_energy_units"`
	ChargeMode           string `json:"charge_mode"`
	Status               string `json:"status"`
	Priority             int    `json:"priority"`
	QueuePosition        int    `json:"queue_position"`
	Version              int    `json:"version"`
	CreatedAt            string `json:"created_at"`
	UpdatedAt            string `json:"updated_at"`
}

type WindowResponse struct {
	ID               string `json:"id"`
	MissionID        string `json:"mission_id"`
	CorridorID       string `json:"corridor_id"`
	RobotName        string `json:"robot_name"`
	EffectiveAt      string `json:"effective_at"`
	DeadlineAt       string `json:"deadline_at"`
	AssignedTo       string `json:"assigned_to"`
	ResponsibleParty string `json:"responsible_party"`
	EscalationLevel  int    `json:"escalation_level"`
	Status           string `json:"status"`
	Version          int    `json:"version"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type MissionRunResponse struct {
	ID                 string `json:"id"`
	MissionID          string `json:"mission_id"`
	RouteReservationID string `json:"route_reservation_id,omitempty"`
	RunType            string `json:"run_type"`
	MissionKind        string `json:"mission_kind"`
	PlannedUnits       int    `json:"planned_units"`
	ActualUnits        int    `json:"actual_units"`
	AssignedTo         string `json:"assigned_to"`
	Status             string `json:"status"`
	StartedAt          string `json:"started_at,omitempty"`
	CompletedAt        string `json:"completed_at,omitempty"`
	Version            int    `json:"version"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

type AssignmentResponse struct {
	ID                 string `json:"id"`
	MissionID          string `json:"mission_id"`
	RouteReservationID string `json:"route_reservation_id,omitempty"`
	TaskType           string `json:"task_type"`
	Location           string `json:"location"`
	AssignedTo         string `json:"assigned_to"`
	ClaimedBy          string `json:"claimed_by"`
	ClaimExpiresAt     string `json:"claim_expires_at,omitempty"`
	LeaseID            string `json:"lease_id,omitempty"`
	Status             string `json:"status"`
	Priority           int    `json:"priority"`
	Version            int    `json:"version"`
	CreatedAt          string `json:"created_at"`
	UpdatedAt          string `json:"updated_at"`
}

type QuotaResponse struct {
	ID             string  `json:"id"`
	QuotaType      string  `json:"quota_type"`
	PeriodDate     string  `json:"period_date"`
	DailyLimit     int     `json:"daily_limit"`
	UsedAmount     int     `json:"used_amount"`
	ReservedAmount int     `json:"reserved_amount"`
	Available      int     `json:"available"`
	Utilization    float64 `json:"utilization"`
	Status         string  `json:"status"`
	Version        int     `json:"version"`
}

type HandoverResponse struct {
	ID          string `json:"id"`
	EntityType  string `json:"entity_type"`
	EntityID    string `json:"entity_id"`
	FromParty   string `json:"from_party"`
	ToParty     string `json:"to_party"`
	Action      string `json:"action"`
	DocumentRef string `json:"document_ref"`
	Status      string `json:"status"`
	Notes       string `json:"notes"`
	Version     int    `json:"version"`
	CreatedAt   string `json:"created_at"`
}

type AuditLogResponse struct {
	ID          string `json:"id"`
	Actor       string `json:"actor"`
	Action      string `json:"action"`
	EntityType  string `json:"entity_type"`
	EntityID    string `json:"entity_id"`
	BeforeState string `json:"before_state,omitempty"`
	AfterState  string `json:"after_state,omitempty"`
	Timestamp   string `json:"timestamp"`
}

type PageMeta struct {
	Page       int  `json:"page"`
	PageSize   int  `json:"page_size"`
	Total      int  `json:"total"`
	TotalPages int  `json:"total_pages"`
	HasNext    bool `json:"has_next"`
	HasPrev    bool `json:"has_prev"`
}

type ListResponse[T any] struct {
	Items []T      `json:"items"`
	Meta  PageMeta `json:"meta"`
}

// --- Conversion helpers ---

func declToResponse(d *domain.MissionRequest) MissionResponse {
	return MissionResponse{
		ID: d.ID, RobotName: d.RobotName, RobotSerial: d.RobotSerial,
		FleetCode: d.FleetCode, PlannedStartAt: d.PlannedStartAt.Format(time.RFC3339),
		MissionKind: d.MissionKind, EstimatedEnergyUnits: d.EstimatedEnergyUnits, ChargeMode: d.ChargeMode,
		Status: string(d.Status), Priority: d.Priority, QueuePosition: d.QueuePosition,
		Version: d.Version, CreatedAt: d.CreatedAt.Format(time.RFC3339),
		UpdatedAt: d.UpdatedAt.Format(time.RFC3339),
	}
}

func windowToResponse(w *domain.RouteReservation) WindowResponse {
	return WindowResponse{
		ID: w.ID, MissionID: w.MissionID, CorridorID: w.CorridorID,
		RobotName: w.RobotName, EffectiveAt: w.EffectiveAt.Format(time.RFC3339),
		DeadlineAt: w.DeadlineAt.Format(time.RFC3339), AssignedTo: w.AssignedTo,
		ResponsibleParty: string(w.ResponsibleParty), EscalationLevel: w.EscalationLevel,
		Status: string(w.Status), Version: w.Version,
		CreatedAt: w.CreatedAt.Format(time.RFC3339), UpdatedAt: w.UpdatedAt.Format(time.RFC3339),
	}
}

func workOrderToResponse(w *domain.MissionRun) MissionRunResponse {
	resp := MissionRunResponse{
		ID: w.ID, MissionID: w.MissionID,
		RouteReservationID: w.RouteReservationID, RunType: string(w.RunType),
		MissionKind: w.MissionKind, PlannedUnits: w.PlannedUnits,
		ActualUnits: w.ActualUnits, AssignedTo: w.AssignedTo,
		Status: string(w.Status), Version: w.Version,
		CreatedAt: w.CreatedAt.Format(time.RFC3339), UpdatedAt: w.UpdatedAt.Format(time.RFC3339),
	}
	if w.StartedAt != nil {
		resp.StartedAt = w.StartedAt.Format(time.RFC3339)
	}
	if w.CompletedAt != nil {
		resp.CompletedAt = w.CompletedAt.Format(time.RFC3339)
	}
	return resp
}

func assignmentToResponse(t *domain.RobotAssignment) AssignmentResponse {
	resp := AssignmentResponse{
		ID: t.ID, MissionID: t.MissionID,
		RouteReservationID: t.RouteReservationID, TaskType: string(t.TaskType),
		Location: t.Location, AssignedTo: t.AssignedTo, ClaimedBy: t.ClaimedBy,
		LeaseID: t.LeaseID, Status: string(t.Status), Priority: t.Priority,
		Version: t.Version, CreatedAt: t.CreatedAt.Format(time.RFC3339),
		UpdatedAt: t.UpdatedAt.Format(time.RFC3339),
	}
	if t.ClaimExpiresAt != nil {
		resp.ClaimExpiresAt = t.ClaimExpiresAt.Format(time.RFC3339)
	}
	return resp
}

func quotaToResponse(q *domain.Quota) QuotaResponse {
	return QuotaResponse{
		ID: q.ID, QuotaType: string(q.QuotaType), PeriodDate: q.PeriodDate,
		DailyLimit: q.DailyLimit, UsedAmount: q.UsedAmount,
		ReservedAmount: q.ReservedAmount, Available: q.Available(),
		Utilization: q.Utilization(), Status: string(q.Status), Version: q.Version,
	}
}

func handoverToResponse(h *domain.MaintenanceHandover) HandoverResponse {
	return HandoverResponse{
		ID: h.ID, EntityType: string(h.EntityType), EntityID: h.EntityID,
		FromParty: string(h.FromParty), ToParty: string(h.ToParty),
		Action: h.Action, DocumentRef: h.DocumentRef, Status: string(h.Status),
		Notes: h.Notes, Version: h.Version, CreatedAt: h.CreatedAt.Format(time.RFC3339),
	}
}

func auditToResponse(a *domain.AuditLog) AuditLogResponse {
	return AuditLogResponse{
		ID: a.ID, Actor: a.Actor, Action: a.Action,
		EntityType: string(a.EntityType), EntityID: a.EntityID,
		BeforeState: a.BeforeState, AfterState: a.AfterState,
		Timestamp: a.Timestamp.Format(time.RFC3339),
	}
}

func pageMeta[T any](pr domain.PageResult[T]) PageMeta {
	return PageMeta{
		Page: pr.Page, PageSize: pr.PageSize, Total: pr.Total,
		TotalPages: pr.TotalPages, HasNext: pr.HasNext, HasPrev: pr.HasPrev,
	}
}

// parsePageQuery extracts pagination and filter parameters from query strings.
func parsePageQuery(pageStr, pageSizeStr, status string, extraFilters map[string]string) domain.PageQuery {
	q := domain.DefaultPage()
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			q.Page = p
		}
	}
	if pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 {
			q.PageSize = ps
		}
	}
	if status != "" {
		q.Filter["status"] = status
	}
	for k, v := range extraFilters {
		if v != "" {
			q.Filter[k] = v
		}
	}
	return q
}

func parseTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	layouts := []string{time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time: %s", s)
}

func requireNonEmpty(fields map[string]string) error {
	for name, val := range fields {
		if strings.TrimSpace(val) == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}
