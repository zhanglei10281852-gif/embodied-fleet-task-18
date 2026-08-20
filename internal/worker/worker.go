package worker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/apperr"
	"github.com/zhanglei10281852-gif/embodied-fleet-go/internal/assignment"
)

// Worker is the executor process that polls for claimable tasks, claims them,
// processes them, and reports results. It is the second process in the
// scheduler+executor dual-process architecture, collaborating via the shared
// SQLite store (persistent claim records and leases).
type Worker struct {
	taskService  *assignment.Service
	clock        apperr.Clock
	logger       *apperr.Logger
	id           string
	pollInterval time.Duration
	batchSize    int
	running      atomic.Bool
	cancel       context.CancelFunc
	wg           sync.WaitGroup

	stats   Stats
	statsMu sync.Mutex
}

// Stats reports cumulative work done by the worker.
type Stats struct {
	Polls         int
	TasksClaimed  int
	TasksReported int
	TasksFailed   int
	Errors        int
}

// Deps bundles the dependencies for the Worker.
type Deps struct {
	TaskService  *assignment.Service
	Clock        apperr.Clock
	Logger       *apperr.Logger
	ID           string
	PollInterval time.Duration
	BatchSize    int
}

// New creates an executor Worker.
func New(deps Deps) *Worker {
	if deps.ID == "" {
		deps.ID = "worker-default"
	}
	if deps.BatchSize < 1 {
		deps.BatchSize = 5
	}
	return &Worker{
		taskService:  deps.TaskService,
		clock:        deps.Clock,
		logger:       deps.Logger,
		id:           deps.ID,
		pollInterval: deps.PollInterval,
		batchSize:    deps.BatchSize,
	}
}

// Start launches the polling loop. Calling Start twice is a no-op.
func (w *Worker) Start(ctx context.Context) {
	if !w.running.CompareAndSwap(false, true) {
		return
	}
	ctx, w.cancel = context.WithCancel(ctx)
	w.wg.Add(1)
	go w.loop(ctx)
	w.logger.Info("executor worker started",
		apperr.F("id", w.id),
		apperr.F("poll_interval", w.pollInterval))
}

// Stop signals the worker to stop and waits for the loop to exit.
func (w *Worker) Stop() {
	if !w.running.Load() {
		return
	}
	if w.cancel != nil {
		w.cancel()
	}
	w.wg.Wait()
	w.running.Store(false)
	w.logger.Info("executor worker stopped",
		apperr.F("id", w.id),
		apperr.F("claimed", w.stats.TasksClaimed),
		apperr.F("reported", w.stats.TasksReported))
}

// loop is the main polling loop.
func (w *Worker) loop(ctx context.Context) {
	defer w.wg.Done()
	ticker := w.clock.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C():
			w.poll(ctx)
		}
	}
}

// poll queries for claimable tasks and processes as many as the batch size allows.
func (w *Worker) poll(ctx context.Context) {
	w.statsMu.Lock()
	w.stats.Polls++
	w.statsMu.Unlock()

	claimable, err := w.taskService.ListClaimable(ctx, w.batchSize)
	if err != nil {
		w.logger.Error("list claimable tasks failed", err)
		w.incErrors()
		return
	}
	if len(claimable) == 0 {
		return
	}

	for _, task := range claimable {
		if ctx.Err() != nil {
			return
		}
		if err := w.ClaimAndExecute(ctx, task.ID); err != nil {
			continue
		}
	}
}

// ClaimAndExecute claims a single task by ID and reports completion.
// This is the core executor flow: claim → execute → report.
func (w *Worker) ClaimAndExecute(ctx context.Context, taskID string) error {
	claim, err := w.taskService.Claim(ctx, assignment.ClaimRequest{
		TaskID:     taskID,
		ExecutorID: w.id,
	})
	if err != nil {
		if apperr.IsConflict(err) || apperr.IsNotFound(err) || apperr.IsInvalidTransition(err) {
			w.logger.Debug("claim lost or not found", apperr.F("task_id", taskID))
			return nil
		}
		w.incErrors()
		return fmt.Errorf("claim task %s: %w", taskID, err)
	}

	w.statsMu.Lock()
	w.stats.TasksClaimed++
	w.statsMu.Unlock()

	w.logger.Info("claimed task",
		apperr.F("task_id", taskID),
		apperr.F("lease_id", claim.LeaseID),
		apperr.F("expires", claim.ExpiresAt))

	result := "completed"
	reportData := fmt.Sprintf(`{"executor":"%s","completed_at":"%s"}`, w.id, w.clock.Now().Format(time.RFC3339))

	err = w.taskService.Report(ctx, assignment.ReportRequest{
		TaskID:     taskID,
		ExecutorID: w.id,
		Result:     result,
		ReportData: reportData,
	})
	if err != nil {
		w.statsMu.Lock()
		w.stats.TasksFailed++
		w.statsMu.Unlock()
		w.logger.Error("report task failed", err, apperr.F("task_id", taskID))
		return err
	}

	w.statsMu.Lock()
	w.stats.TasksReported++
	w.statsMu.Unlock()
	w.logger.Info("reported task", apperr.F("task_id", taskID), apperr.F("result", result))
	return nil
}

// ExecuteOnce runs a single poll-and-execute cycle synchronously.
func (w *Worker) ExecuteOnce(ctx context.Context) (int, error) {
	claimable, err := w.taskService.ListClaimable(ctx, w.batchSize)
	if err != nil {
		return 0, err
	}
	executed := 0
	for _, t := range claimable {
		if err := w.ClaimAndExecute(ctx, t.ID); err != nil {
			continue
		}
		executed++
	}
	return executed, nil
}

func (w *Worker) incErrors() {
	w.statsMu.Lock()
	w.stats.Errors++
	w.statsMu.Unlock()
}

// Stats returns a snapshot of worker statistics.
func (w *Worker) Stats() Stats {
	w.statsMu.Lock()
	defer w.statsMu.Unlock()
	return w.stats
}

// ID returns the worker's executor ID.
func (w *Worker) ID() string { return w.id }
