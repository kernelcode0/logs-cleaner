package config

import (
	"fmt"
	"strings"

	"github.com/robfig/cron/v3"
	"github.com/spf13/viper"
)

type CleanupMode string

const (
	CleanupModeTruncate  CleanupMode = "truncate"
	CleanupModeDelete    CleanupMode = "delete"
	CleanupModeRotate    CleanupMode = "rotate"
	CleanupModeCompress  CleanupMode = "compress"
)

type EmailTheme string

const (
	EmailThemeDark  EmailTheme = "dark"
	EmailThemeLight EmailTheme = "light"
)

type Config struct {
	ServerID        string                `mapstructure:"server_id"`
	DryRun          bool                  `mapstructure:"dry_run"`
	Cleanup         CleanupConfig         `mapstructure:"cleanup"`
	Schedule        ScheduleConfig        `mapstructure:"schedule"`
	Email           EmailConfig           `mapstructure:"email"`
	Notifications   NotificationsConfig   `mapstructure:"notifications"`
	Server          ServerConfig          `mapstructure:"server"`
	Storage         StorageConfig         `mapstructure:"storage"`
	Telemetry       TelemetryConfig       `mapstructure:"telemetry"`
	Recommendations RecommendationsConfig `mapstructure:"recommendations"`
	Log             LogConfig             `mapstructure:"log"`
}

type CleanupConfig struct {
	ThresholdMB        int         `mapstructure:"threshold_mb"`
	TopConsumers       int         `mapstructure:"top_consumers"`
	Mode               CleanupMode `mapstructure:"mode"`
	ExcludedContainers []string    `mapstructure:"excluded_containers"`
	LogGlob            string      `mapstructure:"log_glob"`
}

type ScheduleConfig struct {
	Cron string `mapstructure:"cron"`
}

type EmailConfig struct {
	Enabled       bool       `mapstructure:"enabled"`
	SMTPHost      string     `mapstructure:"smtp_host"`
	SMTPPort      int        `mapstructure:"smtp_port"`
	SMTPUser      string     `mapstructure:"smtp_user"`
	SMTPPassword  string     `mapstructure:"smtp_password"`
	SMTPFrom      string     `mapstructure:"smtp_from"`
	To            []string   `mapstructure:"to"`
	SubjectPrefix string     `mapstructure:"subject_prefix"`
	Theme         EmailTheme `mapstructure:"theme"`
}

type NotificationsConfig struct {
	Slack SlackConfig `mapstructure:"slack"`
	Teams TeamsConfig `mapstructure:"teams"`
}

type SlackConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	WebhookURL string `mapstructure:"webhook_url"`
}

type TeamsConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	WebhookURL string `mapstructure:"webhook_url"`
}

type ServerConfig struct {
	Listen   string     `mapstructure:"listen"`
	Enabled  bool       `mapstructure:"enabled"`
	Auth     AuthConfig `mapstructure:"auth"`
}

type AuthConfig struct {
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
}

type StorageConfig struct {
	Path string `mapstructure:"path"`
}

type TelemetryConfig struct {
	PublicIPURL string `mapstructure:"public_ip_url"`
}

type RecommendationsConfig struct {
	WindowDays   int `mapstructure:"window_days"`
	TriggerCount int `mapstructure:"trigger_count"`
}

type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

func Load() (*Config, error) {
	v := viper.New()

	v.SetDefault("server_id", "default")
	v.SetDefault("dry_run", false)
	v.SetDefault("cleanup.threshold_mb", 100)
	v.SetDefault("cleanup.top_consumers", 5)
	v.SetDefault("cleanup.mode", "truncate")
	v.SetDefault("cleanup.excluded_containers", []string{})
	v.SetDefault("cleanup.log_glob", "/var/lib/docker/containers/**/*-json.log")
	v.SetDefault("schedule.cron", "0 */12 * * *")
	v.SetDefault("email.enabled", true)
	v.SetDefault("email.smtp_port", 587)
	v.SetDefault("email.subject_prefix", "[Docker Log Cleanup]")
	v.SetDefault("email.theme", "dark")
	v.SetDefault("notifications.slack.enabled", false)
	v.SetDefault("notifications.teams.enabled", false)
	v.SetDefault("server.listen", ":8080")
	v.SetDefault("server.enabled", true)
	v.SetDefault("server.auth.username", "")
	v.SetDefault("server.auth.password", "")
	v.SetDefault("storage.path", "/data/cleanup.db")
	v.SetDefault("telemetry.public_ip_url", "https://api.ipify.org")
	v.SetDefault("recommendations.window_days", 30)
	v.SetDefault("recommendations.trigger_count", 18)
	v.SetDefault("log.level", "info")
	v.SetDefault("log.format", "json")

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("/etc/docker-cleanup-agent/")
	v.AddConfigPath("./config")
	v.AddConfigPath(".")

	v.SetEnvPrefix("AGENT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("reading config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func MustLoad() *Config {
	cfg, err := Load()
	if err != nil {
		panic(fmt.Sprintf("config: %v", err))
	}
	return cfg
}

func validate(cfg *Config) error {
	validModes := map[CleanupMode]bool{
		CleanupModeTruncate: true,
		CleanupModeDelete:   true,
		CleanupModeRotate:   true,
		CleanupModeCompress: true,
	}
	if !validModes[cfg.Cleanup.Mode] {
		return fmt.Errorf("invalid cleanup.mode %q: must be one of truncate, delete, rotate, compress", cfg.Cleanup.Mode)
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	if _, err := parser.Parse(cfg.Schedule.Cron); err != nil {
		return fmt.Errorf("invalid schedule.cron %q: %w", cfg.Schedule.Cron, err)
	}

	if cfg.Email.SMTPPort < 1 || cfg.Email.SMTPPort > 65535 {
		return fmt.Errorf("invalid email.smtp_port %d", cfg.Email.SMTPPort)
	}

	return nil
}
