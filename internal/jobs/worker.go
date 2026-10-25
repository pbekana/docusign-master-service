package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/your-org/docusign-master-service/internal/config"
)

// WorkerPool manages a pool of goroutines that process extraction jobs.
type WorkerPool struct {
	jobs       chan uuid.UUID
	wg         sync.WaitGroup
	maxWorkers int
	svc        *JobService
	repo       *JobRepository
	logger     *slog.Logger
	cfg        *config.Config
}

// NewWorkerPool creates a WorkerPool. Call Start to launch worker goroutines.
func NewWorkerPool(
	svc *JobService,
	repo *JobRepository,
	logger *slog.Logger,
	cfg *config.Config,
) *WorkerPool {
	maxWorkers := cfg.MaxConcurrentJobs
	if maxWorkers <= 0 {
		maxWorkers = 3
	}
	return &WorkerPool{
		jobs:       make(chan uuid.UUID, maxWorkers*4),
		maxWorkers: maxWorkers,
		svc:        svc,
		repo:       repo,
		logger:     logger,
		cfg:        cfg,
	}
}

// Start launches maxWorkers goroutines. Each goroutine reads job IDs from the
// jobs channel, runs the job, and records the outcome.
// ctx is used to stop workers when the service shuts down.
func (p *WorkerPool) Start(ctx context.Context) {
	for i := 0; i < p.maxWorkers; i++ {
		p.wg.Add(1)
		go p.runWorker(ctx)
	}
}

// runWorker processes jobs until the jobs channel is closed or ctx is cancelled.
func (p *WorkerPool) runWorker(ctx context.Context) {
	defer p.wg.Done()

	for {
		select {
		case jobID, ok := <-p.jobs:
			if !ok {
				// Channel closed — worker exits.
				return
			}
			p.processJob(ctx, jobID)
		case <-ctx.Done():
			return
		}
	}
}

// processJob runs a single job with a heartbeat goroutine.
func (p *WorkerPool) processJob(ctx context.Context, jobID uuid.UUID) {
	p.logger.Info("worker picking up job", "job_id", jobID)

	// Start heartbeat goroutine.
	done := make(chan struct{})
	go p.heartbeat(ctx, jobID, done)
	defer close(done)

	// Use a child context so we can cancel this job independently.
	jobCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	err := p.svc.RunJob(jobCtx, jobID)
	if err != nil {
		p.logger.Error("job failed", "job_id", jobID, "error", err)
	} else {
		p.logger.Info("job finished successfully", "job_id", jobID)
	}
}

// heartbeat sends periodic heartbeats to the database while the job runs.
// It stops when the done channel is closed.
func (p *WorkerPool) heartbeat(ctx context.Context, jobID uuid.UUID, done <-chan struct{}) {
	interval := time.Duration(p.cfg.HeartbeatIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.repo.UpdateHeartbeat(context.Background(), jobID); err != nil {
				p.logger.Warn("heartbeat failed", "job_id", jobID, "error", err)
			}
		}
	}
}

// Enqueue adds jobID to the worker queue. Returns an error if the queue is full.
func (p *WorkerPool) Enqueue(jobID uuid.UUID) error {
	select {
	case p.jobs <- jobID:
		return nil
	default:
		return fmt.Errorf("worker pool: queue full, cannot enqueue job %s", jobID)
	}
}

// RecoverOnStartup re-enqueues PENDING jobs and marks stale in-progress jobs as CRASHED.
func (p *WorkerPool) RecoverOnStartup(ctx context.Context) {
	// Mark stale in-progress jobs as crashed first.
	n, err := p.repo.MarkCrashed(ctx, p.cfg.CrashTimeoutMinutes)
	if err != nil {
		p.logger.Error("crash recovery failed", "error", err)
	} else if n > 0 {
		p.logger.Info("marked stale jobs as crashed", "count", n)
	}

	// Re-enqueue PENDING jobs.
	pending, err := p.repo.ListByStatus(ctx, StatusPending)
	if err != nil {
		p.logger.Error("could not load pending jobs for recovery", "error", err)
		return
	}

	for _, job := range pending {
		if enqErr := p.Enqueue(job.ID); enqErr != nil {
			p.logger.Warn("could not re-enqueue pending job", "job_id", job.ID, "error", enqErr)
		} else {
			p.logger.Info("re-enqueued pending job", "job_id", job.ID)
		}
	}
}

// Shutdown closes the jobs channel and waits for all workers to finish,
// respecting the ctx deadline.
func (p *WorkerPool) Shutdown(ctx context.Context) error {
	close(p.jobs)

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("worker pool shutdown complete")
		return nil
	case <-ctx.Done():
		p.logger.Warn("worker pool shutdown timed out")
		return ctx.Err()
	}
}
