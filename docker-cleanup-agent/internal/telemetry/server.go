package telemetry

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ServerTelemetry holds a snapshot of host-level metrics.
type ServerTelemetry struct {
	Hostname       string
	PublicIP       string
	OSInfo         string
	Uptime         string
	CPULoad1       float64
	CPULoad5       float64
	CPULoad15      float64
	RAMUsedBytes   uint64
	RAMTotalBytes  uint64
	RAMUsedPct     float64
	DiskUsedBytes  uint64
	DiskTotalBytes uint64
	DiskUsedPct    float64
	CollectedAt    time.Time
}

// RAMUsedFormatted returns a human-readable RAM usage string like "3.2 GB / 16.0 GB".
func (t ServerTelemetry) RAMUsedFormatted() string {
	return fmt.Sprintf("%s / %s", formatBytes(t.RAMUsedBytes), formatBytes(t.RAMTotalBytes))
}

// DiskUsedFormatted returns a human-readable disk usage string.
func (t ServerTelemetry) DiskUsedFormatted() string {
	return fmt.Sprintf("%s free out of %s", formatBytes(t.DiskTotalBytes-t.DiskUsedBytes), formatBytes(t.DiskTotalBytes))
}

func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// Collect gathers all server telemetry. Non-fatal fields degrade to zero values on error.
func Collect(ctx context.Context, publicIPURL string, logger *slog.Logger) (ServerTelemetry, error) {
	t := ServerTelemetry{CollectedAt: time.Now().UTC()}

	hostname, err := os.Hostname()
	if err != nil {
		logger.Warn("hostname", "error", err)
	}
	t.Hostname = hostname

	t.PublicIP = fetchPublicIP(ctx, publicIPURL, logger)
	t.OSInfo = readOSInfo(logger)
	t.Uptime, _ = readUptime()
	t.CPULoad1, t.CPULoad5, t.CPULoad15 = readLoadAvg(logger)
	t.RAMUsedBytes, t.RAMTotalBytes = readMemInfo(logger)
	if t.RAMTotalBytes > 0 {
		t.RAMUsedPct = float64(t.RAMUsedBytes) / float64(t.RAMTotalBytes) * 100
	}
	t.DiskUsedBytes, t.DiskTotalBytes = readDiskUsage(logger)
	if t.DiskTotalBytes > 0 {
		t.DiskUsedPct = float64(t.DiskUsedBytes) / float64(t.DiskTotalBytes) * 100
	}

	return t, nil
}

func fetchPublicIP(ctx context.Context, url string, logger *slog.Logger) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Warn("public IP lookup", "error", err)
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	return strings.TrimSpace(string(body))
}

func readOSInfo(logger *slog.Logger) string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return "Linux"
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			val := strings.TrimPrefix(line, "PRETTY_NAME=")
			return strings.Trim(val, `"`)
		}
	}
	return "Linux"
}

func readUptime() (string, error) {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty /proc/uptime")
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "", err
	}
	return formatUptime(int64(secs)), nil
}

func formatUptime(secs int64) string {
	days := secs / 86400
	hours := (secs % 86400) / 3600
	mins := (secs % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("up %d days, %d hours, %d minutes", days, hours, mins)
	}
	return fmt.Sprintf("up %d hours, %d minutes", hours, mins)
}

func readLoadAvg(logger *slog.Logger) (load1, load5, load15 float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		logger.Warn("read /proc/loadavg", "error", err)
		return
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return
	}
	load1, _ = strconv.ParseFloat(fields[0], 64)
	load5, _ = strconv.ParseFloat(fields[1], 64)
	load15, _ = strconv.ParseFloat(fields[2], 64)
	return
}

func readMemInfo(logger *slog.Logger) (used, total uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		logger.Warn("read /proc/meminfo", "error", err)
		return
	}
	defer f.Close()

	var memTotal, memAvail uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			memTotal = val * 1024
		case "MemAvailable:":
			memAvail = val * 1024
		}
	}
	if memTotal > 0 && memAvail <= memTotal {
		return memTotal - memAvail, memTotal
	}
	return 0, memTotal
}

func readDiskUsage(logger *slog.Logger) (used, total uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		logger.Warn("statfs /", "error", err)
		return
	}
	blockSize := uint64(stat.Bsize)
	total = stat.Blocks * blockSize
	avail := stat.Bavail * blockSize
	if total >= avail {
		used = total - avail
	}
	return
}
