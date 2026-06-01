package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/myorg/docker-cleanup-agent/internal/config"
	"github.com/myorg/docker-cleanup-agent/internal/reporting"
)

// SlackNotifier sends cleanup summaries to a Slack incoming webhook.
type SlackNotifier struct {
	cfg    config.SlackConfig
	client *http.Client
	logger *slog.Logger
}

// NewSlack creates a SlackNotifier. Returns a no-op notifier if cfg.Enabled is false.
func NewSlack(cfg config.SlackConfig, logger *slog.Logger) *SlackNotifier {
	return &SlackNotifier{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		logger: logger,
	}
}

func (n *SlackNotifier) Notify(ctx context.Context, r reporting.Report) error {
	if !n.cfg.Enabled || n.cfg.WebhookURL == "" {
		return nil
	}

	payload := map[string]any{
		"blocks": []any{
			map[string]any{
				"type": "header",
				"text": map[string]any{
					"type": "plain_text",
					"text": fmt.Sprintf("Docker Cleanup: %s", r.ServerID),
				},
			},
			map[string]any{
				"type": "section",
				"fields": []any{
					map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*Logs Cleaned:* %d", r.CleanupResult.LogsCleaned)},
					map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*Space Reclaimed:* %.1f MB", r.CleanupResult.SpaceReclaimedMB)},
					map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*Host:* %s", r.Server.Hostname)},
					map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*Duration:* %dms", r.CleanupResult.DurationMS)},
				},
			},
			map[string]any{
				"type": "context",
				"elements": []any{
					map[string]any{
						"type": "mrkdwn",
						"text": fmt.Sprintf("%s | dry_run: %v", r.GeneratedAt.UTC().Format(time.RFC3339), r.CleanupResult.DryRun),
					},
				},
			},
		},
	}

	return n.post(ctx, payload)
}

func (n *SlackNotifier) post(ctx context.Context, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("slack: marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("slack: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack: post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack: unexpected status %d", resp.StatusCode)
	}
	n.logger.Info("slack notification sent", "server_id", "")
	return nil
}
