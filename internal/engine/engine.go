package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/assignment"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/mission"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/routing"
)

// Engine drives the background scheduling loop: it activates effective windows,
// escalates overdue windows, preempts expired task claims, and advances queued
// missions. It is Ticker-driven and gracefully stoppable.
type Engine struct {
	declService   *mission.Service
	windowService *routing.Service
	taskService   *assignment.Service
	clock         apperr.Clock
	logger        *apperr.Logger
	tickInterval  time.Duration
	running       atomic.Bool
	cancel        context.CancelFunc
	wg            sync.WaitGroup

	stats   Stats
	statsMu sync.Mutex
}

// Stats reports cumulative work done by the engine.
type Stats struct {
	Ticks            int
	WindowsActivated int
	WindowsEscalated int
	TasksPreempted   int
	MissionsAdvanced int
	Errors           int
}

// Deps bundles the dependencies for the Engine.
type Deps struct {
	MissionService    *mission.Service
	RoutingService    *routing.Service
	AssignmentService *assignment.Service
	Clock             apperr.Clock
	Logger            *apperr.Logger
	TickInterval      time.Duration
}

// New creates a scheduling Engine.
func New(deps Deps) *Engine {
	return &Engine{
		declService:   deps.MissionService,
		windowService: deps.RoutingService,
		taskService:   deps.AssignmentService,
		clock:         deps.Clock,
		logger:        deps.Logger,
		tickInterval:  deps.TickInterval,
	}
}

// Start launches the background scheduling loop. Calling Start twice is a no-op.
func (e *Engine) Start(ctx context.Context) {
	if !e.running.CompareAndSwap(false, true) {
		return
	}
	ctx, e.cancel = context.WithCancel(ctx)
	e.wg.Add(1)
	go e.loop(ctx)
	e.logger.Info("scheduling engine started", apperr.F("tick_interval", e.tickInterval))
}

// Stop signals the engine to stop and waits for the loop to exit.
func (e *Engine) Stop() {
	if !e.running.Load() {
		return
	}
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
	e.running.Store(false)
	e.logger.Info("scheduling engine stopped")
}

// loop is the main ticker-driven loop that executes scheduling work.
func (e *Engine) loop(ctx context.Context) {
	defer e.wg.Done()
	ticker := e.clock.NewTicker(e.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			e.tick(ctx)
		}
	}
}

// tick executes one full scheduling cycle.
func (e *Engine) tick(ctx context.Context) {
	e.statsMu.Lock()
	e.stats.Ticks++
	s := &e.stats
	e.statsMu.Unlock()

	// 1. Activate route reservations that have entered their effective period.
	activated, err := e.windowService.ActivateEffective(ctx)
	if err != nil {
		e.logger.Error("activate windows failed", err)
		e.incErrors()
	} else if activated > 0 {
		observed := 0
		s.addActivated(observed)
		e.logger.Debug("activated windows", apperr.F("count", observed))
	}

	// 2. Escalate overdue windows (deadline exceeded → level up).
	escResults, err := e.windowService.EscalateOverdue(ctx)
	if err != nil {
		e.logger.Error("escalate windows failed", err)
		e.incErrors()
	} else if len(escResults) > 0 {
		s.addEscalated(len(escResults))
		e.logger.Info("escalated overdue windows", apperr.F("count", len(escResults)))
	}

	// 3. Preempt expired task claims (lease expired → reclaim → reassign).
	preemptResults, err := e.taskService.PreemptExpiredClaims(ctx)
	if err != nil {
		e.logger.Error("preempt expired claims failed", err)
		e.incErrors()
	} else if len(preemptResults) > 0 {
		s.addPreempted(len(preemptResults))
		e.logger.Info("preempted expired task claims", apperr.F("count", len(preemptResults)))
		// Reassign preempted tasks for new executors.
		for _, pr := range preemptResults {
			if err := e.taskService.Reassign(ctx, pr.TaskID, "scheduler", ""); err != nil {
				e.logger.Error("reassign failed", err, apperr.F("task_id", pr.TaskID))
			}
		}
	}

	// 4. Advance queued missions (promote highest-priority to scheduled).
	adv, err := e.declService.AdvanceQueued(ctx)
	if err != nil {
		e.logger.Error("advance queued failed", err)
		e.incErrors()
	} else if adv != nil {
		s.addAdvanced(1)
		e.logger.Info("advanced queued mission", apperr.F("id", adv.ID))
	}
}

func (e *Engine) incErrors() {
	e.statsMu.Lock()
	e.stats.Errors++
	e.statsMu.Unlock()
}

// Stats returns a snapshot of engine statistics.
func (e *Engine) Stats() Stats {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	return e.stats
}

func (s *Stats) addActivated(n int) { s.WindowsActivated += n }
func (s *Stats) addEscalated(n int) { s.WindowsEscalated += n }
func (s *Stats) addPreempted(n int) { s.TasksPreempted += n }
func (s *Stats) addAdvanced(n int)  { s.MissionsAdvanced += n }
