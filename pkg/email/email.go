// Package email provides a minimal Sender interface and an SMTP implementation
// for transactional emails (signup verification, notifications).
package email

import (
	"fmt"
	"net/smtp"
	"os"
	"strconv"
	"strings"
)

// Sender sends transactional emails.
type Sender interface {
	Send(to, subject, htmlBody, textBody string) error
}

// NopSender silently discards all emails. Used when SMTP is not configured.
type NopSender struct{}

func (NopSender) Send(_, _, _, _ string) error { return nil }

// SMTPSender sends emails via SMTP with PLAIN authentication.
type SMTPSender struct {
	host string
	port int
	auth smtp.Auth
	from string
}

// NewSMTPSenderFromEnv builds an SMTPSender from environment variables.
// Returns NopSender if MSG2AGENT_SMTP_HOST is not set (email sending disabled).
// Returns an error if SMTP is enabled but MSG2AGENT_SMTP_FROM is empty.
func NewSMTPSenderFromEnv() (Sender, error) {
	host := os.Getenv("MSG2AGENT_SMTP_HOST")
	if host == "" {
		return NopSender{}, nil
	}
	from := strings.TrimSpace(os.Getenv("MSG2AGENT_SMTP_FROM"))
	if from == "" {
		return nil, fmt.Errorf("email sending is enabled (MSG2AGENT_SMTP_HOST is set) but MSG2AGENT_SMTP_FROM is empty; set MSG2AGENT_SMTP_FROM to the sender address (e.g. noreply@example.com)")
	}
	port := 587
	if p := os.Getenv("MSG2AGENT_SMTP_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	user := os.Getenv("MSG2AGENT_SMTP_USER")
	pass := os.Getenv("MSG2AGENT_SMTP_PASS")
	var auth smtp.Auth
	if user != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}
	return &SMTPSender{host: host, port: port, auth: auth, from: from}, nil
}

// Send delivers an email with an HTML body and a plain-text fallback.
func (s *SMTPSender) Send(to, subject, htmlBody, textBody string) error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	msg := buildMIME(s.from, to, subject, htmlBody, textBody)
	return smtp.SendMail(addr, s.auth, s.from, []string{to}, []byte(msg))
}

func buildMIME(from, to, subject, htmlBody, textBody string) string {
	boundary := "msg2agent-mime-boundary"
	var b strings.Builder
	b.WriteString("From: msg2agent <" + from + ">\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n\r\n")
	b.WriteString(textBody + "\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=utf-8\r\n\r\n")
	b.WriteString(htmlBody + "\r\n")
	b.WriteString("--" + boundary + "--\r\n")
	return b.String()
}
