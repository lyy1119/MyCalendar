package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/model"
)

// CategoryUncategorized is the category every event starts in. It is the only
// fixed row left: "deleted" is now a column on the event (trash), not a
// category, so categories are purely groupings.
const CategoryUncategorized int64 = 0

const categoryColumns = `id, name, color, display`

// ListCategories returns every category, system rows first and then user rows
// in id order.
func (s *Store) ListCategories(ctx context.Context) ([]model.Category, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+categoryColumns+` FROM categories ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	var out []model.Category
	for rows.Next() {
		var c model.Category
		if err := rows.Scan(&c.ID, &c.Name, &c.Color, &c.Display); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories: %w", err)
	}
	return out, nil
}

// GetCategory loads one category.
func (s *Store) GetCategory(ctx context.Context, id int64) (model.Category, error) {
	var c model.Category
	err := s.db.QueryRowContext(ctx,
		`SELECT `+categoryColumns+` FROM categories WHERE id = ?`, id).
		Scan(&c.ID, &c.Name, &c.Color, &c.Display)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Category{}, apperr.ErrNotFound
	}
	if err != nil {
		return model.Category{}, fmt.Errorf("get category: %w", err)
	}
	return c, nil
}

// CreateCategory inserts a category and returns it with its assigned id.
//
// The id is left to SQLite, which continues from 0 -- the highest fixed id --
// so user categories number from 1.
func (s *Store) CreateCategory(ctx context.Context, name, color string, display bool) (model.Category, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO categories (name, color, display) VALUES (?, ?, ?)`, name, color, display)
	if err != nil {
		return model.Category{}, fmt.Errorf("insert category: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Category{}, fmt.Errorf("read inserted category id: %w", err)
	}
	return s.GetCategory(ctx, id)
}

// UpdateCategory renames, recolours or shows/hides one category.
func (s *Store) UpdateCategory(ctx context.Context, id int64, name, color string, display bool) (model.Category, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE categories SET name = ?, color = ?, display = ? WHERE id = ?`, name, color, display, id)
	if err != nil {
		return model.Category{}, fmt.Errorf("update category: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return model.Category{}, fmt.Errorf("update category rows: %w", err)
	}
	if affected == 0 {
		return model.Category{}, apperr.ErrNotFound
	}
	return s.GetCategory(ctx, id)
}

// DeleteCategory removes a category, moving its events to the default category
// in the same transaction so no event is left pointing at nothing.
//
// Category 0 is refused rather than deleted: it is where everything else falls
// back to.
func (s *Store) DeleteCategory(ctx context.Context, id int64) (moved int64, err error) {
	if id == CategoryUncategorized {
		return 0, apperr.NewConflict("category_protected",
			"the default category is part of the API and cannot be removed")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin category delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`UPDATE events SET category_id = ? WHERE category_id = ?`, CategoryUncategorized, id); err != nil {
		return 0, fmt.Errorf("reassign events: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id)
	if err != nil {
		return 0, fmt.Errorf("delete category: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete category rows: %w", err)
	}
	if affected == 0 {
		return 0, apperr.ErrNotFound
	}

	var count int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE category_id = ?`, CategoryUncategorized).Scan(&count); err != nil {
		return 0, fmt.Errorf("count uncategorized events: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit category delete: %w", err)
	}
	return int64(count), nil
}

// CategoryExists reports whether a category id is present.
func (s *Store) CategoryExists(ctx context.Context, id int64) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM categories WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check category: %w", err)
	}
	return true, nil
}

// SetEventCategory moves one event to a category.
func (s *Store) SetEventCategory(ctx context.Context, id, categoryID, now int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE events SET category_id = ?, updated_at = ? WHERE id = ?`, categoryID, now, id)
	if err != nil {
		return fmt.Errorf("set event category: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("set event category rows: %w", err)
	}
	if affected == 0 {
		return apperr.ErrNotFound
	}
	return nil
}
