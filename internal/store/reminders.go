package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/lyy1119/MyCalendar/internal/apperr"
	"github.com/lyy1119/MyCalendar/internal/model"
)

// ReminderFilter narrows a reminder query.
type ReminderFilter struct {
	// EventID keeps only the reminders of one event.
	EventID *int64
	// IncludeDeleted also returns reminders whose event is in the trash. Off by
	// default: a trashed event must not notify anybody, and a client polling
	// this endpoint is looking for something to act on. The query parameter that
	// sets it is `deleted`, matching the column.
	IncludeDeleted bool
	// From and To bound remind_at, half-open.
	From *int64
	To   *int64
	// Limit of 0 means no limit; Offset is only meaningful with a limit or on
	// its own.
	Limit  int
	Offset int
}

// CreateReminder attaches a reminder to an event.
func (s *Store) CreateReminder(ctx context.Context, eventID, remindAt, now int64) (model.Reminder, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO reminders (event_id, remind_at, created_at) VALUES (?, ?, ?)`,
		eventID, remindAt, now)
	if err != nil {
		return model.Reminder{}, fmt.Errorf("insert reminder: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Reminder{}, fmt.Errorf("read inserted reminder id: %w", err)
	}
	return s.GetReminder(ctx, id)
}

// GetReminder loads a single reminder.
func (s *Store) GetReminder(ctx context.Context, id int64) (model.Reminder, error) {
	var r model.Reminder
	err := s.db.QueryRowContext(ctx,
		`SELECT `+reminderColumns+` FROM reminders WHERE id = ?`, id).
		Scan(&r.ID, &r.EventID, &r.RemindAt, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Reminder{}, apperr.ErrNotFound
	}
	if err != nil {
		return model.Reminder{}, fmt.Errorf("get reminder: %w", err)
	}
	return r, nil
}

// ListReminders returns reminders ordered by remind_at then id.
func (s *Store) ListReminders(ctx context.Context, f ReminderFilter) ([]model.Reminder, error) {
	var (
		conds []string
		args  []any
	)

	if f.EventID != nil {
		conds = append(conds, `r.event_id = ?`)
		args = append(args, *f.EventID)
	}
	if f.From != nil {
		conds = append(conds, `r.remind_at >= ?`)
		args = append(args, *f.From)
	}
	if f.To != nil {
		conds = append(conds, `r.remind_at < ?`)
		args = append(args, *f.To)
	}

	// The join is only needed to ask about the owning event, so it is always
	// present and every column is qualified; the reminder's own columns are
	// unambiguous, but event_id would not be.
	query := `SELECT ` + reminderColumnsQualified + `
	          FROM reminders r JOIN events e ON e.id = r.event_id`
	if !f.IncludeDeleted {
		conds = append(conds, `e.deleted = 0`)
	}
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, " AND ")
	}
	query += ` ORDER BY r.remind_at ASC, r.id ASC`

	if f.Limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, f.Limit, f.Offset)
	} else if f.Offset > 0 {
		query += ` LIMIT -1 OFFSET ?`
		args = append(args, f.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list reminders: %w", err)
	}
	defer rows.Close()

	var out []model.Reminder
	for rows.Next() {
		var r model.Reminder
		if err := rows.Scan(&r.ID, &r.EventID, &r.RemindAt, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan reminder: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reminders: %w", err)
	}
	return out, nil
}

// UpdateReminder moves a reminder to a new time. created_at is left alone.
func (s *Store) UpdateReminder(ctx context.Context, id, remindAt int64) (model.Reminder, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE reminders SET remind_at = ? WHERE id = ?`, remindAt, id)
	if err != nil {
		return model.Reminder{}, fmt.Errorf("update reminder: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return model.Reminder{}, fmt.Errorf("update reminder rows: %w", err)
	}
	if affected == 0 {
		return model.Reminder{}, apperr.ErrNotFound
	}
	return s.GetReminder(ctx, id)
}

// DeleteReminder removes one reminder.
func (s *Store) DeleteReminder(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM reminders WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete reminder: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete reminder rows: %w", err)
	}
	if affected == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// FindReminderAt returns the reminder of an event at an exact time, which is
// how a duplicate create is detected.
func (s *Store) FindReminderAt(ctx context.Context, eventID, remindAt int64) (model.Reminder, error) {
	var r model.Reminder
	err := s.db.QueryRowContext(ctx,
		`SELECT `+reminderColumns+` FROM reminders WHERE event_id = ? AND remind_at = ? ORDER BY id ASC LIMIT 1`,
		eventID, remindAt).
		Scan(&r.ID, &r.EventID, &r.RemindAt, &r.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.Reminder{}, apperr.ErrNotFound
	}
	if err != nil {
		return model.Reminder{}, fmt.Errorf("find reminder: %w", err)
	}
	return r, nil
}

// CountRemindersForEvent counts the reminders of one event.
func (s *Store) CountRemindersForEvent(ctx context.Context, eventID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM reminders WHERE event_id = ?`, eventID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count reminders: %w", err)
	}
	return count, nil
}

// EventExists reports whether an event id is present.
//
// Checked explicitly rather than relying on the foreign key violation so that
// the caller can return a field-level error instead of a driver message.
func (s *Store) EventExists(ctx context.Context, id int64) (bool, error) {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM events WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check event: %w", err)
	}
	return true, nil
}
