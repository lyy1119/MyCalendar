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

const (
	eventColumns    = `id, title, notes, location, start_at, end_at, is_all_day, event_date, is_task, is_completed, created_at, updated_at, category_id, canceled, deleted`
	reminderColumns = `id, event_id, remind_at, created_at`
	// Same columns, qualified for a query that joins events.
	// Only ListReminders needs this -- it is the one place that has to ask about
	// the owning event, and there event_id would otherwise be ambiguous.
	reminderColumnsQualified = `r.id, r.event_id, r.remind_at, r.created_at`

	// reminderChunk bounds the size of an IN (...) clause; SQLite limits the
	// number of bound parameters per statement.
	reminderChunk = 400
)

// querier is satisfied by both *sql.DB and *sql.Tx.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// rowScanner is satisfied by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// CreateEvent inserts an event and its reminders in one transaction.
func (s *Store) CreateEvent(ctx context.Context, ev model.Event, reminders []int64) (model.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Event{}, fmt.Errorf("begin create: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO events (title, notes, location, start_at, end_at, is_all_day, event_date, is_task, is_completed, created_at, updated_at, category_id, canceled, deleted)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.Title, strOrNil(ev.Notes), strOrNil(ev.Location), ev.StartAt, intOrNil(ev.EndAt), boolInt(ev.IsAllDay),
		strOrNil(ev.EventDate), boolInt(ev.IsTask), boolInt(ev.IsCompleted), ev.CreatedAt, ev.UpdatedAt, ev.CategoryID,
		boolInt(ev.Canceled), boolInt(ev.Deleted))
	if err != nil {
		return model.Event{}, fmt.Errorf("insert event: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.Event{}, fmt.Errorf("read inserted id: %w", err)
	}
	ev.ID = id

	if err := insertReminders(ctx, tx, id, reminders, ev.CreatedAt); err != nil {
		return model.Event{}, err
	}

	created, err := scanEvent(tx.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM events WHERE id = ?`, id))
	if err != nil {
		return model.Event{}, fmt.Errorf("reread created event: %w", err)
	}
	byEvent, err := loadReminders(ctx, tx, []int64{id})
	if err != nil {
		return model.Event{}, err
	}
	if r, ok := byEvent[id]; ok {
		created.Reminders = r
	}

	if err := tx.Commit(); err != nil {
		return model.Event{}, fmt.Errorf("commit create: %w", err)
	}
	return created, nil
}

// GetEvent loads a single event together with its reminders.
func (s *Store) GetEvent(ctx context.Context, id int64) (model.Event, error) {
	ev, err := scanEvent(s.db.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM events WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Event{}, apperr.ErrNotFound
	}
	if err != nil {
		return model.Event{}, fmt.Errorf("get event: %w", err)
	}
	byEvent, err := loadReminders(ctx, s.db, []int64{id})
	if err != nil {
		return model.Event{}, err
	}
	if r, ok := byEvent[id]; ok {
		ev.Reminders = r
	}
	return ev, nil
}

// UpdateEvent writes a fully normalised event back to the database.
//
// When replaceReminders is true the existing reminder rows are replaced by the
// supplied set; otherwise reminders are left untouched.
func (s *Store) UpdateEvent(ctx context.Context, ev model.Event, reminders []int64, replaceReminders bool) (model.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Event{}, fmt.Errorf("begin update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`UPDATE events
		    SET title = ?, notes = ?, location = ?, start_at = ?, end_at = ?, is_all_day = ?,
		        event_date = ?, is_task = ?, is_completed = ?, updated_at = ?, category_id = ?,
		        canceled = ?, deleted = ?
		  WHERE id = ?`,
		ev.Title, strOrNil(ev.Notes), strOrNil(ev.Location), ev.StartAt, intOrNil(ev.EndAt), boolInt(ev.IsAllDay),
		strOrNil(ev.EventDate), boolInt(ev.IsTask), boolInt(ev.IsCompleted), ev.UpdatedAt, ev.CategoryID,
		boolInt(ev.Canceled), boolInt(ev.Deleted), ev.ID)
	if err != nil {
		return model.Event{}, fmt.Errorf("update event: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return model.Event{}, fmt.Errorf("update event rows: %w", err)
	}
	if affected == 0 {
		return model.Event{}, apperr.ErrNotFound
	}

	if replaceReminders {
		if _, err := tx.ExecContext(ctx, `DELETE FROM reminders WHERE event_id = ?`, ev.ID); err != nil {
			return model.Event{}, fmt.Errorf("clear reminders: %w", err)
		}
		if err := insertReminders(ctx, tx, ev.ID, reminders, ev.UpdatedAt); err != nil {
			return model.Event{}, err
		}
	}

	updated, err := scanEvent(tx.QueryRowContext(ctx, `SELECT `+eventColumns+` FROM events WHERE id = ?`, ev.ID))
	if err != nil {
		return model.Event{}, fmt.Errorf("reread updated event: %w", err)
	}
	byEvent, err := loadReminders(ctx, tx, []int64{ev.ID})
	if err != nil {
		return model.Event{}, err
	}
	if r, ok := byEvent[ev.ID]; ok {
		updated.Reminders = r
	}

	if err := tx.Commit(); err != nil {
		return model.Event{}, fmt.Errorf("commit update: %w", err)
	}
	return updated, nil
}

// DeleteEvent removes an event. Reminder rows go with it via ON DELETE CASCADE.
func (s *Store) DeleteEvent(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete event: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete event rows: %w", err)
	}
	if affected == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// ListEvents runs a range query and attaches the reminders of every match.
//
// The range predicate is the interesting part. A timed event is located by its
// absolute instants and matches whenever [start_at, end_at) intersects the
// window, which is what makes a multi-day event appear in every day, week and
// month it spans. A null end_at is treated as a one-second instant so that a
// point event sitting exactly on a window edge is still found.
//
// An all-day event is located by event_date instead, because its stored
// timestamps are only a rendering of that date in one particular timezone. A
// date is a date in every timezone, so comparing the date string against the
// window's local date span keeps all-day results stable for any tz parameter.
func (s *Store) ListEvents(ctx context.Context, f model.EventFilter) ([]model.Event, error) {
	var (
		conds []string
		args  []any
	)

	conds = append(conds, `(
		    (is_all_day = 1 AND event_date IS NOT NULL AND event_date >= ? AND event_date < ?)
		 OR (is_all_day = 0 AND start_at < ? AND COALESCE(end_at, start_at + 1) > ?)
		 OR (is_all_day = 1 AND event_date IS NULL AND start_at < ? AND COALESCE(end_at, start_at + 1) > ?)
		)`)
	args = append(args,
		f.StartDate, f.EndDate,
		f.End.Unix(), f.Start.Unix(),
		f.End.Unix(), f.Start.Unix(),
	)

	// A trashed event is still a row, but it is not on the calendar any more,
	// so a listing leaves it out unless the caller asks for the trash by name.
	switch {
	case f.Deleted != nil:
		conds = append(conds, `deleted = ?`)
		args = append(args, boolInt(*f.Deleted))
	default:
		conds = append(conds, `deleted = 0`)
	}
	if f.CategoryID != nil {
		conds = append(conds, `category_id = ?`)
		args = append(args, *f.CategoryID)
	}
	if f.Canceled != nil {
		conds = append(conds, `canceled = ?`)
		args = append(args, boolInt(*f.Canceled))
	}
	if f.IsTask != nil {
		conds = append(conds, `is_task = ?`)
		args = append(args, boolInt(*f.IsTask))
	}
	if f.IsCompleted != nil {
		conds = append(conds, `is_completed = ?`)
		args = append(args, boolInt(*f.IsCompleted))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		// Which column the term is matched against is the caller's choice: the
		// trash page offers title, location, notes or a date.
		switch f.SearchField {
		case "date":
			// The service resolves the date into the day's bounds in the request
			// timezone, so an event matches when it starts inside that day -- or
			// when it is an all-day event stored on that date.
			conds = append(conds, `(event_date = ? OR (is_all_day = 0 AND start_at >= ? AND start_at < ?))`)
			args = append(args, q, derefInt(f.SearchFrom), derefInt(f.SearchTo))
		case "location":
			conds = append(conds, `location LIKE ? ESCAPE '\'`)
			args = append(args, "%"+escapeLike(q)+"%")
		case "notes":
			conds = append(conds, `notes LIKE ? ESCAPE '\'`)
			args = append(args, "%"+escapeLike(q)+"%")
		default:
			conds = append(conds, `title LIKE ? ESCAPE '\'`)
			args = append(args, "%"+escapeLike(q)+"%")
		}
	}

	query := `SELECT ` + eventColumns + ` FROM events WHERE ` + strings.Join(conds, " AND ") +
		` ORDER BY is_all_day DESC, start_at ASC, id ASC`

	if f.Limit > 0 {
		query += ` LIMIT ? OFFSET ?`
		args = append(args, f.Limit, f.Offset)
	} else if f.Offset > 0 {
		// SQLite requires a LIMIT clause before OFFSET; -1 means "no limit".
		query += ` LIMIT -1 OFFSET ?`
		args = append(args, f.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var (
		events []model.Event
		ids    []int64
	)
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, ev)
		ids = append(ids, ev.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}

	byEvent, err := loadReminders(ctx, s.db, ids)
	if err != nil {
		return nil, err
	}
	for i := range events {
		if r, ok := byEvent[events[i].ID]; ok {
			events[i].Reminders = r
		}
	}
	return events, nil
}

// insertReminders writes reminder rows for one event.
//
// Times arrive as Unix seconds: the service resolves any timezone-dependent
// input before it reaches the database.
func insertReminders(ctx context.Context, tx *sql.Tx, eventID int64, remindAt []int64, now int64) error {
	for _, at := range remindAt {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO reminders (event_id, remind_at, created_at) VALUES (?, ?, ?)`,
			eventID, at, now); err != nil {
			return fmt.Errorf("insert reminder: %w", err)
		}
	}
	return nil
}

// loadReminders fetches the reminders of many events, chunking the IN clause.
func loadReminders(ctx context.Context, q querier, ids []int64) (map[int64][]model.Reminder, error) {
	out := make(map[int64][]model.Reminder, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	for start := 0; start < len(ids); start += reminderChunk {
		stop := min(start+reminderChunk, len(ids))
		chunk := ids[start:stop]

		query := `SELECT ` + reminderColumns + ` FROM reminders WHERE event_id IN (` +
			strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",") +
			`) ORDER BY event_id ASC, remind_at ASC, id ASC`
		args := make([]any, 0, len(chunk))
		for _, id := range chunk {
			args = append(args, id)
		}

		rows, err := q.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("query reminders: %w", err)
		}
		for rows.Next() {
			var r model.Reminder
			if err := rows.Scan(&r.ID, &r.EventID, &r.RemindAt, &r.CreatedAt); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scan reminder: %w", err)
			}
			out[r.EventID] = append(out[r.EventID], r)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("iterate reminders: %w", err)
		}
		rows.Close()
	}
	return out, nil
}

// scanEvent reads one row in the canonical column order.
func scanEvent(sc rowScanner) (model.Event, error) {
	var (
		ev        model.Event
		notes     sql.NullString
		location  sql.NullString
		endAt     sql.NullInt64
		eventDate sql.NullString
		allDay    int64
		task      int64
		completed int64
		canceled  int64
		deleted   int64
	)
	if err := sc.Scan(
		&ev.ID, &ev.Title, &notes, &location, &ev.StartAt, &endAt, &allDay,
		&eventDate, &task, &completed, &ev.CreatedAt, &ev.UpdatedAt, &ev.CategoryID,
		&canceled, &deleted,
	); err != nil {
		return model.Event{}, err
	}
	ev.Notes = nullStr(notes)
	ev.Location = nullStr(location)
	ev.EndAt = nullInt(endAt)
	ev.EventDate = nullStr(eventDate)
	ev.IsAllDay = allDay != 0
	ev.IsTask = task != 0
	ev.IsCompleted = completed != 0
	ev.Canceled = canceled != 0
	ev.Deleted = deleted != 0
	ev.Reminders = []model.Reminder{}
	return ev, nil
}

// derefInt renders an optional bound as a value the driver can bind.
func derefInt(p *int64) any {
	if p == nil {
		return 0
	}
	return *p
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func nullStr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func nullInt(ni sql.NullInt64) *int64 {
	if !ni.Valid {
		return nil
	}
	v := ni.Int64
	return &v
}

func strOrNil(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func intOrNil(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// escapeLike neutralises LIKE wildcards in user input.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
