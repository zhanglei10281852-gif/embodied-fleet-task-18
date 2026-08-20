package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/assignment"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/audit"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/auth"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/config"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/domain"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/engine"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/handover"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/mission"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/missionrun"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/quota"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/routing"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/store"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/worker"
)

// Server is the HTTP entry point for the scheduler process.
// It wires together all business services, the scheduling engine,
// and the HTTP router. The executor process uses a separate Worker
// that shares the same SQLite database.
type Server struct {
	cfg            *config.Config
	store          store.Store
	declSvc        *mission.Service
	reservationSvc *routing.Service
	orderSvc       *missionrun.Service
	taskSvc        *assignment.Service
	quotaSvc       *quota.Service
	handoverSvc    *handover.Service
	auditSvc       *audit.Recorder
	authSvc        *auth.Service
	engine         *engine.Engine
	worker         *worker.Worker
	logger         *apperr.Logger
	httpServer     *http.Server
}

// Deps bundles all dependencies needed to construct the Server.
type Deps struct {
	Cfg    *config.Config
	Store  store.Store
	Clock  apperr.Clock
	Logger *apperr.Logger
}

// New constructs a Server with all services wired up.
func New(deps Deps) (*Server, error) {
	auditRecorder := audit.New(deps.Store, deps.Clock)
	authService := auth.New(deps.Store, deps.Clock, deps.Cfg.Auth.SessionTTL, deps.Cfg.Auth.PasswordCost)
	if err := authService.Bootstrap(context.Background(), []auth.BootstrapUser{
		{Username: deps.Cfg.Auth.DispatcherUsername, Password: deps.Cfg.Auth.DispatcherPassword, Role: domain.RoleDispatcher},
		{Username: deps.Cfg.Auth.EngineerUsername, Password: deps.Cfg.Auth.EngineerPassword, Role: domain.RoleEngineer},
		{Username: deps.Cfg.Auth.AuditorUsername, Password: deps.Cfg.Auth.AuditorPassword, Role: domain.RoleAuditor},
	}); err != nil {
		return nil, err
	}

	declSvc := mission.New(mission.Deps{
		Missions:            deps.Store,
		Quotas:              deps.Store,
		Idempotency:         deps.Store,
		Handovers:           deps.Store,
		Audit:               auditRecorder,
		Clock:               deps.Clock,
		Logger:              deps.Logger,
		FastChargeLimit:     deps.Cfg.Quotas.DailyFastChargeLimit,
		StandardChargeLimit: deps.Cfg.Quotas.DailyStandardChargeLimit,
	})

	reservationSvc := routing.New(routing.Deps{
		Windows:      deps.Store,
		Missions:     deps.Store,
		Escalations:  deps.Store,
		Handovers:    deps.Store,
		Audit:        auditRecorder,
		Clock:        deps.Clock,
		Logger:       deps.Logger,
		LeaseTimeout: deps.Cfg.Scheduler.LeaseTimeout,
	})

	orderSvc := missionrun.New(missionrun.Deps{
		Orders: deps.Store,
		Audit:  auditRecorder,
		Clock:  deps.Clock,
		Logger: deps.Logger,
	})

	taskSvc := assignment.New(assignment.Deps{
		Tasks:        deps.Store,
		Leases:       deps.Store,
		Executions:   deps.Store,
		Audit:        auditRecorder,
		Clock:        deps.Clock,
		Logger:       deps.Logger,
		LeaseTimeout: deps.Cfg.Scheduler.LeaseTimeout,
	})

	quotaSvc := quota.New(quota.Deps{
		Quotas:              deps.Store,
		Audit:               auditRecorder,
		Clock:               deps.Clock,
		Logger:              deps.Logger,
		FastChargeLimit:     deps.Cfg.Quotas.DailyFastChargeLimit,
		StandardChargeLimit: deps.Cfg.Quotas.DailyStandardChargeLimit,
	})

	handoverSvc := handover.New(handover.Deps{
		Handovers: deps.Store,
		Audit:     auditRecorder,
		Clock:     deps.Clock,
		Logger:    deps.Logger,
	})

	eng := engine.New(engine.Deps{
		MissionService:    declSvc,
		RoutingService:    reservationSvc,
		AssignmentService: taskSvc,
		Clock:             deps.Clock,
		Logger:            deps.Logger,
		TickInterval:      deps.Cfg.Scheduler.TickInterval,
	})

	srv := &Server{
		cfg:            deps.Cfg,
		store:          deps.Store,
		declSvc:        declSvc,
		reservationSvc: reservationSvc,
		orderSvc:       orderSvc,
		taskSvc:        taskSvc,
		quotaSvc:       quotaSvc,
		handoverSvc:    handoverSvc,
		auditSvc:       auditRecorder,
		authSvc:        authService,
		engine:         eng,
		logger:         deps.Logger,
	}

	return srv, nil
}

// SetWorker attaches an executor worker for inline execution (testing/debug).
func (s *Server) SetWorker(w *worker.Worker) {
	s.worker = w
}

// Router returns the configured chi router with all routes registered.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	s.registerRoutes(r)
	return r
}

// Start begins listening for HTTP requests and launches the scheduling engine.
func (s *Server) Start(ctx context.Context) error {
	handler := s.buildHandler()
	s.httpServer = &http.Server{
		Addr:         addr(s.cfg),
		Handler:      handler,
		ReadTimeout:  s.cfg.Server.ReadTimeout,
		WriteTimeout: s.cfg.Server.WriteTimeout,
		IdleTimeout:  s.cfg.Server.IdleTimeout,
	}

	s.engine.Start(ctx)

	s.logger.Info("http server starting", apperr.F("port", s.cfg.Server.Port))
	if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown gracefully stops the HTTP server and scheduling engine.
func (s *Server) Shutdown(ctx context.Context) error {
	s.engine.Stop()
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

func (s *Server) buildHandler() http.Handler {
	r := chi.NewRouter()
	s.registerRoutes(r)

	var handler http.Handler = r
	handler = requestIDMiddleware(handler)
	handler = corsMiddleware(handler)
	handler = recoveryMiddleware(handler)
	handler = timeoutMiddleware(s.cfg.Server.WriteTimeout, handler)

	var zlog zerolog.Logger
	handler = loggingMiddleware(&zlog, handler)
	return handler
}

func addr(cfg *config.Config) string {
	return ":" + itoa(cfg.Server.Port)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// HealthHandler returns a simple health check handler.
func (s *Server) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "healthy",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}
}

// ReadyHandler returns a readiness check that verifies the database is accessible.
func (s *Server) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		_, err := s.store.ListAllQuotas(ctx)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "not ready",
				"error":  err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	}
}
