package telemetry

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/myorg/docker-cleanup-agent/internal/docker"
)

// DockerTelemetry holds Docker daemon statistics.
type DockerTelemetry struct {
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
	CollectedAt        time.Time
}

// ContainersFormatted returns "N running / M total".
func (t DockerTelemetry) ContainersFormatted() string {
	return strings.Join([]string{
		itoa(t.RunningContainers) + " running",
		itoa(t.TotalContainers) + " total",
	}, " / ")
}

func itoa(n int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + intToStr(n))
}

func intToStr(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 20)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	if neg {
		buf = append(buf, '-')
	}
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}
	return string(buf)
}

// CollectDocker queries the Docker daemon for fleet-level statistics.
func CollectDocker(ctx context.Context, c *docker.Client, logger *slog.Logger) (DockerTelemetry, error) {
	summary, err := docker.CollectDockerSummary(ctx, c, logger)
	if err != nil {
		return DockerTelemetry{}, err
	}
	return DockerTelemetry{
		Version:            summary.Version,
		APIVersion:         summary.APIVersion,
		ImageCount:         summary.ImageCount,
		DanglingImageCount: summary.DanglingImageCount,
		VolumeCount:        summary.VolumeCount,
		UnusedVolumeCount:  summary.UnusedVolumeCount,
		TotalContainers:    summary.TotalContainers,
		RunningContainers:  summary.RunningContainers,
		StoppedContainers:  summary.StoppedContainers,
		PausedContainers:   summary.PausedContainers,
		CollectedAt:        time.Now().UTC(),
	}, nil
}
