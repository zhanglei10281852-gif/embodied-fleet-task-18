package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/routing"
)

func (s *Server) CreateReservation() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateReservationRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		effectiveAt, err := parseTime(req.EffectiveAt)
		if err != nil {
			writeValidation(w, r, "invalid effective_at")
			return
		}
		deadlineAt, err := parseTime(req.DeadlineAt)
		if err != nil {
			writeValidation(w, r, "invalid deadline_at")
			return
		}
		win, err := s.reservationSvc.Create(r.Context(), routing.CreateRequest{
			MissionID:        req.MissionID,
			CorridorID:       req.CorridorID,
			RobotName:        req.RobotName,
			EffectiveAt:      effectiveAt,
			DeadlineAt:       deadlineAt,
			ResponsibleParty: domain.PartyRole(req.ResponsibleParty),
			RequestID:        requestIDFromContext(r.Context()),
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, windowToResponse(win))
	}
}

func (s *Server) BatchAllocateWindows() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req BatchWindowRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		var items []routing.BatchItem
		for _, ri := range req.Items {
			effectiveAt, err := parseTime(ri.EffectiveAt)
			if err != nil {
				writeValidation(w, r, "invalid effective_at in batch item")
				return
			}
			deadlineAt, err := parseTime(ri.DeadlineAt)
			if err != nil {
				writeValidation(w, r, "invalid deadline_at in batch item")
				return
			}
			items = append(items, routing.BatchItem{
				MissionID:        ri.MissionID,
				CorridorID:       ri.CorridorID,
				RobotName:        ri.RobotName,
				EffectiveAt:      effectiveAt,
				DeadlineAt:       deadlineAt,
				ResponsibleParty: domain.PartyRole(ri.ResponsibleParty),
			})
		}
		actor := actorFromRequest(r)
		windows, err := s.reservationSvc.BatchAllocate(r.Context(), actor, items, requestIDFromContext(r.Context()))
		if err != nil {
			writeError(w, r, err)
			return
		}
		resp := make([]WindowResponse, 0, len(windows))
		for _, win := range windows {
			resp = append(resp, windowToResponse(win))
		}
		writeJSON(w, http.StatusCreated, map[string]any{"items": resp, "count": len(resp)})
	}
}

func (s *Server) ListReservations() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := parsePageQuery(
			r.URL.Query().Get("page"),
			r.URL.Query().Get("page_size"),
			r.URL.Query().Get("status"),
			map[string]string{
				"corridor_id": r.URL.Query().Get("corridor"),
				"robot_name":  r.URL.Query().Get("robot"),
			},
		)
		result, err := s.reservationSvc.List(r.Context(), q)
		if err != nil {
			writeError(w, r, err)
			return
		}
		items := make([]WindowResponse, 0, len(result.Items))
		for _, win := range result.Items {
			items = append(items, windowToResponse(win))
		}
		writeJSON(w, http.StatusOK, ListResponse[WindowResponse]{
			Items: items, Meta: pageMeta(result),
		})
	}
}

func (s *Server) GetReservation() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		win, err := s.reservationSvc.Get(r.Context(), id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, windowToResponse(win))
	}
}

func (s *Server) ReleaseReservation() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		actor := actorFromRequest(r)
		if err := s.reservationSvc.Release(r.Context(), id, actor, requestIDFromContext(r.Context())); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "released", "id": id})
	}
}

func (s *Server) InterveneReservation() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct {
			TargetState string `json:"target_state"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		if err := s.reservationSvc.ForceIntervene(r.Context(), id, actorFromRequest(r), domain.ReservationStatus(req.TargetState), requestIDFromContext(r.Context())); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": req.TargetState, "id": id})
	}
}
