package notification

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

type SMTPConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string
	TLS      bool
}

type SMTPEmailSender struct {
	cfg SMTPConfig
}

func NewSMTPEmailSender(cfg SMTPConfig) *SMTPEmailSender {
	return &SMTPEmailSender{cfg: cfg}
}

func (s *SMTPEmailSender) SendAlert(ctx context.Context, subject, body string) error {
	to := s.cfg.User
	if to == "" {
		to = s.cfg.From
	}

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html><body>
<div style="font-family:Arial,sans-serif;max-width:600px;margin:0 auto;">
<h2 style="color:#1a1a2e;">%s</h2>
<div style="padding:16px;background:#f5f5f5;border-radius:8px;">
<pre style="white-space:pre-wrap;font-size:14px;">%s</pre>
</div>
<p style="color:#888;font-size:12px;margin-top:16px;">Argus Notification Service</p>
</div>
</body></html>`, subject, body)

	msg := strings.Builder{}
	msg.WriteString(fmt.Sprintf("From: %s\r\n", s.cfg.From))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)

	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	var auth smtp.Auth
	if s.cfg.User != "" {
		auth = smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host)
	}

	if s.cfg.TLS {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: s.cfg.Host})
		if err != nil {
			return fmt.Errorf("notification: smtp tls dial: %w", err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, s.cfg.Host)
		if err != nil {
			return fmt.Errorf("notification: smtp client: %w", err)
		}
		defer client.Close()

		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("notification: smtp auth: %w", err)
			}
		}
		if err := client.Mail(s.cfg.From); err != nil {
			return fmt.Errorf("notification: smtp mail from: %w", err)
		}
		if err := client.Rcpt(to); err != nil {
			return fmt.Errorf("notification: smtp rcpt to: %w", err)
		}
		w, err := client.Data()
		if err != nil {
			return fmt.Errorf("notification: smtp data: %w", err)
		}
		if _, err := w.Write([]byte(msg.String())); err != nil {
			return fmt.Errorf("notification: smtp write: %w", err)
		}
		return w.Close()
	}

	return smtp.SendMail(addr, auth, s.cfg.From, []string{to}, []byte(msg.String()))
}
