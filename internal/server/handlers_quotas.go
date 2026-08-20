package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/quota"
)

func (s *Server) ListQuotas() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := parsePageQuery(
			r.URL.Query().Get("page"),
			r.URL.Query().Get("page_size"),
			r.URL.Query().Get("status"),
			map[string]string{
				"quota_type":  r.URL.Query().Get("type"),
				"period_date": r.URL.Query().Get("date"),
			},
		)
		result, err := s.quotaSvc.List(r.Context(), q)
		if err != nil {
			writeError(w, r, err)
			return
		}
		items := make([]QuotaResponse, 0, len(result.Items))
		for _, qt := range result.Items {
			items = append(items, quotaToResponse(qt))
		}
		writeJSON(w, http.StatusOK, ListResponse[QuotaResponse]{
			Items: items, Meta: pageMeta(result),
		})
	}
}

func (s *Server) GetQuota() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		qt, err := s.quotaSvc.Get(r.Context(), id)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, quotaToResponse(qt))
	}
}

func (s *Server) ReserveQuota() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req ReserveQuotaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		actor := actorFromRequest(r)
		result, err := s.quotaSvc.Reserve(r.Context(), quota.ReserveRequest{
			QuotaType: domain.QuotaType(req.QuotaType),
			Amount:    req.Amount,
			Actor:     actor,
			RequestID: requestIDFromContext(r.Context()),
		})
		if err != nil {
			writeError(w, r, err)
			return
		}
		status := http.StatusCreated
		if result.Rejected {
			status = http.StatusUnprocessableEntity
		}
		writeJSON(w, status, result)
	}
}

func (s *Server) CommitQuota() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct {
			Amount  int `json:"amount"`
			Version int `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		actor := actorFromRequest(r)
		if err := s.quotaSvc.Commit(r.Context(), id, req.Amount, req.Version, actor, requestIDFromContext(r.Context())); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "committed", "id": id})
	}
}

func (s *Server) ReleaseQuotaHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var req struct {
			Amount  int `json:"amount"`
			Version int `json:"version"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeValidation(w, r, "invalid JSON body")
			return
		}
		actor := actorFromRequest(r)
		if err := s.quotaSvc.Release(r.Context(), id, req.Amount, req.Version, actor, requestIDFromContext(r.Context())); err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "released", "id": id})
	}
}

// GetBacklog returns aggregated backlog counts across all entity types.
func (s *Server) GetBacklog() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		declBacklog, err := s.declSvc.Backlog(ctx)
		if err != nil {
			writeError(w, r, err)
			return
		}
		windowBacklog, err := s.reservationSvc.Backlog(ctx)
		if err != nil {
			writeError(w, r, err)
			return
		}
		orderBacklog, err := s.orderSvc.Backlog(ctx)
		if err != nil {
			writeError(w, r, err)
			return
		}
		taskBacklog, err := s.taskSvc.Backlog(ctx)
		if err != nil {
			writeError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"missions":           declBacklog,
			"route_reservations": windowBacklog,
			"mission_runs":       orderBacklog,
			"robot_assignments":  taskBacklog,
		})
	}
}

// EngineStats returns scheduling engine statistics.
func (s *Server) EngineStats() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.engine.Stats())
	}
}

// ListExecutions returns a paginated list of execution records.
func (s *Server) ListExecutions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := parsePageQuery(
			r.URL.Query().Get("page"),
			r.URL.Query().Get("page_size"),
			"",
			map[string]string{
				"executor_id": r.URL.Query().Get("executor"),
				"task_id":     r.URL.Query().Get("task"),
			},
		)
		result, err := s.store.ListExecutions(r.Context(), q)
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

// ExportReconciliation generates a CSV reconciliation export.
func (s *Server) ExportReconciliation() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=reconciliation.csv")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("entity_type,entity_id,status,version,created_at,updated_at\n"))

		decls, _ := s.store.ListMissions(ctx, domain.DefaultPage())
		for _, d := range decls.Items {
			_, _ = w.Write([]byte("mission," + d.ID + "," + string(d.Status) + "," +
				strconv.Itoa(d.Version) + "," + d.CreatedAt.Format("2006-01-02T15:04:05Z07:00") + "," +
				d.UpdatedAt.Format("2006-01-02T15:04:05Z07:00") + "\n"))
		}

		windows, _ := s.store.ListReservations(ctx, domain.DefaultPage())
		for _, win := range windows.Items {
			_, _ = w.Write([]byte("route_reservation," + win.ID + "," + string(win.Status) + "," +
				strconv.Itoa(win.Version) + "," + win.CreatedAt.Format("2006-01-02T15:04:05Z07:00") + "," +
				win.UpdatedAt.Format("2006-01-02T15:04:05Z07:00") + "\n"))
		}

		orders, _ := s.store.ListMissionRuns(ctx, domain.DefaultPage())
		for _, wo := range orders.Items {
			_, _ = w.Write([]byte("mission_run," + wo.ID + "," + string(wo.Status) + "," +
				strconv.Itoa(wo.Version) + "," + wo.CreatedAt.Format("2006-01-02T15:04:05Z07:00") + "," +
				wo.UpdatedAt.Format("2006-01-02T15:04:05Z07:00") + "\n"))
		}

		tasks, _ := s.store.ListAssignments(ctx, domain.DefaultPage())
		for _, t := range tasks.Items {
			_, _ = w.Write([]byte("robot_assignment," + t.ID + "," + string(t.Status) + "," +
				strconv.Itoa(t.Version) + "," + t.CreatedAt.Format("2006-01-02T15:04:05Z07:00") + "," +
				t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00") + "\n"))
		}
	}
}
