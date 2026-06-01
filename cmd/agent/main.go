package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kernelcode0/logs-cleaner/internal/cleanup"
	"github.com/kernelcode0/logs-cleaner/internal/config"
	"github.com/kernelcode0/logs-cleaner/internal/docker"
	"github.com/kernelcode0/logs-cleaner/internal/email"
	"github.com/kernelcode0/logs-cleaner/internal/metrics"
	"github.com/kernelcode0/logs-cleaner/internal/notification"
	"github.com/kernelcode0/logs-cleaner/internal/reporting"
	"github.com/kernelcode0/logs-cleaner/internal/scheduler"
	"github.com/kernelcode0/logs-cleaner/internal/server"
	"github.com/kernelcode0/logs-cleaner/internal/storage"
	"github.com/kernelcode0/logs-cleaner/internal/telemetry"
)

func main() {
	cfg := config.MustLoad()

	logger := buildLogger(cfg.Log)
	logger = logger.With("server_id", cfg.ServerID)

	logger.Info("docker-cleanup-agent starting", "dry_run", cfg.DryRun)

	// Storage
	db, err := storage.New(cfg.Storage.Path, logger)
	if err != nil {
		logger.Error("open storage", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Docker client
	dockerClient, err := docker.New()
	if err != nil {
		logger.Error("docker client", "error", err)
		os.Exit(1)
	}
	defer dockerClient.Close()

	// Metrics registry
	reg := metrics.NewRegistry(cfg.ServerID)
	selfMon := metrics.NewSelfMonitor()

	// Cleaner
	cleaner, err := cleanup.New(cfg, dockerClient, db, logger)
	if err != nil {
		logger.Error("cleaner init", "error", err)
		os.Exit(1)
	}

	// Email sender
	mailer := email.New(cfg.Email, logger)

	// Notifications
	slackNotif := notification.NewSlack(cfg.Notifications.Slack, logger)
	teamsNotif := notification.NewTeams(cfg.Notifications.Teams, logger)

	// HTTP server
	httpServer := server.New(cfg.Server, db, reg, dockerClient, cleaner, cfg.ServerID, logger)

	// Build the pipeline closure executed on each cron tick
	pipeline := func(ctx context.Context) error {
		result, err := cleaner.Run(ctx)
		if err != nil {
			selfMon.IncrCleanupError()
			logger.Error("cleanup run failed", "error", err)
			return err
		}
		selfMon.SetLastDurationMS(result.DurationMS)

		// Collect telemetry
		serverTel, err := telemetry.Collect(ctx, cfg.Telemetry.PublicIPURL, logger)
		if err != nil {
			selfMon.IncrDockerAPIFailure()
		}

		dockerTel, err := telemetry.CollectDocker(ctx, dockerClient, logger)
		if err != nil {
			selfMon.IncrDockerAPIFailure()
		}

		// Update Prometheus
		reg.RecordRun(result)
		reg.RecordServerTelemetry(serverTel)
		reg.RecordDockerTelemetry(dockerTel)

		// Recommendations
		recs, _ := reporting.Analyze(ctx, db, result.ContainerInsights,
			cfg.Recommendations.WindowDays, cfg.Recommendations.TriggerCount, logger)

		// Recent history for trend table
		recentRuns, _ := db.ListRuns(ctx, cfg.ServerID, 5)

		// Determine next run time
		var nextRun time.Time
		// nextRun is set by scheduler after Start(), but for now leave zero

		// Build report
		report := reporting.Build(
			cfg.ServerID,
			result,
			serverTel,
			dockerTel,
			recs,
			recentRuns,
			selfMon.Snapshot(),
			nextRun,
		)

		// Render and send email
		if cfg.Email.Enabled {
			html, err := email.RenderReport(report, string(cfg.Email.Theme))
			if err != nil {
				logger.Error("render email template", "error", err)
			} else {
				subject := reporting.SubjectLine(cfg.Email.SubjectPrefix, cfg.ServerID, report.GeneratedAt)
				if err := mailer.Send(ctx, subject, html); err != nil {
					selfMon.IncrSMTPFailure()
					logger.Error("send email", "error", err)
				}
			}
		}

		// Slack notification
		if err := slackNotif.Notify(ctx, report); err != nil {
			selfMon.IncrNotifFailure()
			logger.Warn("slack notify failed", "error", err)
		}

		// Teams notification
		if err := teamsNotif.Notify(ctx, report); err != nil {
			selfMon.IncrNotifFailure()
			logger.Warn("teams notify failed", "error", err)
		}

		return nil
	}

	// Scheduler
	sched, err := scheduler.New(cfg.Schedule, pipeline, logger)
	if err != nil {
		logger.Error("scheduler init", "error", err)
		os.Exit(1)
	}

	// Start HTTP server
	if cfg.Server.Enabled {
		if err := httpServer.Start(); err != nil {
			logger.Error("http server start", "error", err)
			os.Exit(1)
		}
	}

	// Start scheduler
	if err := sched.Start(); err != nil {
		logger.Error("scheduler start", "error", err)
		os.Exit(1)
	}

	// Run immediately on startup
	logger.Info("running initial cleanup cycle")
	startCtx, startCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	if err := sched.RunNow(startCtx); err != nil {
		logger.Error("initial run failed", "error", err)
	}
	startCancel()

	// Block until signal
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()

	logger.Info("shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	sched.Stop()
	if cfg.Server.Enabled {
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("http server shutdown", "error", err)
		}
	}

	logger.Info("shutdown complete")
}

func buildLogger(cfg config.LogConfig) *slog.Logger {
	var level slog.Level
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}
	if cfg.Format == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
