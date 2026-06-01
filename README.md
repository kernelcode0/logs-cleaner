# logs-cleaner

A production-grade Go application that monitors and cleans Docker container log files, collects server and Docker telemetry, and delivers professional HTML reports via email. Designed to run on any Linux Docker host.

## Features

- **Log cleanup** — Discovers and truncates oversized Docker container logs (`-json.log` files)
- **Dry run mode** — Generate reports without truncating any files
- **Configurable exclusion list** — Protect critical containers from cleanup
- **Email reports** — Professional dark/light HTML emails with system stats, progress bars, and top consumers table
- **Slack notifications** — Webhook summary after each cleanup run
- **Microsoft Teams notifications** — Adaptive Card webhook support
- **Health endpoint** — `GET /health` for container orchestration
- **Prometheus metrics** — `GET /metrics` with cleanup, Docker, and server gauges
- **REST API** — `GET /api/history`, `/api/stats`, `/api/containers`
- **SQLite history** — Persistent run history with per-container tracking
- **Log rotation recommendations** — Flags containers cleaned 18+ times in 30 days
- **Self-monitoring** — Tracks SMTP failures, Docker API errors, cleanup errors
- **Cron scheduler** — Runs every 12h by default (configurable)
- **Multi-stage Docker build** — 7.8 MB final image from scratch

---

## Quick Start

```bash
# 1. Copy and edit environment file
cp .env.example .env
$EDITOR .env

# 2. Copy and edit config
cp config/config.yaml ./config/config.yaml
$EDITOR config/config.yaml

# 3. Start
docker-compose up -d

# 4. Verify
curl http://localhost:8080/health
curl http://localhost:8080/api/stats
```

---

## Deployment

### docker-compose (recommended)

```bash
docker-compose up -d
docker-compose logs -f
```

### docker run

```bash
docker run -d \
  --name logs-cleaner \
  --restart unless-stopped \
  --user 0:0 \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v /var/lib/docker/containers:/var/lib/docker/containers:rw \
  -v ./data:/data \
  -v ./config/config.yaml:/etc/logs-cleaner/config.yaml:ro \
  -p 8080:8080 \
  --env-file .env \
  logs-cleaner:latest
```

---

## Configuration

All settings can be overridden via environment variables with the `AGENT_` prefix.
Nested keys use `_` as separator: `email.smtp_host` → `AGENT_EMAIL_SMTP_HOST`.

### config/config.yaml reference

```yaml
server_id: "production-eu-01"    # Identifies this host in reports & API
dry_run: false                    # true = reports only, no truncation

cleanup:
  threshold_mb: 100               # Truncate logs larger than this
  top_consumers: 5                # How many containers to show in report
  mode: "truncate"                # truncate | delete (rotate/compress: future)
  excluded_containers:
    - "db-prod"                   # Never truncate these
    - "auth-server"
  log_glob: "/var/lib/docker/containers/**/*-json.log"

schedule:
  cron: "0 */12 * * *"           # Every 12 hours

email:
  enabled: true
  smtp_host: "smtp.example.com"
  smtp_port: 587
  smtp_user: ""
  smtp_password: ""               # Use AGENT_EMAIL_SMTP_PASSWORD env var
  smtp_from: "docker-cleanup@example.com"
  to: ["ops@example.com"]
  theme: "dark"                   # dark | light

notifications:
  slack:
    enabled: false
    webhook_url: ""               # Use AGENT_NOTIF_SLACK_WEBHOOK_URL
  teams:
    enabled: false
    webhook_url: ""               # Use AGENT_NOTIF_TEAMS_WEBHOOK_URL

server:
  listen: ":8080"

storage:
  path: "/data/cleanup.db"

recommendations:
  window_days: 30
  trigger_count: 18              # Flag containers cleaned this many times

log:
  level: "info"                  # debug | info | warn | error
  format: "json"                 # json | text
```

---

## API Reference

### GET /health

```json
{"status": "ok", "version": "1.0.0", "server_id": "production-eu-01"}
```

Returns `503` with `"status": "degraded"` if Docker socket is unreachable.

### GET /metrics

Prometheus text format. Key metrics:

```
cleanup_runs_total{server_id, dry_run}
cleanup_logs_truncated_total{server_id}
cleanup_space_reclaimed_mb_total{server_id}
cleanup_failures_total{server_id, phase}
cleanup_last_duration_seconds{server_id}
docker_running_containers{server_id}
server_ram_used_percent{server_id}
server_disk_used_percent{server_id}
```

### GET /api/history?limit=20&server_id=<id>

Returns the last N cleanup runs.

### GET /api/stats?server_id=<id>

Returns aggregate totals: runs, logs cleaned, space reclaimed, average duration.

### GET /api/containers?limit=10&window_days=30

Returns top containers by cleanup frequency in the rolling window.

---

## Security Notes

- **Credentials** — SMTP password and webhook URLs are only read from environment variables. Never logged.
- **Non-root** — Dockerfile runs as `USER 65534:65534` (nobody). The compose file uses `user: "0:0"` by default because Docker log files are owned by root. To run non-root: grant group-write on `/var/lib/docker/containers` on the host, then remove the `user:` override.
- **Minimal image** — Built `FROM scratch`: no shell, no package manager, ~7.8 MB total.
- **Read-only socket** — Docker socket is mounted `:ro`; log truncation writes directly to log files on the host filesystem.

---

## Building from Source

```bash
# Requirements: Go 1.22+, CGO_ENABLED=0
CGO_ENABLED=0 go build -o logs-cleaner ./cmd/agent

# Or via Docker (no local Go required):
docker build -t logs-cleaner:dev .
```

---

## Upgrade from Bash Script

The Go agent preserves all behavior from `claer-docker-logs.sh`:

| Bash feature | Go equivalent |
|---|---|
| `LOG_THRESHOLD_MB=100` | `cleanup.threshold_mb: 100` |
| `EXCLUDED_CONTAINERS=(...)` | `cleanup.excluded_containers: [...]` |
| `LOGS_TO_REPORT=5` | `cleanup.top_consumers: 5` |
| `TO_EMAIL="..."` | `email.to: [...]` |
| `msmtp` | Embedded SMTP via `go-mail` |
| `curl https://api.ipify.org` | `telemetry.public_ip_url` |
| Cron every 12h | `schedule.cron: "0 */12 * * *"` |
| `truncate -s 0` | `cleanup.mode: truncate` |

New capabilities not in the Bash script: dry run mode, Slack/Teams notifications, health endpoint, Prometheus metrics, REST API, SQLite history, log rotation recommendations, self-monitoring, dark/light email themes.

---

## Contributing

We welcome contributions! Please see our [Contributing Guidelines](CONTRIBUTING.md) to get started, and note that all contributors are expected to follow our [Code of Conduct](CODE_OF_CONDUCT.md).