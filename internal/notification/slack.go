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

	statusIcon := "✅"
	if r.CleanupResult.DryRun {
		statusIcon = "🛡️"
	}

	bodyText := fmt.Sprintf(
		"*Host:* %s (%s)\n"+
			"*OS:* %s\n"+
			"*Logs Cleaned:* %d\n"+
			"*Space Reclaimed:* %.1f MB\n"+
			"*Memory Used:* %s (%.0f%%)\n"+
			"*Disk Used:* %s (%.0f%%)\n"+
			"*Containers:* %d running / %d total\n"+
			"*Images:* %d (%d dangling)",
		r.Server.Hostname, r.Server.PublicIP,
		r.Server.OSInfo,
		r.CleanupResult.LogsCleaned,
		r.CleanupResult.SpaceReclaimedMB,
		r.Server.RAMUsedFormatted(), r.Server.RAMUsedPct,
		r.Server.DiskUsedFormatted(), r.Server.DiskUsedPct,
		r.Docker.RunningContainers, r.Docker.TotalContainers,
		r.Docker.ImageCount, r.Docker.DanglingImageCount,
	)

	blocks := []any{
		map[string]any{
			"type": "header",
			"text": map[string]any{
				"type": "plain_text",
				"text": fmt.Sprintf("%s Docker Cleanup: %s", statusIcon, r.ServerID),
				"emoji": true,
			},
		},
		map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": bodyText,
			},
		},
	}

	if len(r.CleanupResult.TopConsumers) > 0 {
		topText := "*Top Space Consumers:*\n"
		for i, c := range r.CleanupResult.TopConsumers {
			if i >= 3 {
				break
			}
			topText += fmt.Sprintf("• `%s` (%.1f MB)\n", c.ContainerName, c.SizeMB)
		}
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": topText,
			},
		})
	}

	blocks = append(blocks, map[string]any{
		"type": "context",
		"elements": []any{
			map[string]any{
				"type": "mrkdwn",
				"text": fmt.Sprintf("Duration: %dms | dry_run: %v | %s", r.CleanupResult.DurationMS, r.CleanupResult.DryRun, r.GeneratedAt.UTC().Format(time.RFC3339)),
			},
		},
	})

	payload := map[string]any{
		"blocks": blocks,
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
