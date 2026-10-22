package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/model"
	"github.com/lyy1119/MyCalendar/internal/store"
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

// CreateCategory adds a category. A new category starts visible unless the
// request says otherwise: creating something and not seeing it would be a
// puzzle, not a preference.
func (s *Service) CreateCategory(ctx context.Context, in model.CategoryInput) (model.Category, error) {
	name, color, display, verr := s.categoryFields(in, "", "", true)
	if verr != nil {
		return model.Category{}, verr
	}
	return s.store.CreateCategory(ctx, name, color, display)
}

// UpdateCategory renames or recolours a category. Both fields are optional, and
// omitting one leaves it as it was.
//
// The deleted category (-1) keeps its name: "deleted" is a state the API names,
// not a grouping the user chose, so renaming it would make the meaning of a
// category_id of -1 depend on data. Its colour stays editable, which is what
// people actually want to change about it. Category 0 is fully editable -- it
// is just the default grouping.
func (s *Service) UpdateCategory(ctx context.Context, id int64, in model.CategoryInput) (model.Category, error) {
	current, err := s.store.GetCategory(ctx, id)
	if err != nil {
		return model.Category{}, err
	}
	if id == store.CategoryDeleted && in.Name.Set {
		wanted := ""
		if in.Name.Value != nil {
			wanted = strings.TrimSpace(*in.Name.Value)
		}
		// Sending the name it already has is not a change, so it is allowed;
		// only an actual rename is refused.
		if wanted != current.Name {
			return model.Category{}, apperr.NewConflict("category_name_fixed",
				"the deleted category (-1) keeps its name; rename is refused because -1 is a state the API names. Its colour can still be changed")
		}
	}
	name, color, display, verr := s.categoryFields(in, current.Name, current.Color, current.Display)
	if verr != nil {
		return model.Category{}, verr
	}
	return s.store.UpdateCategory(ctx, id, name, color, display)
}

// DeleteCategory removes a category and moves its events to uncategorized.
//
// The two system categories are refused: -1 is what marks an event as deleted
// and 0 is the fallback, so neither can go away.
func (s *Service) DeleteCategory(ctx context.Context, id int64) error {
	_, err := s.store.DeleteCategory(ctx, id)
	return err
}

// categoryFields resolves and validates a category's name, colour and
// visibility, falling back to the current values for fields the request left
// alone.
func (s *Service) categoryFields(
	in model.CategoryInput,
	currentName, currentColor string,
	currentDisplay bool,
) (name, color string, display bool, verr *apperr.Validation) {
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

	display = currentDisplay
	if in.Display.Set {
		if in.Display.Value == nil {
			// A boolean has no null; refuse rather than guess which way it meant.
			verr.Add("display", "must not be null")
		} else {
			display = *in.Display.Value
		}
	}

	if verr.Has() {
		return "", "", false, verr
	}
	return name, color, display, nil
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
