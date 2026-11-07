package mail

import (
	"context"
	"strings"
	"testing"
)

func TestBuildMessage(t *testing.T) {
	msg := string(BuildMessage("noreply@x.org", "ann@example.com", "Subject here", "Hi Ann,\r\nhttps://x.org/verify?token=abc"))
	for _, want := range []string{
		"From: noreply@x.org\r\n",
		"To: ann@example.com\r\n",
		"Subject: Subject here\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"\r\n\r\nHi Ann,",
		"https://x.org/verify?token=abc",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q", want)
		}
	}
}

func TestSMTP_RejectsHeaderInjection(t *testing.T) {
	m := SMTP{Host: "127.0.0.1", Port: 1, From: "noreply@x.org"}
	for name, send := range map[string]func() error{
		"to":      func() error { return m.Send(context.Background(), "ann@example.com\r\nBcc: victim@x.org", "s", "b") },
		"subject": func() error { return m.Send(context.Background(), "ann@example.com", "s\r\nBcc: victim@x.org", "b") },
	} {
		if err := send(); err == nil || !strings.Contains(err.Error(), "invalid email header") {
			t.Fatalf("%s: err = %v, want header injection rejected before connecting", name, err)
		}
	}
}

func TestSanitizeLine(t *testing.T) {
	if got := SanitizeLine("Ann\r\nBcc: x"); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("SanitizeLine left a line break: %q", got)
	}
}

func TestLogSender(t *testing.T) {
	if err := (LogSender{}).Send(context.Background(), "a@b.co", "s", "b"); err != nil {
		t.Fatal(err)
	}
}
