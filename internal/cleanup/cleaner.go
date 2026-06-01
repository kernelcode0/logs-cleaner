package cleanup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kernelcode0/logs-cleaner/internal/config"
	"github.com/kernelcode0/logs-cleaner/internal/docker"
	"github.com/kernelcode0/logs-cleaner/internal/storage"
)

var ErrAlreadyRunning = errors.New("cleanup: a run is already in progress")

// ContainerLogEntry represents a single log file entry.
type ContainerLogEntry struct {
	ContainerID   string
	ContainerName string
	Image         string
	LogFilePath   string
	SizeMB        float64
	WasTruncated  bool
	IsExcluded    bool
}

// CleanupError captures a non-fatal error during a cleanup cycle.
type CleanupError struct {
	Phase string
	Path  string
	Err   error
}

func (e CleanupError) Error() string {
	return fmt.Sprintf("[%s] %s: %v", e.Phase, e.Path, e.Err)
}

// Result holds the outcome of a single cleanup cycle.
type Result struct {
	StartedAt          time.Time
	FinishedAt         time.Time
	DurationMS         int64
	LogsCleaned        int
	SpaceReclaimedMB   float64
	Errors             []CleanupError
	TopConsumers       []ContainerLogEntry
	DryRun             bool
	ContainerInsights  []docker.ContainerInsight
	AllEntries         []ContainerLogEntry
}

// Cleaner is the primary interface for the cleanup subsystem.
type Cleaner interface {
	Run(ctx context.Context) (Result, error)
	LastResult() Result
}

type cleaner struct {
	cfg        *config.Config
	docker     *docker.Client
	db         *storage.DB
	policy     Policy
	logger     *slog.Logger
	mu         sync.Mutex
	lastResult Result
}

// New creates a Cleaner with all dependencies injected.
func New(cfg *config.Config, dockerClient *docker.Client, db *storage.DB, logger *slog.Logger) (Cleaner, error) {
	policy, err := ForMode(cfg.Cleanup.Mode)
	if err != nil {
		return nil, err
	}
	return &cleaner{
		cfg:    cfg,
		docker: dockerClient,
		db:     db,
		policy: policy,
		logger: logger,
	}, nil
}

func (c *cleaner) LastResult() Result {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastResult
}

func (c *cleaner) Run(ctx context.Context) (Result, error) {
	if !c.mu.TryLock() {
		return Result{}, ErrAlreadyRunning
	}
	defer c.mu.Unlock()

	result := Result{
		StartedAt: time.Now().UTC(),
		DryRun:    c.cfg.DryRun,
	}

	logFiles, err := c.discoverLogFiles()
	if err != nil {
		return result, fmt.Errorf("discover log files: %w", err)
	}
	c.logger.Info("discovered log files", "count", len(logFiles))

	excluded := make(map[string]bool, len(c.cfg.Cleanup.ExcludedContainers))
	for _, name := range c.cfg.Cleanup.ExcludedContainers {
		excluded[name] = true
	}

	type logEntry struct {
		path    string
		sizeMB  float64
		cid     string
		name    string
		image   string
	}

	var entries []logEntry

	for _, path := range logFiles {
		info, err := os.Stat(path)
		if err != nil {
			result.Errors = append(result.Errors, CleanupError{Phase: "stat", Path: path, Err: err})
			continue
		}
		sizeMB := float64(info.Size()) / 1024 / 1024

		cid, _ := docker.ExtractContainerID(path)
		name := cid
		image := ""

		if c.docker != nil {
			ci, err := c.docker.InspectByID(ctx, cid)
			if err == nil {
				name = strings.TrimPrefix(ci.Name, "/")
				image = ci.Config.Image
			}
		}

		// Record log size snapshot before any truncation
		if c.db != nil {
			_ = c.db.RecordLogSizeSnapshot(ctx, cid, name, sizeMB)
		}

		entries = append(entries, logEntry{
			path:   path,
			sizeMB: sizeMB,
			cid:    cid,
			name:   name,
			image:  image,
		})
	}

	// Sort by size descending for top-consumers list
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].sizeMB > entries[j].sizeMB
	})

	topN := c.cfg.Cleanup.TopConsumers
	if topN <= 0 {
		topN = 5
	}

	all := make([]ContainerLogEntry, 0, len(entries))
	for i, e := range entries {
		cle := ContainerLogEntry{
			ContainerID:   e.cid,
			ContainerName: e.name,
			Image:         e.image,
			LogFilePath:   e.path,
			SizeMB:        e.sizeMB,
			IsExcluded:    excluded[e.name],
		}
		if i < topN {
			result.TopConsumers = append(result.TopConsumers, cle)
		}
		all = append(all, cle)
	}
	result.AllEntries = all

	// Process truncation for files exceeding threshold
	thresholdBytes := float64(c.cfg.Cleanup.ThresholdMB)
	for i := range all {
		e := &all[i]
		if e.SizeMB < thresholdBytes {
			continue
		}
		if e.IsExcluded {
			c.logger.Debug("skipping excluded container", "container", e.ContainerName, "size_mb", e.SizeMB)
			continue
		}

		if c.cfg.DryRun {
			c.logger.Info("dry run: would truncate", "container", e.ContainerName, "path", e.LogFilePath, "size_mb", e.SizeMB)
			e.WasTruncated = false
		} else {
			reclaimed, err := c.policy.Apply(ctx, e.LogFilePath)
			if err != nil {
				c.logger.Error("truncate failed", "container", e.ContainerName, "path", e.LogFilePath, "error", err)
				result.Errors = append(result.Errors, CleanupError{Phase: "truncate", Path: e.LogFilePath, Err: err})
				continue
			}
			e.WasTruncated = true
			result.LogsCleaned++
			result.SpaceReclaimedMB += float64(reclaimed) / 1024 / 1024
			c.logger.Info("truncated log", "container", e.ContainerName, "reclaimed_mb", float64(reclaimed)/1024/1024)
		}
	}

	result.FinishedAt = time.Now().UTC()
	result.DurationMS = result.FinishedAt.Sub(result.StartedAt).Milliseconds()

	// Collect Docker insights for reporting
	if c.docker != nil {
		logPaths := make([]string, len(entries))
		for i, e := range entries {
			logPaths[i] = e.path
		}
		result.ContainerInsights = docker.CollectInsights(ctx, c.docker, logPaths, c.cfg.Cleanup.ExcludedContainers, nil, c.logger)
		for i := range result.ContainerInsights {
			for _, e := range entries {
				if result.ContainerInsights[i].ID == e.cid {
					result.ContainerInsights[i].LogSizeMB = e.sizeMB
					break
				}
			}
		}
	}

	// Persist to database
	if c.db != nil {
		errStrings := make([]string, len(result.Errors))
		for i, ce := range result.Errors {
			errStrings[i] = ce.Error()
		}
		containers := make([]storage.ContainerCleanInput, 0, len(all))
		for _, e := range all {
			containers = append(containers, storage.ContainerCleanInput{
				ContainerName: e.ContainerName,
				Image:         e.Image,
				SizeMB:        e.SizeMB,
				WasTruncated:  e.WasTruncated,
			})
		}
		_, dbErr := c.db.RecordRun(ctx, storage.CleanupRunInput{
			ServerID:            c.cfg.ServerID,
			LogsCleaned:         result.LogsCleaned,
			SpaceReclaimedMB:    result.SpaceReclaimedMB,
			ExecutionDurationMS: result.DurationMS,
			Errors:              errStrings,
			DryRun:              result.DryRun,
			Containers:          containers,
		})
		if dbErr != nil {
			c.logger.Error("persist run to db", "error", dbErr)
		}
	}

	c.lastResult = result
	c.logger.Info("cleanup complete",
		"logs_cleaned", result.LogsCleaned,
		"space_reclaimed_mb", result.SpaceReclaimedMB,
		"duration_ms", result.DurationMS,
		"dry_run", result.DryRun,
	)
	return result, nil
}

func (c *cleaner) discoverLogFiles() ([]string, error) {
	pattern := c.cfg.Cleanup.LogGlob
	if strings.Contains(pattern, "**") {
		return doubleStarGlob(pattern)
	}
	return filepath.Glob(pattern)
}

// doubleStarGlob expands patterns containing ** by walking the Docker containers dir.
func doubleStarGlob(pattern string) ([]string, error) {
	// For the standard Docker log pattern, walk /var/lib/docker/containers
	baseDir := "/var/lib/docker/containers"
	if idx := strings.Index(pattern, "**"); idx > 0 {
		baseDir = strings.TrimRight(pattern[:idx], "/")
	}

	var matches []string
	err := filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable directories
		}
		if !d.IsDir() && strings.HasSuffix(path, "-json.log") {
			matches = append(matches, path)
		}
		return nil
	})
	return matches, err
}
