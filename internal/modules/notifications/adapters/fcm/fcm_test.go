package fcm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"firebase.google.com/go/v4/messaging"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
)

var (
	errUnregistered = errors.New("UNREGISTERED")
	errInvalid      = errors.New("INVALID_ARGUMENT")
)

func fcmError(code string) error {
	if code == "UNREGISTERED" {
		return errUnregistered
	}
	return errInvalid
}

func newPusher(f *fakeFCM) *Pusher {
	return &Pusher{
		client:          f,
		unregistered:    func(err error) bool { return errors.Is(err, errUnregistered) },
		invalidArgument: func(err error) bool { return errors.Is(err, errInvalid) },
	}
}

// fakeFCM answers per token: tokens starting with "gone" are unregistered,
// "bad" are rejected, "flaky" fail transiently; fail makes a call fail whole.
type fakeFCM struct {
	calls [][]string
	fail  error
	sent  *messaging.MulticastMessage
}

func (f *fakeFCM) SendEachForMulticast(
	_ context.Context,
	m *messaging.MulticastMessage,
) (*messaging.BatchResponse, error) {
	f.calls = append(f.calls, m.Tokens)
	f.sent = m
	if f.fail != nil {
		return nil, f.fail
	}
	res := &messaging.BatchResponse{}
	for _, tok := range m.Tokens {
		r := &messaging.SendResponse{Success: true}
		switch {
		case strings.HasPrefix(tok, "gone"):
			r = &messaging.SendResponse{Error: fcmError("UNREGISTERED")}
		case strings.HasPrefix(tok, "bad"):
			r = &messaging.SendResponse{Error: fcmError("INVALID_ARGUMENT")}
		case strings.HasPrefix(tok, "flaky"):
			r = &messaging.SendResponse{Error: errors.New("unavailable")}
		}
		res.Responses = append(res.Responses, r)
	}
	return res, nil
}

func TestPush_BatchesAndSortsOutInvalidTokens(t *testing.T) {
	f := &fakeFCM{}
	p := newPusher(f)
	tokens := []string{"gone-1", "bad-1", "ok-1"}
	for i := range 600 {
		tokens = append(tokens, fmt.Sprintf("ok-%d", i+2))
	}
	msg := domain.Message{Title: "New task", Body: "Ann added", Data: map[string]string{"type": "todo.created"}}

	invalid, err := p.Push(context.Background(), tokens, msg)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(invalid, []string{"gone-1", "bad-1"}) {
		t.Fatalf("invalid = %v", invalid)
	}
	if len(f.calls) != 2 || len(f.calls[0]) != 500 || len(f.calls[1]) != 103 {
		t.Fatalf("batches = %d (%d, %d), want 500 + 103", len(f.calls), len(f.calls[0]), len(f.calls[len(f.calls)-1]))
	}
	if f.sent.Notification.Title != "New task" || f.sent.Data["type"] != "todo.created" {
		t.Fatalf("message = %+v", f.sent)
	}
}

func TestPush_ReportsOtherFailures(t *testing.T) {
	p := newPusher(&fakeFCM{})
	invalid, err := p.Push(context.Background(), []string{"flaky-1", "gone-1", "ok"}, domain.Message{})
	if err == nil || !slices.Equal(invalid, []string{"gone-1"}) {
		t.Fatalf("invalid = %v, err = %v; want gone-1 and an error", invalid, err)
	}

	down := &fakeFCM{fail: errors.New("fcm down")}
	if _, err := newPusher(down).Push(context.Background(), []string{"ok"}, domain.Message{}); err == nil {
		t.Fatal("want an error when FCM is down")
	}
}

func TestPush_RejectedMessageKeepsTheTokens(t *testing.T) {
	f := &fakeFCM{}
	p := newPusher(f)
	invalid, err := p.Push(context.Background(), []string{"bad-1", "bad-2", "bad-3"}, domain.Message{})
	if len(invalid) != 0 {
		t.Fatalf("invalid = %v; a message every token rejects must not cost the recipients their devices", invalid)
	}
	if !errors.Is(err, errInvalid) {
		t.Fatalf("err = %v, want the rejection reported", err)
	}

	// One token alone can't tell, and is dropped.
	if invalid, _ := p.Push(context.Background(), []string{"bad-1"}, domain.Message{}); !slices.Equal(invalid, []string{"bad-1"}) {
		t.Fatalf("single bad token: invalid = %v", invalid)
	}
}

// shortFCM answers without a response per token.
type shortFCM struct{ res *messaging.BatchResponse }

func (f shortFCM) SendEachForMulticast(context.Context, *messaging.MulticastMessage) (*messaging.BatchResponse, error) {
	return f.res, nil
}

func TestPush_UnexpectedResponsesAreErrors(t *testing.T) {
	for name, res := range map[string]*messaging.BatchResponse{
		"nil response":       nil,
		"too few responses":  {Responses: []*messaging.SendResponse{{Success: true}}},
		"nil token response": {Responses: []*messaging.SendResponse{{Success: true}, nil}},
	} {
		p := newPusher(nil)
		p.client = shortFCM{res: res}
		invalid, err := p.Push(context.Background(), []string{"a", "b"}, domain.Message{})
		if err == nil || len(invalid) != 0 {
			t.Errorf("%s: invalid = %v, err = %v; want an error and no tokens dropped", name, invalid, err)
		}
	}
}
