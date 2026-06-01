package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/kernelcode0/logs-cleaner/internal/config"
)

// Scheduler manages the periodic execution of cleanup cycles.
type Scheduler struct {
	cron     *cron.Cron
	cfg      config.ScheduleConfig
	pipeline func(ctx context.Context) error
	logger   *slog.Logger
	entryID  cron.EntryID
}

// New creates a Scheduler. The pipeline func is called on each scheduled tick.
func New(cfg config.ScheduleConfig, pipeline func(ctx context.Context) error, logger *slog.Logger) (*Scheduler, error) {
	c := cron.New(
		cron.WithLocation(time.UTC),
		cron.WithLogger(cron.DefaultLogger),
	)
	return &Scheduler{
		cron:     c,
		cfg:      cfg,
		pipeline: pipeline,
		logger:   logger,
	}, nil
}

// Start registers the cron job and starts the background scheduler goroutine.
func (s *Scheduler) Start() error {
	id, err := s.cron.AddFunc(s.cfg.Cron, func() {
		ctx := context.Background()
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("scheduler: panic in pipeline", "panic", r)
			}
		}()
		if err := s.pipeline(ctx); err != nil {
			s.logger.Error("scheduler: pipeline error", "error", err)
		}
	})
	if err != nil {
		return fmt.Errorf("scheduler: register cron %q: %w", s.cfg.Cron, err)
	}
	s.entryID = id
	s.cron.Start()
	s.logger.Info("scheduler started", "cron", s.cfg.Cron)
	return nil
}

// RunNow executes the pipeline immediately (used for startup run if configured).
func (s *Scheduler) RunNow(ctx context.Context) error {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("scheduler: panic in immediate run", "panic", r)
		}
	}()
	return s.pipeline(ctx)
}

// Stop gracefully stops the scheduler, waiting for any in-progress run to finish.
func (s *Scheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	s.logger.Info("scheduler stopped")
}

// NextRun returns the time of the next scheduled execution.
func (s *Scheduler) NextRun() time.Time {
	entry := s.cron.Entry(s.entryID)
	return entry.Next
}
