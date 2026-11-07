// Package domain holds the notifications module's entities and rules: the
// devices a user receives push notifications on, which events notify whom,
// and the messages they get. No HTTP, storage or Firebase here.
package domain

import (
	"strings"
	"time"
	"unicode"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/access"
	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

const (
	// MaxDevicesPerUser bounds the tokens kept per account; registering one
	// more drops the least recently registered.
	MaxDevicesPerUser = 10
	MaxTokenLength    = 4096
)

type Platform string

const (
	Android Platform = "android"
	IOS     Platform = "ios"
	Web     Platform = "web"
)

var (
	ErrInvalidToken    = apperrors.Validation("INVALID_DEVICE_TOKEN", "token must be an FCM registration token")
	ErrInvalidPlatform = apperrors.Validation("INVALID_PLATFORM", "platform must be one of android, ios, web")
	ErrDeviceNotFound  = apperrors.NotFound("DEVICE_NOT_FOUND", "device not found")
)

// Device is an FCM registration token of one of the user's app installs or
// browsers. A token belongs to one user: registering it again (another user
// signed in on the device) moves it.
type Device struct {
	Token     string
	UserID    string
	Platform  Platform
	CreatedAt time.Time
	// UpdatedAt is the last registration; clients re-register on start-up,
	// so stale devices sort first.
	UpdatedAt time.Time
}

func NewDevice(userID, token, platform string, now time.Time) (*Device, error) {
	token, err := NormalizeToken(token)
	if err != nil {
		return nil, err
	}
	p, err := ParsePlatform(platform)
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	return &Device{
		Token:     token,
		UserID:    userID,
		Platform:  p,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// NormalizeToken trims a registration token and rejects what can't be one.
// FCM tokens are opaque, so only the shape is checked.
func NormalizeToken(token string) (string, error) {
	token = strings.TrimSpace(token)
	empty := token == ""
	tooLong := len(token) > MaxTokenLength
	if empty || tooLong || strings.ContainsFunc(token, unicode.IsSpace) {
		return "", ErrInvalidToken
	}
	return token, nil
}

func ParsePlatform(s string) (Platform, error) {
	switch p := Platform(strings.ToLower(strings.TrimSpace(s))); p {
	case Android, IOS, Web:
		return p, nil
	}
	return "", ErrInvalidPlatform
}

// Message is a push notification: what the user sees, plus data for the app.
type Message struct {
	Title string
	Body  string
	Data  map[string]string
}

// TodoCreated is a todo added to a team project.
type TodoCreated struct {
	TeamID    string
	ProjectID string
	TodoID    string
	Title     string
	ActorID   string
	ActorName string
	ActorRole access.Role
}

// Notifies reports whether the event notifies the team: tasks added by the
// team's owner or admins do, tasks members add for themselves don't.
func (e TodoCreated) Notifies() bool { return e.TeamID != "" && e.ActorRole.CanManage() }

// Recipients are everyone in the team but the person who added the task.
func (e TodoCreated) Recipients(roster access.Roster) []string {
	out := make([]string, 0, len(roster.Members))
	for userID := range roster.Members {
		if userID != e.ActorID {
			out = append(out, userID)
		}
	}
	return out
}

// Message is what the recipients see.
func (e TodoCreated) Message(teamName string) Message {
	who := strings.TrimSpace(e.ActorName)
	if who == "" {
		who = "An admin"
	}
	return Message{
		Title: "New task in " + teamName,
		Body:  who + " added “" + e.Title + "”",
		Data: map[string]string{
			"type":      "todo.created",
			"teamId":    e.TeamID,
			"projectId": e.ProjectID,
			"todoId":    e.TodoID,
		},
	}
}
