package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/assignment"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func (s *Server) CreateAssignment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateAssignmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		if req.Priority == 0 {
			req.Priority = 5
		}
		task, err := s.taskSvc.Create(r.Context(), assignment.CreateRequest{
			MissionID:          req.MissionID,
			RouteReservationID: req.RouteReservationID,
			TaskType:           domain.AssignmentType(req.TaskType),
			Location:           req.Location,
			Priority:           req.Priority,
			RequestID:          requestIDFromContext(r.Context()),
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, assignmentToResponse(task))
	}
}

func (s *Server) ListAssignments() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := parsePageQuery(
			r.URL.Query().Get("page"),
			r.URL.Query().Get("page_size"),
			r.URL.Query().Get("status"),
			map[string]string{
				"task_type":  r.URL.Query().Get("type"),
				"claimed_by": r.URL.Query().Get("executor"),
			},
		)
		result, err := s.taskSvc.List(r.Context(), q)
		if err != nil {
			writeError(w, r, err)
			return
		}
		items := make([]AssignmentResponse, 0, len(result.Items))
		for _, t := range result.Items {
			items = append(items, assignmentToResponse(t))
		}
		writeJSON(w, http.StatusOK, ListResponse[AssignmentResponse]{
			Items: items, Meta: pageMeta(result),
		})
	}
}

func (s *Server) GetAssignment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		task, err := s.taskSvc.Get(r.Context(), id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, assignmentToResponse(task))
	}
}

func (s *Server) AssignAssignment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct {
			Assignee string `json:"assignee"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		actor := actorFromRequest(r)
		if err := s.taskSvc.Assign(r.Context(), id, req.Assignee, actor, requestIDFromContext(r.Context())); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "assigned", "id": id})
	}
}

func (s *Server) ClaimAssignment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req ClaimTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		result, err := s.taskSvc.Claim(r.Context(), assignment.ClaimRequest{
			TaskID:     id,
			ExecutorID: actorFromRequest(r),
			RequestID:  requestIDFromContext(r.Context()),
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"task_id":    result.TaskID,
			"lease_id":   result.LeaseID,
			"expires_at": result.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
}

func (s *Server) ReportAssignment() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req ReportTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		if err := s.taskSvc.Report(r.Context(), assignment.ReportRequest{
			TaskID:     id,
			ExecutorID: actorFromRequest(r),
			Result:     req.Result,
			ReportData: req.ReportData,
			RequestID:  requestIDFromContext(r.Context()),
		}); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "completed", "id": id})
	}
}
