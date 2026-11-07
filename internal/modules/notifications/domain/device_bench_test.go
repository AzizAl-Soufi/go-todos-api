package domain

import (
	"fmt"
	"strings"
	"testing"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
)

// fcmToken has the shape and length of a real FCM registration token.
var fcmToken = "fMEP0vJqS0yH0lJk7vRg:APA91bH" + strings.Repeat("x7Kq_9-Zt", 15)

func BenchmarkDeviceID(b *testing.B) {
	for b.Loop() {
		DeviceID(fcmToken)
	}
}

func BenchmarkNormalizeToken(b *testing.B) {
	padded := "  " + fcmToken + "\n"
	for b.Loop() {
		if _, err := NormalizeToken(padded); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTodoCreated_Recipients(b *testing.B) {
	for _, size := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("members=%d", size), func(b *testing.B) {
			roster := access.Roster{TeamID: "t1", Name: "Platform", Members: make(map[string]access.Role, size)}
			for i := range size {
				roster.Members[fmt.Sprintf("user-%d", i)] = access.Member
			}
			e := TodoCreated{TeamID: "t1", ActorID: "user-0", ActorRole: access.Admin}
			b.ReportAllocs()
			for b.Loop() {
				e.Recipients(roster)
			}
		})
	}
}

func BenchmarkTodoCreated_Message(b *testing.B) {
	for name, title := range map[string]string{
		"short":     "Ship the release notes",
		"truncated": strings.Repeat("🚀", MaxBodyLength*2), // the slow path: cut to fit
	} {
		b.Run(name, func(b *testing.B) {
			e := TodoCreated{TeamID: "t1", ProjectID: "p1", TodoID: "td1", Title: title, ActorName: "Bo", ActorRole: access.Admin}
			b.ReportAllocs()
			for b.Loop() {
				e.Message("Platform")
			}
		})
	}
}
