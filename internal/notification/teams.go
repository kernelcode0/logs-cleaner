package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kernelcode0/logs-cleaner/internal/config"
	"github.com/kernelcode0/logs-cleaner/internal/reporting"
)

// TeamsNotifier sends cleanup summaries to a Microsoft Teams incoming webhook.
type TeamsNotifier struct {
	cfg    config.TeamsConfig
	client *http.Client
	logger *slog.Logger
}

// NewTeams creates a TeamsNotifier. Returns a no-op notifier if cfg.Enabled is false.
func NewTeams(cfg config.TeamsConfig, logger *slog.Logger) *TeamsNotifier {
	return &TeamsNotifier{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

func (n *TeamsNotifier) Notify(ctx context.Context, r reporting.Report) error {
	if !n.cfg.Enabled || n.cfg.WebhookURL == "" {
		return nil
	}

	payload := map[string]any{
		"type": "message",
		"attachments": []any{
			map[string]any{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content": map[string]any{
					"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
					"type":    "AdaptiveCard",
					"version": "1.4",
					"body": []any{
						map[string]any{
							"type":   "TextBlock",
							"size":   "Large",
							"weight": "Bolder",
							"text":   fmt.Sprintf("Docker Cleanup: %s", r.ServerID),
						},
						map[string]any{
							"type": "FactSet",
							"facts": []any{
								map[string]any{"title": "Logs Cleaned", "value": fmt.Sprintf("%d", r.CleanupResult.LogsCleaned)},
								map[string]any{"title": "Space Reclaimed", "value": fmt.Sprintf("%.1f MB", r.CleanupResult.SpaceReclaimedMB)},
								map[string]any{"title": "Host", "value": r.Server.Hostname},
								map[string]any{"title": "Duration", "value": fmt.Sprintf("%dms", r.CleanupResult.DurationMS)},
								map[string]any{"title": "Dry Run", "value": fmt.Sprintf("%v", r.CleanupResult.DryRun)},
							},
						},
						map[string]any{
							"type":     "TextBlock",
							"text":     r.GeneratedAt.UTC().Format(time.RFC3339),
							"isSubtle": true,
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("teams: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("teams: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("teams: post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("teams: unexpected status %d", resp.StatusCode)
	}
	n.logger.Info("teams notification sent")
	return nil
}
