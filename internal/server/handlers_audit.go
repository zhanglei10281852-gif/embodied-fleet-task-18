package server

import (
	"net/http"

	"encoding/json"

	"github.com/go-chi/chi/v5"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
)

func (s *Server) ListAuditLogs() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := parsePageQuery(
			r.URL.Query().Get("page"),
			r.URL.Query().Get("page_size"),
			"",
			map[string]string{
				"actor":       r.URL.Query().Get("actor"),
				"action":      r.URL.Query().Get("action"),
				"entity_type": r.URL.Query().Get("entity_type"),
				"entity_id":   r.URL.Query().Get("entity_id"),
			},
		)
		result, err := s.auditSvc.List(r.Context(), q)
		if err != nil {
			writeError(w, r, err)
			return
		}
		items := make([]AuditLogResponse, 0, len(result.Items))
		for _, a := range result.Items {
			items = append(items, auditToResponse(a))
		}
		writeJSON(w, http.StatusOK, ListResponse[AuditLogResponse]{
			Items: items, Meta: pageMeta(result),
		})
	}
}

func (s *Server) ListEscalations() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := parsePageQuery(
			r.URL.Query().Get("page"),
			r.URL.Query().Get("page_size"),
			"",
			map[string]string{
				"entity_type": r.URL.Query().Get("entity_type"),
				"entity_id":   r.URL.Query().Get("entity_id"),
			},
		)
		result, err := s.store.ListEscalations(r.Context(), q)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items": result.Items,
			"meta":  pageMeta(result),
		})
	}
}

// ForceIntervene provides a generic forced-intervention endpoint for supervisors.
func (s *Server) ForceIntervene() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req InterveneRequest
		if err := decodeJSON(r, &req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		actor := actorFromRequest(r)
		switch domain.EntityType(req.EntityType) {
		case domain.EntityRouteReservation:
			err := s.reservationSvc.ForceIntervene(r.Context(), req.EntityID, actor,
				domain.ReservationStatus(req.TargetState), requestIDFromContext(r.Context()))
			if err != nil {
				writeError(w, r, err)
				return
			}
		case domain.EntityMission:
			if req.TargetState == "cancelled" {
				err := s.declSvc.Cancel(r.Context(), req.EntityID, actor, requestIDFromContext(r.Context()))
				if err != nil {
					writeError(w, r, err)
					return
				}
			} else {
				writeValidation(w, r, "unsupported target state for mission")
				return
			}
		default:
			writeValidation(w, r, "unsupported entity type for intervention")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"status": req.TargetState,
			"id":     req.EntityID,
		})
	}
}

// decodeJSON is a helper that decodes a JSON request body into v.
func decodeJSON(r *http.Request, v any) error {
	_ = chi.URLParam(r, "id")
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}
