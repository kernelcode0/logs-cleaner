package email

import (
	"github.com/myorg/docker-cleanup-agent/internal/reporting"
	"github.com/myorg/docker-cleanup-agent/templates"
)

// RenderReport renders the HTML report for the given theme.
func RenderReport(r reporting.Report, theme string) (string, error) {
	return reporting.RenderHTML(r, templates.FS, string(theme))
}
