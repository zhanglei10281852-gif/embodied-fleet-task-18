package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/missionrun"
)

func (s *Server) CreateMissionRun() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateMissionRunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		wo, err := s.orderSvc.Create(r.Context(), missionrun.CreateRequest{
			MissionID:          req.MissionID,
			RouteReservationID: req.RouteReservationID,
			RunType:            domain.MissionRunType(req.RunType),
			MissionKind:        req.MissionKind,
			PlannedUnits:       req.PlannedUnits,
			AssignedTo:         req.AssignedTo,
			RequestID:          requestIDFromContext(r.Context()),
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, workOrderToResponse(wo))
	}
}

func (s *Server) ListMissionRuns() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := parsePageQuery(
			r.URL.Query().Get("page"),
			r.URL.Query().Get("page_size"),
			r.URL.Query().Get("status"),
			map[string]string{
				"run_type":   r.URL.Query().Get("type"),
				"mission_id": r.URL.Query().Get("mission"),
			},
		)
		result, err := s.orderSvc.List(r.Context(), q)
		if err != nil {
			writeError(w, r, err)
			return
		}
		items := make([]MissionRunResponse, 0, len(result.Items))
		for _, wo := range result.Items {
			items = append(items, workOrderToResponse(wo))
		}
		writeJSON(w, http.StatusOK, ListResponse[MissionRunResponse]{
			Items: items, Meta: pageMeta(result),
		})
	}
}

func (s *Server) GetMissionRun() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		wo, err := s.orderSvc.Get(r.Context(), id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, workOrderToResponse(wo))
	}
}

func (s *Server) AssignMissionRun() http.HandlerFunc {
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
		if err := s.orderSvc.Assign(r.Context(), id, req.Assignee, actor, requestIDFromContext(r.Context())); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "assigned", "id": id})
	}
}

func (s *Server) StartMissionRun() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		actor := actorFromRequest(r)
		if err := s.orderSvc.StartProgress(r.Context(), id, actor, requestIDFromContext(r.Context())); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "in_progress", "id": id})
	}
}

func (s *Server) CompleteMissionRun() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req CompleteMissionRunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		actor := actorFromRequest(r)
		if err := s.orderSvc.Complete(r.Context(), missionrun.CompleteRequest{
			ID:          id,
			ActualUnits: req.ActualUnits,
			Actor:       actor,
			RequestID:   requestIDFromContext(r.Context()),
		}); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "completed", "id": id})
	}
}

func (s *Server) CancelMissionRun() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		actor := actorFromRequest(r)
		if err := s.orderSvc.Cancel(r.Context(), id, actor, requestIDFromContext(r.Context())); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "id": id})
	}
}
