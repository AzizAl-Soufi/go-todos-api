// Package fcm sends push notifications with Firebase Cloud Messaging through
// the Firebase Admin SDK (application.Pusher). Sending needs a service
// account with the Firebase Cloud Messaging API enabled.
package fcm

import (
	"context"
	"errors"
	"fmt"

	fb "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/application"
	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
)

// batchSize is FCM's limit of tokens per multicast.
const batchSize = 500

// multicaster is the part of messaging.Client the adapter uses.
type multicaster interface {
	SendEachForMulticast(ctx context.Context, m *messaging.MulticastMessage) (*messaging.BatchResponse, error)
}

type Pusher struct {
	client multicaster
	// unregistered and invalidArgument classify per-token errors; they are
	// the SDK's checks, swapped in tests (its error type is internal).
	unregistered    func(error) bool
	invalidArgument func(error) bool
}

var _ application.Pusher = (*Pusher)(nil)

// New connects to projectID with a service account key.
func New(ctx context.Context, projectID, credentialsFile string) (*Pusher, error) {
	if projectID == "" || credentialsFile == "" {
		return nil, errors.New("fcm: project ID and service account are required")
	}
	app, err := fb.NewApp(ctx, &fb.Config{ProjectID: projectID}, option.WithAuthCredentialsFile(option.ServiceAccount, credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("fcm: %w", err)
	}
	c, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("fcm: %w", err)
	}
	return &Pusher{
		client:          c,
		unregistered:    messaging.IsUnregistered,
		invalidArgument: messaging.IsInvalidArgument,
	}, nil
}

// Push sends m to tokens in batches of 500. Tokens FCM reports as
// unregistered or malformed are returned as invalid; other failures (FCM
// down, quota) are returned as err after every batch was tried.
//
// INVALID_ARGUMENT means something in the request was invalid, the token or
// the message. When every token of a batch gets it, the message is at fault
// and no token is returned as invalid: otherwise one bad message would make
// every recipient's devices be forgotten.
func (p *Pusher) Push(ctx context.Context, tokens []string, m domain.Message) ([]string, error) {
	var invalid []string
	var errs []error
	for start := 0; start < len(tokens); start += batchSize {
		batch := tokens[start:min(start+batchSize, len(tokens))]
		res, err := p.client.SendEachForMulticast(ctx, message(batch, m))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if res == nil || len(res.Responses) != len(batch) {
			// The SDK answers once per token; anything else means we can't
			// tell which tokens were delivered, so none is called invalid.
			errs = append(errs, fmt.Errorf("unexpected response for %d tokens", len(batch)))
			continue
		}
		if p.messageRejected(batch, res.Responses) {
			errs = append(errs, fmt.Errorf("message rejected: %w", res.Responses[0].Error))
			continue
		}
		for i, r := range res.Responses {
			switch {
			case r == nil:
				errs = append(errs, fmt.Errorf("no response for token %d of the batch", i))
			case r.Success:
				continue
			case p.unregistered(r.Error) || p.invalidArgument(r.Error):
				invalid = append(invalid, batch[i]) //nolint:gosec // G602: len(res.Responses) == len(batch), checked above
			default:
				errs = append(errs, r.Error)
			}
		}
	}
	if len(errs) > 0 {
		return invalid, fmt.Errorf("fcm: %d failures sending to %d devices: %w", len(errs), len(tokens), errors.Join(errs...))
	}
	return invalid, nil
}

// messageRejected reports whether FCM refused the message rather than the
// tokens: every token of a batch of several failed with INVALID_ARGUMENT. A
// single token can't tell the two apart; it is treated as a bad token, since
// domain.Message keeps messages well within FCM's limits.
func (p *Pusher) messageRejected(batch []string, responses []*messaging.SendResponse) bool {
	if len(batch) < 2 {
		return false
	}
	for _, r := range responses {
		if r == nil || r.Success || !p.invalidArgument(r.Error) {
			return false
		}
	}
	return true
}

func message(tokens []string, m domain.Message) *messaging.MulticastMessage {
	return &messaging.MulticastMessage{
		// Registration tokens are what the client SDKs' getToken returns.
		Tokens:       tokens, //nolint:staticcheck // see the README on FCM installation IDs
		Notification: &messaging.Notification{Title: m.Title, Body: m.Body},
		Data:         m.Data,
	}
}
