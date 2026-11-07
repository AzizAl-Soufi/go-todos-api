package domain

import (
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AzizAl-Soufi/go-todos-api/internal/platform/apperrors"
)

const (
	MaxNameLength        = 100
	MaxDescriptionLength = 2_000
	MaxIconBytes         = 32
)

var (
	ErrInvalidName        = apperrors.Validation("INVALID_NAME", "name is required")
	ErrNameTooLong        = apperrors.Validationf("NAME_TOO_LONG", "name must be at most %d characters", MaxNameLength)
	ErrDescriptionTooLong = apperrors.Validationf("DESCRIPTION_TOO_LONG", "description must be at most %d characters", MaxDescriptionLength)
	ErrIconTooLong        = apperrors.Validationf("ICON_TOO_LONG", "icon must be at most %d bytes", MaxIconBytes)
	ErrInvalidColor       = apperrors.Validation("INVALID_COLOR", "color must be one of default, gray, brown, orange, yellow, green, blue, purple, pink, red")
	ErrProjectNotFound    = apperrors.NotFound("PROJECT_NOT_FOUND", "project not found")
	ErrCollectionNotFound = apperrors.NotFound("COLLECTION_NOT_FOUND", "collection not found")
)

// Colors is the palette for projects and collections ("default" is no color).
var Colors = []string{"default", "gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"}

// Meta is what projects and collections share: how they are shown and
// whether they are archived.
type Meta struct {
	Name        string
	Description string
	Icon        string // usually an emoji
	Color       string
	ArchivedAt  *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (m *Meta) Archived() bool { return m.ArchivedAt != nil }

// MetaChanges is a partial update of Meta; nil fields are left untouched.
type MetaChanges struct {
	Name        *string
	Description *string
	Icon        *string
	Color       *string
	Archived    *bool
}

func (c MetaChanges) Empty() bool {
	return c.Name == nil && c.Description == nil && c.Icon == nil && c.Color == nil && c.Archived == nil
}

// NewMeta builds the Meta of a new project or collection from the fields set
// in c, the same MetaChanges an update takes; the name is required.
func NewMeta(c MetaChanges, now time.Time) (Meta, error) {
	if c.Name == nil {
		return Meta{}, ErrInvalidName
	}
	now = Timestamp(now)
	m := Meta{Color: "default", CreatedAt: now, UpdatedAt: now}
	err := m.Apply(c, now)
	return m, err
}

// Apply validates and applies c at time now. On error m is unchanged.
func (m *Meta) Apply(c MetaChanges, now time.Time) error {
	if c.Empty() {
		return nil
	}
	now = Timestamp(now)
	next := *m
	if c.Name != nil {
		name := strings.TrimSpace(*c.Name)
		switch {
		case name == "":
			return ErrInvalidName
		case utf8.RuneCountInString(name) > MaxNameLength:
			return ErrNameTooLong
		}
		next.Name = name
	}
	if c.Description != nil {
		if utf8.RuneCountInString(*c.Description) > MaxDescriptionLength {
			return ErrDescriptionTooLong
		}
		next.Description = *c.Description
	}
	if c.Icon != nil {
		icon := strings.TrimSpace(*c.Icon)
		if len(icon) > MaxIconBytes {
			return ErrIconTooLong
		}
		next.Icon = icon
	}
	if c.Color != nil {
		color := *c.Color
		if color == "" {
			color = "default"
		}
		if !slices.Contains(Colors, color) {
			return ErrInvalidColor
		}
		next.Color = color
	}
	if c.Archived != nil && *c.Archived != next.Archived() {
		next.ArchivedAt = nil
		if *c.Archived {
			next.ArchivedAt = &now
		}
	}
	next.UpdatedAt = now
	*m = next
	return nil
}

// Project is a list of todos (like a Google Tasks list or a Notion page).
type Project struct {
	ID     string
	UserID string
	// CollectionID is the collection the project is filed under; "" is none.
	CollectionID string
	Meta
}

// Collection groups projects (e.g. "Work", "Personal").
type Collection struct {
	ID     string
	UserID string
	Meta
}

// ArchiveFilter selects projects or collections by archive state.
type ArchiveFilter int

const (
	ActiveOnly ArchiveFilter = iota
	ArchivedOnly
	AnyArchiveState
)

func (f ArchiveFilter) Matches(m Meta) bool {
	switch f {
	case ActiveOnly:
		return !m.Archived()
	case ArchivedOnly:
		return m.Archived()
	default:
		return true
	}
}

// ProjectFilter selects a user's projects.
type ProjectFilter struct {
	CollectionID string // "" means any collection (or none)
	Archive      ArchiveFilter
}

func (f ProjectFilter) Matches(p *Project) bool {
	return f.Archive.Matches(p.Meta) && (f.CollectionID == "" || p.CollectionID == f.CollectionID)
}
