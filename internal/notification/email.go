package notification

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"mime/multipart"
	"net/smtp"
	"net/textproto"
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

func (s *SMTPEmailSender) smtpAuth() smtp.Auth {
	if s.cfg.User != "" {
		return smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host)
	}
	return nil
}

func (s *SMTPEmailSender) smtpSend(addr string, auth smtp.Auth, rawMsg []byte, to string) error {
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
		if _, err := w.Write(rawMsg); err != nil {
			return fmt.Errorf("notification: smtp write: %w", err)
		}
		return w.Close()
	}

	return smtp.SendMail(addr, auth, s.cfg.From, []string{to}, rawMsg)
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
	return s.smtpSend(addr, s.smtpAuth(), []byte(msg.String()), to)
}

// SendTo sends a multipart/alternative (text + HTML) message to a single recipient.
// Independent of SendAlert's alert-specific formatting; used by flows like password reset.
func (s *SMTPEmailSender) SendTo(ctx context.Context, to, subject, textBody, htmlBody string) error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	var bodyBuf bytes.Buffer
	mw := multipart.NewWriter(&bodyBuf)

	pw, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type": {"text/plain; charset=utf-8"},
	})
	if err != nil {
		return fmt.Errorf("notification: sendto create text part: %w", err)
	}
	if _, err := fmt.Fprint(pw, textBody); err != nil {
		return fmt.Errorf("notification: sendto write text part: %w", err)
	}

	pw, err = mw.CreatePart(textproto.MIMEHeader{
		"Content-Type": {"text/html; charset=utf-8"},
	})
	if err != nil {
		return fmt.Errorf("notification: sendto create html part: %w", err)
	}
	if _, err := fmt.Fprint(pw, htmlBody); err != nil {
		return fmt.Errorf("notification: sendto write html part: %w", err)
	}

	if err := mw.Close(); err != nil {
		return fmt.Errorf("notification: sendto close multipart: %w", err)
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("From: %s\r\n", s.cfg.From))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%q\r\n", mw.Boundary()))
	msg.WriteString("\r\n")
	msg.Write(bodyBuf.Bytes())

	rawMsg := []byte(msg.String())

	doneCh := make(chan error, 1)
	go func() {
		doneCh <- s.smtpSend(addr, s.smtpAuth(), rawMsg, to)
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("notification: sendto context cancelled: %w", ctx.Err())
	case err := <-doneCh:
		if err != nil {
			return fmt.Errorf("notification: sendto smtp: %w", err)
		}
		return nil
	}
}
