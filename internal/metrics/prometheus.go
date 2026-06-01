package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/kernelcode0/logs-cleaner/internal/cleanup"
	"github.com/kernelcode0/logs-cleaner/internal/telemetry"
)

// Registry holds all Prometheus collectors for the agent.
type Registry struct {
	CleanupRunsTotal          *prometheus.CounterVec
	CleanupLogsTruncatedTotal *prometheus.CounterVec
	CleanupSpaceReclaimedMB   *prometheus.CounterVec
	CleanupFailuresTotal      *prometheus.CounterVec

	LastCleanupDurationSeconds *prometheus.GaugeVec
	LastSpaceReclaimedMB       *prometheus.GaugeVec
	DockerRunningContainers    *prometheus.GaugeVec
	DockerTotalContainers      *prometheus.GaugeVec
	ServerRAMUsedPct           *prometheus.GaugeVec
	ServerDiskUsedPct          *prometheus.GaugeVec

	inner *prometheus.Registry
}

// NewRegistry creates and registers all metrics with a new private prometheus.Registry.
func NewRegistry(serverID string) *Registry {
	reg := prometheus.NewRegistry()

	labels := prometheus.Labels{"server_id": serverID}

	r := &Registry{inner: reg}

	r.CleanupRunsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cleanup_runs_total",
		Help:        "Total number of cleanup cycles executed.",
		ConstLabels: labels,
	}, []string{"dry_run"})

	r.CleanupLogsTruncatedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cleanup_logs_truncated_total",
		Help:        "Total number of log files truncated across all runs.",
		ConstLabels: labels,
	}, []string{})

	r.CleanupSpaceReclaimedMB = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cleanup_space_reclaimed_mb_total",
		Help:        "Cumulative MB reclaimed across all cleanup runs.",
		ConstLabels: labels,
	}, []string{})

	r.CleanupFailuresTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "cleanup_failures_total",
		Help:        "Cleanup errors by phase.",
		ConstLabels: labels,
	}, []string{"phase"})

	r.LastCleanupDurationSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cleanup_last_duration_seconds",
		Help:        "Duration of the most recent cleanup run in seconds.",
		ConstLabels: labels,
	}, []string{})

	r.LastSpaceReclaimedMB = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "cleanup_last_space_reclaimed_mb",
		Help:        "MB reclaimed in the last cleanup run.",
		ConstLabels: labels,
	}, []string{})

	r.DockerRunningContainers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "docker_running_containers",
		Help:        "Currently running containers.",
		ConstLabels: labels,
	}, []string{})

	r.DockerTotalContainers = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "docker_total_containers",
		Help:        "Total containers (all states).",
		ConstLabels: labels,
	}, []string{})

	r.ServerRAMUsedPct = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "server_ram_used_percent",
		Help:        "RAM usage percentage.",
		ConstLabels: labels,
	}, []string{})

	r.ServerDiskUsedPct = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "server_disk_used_percent",
		Help:        "Root filesystem usage percentage.",
		ConstLabels: labels,
	}, []string{})

	reg.MustRegister(
		r.CleanupRunsTotal,
		r.CleanupLogsTruncatedTotal,
		r.CleanupSpaceReclaimedMB,
		r.CleanupFailuresTotal,
		r.LastCleanupDurationSeconds,
		r.LastSpaceReclaimedMB,
		r.DockerRunningContainers,
		r.DockerTotalContainers,
		r.ServerRAMUsedPct,
		r.ServerDiskUsedPct,
	)

	return r
}

// Handler returns an http.Handler that serves /metrics.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.inner, promhttp.HandlerOpts{})
}

// RecordRun updates all counters and gauges after a cleanup cycle completes.
func (r *Registry) RecordRun(result cleanup.Result) {
	dryRunLabel := "false"
	if result.DryRun {
		dryRunLabel = "true"
	}

	r.CleanupRunsTotal.WithLabelValues(dryRunLabel).Inc()
	r.CleanupLogsTruncatedTotal.WithLabelValues().Add(float64(result.LogsCleaned))
	r.CleanupSpaceReclaimedMB.WithLabelValues().Add(result.SpaceReclaimedMB)
	r.LastCleanupDurationSeconds.WithLabelValues().Set(float64(result.DurationMS) / 1000)
	r.LastSpaceReclaimedMB.WithLabelValues().Set(result.SpaceReclaimedMB)

	// Count errors by phase
	phaseCounts := make(map[string]float64)
	for _, e := range result.Errors {
		phaseCounts[e.Phase]++
	}
	for phase, count := range phaseCounts {
		r.CleanupFailuresTotal.WithLabelValues(phase).Add(count)
	}
}

// RecordDockerTelemetry updates Docker-related gauges.
func (r *Registry) RecordDockerTelemetry(t telemetry.DockerTelemetry) {
	r.DockerRunningContainers.WithLabelValues().Set(float64(t.RunningContainers))
	r.DockerTotalContainers.WithLabelValues().Set(float64(t.TotalContainers))
}

// RecordServerTelemetry updates server-related gauges.
func (r *Registry) RecordServerTelemetry(t telemetry.ServerTelemetry) {
	r.ServerRAMUsedPct.WithLabelValues().Set(t.RAMUsedPct)
	r.ServerDiskUsedPct.WithLabelValues().Set(t.DiskUsedPct)
}
