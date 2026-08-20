package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

// registerRoutes wires all HTTP routes to their handlers. Each route has a
// distinct business semantic — no alias routes or health-check padding.
func (s *Server) registerRoutes(r chi.Router) {
	r.Get("/health", s.HealthHandler())
	r.Get("/ready", s.ReadyHandler())

	r.Route("/api/v1", func(r chi.Router) {
		r.Post("/auth/login", s.Login())

		r.Group(func(r chi.Router) {
			r.Use(s.authenticate)
			r.Get("/auth/me", s.Me())
			r.Post("/auth/logout", s.Logout())

			dispatch := s.requireRoles(domain.RoleDispatcher)
			field := s.requireRoles(domain.RoleEngineer)
			auditOnly := s.requireRoles(domain.RoleAuditor)

			r.Get("/missions", s.ListMissions())
			r.Get("/missions/{id}", s.GetMission())
			r.With(dispatch).Post("/missions", s.SubmitMission())
			r.With(dispatch).Put("/missions/{id}/cancel", s.CancelMission())
			r.With(dispatch).Put("/missions/{id}/priority", s.UpdateMissionPriority())

			r.Get("/route-reservations", s.ListReservations())
			r.Get("/route-reservations/{id}", s.GetReservation())
			r.With(dispatch).Post("/route-reservations", s.CreateReservation())
			r.With(dispatch).Post("/route-reservations/batch", s.BatchAllocateWindows())
			r.With(dispatch).Put("/route-reservations/{id}/release", s.ReleaseReservation())
			r.With(dispatch).Put("/route-reservations/{id}/intervene", s.InterveneReservation())

			r.Get("/mission-runs", s.ListMissionRuns())
			r.Get("/mission-runs/{id}", s.GetMissionRun())
			r.With(dispatch).Post("/mission-runs", s.CreateMissionRun())
			r.With(dispatch).Put("/mission-runs/{id}/assign", s.AssignMissionRun())
			r.With(field).Put("/mission-runs/{id}/start", s.StartMissionRun())
			r.With(field).Put("/mission-runs/{id}/complete", s.CompleteMissionRun())
			r.With(dispatch).Put("/mission-runs/{id}/cancel", s.CancelMissionRun())

			r.Get("/robot-assignments", s.ListAssignments())
			r.Get("/robot-assignments/{id}", s.GetAssignment())
			r.With(dispatch).Post("/robot-assignments", s.CreateAssignment())
			r.With(dispatch).Put("/robot-assignments/{id}/assign", s.AssignAssignment())
			r.With(field).Put("/robot-assignments/{id}/claim", s.ClaimAssignment())
			r.With(field).Put("/robot-assignments/{id}/report", s.ReportAssignment())

			r.Get("/quotas", s.ListQuotas())
			r.Get("/quotas/{id}", s.GetQuota())
			r.With(dispatch).Post("/quotas/reserve", s.ReserveQuota())
			r.With(dispatch).Put("/quotas/{id}/commit", s.CommitQuota())
			r.With(dispatch).Put("/quotas/{id}/release", s.ReleaseQuotaHandler())

			r.Get("/maintenance-handovers", s.ListHandovers())
			r.Get("/maintenance-handovers/{id}", s.GetHandover())
			r.With(field).Post("/maintenance-handovers", s.CreateHandover())
			r.With(field).Put("/maintenance-handovers/{id}/confirm", s.ConfirmHandover())
			r.With(field).Put("/maintenance-handovers/{id}/reject", s.RejectHandover())

			r.With(auditOnly).Get("/audit-logs", s.ListAuditLogs())
			r.With(auditOnly).Get("/escalations", s.ListEscalations())
			r.With(auditOnly).Get("/backlog", s.GetBacklog())
			r.With(auditOnly).Get("/export/reconciliation", s.ExportReconciliation())
			r.With(auditOnly).Get("/executions", s.ListExecutions())
			r.With(auditOnly).Get("/engine/stats", s.EngineStats())
			r.With(s.requireRoles(domain.RoleDispatcher, domain.RoleAuditor)).Post("/intervene", s.ForceIntervene())
		})
	})
}
