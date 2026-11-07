package mail

import (
	"context"
	"strings"
	"testing"
)

func TestBuildMessage(t *testing.T) {
	msg := string(buildMessage("noreply@x.org", "ann@example.com", "Subject here", verificationBody("Ann", "https://x.org/verify?token=abc")))

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

func TestPasswordResetBody(t *testing.T) {
	body := passwordResetBody("Ann", "https://x.org/reset-password?token=abc")
	if !strings.Contains(body, "Hi Ann,") || !strings.Contains(body, "https://x.org/reset-password?token=abc") {
		t.Fatalf("body = %q", body)
	}
}

func TestSMTPMailer_RejectsHeaderInjection(t *testing.T) {
	m := SMTPMailer{Host: "127.0.0.1", Port: 1, From: "noreply@x.org"}
	ctx := context.Background()
	for name, send := range map[string]func(to, name, link string) error{
		"verification":   func(to, n, l string) error { return m.SendVerificationEmail(ctx, to, n, l) },
		"password reset": func(to, n, l string) error { return m.SendPasswordResetEmail(ctx, to, n, l) },
	} {
		err := send("ann@example.com\r\nBcc: victim@x.org", "Ann", "link")
		if err == nil || !strings.Contains(err.Error(), "invalid email header") {
			t.Fatalf("%s: err = %v, want header injection rejected before connecting", name, err)
		}
	}
}

func TestSanitizeLine(t *testing.T) {
	if got := sanitizeLine("Ann\r\nBcc: x"); strings.ContainsAny(got, "\r\n") {
		t.Fatalf("sanitizeLine left a line break: %q", got)
	}
}

func TestLogMailer(t *testing.T) {
	if err := (LogMailer{}).SendVerificationEmail(context.Background(), "a@b.co", "A", "link"); err != nil {
		t.Fatal(err)
	}
	if err := (LogMailer{}).SendPasswordResetEmail(context.Background(), "a@b.co", "A", "link"); err != nil {
		t.Fatal(err)
	}
}
