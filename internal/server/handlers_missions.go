package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/mission"
)

func (s *Server) SubmitMission() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req SubmitMissionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body: "+err.Error())
			return
		}
		plannedStartAt, err := parseTime(req.PlannedStartAt)
		if err != nil {
			writeValidation(w, r, "invalid planned_start_at: "+err.Error())
			return
		}
		if req.Priority == 0 {
			req.Priority = 5
		}
		result, err := s.declSvc.Submit(r.Context(), mission.SubmitRequest{
			RobotName:            req.RobotName,
			RobotSerial:          req.RobotSerial,
			FleetCode:            req.FleetCode,
			PlannedStartAt:       plannedStartAt,
			RoutePreference:      req.RoutePreference,
			MissionKind:          req.MissionKind,
			EstimatedEnergyUnits: req.EstimatedEnergyUnits,
			ChargeMode:           req.ChargeMode,
			RequestedBy:          actorFromRequest(r),
			RequestingParty:      partyFromRequest(r),
			Priority:             req.Priority,
			IdempotencyKey:       req.IdempotencyKey,
			RequestID:            requestIDFromContext(r.Context()),
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func (s *Server) ListMissions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := parsePageQuery(
			r.URL.Query().Get("page"),
			r.URL.Query().Get("page_size"),
			r.URL.Query().Get("status"),
			map[string]string{
				"requesting_party": r.URL.Query().Get("party"),
				"robot_name":       r.URL.Query().Get("robot"),
			},
		)
		result, err := s.declSvc.List(r.Context(), q)
		if err != nil {
			writeError(w, r, err)
			return
		}
		items := make([]MissionResponse, 0, len(result.Items))
		for _, d := range result.Items {
			items = append(items, declToResponse(d))
		}
		writeJSON(w, http.StatusOK, ListResponse[MissionResponse]{
			Items: items, Meta: pageMeta(result),
		})
	}
}

func (s *Server) GetMission() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		decl, err := s.declSvc.Get(r.Context(), id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, declToResponse(decl))
	}
}

func (s *Server) CancelMission() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		actor := actorFromRequest(r)
		if err := s.declSvc.Cancel(r.Context(), id, actor, requestIDFromContext(r.Context())); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled", "id": id})
	}
}

func (s *Server) UpdateMissionPriority() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req UpdatePriorityRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		actor := actorFromRequest(r)
		if err := s.declSvc.UpdatePriority(r.Context(), id, actor, req.Priority, requestIDFromContext(r.Context())); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "priority_updated", "id": id})
	}
}
