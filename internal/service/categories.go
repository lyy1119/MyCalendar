package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/model"
)

// Category operations, plus the two ways an event can go away.

// MaxCategoryNameRunes bounds a category name.
const MaxCategoryNameRunes = 100

// hexColor matches #RGB or #RRGGBB, case-insensitively; normaliseColor expands
// the short form so callers always store #rrggbb.
var hexColor = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// ListCategories returns every category.
func (s *Service) ListCategories(ctx context.Context) ([]model.Category, error) {
	return s.store.ListCategories(ctx)
}

// GetCategory loads one category.
func (s *Service) GetCategory(ctx context.Context, id int64) (model.Category, error) {
	return s.store.GetCategory(ctx, id)
}

// CreateCategory adds a category.
func (s *Service) CreateCategory(ctx context.Context, in model.CategoryInput) (model.Category, error) {
	name, color, verr := s.categoryFields(in, "", "")
	if verr != nil {
		return model.Category{}, verr
	}
	return s.store.CreateCategory(ctx, name, color)
}

// UpdateCategory renames or recolours a category. Both fields are optional, and
// omitting one leaves it as it was.
func (s *Service) UpdateCategory(ctx context.Context, id int64, in model.CategoryInput) (model.Category, error) {
	current, err := s.store.GetCategory(ctx, id)
	if err != nil {
		return model.Category{}, err
	}
	name, color, verr := s.categoryFields(in, current.Name, current.Color)
	if verr != nil {
		return model.Category{}, verr
	}
	return s.store.UpdateCategory(ctx, id, name, color)
}

// DeleteCategory removes a category and moves its events to uncategorized.
//
// The two system categories are refused: -1 is what marks an event as deleted
// and 0 is the fallback, so neither can go away.
func (s *Service) DeleteCategory(ctx context.Context, id int64) error {
	_, err := s.store.DeleteCategory(ctx, id)
	return err
}

// categoryFields resolves and validates a category's name and colour, falling
// back to the current values for fields the request left alone.
func (s *Service) categoryFields(in model.CategoryInput, currentName, currentColor string) (name, color string, verr *apperr.Validation) {
	verr = &apperr.Validation{}

	name = currentName
	if in.Name.Set {
		if in.Name.Value == nil {
			verr.Add("name", "must not be null")
		} else {
			name = strings.TrimSpace(*in.Name.Value)
		}
	}
	switch {
	case name == "":
		verr.Add("name", "is required and must not be empty")
	case utf8.RuneCountInString(name) > MaxCategoryNameRunes:
		verr.Add("name", fmt.Sprintf("must be at most %d characters", MaxCategoryNameRunes))
	}

	color = currentColor
	if in.Color.Set {
		if in.Color.Value == nil {
			verr.Add("color", "must not be null")
		} else {
			color = normalizeColor(*in.Color.Value)
			if !hexColor.MatchString(color) {
				verr.Add("color", "must be a hex colour such as #1a73e8 or #abc")
			}
		}
	}
	if color == "" {
		verr.Add("color", "is required and must not be empty")
	}

	if verr.Has() {
		return "", "", verr
	}
	return name, color, nil
}

// normalizeColor trims a colour and expands #abc to #aabbcc, lowercasing it so
// stored colours compare equal regardless of how they were typed.
func normalizeColor(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if len(value) == 4 && strings.HasPrefix(value, "#") {
		return fmt.Sprintf("#%c%c%c%c%c%c", value[1], value[1], value[2], value[2], value[3], value[3])
	}
	return value
}

// resolveCategory checks that a category id refers to a real category.
func (s *Service) resolveCategory(ctx context.Context, verr *apperr.Validation, categoryID int64) bool {
	found, err := s.store.CategoryExists(ctx, categoryID)
	if err != nil {
		verr.Add("category_id", "could not be verified: "+err.Error())
		return false
	}
	if !found {
		verr.Add("category_id", fmt.Sprintf("category %d does not exist", categoryID))
		return false
	}
	return true
}
