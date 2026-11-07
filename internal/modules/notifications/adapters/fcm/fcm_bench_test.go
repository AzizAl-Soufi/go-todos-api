package fcm

import (
	"context"
	"fmt"
	"testing"

	"firebase.google.com/go/v4/messaging"

	"github.com/AzizAl-Soufi/go-todos-api/internal/modules/notifications/domain"
)

// deliveringFCM accepts every token. It reuses one batch of responses so the
// benchmark measures the adapter (batching, sorting out failures), not the fake.
type deliveringFCM struct{ ok []*messaging.SendResponse }

func newDeliveringFCM() deliveringFCM {
	ok := make([]*messaging.SendResponse, batchSize)
	for i := range ok {
		ok[i] = &messaging.SendResponse{Success: true}
	}
	return deliveringFCM{ok: ok}
}

func (f deliveringFCM) SendEachForMulticast(
	_ context.Context,
	m *messaging.MulticastMessage,
) (*messaging.BatchResponse, error) {
	return &messaging.BatchResponse{Responses: f.ok[:len(m.Tokens)]}, nil
}

func BenchmarkPush(b *testing.B) {
	msg := domain.Message{
		Title: "New task in Platform",
		Body:  "Bo added “Ship it”",
		Data:  map[string]string{"type": "todo.created", "teamId": "t1", "projectId": "p1", "todoId": "td1"},
	}
	for _, n := range []int{10, 500, 5000} {
		b.Run(fmt.Sprintf("tokens=%d", n), func(b *testing.B) {
			tokens := make([]string, n)
			for i := range tokens {
				tokens[i] = fmt.Sprintf("token-%d", i)
			}
			p := newPusher(nil)
			p.client = newDeliveringFCM()
			ctx := context.Background()
			b.ReportAllocs()
			for b.Loop() {
				if _, err := p.Push(ctx, tokens, msg); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
