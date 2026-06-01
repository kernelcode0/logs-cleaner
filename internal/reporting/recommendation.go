package reporting

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/myorg/docker-cleanup-agent/internal/docker"
)

// Recommendation flags a container for log rotation setup.
type Recommendation struct {
	ContainerName string
	Image         string
	CleanCount    int
	WindowDays    int
	TriggerCount  int
	Message       string
}

// StorageReader provides cleanup frequency queries for the recommendation engine.
type StorageReader interface {
	GetContainerCleanupCount(ctx context.Context, containerName string, windowDays int) (int, error)
}

// Analyze inspects cleanup history and returns recommendations for containers
// that have been cleaned >= triggerCount times in the past windowDays.
func Analyze(
	ctx context.Context,
	db StorageReader,
	insights []docker.ContainerInsight,
	windowDays int,
	triggerCount int,
	logger *slog.Logger,
) ([]Recommendation, error) {
	seen := make(map[string]bool)
	var recs []Recommendation

	for _, insight := range insights {
		if insight.Name == "" || seen[insight.Name] {
			continue
		}
		seen[insight.Name] = true

		count, err := db.GetContainerCleanupCount(ctx, insight.Name, windowDays)
		if err != nil {
			logger.Warn("recommendation engine query failed", "container", insight.Name, "error", err)
			continue
		}
		if count >= triggerCount {
			recs = append(recs, Recommendation{
				ContainerName: insight.Name,
				Image:         insight.Image,
				CleanCount:    count,
				WindowDays:    windowDays,
				TriggerCount:  triggerCount,
				Message: fmt.Sprintf(
					"Container '%s' has been cleaned %d times in %d days. "+
						"Consider configuring Docker log rotation (--log-opt max-size=50m --log-opt max-file=3).",
					insight.Name, count, windowDays,
				),
			})
		}
	}
	return recs, nil
}
