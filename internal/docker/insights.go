package docker

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

// ContainerInsight holds enriched per-container data.
type ContainerInsight struct {
	ID                     string
	Name                   string
	Image                  string
	ImageID                string
	Status                 string
	RestartCount           int
	LogSizeMB              float64
	ImageSizeMB            float64
	CreatedAt              time.Time
	LogGrowthRateMBPerHour float64
	IsExcluded             bool
}

// DockerSummary holds fleet-level Docker statistics.
type DockerSummary struct {
	Version            string
	APIVersion         string
	ImageCount         int
	DanglingImageCount int
	VolumeCount        int
	UnusedVolumeCount  int
	TotalContainers    int
	RunningContainers  int
	StoppedContainers  int
	PausedContainers   int
}

// StorageReader provides read-only access to historical log size data.
type StorageReader interface {
	GetLogSizeHistory(ctx context.Context, containerID string, windowDays int) ([]LogSizeRecord, error)
}

// LogSizeRecord is a point-in-time size snapshot for a container's log.
type LogSizeRecord struct {
	Timestamp time.Time
	SizeMB    float64
}

// ExtractContainerID parses the container ID from a Docker log file path.
// Path format: /var/lib/docker/containers/<id>/<id>-json.log
func ExtractContainerID(logFilePath string) (string, error) {
	return filepath.Base(filepath.Dir(logFilePath)), nil
}

// CollectInsights returns enriched ContainerInsight for each discovered log file path.
func CollectInsights(
	ctx context.Context,
	c *Client,
	logFilePaths []string,
	excludedContainers []string,
	db StorageReader,
	logger *slog.Logger,
) []ContainerInsight {
	excluded := make(map[string]bool, len(excludedContainers))
	for _, name := range excludedContainers {
		excluded[name] = true
	}

	insights := make([]ContainerInsight, 0, len(logFilePaths))
	for _, path := range logFilePaths {
		cid, err := ExtractContainerID(path)
		if err != nil {
			logger.Warn("extract container id", "path", path, "error", err)
			continue
		}

		insight := ContainerInsight{ID: cid}

		info, err := c.InspectByID(ctx, cid)
		if err != nil {
			logger.Debug("inspect container", "id", cid, "error", err)
			insight.Name = cid[:12]
			insights = append(insights, insight)
			continue
		}

		insight.Name = strings.TrimPrefix(info.Name, "/")
		insight.Image = info.Config.Image
		insight.ImageID = info.Image
		insight.Status = info.State.Status
		insight.RestartCount = info.RestartCount
		if info.Created != "" {
			insight.CreatedAt, _ = time.Parse(time.RFC3339Nano, info.Created)
		}
		insight.IsExcluded = excluded[insight.Name]

		// Compute growth rate from history
		if db != nil {
			history, err := db.GetLogSizeHistory(ctx, cid, 7)
			if err == nil && len(history) >= 2 {
				newest := history[len(history)-1]
				oldest := history[0]
				if newest.SizeMB > oldest.SizeMB {
					hours := newest.Timestamp.Sub(oldest.Timestamp).Hours()
					if hours > 0 {
						insight.LogGrowthRateMBPerHour = (newest.SizeMB - oldest.SizeMB) / hours
					}
				}
			}
		}

		insights = append(insights, insight)
	}
	return insights
}

// CollectDockerSummary fetches fleet-level Docker statistics.
func CollectDockerSummary(ctx context.Context, c *Client, logger *slog.Logger) (DockerSummary, error) {
	var summary DockerSummary

	ver, err := c.ServerVersion(ctx)
	if err != nil {
		logger.Warn("docker version", "error", err)
	} else {
		summary.Version = ver.Version
		summary.APIVersion = ver.APIVersion
	}

	allImages, err := c.ListImages(ctx)
	if err != nil {
		logger.Warn("list images", "error", err)
	} else {
		for _, img := range allImages {
			if len(img.RepoTags) == 0 {
				summary.DanglingImageCount++
			} else {
				summary.ImageCount++
			}
		}
	}

	vols, err := c.ListVolumes(ctx)
	if err != nil {
		logger.Warn("list volumes", "error", err)
	} else {
		summary.VolumeCount = len(vols.Volumes)
	}

	containers, err := c.ListAllContainers(ctx)
	if err != nil {
		logger.Warn("list containers", "error", err)
	} else {
		summary.TotalContainers = len(containers)
		usedVolumes := make(map[string]bool)
		for _, ct := range containers {
			switch ct.State {
			case "running":
				summary.RunningContainers++
			case "exited":
				summary.StoppedContainers++
			case "paused":
				summary.PausedContainers++
			}
			for _, m := range ct.Mounts {
				if m.Type == "volume" {
					usedVolumes[m.Name] = true
				}
			}
		}
		summary.UnusedVolumeCount = summary.VolumeCount - len(usedVolumes)
		if summary.UnusedVolumeCount < 0 {
			summary.UnusedVolumeCount = 0
		}
	}

	return summary, nil
}

// SetLogSizeMB updates the LogSizeMB field on insights that match the given path.
func SetInsightLogSize(insights []ContainerInsight, cid string, sizeMB float64) {
	for i := range insights {
		if insights[i].ID == cid {
			insights[i].LogSizeMB = sizeMB
			return
		}
	}
}

