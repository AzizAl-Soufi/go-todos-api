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
	// deadToken reports a per-token error meaning the token will never work
	// again; the SDK's checks, swapped in tests (its error type is internal).
	deadToken func(error) bool
}

func isDeadToken(err error) bool {
	return messaging.IsUnregistered(err) || messaging.IsInvalidArgument(err)
}

var _ application.Pusher = (*Pusher)(nil)

// New connects to projectID with a service account key.
func New(ctx context.Context, projectID, credentialsFile string) (*Pusher, error) {
	if projectID == "" || credentialsFile == "" {
		return nil, errors.New("fcm: project ID and service account are required")
	}
	app, err := fb.NewApp(ctx, &fb.Config{ProjectID: projectID}, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("fcm: %w", err)
	}
	c, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("fcm: %w", err)
	}
	return &Pusher{client: c, deadToken: isDeadToken}, nil
}

// Push sends m to tokens in batches of 500. Tokens FCM reports as
// unregistered or malformed are returned as invalid; other failures (FCM
// down, quota) are returned as err after every batch was tried.
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
		for i, r := range res.Responses {
			switch {
			case r.Success || i >= len(batch):
				continue // delivered, or a response FCM shouldn't have sent
			case p.deadToken(r.Error):
				invalid = append(invalid, batch[i])
			default:
				errs = append(errs, r.Error)
			}
		}
	}
	if len(errs) > 0 {
		return invalid, fmt.Errorf("fcm: %d of %d sends failed: %w", len(errs), len(tokens), errors.Join(errs...))
	}
	return invalid, nil
}

func message(tokens []string, m domain.Message) *messaging.MulticastMessage {
	return &messaging.MulticastMessage{
		// Registration tokens are what the client SDKs' getToken returns.
		Tokens:       tokens, //nolint:staticcheck // see the README on FCM installation IDs
		Notification: &messaging.Notification{Title: m.Title, Body: m.Body},
		Data:         m.Data,
	}
}
