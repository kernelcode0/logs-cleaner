package cleanup

import (
	"context"
	"fmt"
	"os"

	"github.com/kernelcode0/logs-cleaner/internal/config"
)

// Policy defines how a log file is cleaned.
type Policy interface {
	Apply(ctx context.Context, logFilePath string) (reclaimedBytes int64, err error)
	Name() string
}

// ForMode returns the Policy for the given CleanupMode.
func ForMode(mode config.CleanupMode) (Policy, error) {
	switch mode {
	case config.CleanupModeTruncate:
		return NewTruncatePolicy(), nil
	case config.CleanupModeDelete:
		return NewDeletePolicy(), nil
	case config.CleanupModeRotate:
		return NewRotatePolicy(), nil
	case config.CleanupModeCompress:
		return NewCompressPolicy(), nil
	default:
		return nil, fmt.Errorf("unknown cleanup mode: %q", mode)
	}
}

// truncatePolicy truncates log files to 0 bytes — mirrors `truncate -s 0`.
type truncatePolicy struct{}

func NewTruncatePolicy() Policy { return &truncatePolicy{} }

func (p *truncatePolicy) Name() string { return "truncate" }

func (p *truncatePolicy) Apply(_ context.Context, logFilePath string) (int64, error) {
	info, err := os.Stat(logFilePath)
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", logFilePath, err)
	}
	size := info.Size()
	if err := os.Truncate(logFilePath, 0); err != nil {
		return 0, fmt.Errorf("truncate %s: %w", logFilePath, err)
	}
	return size, nil
}

// deletePolicy removes the log file entirely.
type deletePolicy struct{}

func NewDeletePolicy() Policy { return &deletePolicy{} }

func (p *deletePolicy) Name() string { return "delete" }

func (p *deletePolicy) Apply(_ context.Context, logFilePath string) (int64, error) {
	info, err := os.Stat(logFilePath)
	if err != nil {
		return 0, fmt.Errorf("stat %s: %w", logFilePath, err)
	}
	size := info.Size()
	if err := os.Remove(logFilePath); err != nil {
		return 0, fmt.Errorf("delete %s: %w", logFilePath, err)
	}
	return size, nil
}

// rotatePolicy is not yet implemented.
type rotatePolicy struct{}

func NewRotatePolicy() Policy { return &rotatePolicy{} }

func (p *rotatePolicy) Name() string { return "rotate" }

func (p *rotatePolicy) Apply(_ context.Context, logFilePath string) (int64, error) {
	return 0, fmt.Errorf("rotate policy not yet implemented")
}

// compressPolicy is not yet implemented.
type compressPolicy struct{}

func NewCompressPolicy() Policy { return &compressPolicy{} }

func (p *compressPolicy) Name() string { return "compress" }

func (p *compressPolicy) Apply(_ context.Context, logFilePath string) (int64, error) {
	return 0, fmt.Errorf("compress policy not yet implemented")
}
