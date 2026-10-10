package email

import (
	"strings"
	"testing"
)

func TestNewSMTPSenderFromEnv_disabled(t *testing.T) {
	t.Setenv("MSG2AGENT_SMTP_HOST", "")
	t.Setenv("MSG2AGENT_SMTP_FROM", "")

	s, err := NewSMTPSenderFromEnv()
	if err != nil {
		t.Fatalf("disabled SMTP: unexpected error: %v", err)
	}
	if _, ok := s.(NopSender); !ok {
		t.Fatalf("disabled SMTP: got %T, want NopSender", s)
	}
}

func TestNewSMTPSenderFromEnv_fromRequired(t *testing.T) {
	t.Setenv("MSG2AGENT_SMTP_HOST", "smtp.example.com")
	t.Setenv("MSG2AGENT_SMTP_FROM", "")

	s, err := NewSMTPSenderFromEnv()
	if err == nil {
		t.Fatal("expected error when SMTP is enabled without MSG2AGENT_SMTP_FROM")
	}
	if s != nil {
		t.Fatalf("sender = %T, want nil on config error", s)
	}
	if !strings.Contains(err.Error(), "MSG2AGENT_SMTP_FROM") {
		t.Errorf("error %q does not mention MSG2AGENT_SMTP_FROM", err)
	}
	if !strings.Contains(err.Error(), "noreply@example.com") {
		t.Errorf("error %q should include the noreply@example.com placeholder", err)
	}
}

func TestNewSMTPSenderFromEnv_configured(t *testing.T) {
	t.Setenv("MSG2AGENT_SMTP_HOST", "smtp.example.com")
	t.Setenv("MSG2AGENT_SMTP_PORT", "2525")
	t.Setenv("MSG2AGENT_SMTP_FROM", "noreply@example.com")
	t.Setenv("MSG2AGENT_SMTP_USER", "user")
	t.Setenv("MSG2AGENT_SMTP_PASS", "pass")

	s, err := NewSMTPSenderFromEnv()
	if err != nil {
		t.Fatalf("configured SMTP: unexpected error: %v", err)
	}
	smtp, ok := s.(*SMTPSender)
	if !ok {
		t.Fatalf("configured SMTP: got %T, want *SMTPSender", s)
	}
	if smtp.from != "noreply@example.com" {
		t.Errorf("from = %q, want noreply@example.com", smtp.from)
	}
	if smtp.host != "smtp.example.com" {
		t.Errorf("host = %q, want smtp.example.com", smtp.host)
	}
	if smtp.port != 2525 {
		t.Errorf("port = %d, want 2525", smtp.port)
	}
	if smtp.auth == nil {
		t.Error("auth is nil; expected PLAIN auth when user is set")
	}
}

func TestNewSMTPSenderFromEnv_whitespaceFromRequired(t *testing.T) {
	t.Setenv("MSG2AGENT_SMTP_HOST", "smtp.example.com")
	t.Setenv("MSG2AGENT_SMTP_FROM", "   ")

	s, err := NewSMTPSenderFromEnv()
	if err == nil {
		t.Fatal("expected error when MSG2AGENT_SMTP_FROM is whitespace only")
	}
	if s != nil {
		t.Fatalf("sender = %T, want nil on config error", s)
	}
}
