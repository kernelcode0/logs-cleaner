package metrics

import (
	"sync/atomic"

	"github.com/myorg/docker-cleanup-agent/internal/reporting"
)

// SelfMonitor tracks agent operational health counters using atomic operations.
type SelfMonitor struct {
	smtpFailures      atomic.Int64
	dockerAPIFailures atomic.Int64
	cleanupErrors     atomic.Int64
	notifFailures     atomic.Int64
	lastDurationMS    atomic.Int64
}

// NewSelfMonitor creates a zeroed SelfMonitor.
func NewSelfMonitor() *SelfMonitor {
	return &SelfMonitor{}
}

func (m *SelfMonitor) IncrSMTPFailure()      { m.smtpFailures.Add(1) }
func (m *SelfMonitor) IncrDockerAPIFailure() { m.dockerAPIFailures.Add(1) }
func (m *SelfMonitor) IncrCleanupError()     { m.cleanupErrors.Add(1) }
func (m *SelfMonitor) IncrNotifFailure()     { m.notifFailures.Add(1) }
func (m *SelfMonitor) SetLastDurationMS(ms int64) { m.lastDurationMS.Store(ms) }

// Snapshot returns a point-in-time copy of all counters.
func (m *SelfMonitor) Snapshot() reporting.SelfMonSnapshot {
	return reporting.SelfMonSnapshot{
		LastCleanupDurationMS: m.lastDurationMS.Load(),
		SMTPFailures:          m.smtpFailures.Load(),
		DockerAPIFailures:     m.dockerAPIFailures.Load(),
		CleanupErrors:         m.cleanupErrors.Load(),
		NotifFailures:         m.notifFailures.Load(),
	}
}
