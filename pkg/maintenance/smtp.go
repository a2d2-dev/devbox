package maintenance

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Notification struct {
	Subject string
	Body    string
}

type Notifier interface {
	Notify(context.Context, Notification) error
}

type SMTPNotifier struct {
	Config func() SMTPConfig
}

func (n SMTPNotifier) Notify(ctx context.Context, event Notification) error {
	if n.Config == nil {
		return nil
	}
	cfg := n.Config()
	if !cfg.Enabled {
		return nil
	}
	return SendSMTP(ctx, cfg, event.Subject, event.Body)
}

func ValidateSMTP(cfg SMTPConfig) error {
	if strings.TrimSpace(cfg.Host) == "" {
		return fmt.Errorf("SMTP server is required")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return fmt.Errorf("SMTP port must be between 1 and 65535")
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return fmt.Errorf("invalid SMTP sender: %w", err)
	}
	if _, err := mail.ParseAddress(cfg.To); err != nil {
		return fmt.Errorf("invalid SMTP recipient: %w", err)
	}
	switch cfg.TLS {
	case "none", "starttls", "tls":
	default:
		return fmt.Errorf("SMTP TLS mode must be none, starttls or tls")
	}
	return nil
}

func SendSMTP(ctx context.Context, cfg SMTPConfig, subject, body string) error {
	if err := ValidateSMTP(cfg); err != nil {
		return err
	}
	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	var conn net.Conn
	var err error
	if cfg.TLS == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("SMTP connection to %s failed: %w", address, err)
	}
	defer conn.Close()
	deadline := time.Now().Add(15 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set SMTP connection deadline: %w", err)
	}
	c, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("SMTP handshake with %s failed: %w", address, err)
	}
	defer c.Close()

	if cfg.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP server %s does not advertise STARTTLS", address)
		}
		if err := c.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("SMTP STARTTLS with %s failed: %w", address, err)
		}
	}
	if cfg.Username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return fmt.Errorf("SMTP server %s does not advertise AUTH", address)
		}
		if err := c.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
			return fmt.Errorf("SMTP authentication failed: %w", err)
		}
	}
	from, _ := mail.ParseAddress(cfg.From)
	to, _ := mail.ParseAddress(cfg.To)
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("SMTP sender rejected: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("SMTP recipient rejected: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA command failed: %w", err)
	}
	message := strings.Join([]string{
		"From: " + cfg.From,
		"To: " + cfg.To,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Date: " + time.Now().Format(time.RFC1123Z),
		"",
		body,
	}, "\r\n")
	if _, err := io.WriteString(w, message); err != nil {
		w.Close()
		return fmt.Errorf("send SMTP message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("finish SMTP message: %w", err)
	}
	if err := c.Quit(); err != nil {
		return fmt.Errorf("SMTP server did not accept message: %w", err)
	}
	return nil
}
