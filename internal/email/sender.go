package email

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	gomail "github.com/wneessen/go-mail"

	"github.com/kernelcode0/logs-cleaner/internal/config"
)

// Sender sends HTML emails via SMTP.
type Sender struct {
	cfg    config.EmailConfig
	logger *slog.Logger
}

// New creates an email Sender. Does not open a connection at construction time.
func New(cfg config.EmailConfig, logger *slog.Logger) *Sender {
	return &Sender{cfg: cfg, logger: logger}
}

// Send delivers an HTML email to all configured recipients.
func (s *Sender) Send(ctx context.Context, subject, htmlBody string) error {
	if !s.cfg.Enabled {
		s.logger.Debug("email disabled, skipping send")
		return nil
	}
	if s.cfg.SMTPHost == "" {
		return fmt.Errorf("email: smtp_host is not configured")
	}

	tlsPolicy := gomail.TLSMandatory
	if s.cfg.SMTPPort == 465 {
		tlsPolicy = gomail.TLSMandatory
	}

	opts := []gomail.Option{
		gomail.WithPort(s.cfg.SMTPPort),
		gomail.WithTLSPolicy(tlsPolicy),
	}
	if s.cfg.SMTPUser != "" {
		opts = append(opts,
			gomail.WithSMTPAuth(gomail.SMTPAuthPlain),
			gomail.WithUsername(s.cfg.SMTPUser),
			gomail.WithPassword(s.cfg.SMTPPassword),
		)
	}

	client, err := gomail.NewClient(s.cfg.SMTPHost, opts...)
	if err != nil {
		return fmt.Errorf("email: create client: %w", err)
	}

	msg := gomail.NewMsg()
	if err := msg.FromFormat("Docker Log Cleaner", s.cfg.SMTPFrom); err != nil {
		return fmt.Errorf("email: set from: %w", err)
	}
	if err := msg.To(s.cfg.To...); err != nil {
		return fmt.Errorf("email: set to: %w", err)
	}
	msg.Subject(subject)
	msg.SetBodyString(gomail.TypeTextHTML, htmlBody)

	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		s.logger.Error("smtp send failed", "to", strings.Join(s.cfg.To, ","), "subject", subject, "error", err)
		return fmt.Errorf("email: send: %w", err)
	}

	s.logger.Info("email sent", "to", strings.Join(s.cfg.To, ","), "subject", subject)
	return nil
}
