package reporting

import (
	"fmt"
	"html/template"
	"io/fs"
	"strings"
	"time"

	"github.com/kernelcode0/logs-cleaner/internal/cleanup"
	"github.com/kernelcode0/logs-cleaner/internal/storage"
	"github.com/kernelcode0/logs-cleaner/internal/telemetry"
)

// SelfMonSnapshot captures agent health counters at report time.
type SelfMonSnapshot struct {
	LastCleanupDurationMS int64
	SMTPFailures          int64
	DockerAPIFailures     int64
	CleanupErrors         int64
	NotifFailures         int64
}

// Report is the complete data model for one cleanup cycle.
type Report struct {
	ServerID        string
	GeneratedAt     time.Time
	CleanupResult   cleanup.Result
	Server          telemetry.ServerTelemetry
	Docker          telemetry.DockerTelemetry
	Recommendations []Recommendation
	RecentRuns      []storage.RunRecord
	SelfMon         SelfMonSnapshot
	NextRunAt       time.Time
}

// Build assembles a Report from all subsystem outputs.
func Build(
	serverID string,
	cleanupResult cleanup.Result,
	server telemetry.ServerTelemetry,
	docker telemetry.DockerTelemetry,
	recommendations []Recommendation,
	recentRuns []storage.RunRecord,
	selfMon SelfMonSnapshot,
	nextRunAt time.Time,
) Report {
	return Report{
		ServerID:        serverID,
		GeneratedAt:     time.Now().UTC(),
		CleanupResult:   cleanupResult,
		Server:          server,
		Docker:          docker,
		Recommendations: recommendations,
		RecentRuns:      recentRuns,
		SelfMon:         selfMon,
		NextRunAt:       nextRunAt,
	}
}

// SubjectLine returns the formatted email subject line.
func SubjectLine(prefix, serverID string, t time.Time) string {
	return fmt.Sprintf("%s %s — %s", prefix, serverID, t.Format("2006-01-02 15:04:05 UTC"))
}

// RenderHTML renders the Report to an HTML string using the named theme template.
func RenderHTML(r Report, templateFS fs.FS, themeName string) (string, error) {
	renderer, err := newRenderer(templateFS)
	if err != nil {
		return "", err
	}
	return renderer.Render(themeName, r)
}

// TemplateRenderer holds parsed templates keyed by theme name.
type TemplateRenderer struct {
	templates map[string]*template.Template
}

func newRenderer(templateFS fs.FS) (*TemplateRenderer, error) {
	funcMap := template.FuncMap{
		"formatMB": func(mb float64) string {
			return fmt.Sprintf("%.1f MB", mb)
		},
		"formatPct": func(pct float64) string {
			return fmt.Sprintf("%.0f%%", pct)
		},
		"pctBarWidth": func(pct float64) string {
			if pct < 0 {
				pct = 0
			}
			if pct > 100 {
				pct = 100
			}
			return fmt.Sprintf("%.0f%%", pct)
		},
		"formatTime": func(t time.Time) string {
			return t.UTC().Format("2006-01-02 15:04:05 UTC")
		},
		"formatDuration": func(ms int64) string {
			return formatDuration(ms)
		},
		"add": func(a, b int) int {
			return a + b
		},
		"isDryRun": func(dry bool) string {
			if dry {
				return "DRY RUN"
			}
			return "Completed"
		},
		"isDryRunBadgeColor": func(dry bool) string {
			if dry {
				return "#f59e0b"
			}
			return "#10b981"
		},
		"cpuLoad": func(r Report) string {
			return fmt.Sprintf("%.2f, %.2f, %.2f", r.Server.CPULoad1, r.Server.CPULoad5, r.Server.CPULoad15)
		},
		"nextRun": func(t time.Time) string {
			if t.IsZero() {
				return "12h"
			}
			return t.UTC().Format("2006-01-02 15:04 UTC")
		},
		"hasRecommendations": func(recs []Recommendation) bool {
			return len(recs) > 0
		},
		"growthRate": func(rate float64) string {
			if rate <= 0 {
				return "—"
			}
			return fmt.Sprintf("%.2f MB/h", rate)
		},
	}

	themes := []string{"dark", "light"}
	templates := make(map[string]*template.Template, len(themes))

	for _, theme := range themes {
		name := fmt.Sprintf("report_%s.html", theme)
		tmpl, err := template.New(name).Funcs(funcMap).ParseFS(templateFS, name)
		if err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
		templates[theme] = tmpl
	}

	return &TemplateRenderer{templates: templates}, nil
}

func (r *TemplateRenderer) Render(theme string, data Report) (string, error) {
	tmpl, ok := r.templates[theme]
	if !ok {
		return "", fmt.Errorf("unknown template theme %q", theme)
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return sb.String(), nil
}

func formatDuration(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	secs := ms / 1000
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	mins := secs / 60
	secs = secs % 60
	return fmt.Sprintf("%dm %ds", mins, secs)
}
