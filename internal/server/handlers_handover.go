package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/handover"
)

func (s *Server) CreateHandover() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateHandoverRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		h, err := s.handoverSvc.Create(r.Context(), handover.CreateRequest{
			EntityType:  domain.EntityType(req.EntityType),
			EntityID:    req.EntityID,
			FromParty:   domain.PartyRole(req.FromParty),
			ToParty:     domain.PartyRole(req.ToParty),
			Action:      req.Action,
			DocumentRef: req.DocumentRef,
			Notes:       req.Notes,
			RequestID:   requestIDFromContext(r.Context()),
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusCreated, handoverToResponse(h))
	}
}

func (s *Server) ListHandovers() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := parsePageQuery(
			r.URL.Query().Get("page"),
			r.URL.Query().Get("page_size"),
			r.URL.Query().Get("status"),
			map[string]string{
				"entity_type": r.URL.Query().Get("entity_type"),
				"entity_id":   r.URL.Query().Get("entity_id"),
				"from_party":  r.URL.Query().Get("from_party"),
			},
		)
		result, err := s.handoverSvc.List(r.Context(), q)
		if err != nil {
			writeError(w, r, err)
			return
		}
		items := make([]HandoverResponse, 0, len(result.Items))
		for _, h := range result.Items {
			items = append(items, handoverToResponse(h))
		}
		writeJSON(w, http.StatusOK, ListResponse[HandoverResponse]{
			Items: items, Meta: pageMeta(result),
		})
	}
}

func (s *Server) GetHandover() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		h, err := s.handoverSvc.Get(r.Context(), id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, handoverToResponse(h))
	}
}

func (s *Server) ConfirmHandover() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		actor := actorFromRequest(r)
		if err := s.handoverSvc.Confirm(r.Context(), id, actor, requestIDFromContext(r.Context())); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "confirmed", "id": id})
	}
}

func (s *Server) RejectHandover() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		actor := actorFromRequest(r)
		if err := s.handoverSvc.Reject(r.Context(), id, actor, requestIDFromContext(r.Context())); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "rejected", "id": id})
	}
}
