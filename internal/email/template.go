package email

import (
	"github.com/kernelcode0/logs-cleaner/internal/reporting"
	"github.com/kernelcode0/logs-cleaner/templates"
)

// RenderReport renders the HTML report for the given theme.
func RenderReport(r reporting.Report, theme string) (string, error) {
	return reporting.RenderHTML(r, templates.FS, string(theme))
}
