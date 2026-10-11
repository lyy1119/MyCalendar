package calendar

import (
	"testing"
	"time"
)

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func mustDate(t *testing.T, value string, loc *time.Location) time.Time {
	t.Helper()
	d, err := ParseDate(value, loc)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", value, err)
	}
	return d
}

func TestDayWindowUsesLocalMidnight(t *testing.T) {
	shanghai := mustLoad(t, "Asia/Shanghai")
	day := mustDate(t, "2026-03-11", shanghai)

	w := Day(day, shanghai)

	if got, want := w.StartDate, "2026-03-11"; got != want {
		t.Errorf("StartDate = %q, want %q", got, want)
	}
	if got, want := w.EndDate, "2026-03-12"; got != want {
		t.Errorf("EndDate = %q, want %q", got, want)
	}
	if got, want := w.StartRFC3339(), "2026-03-11T00:00:00+08:00"; got != want {
		t.Errorf("StartRFC3339 = %q, want %q", got, want)
	}
	// Shanghai has no DST, so a day is exactly 24 hours.
	if got, want := w.EndUnix()-w.StartUnix(), int64(86400); got != want {
		t.Errorf("day length = %d, want %d", got, want)
	}
}

// A day is not always 24 hours: building boundaries from wall-clock midnight
// is what keeps DST transitions honest.
func TestDayWindowAcrossDSTTransitions(t *testing.T) {
	ny := mustLoad(t, "America/New_York")

	tests := []struct {
		name     string
		date     string
		wantSecs int64
	}{
		{"spring forward is 23 hours", "2026-03-08", 23 * 3600},
		{"fall back is 25 hours", "2026-11-01", 25 * 3600},
		{"ordinary day is 24 hours", "2026-06-15", 24 * 3600},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := Day(mustDate(t, tc.date, ny), ny)
			if got := w.EndUnix() - w.StartUnix(); got != tc.wantSecs {
				t.Errorf("day length = %d seconds, want %d", got, tc.wantSecs)
			}
			if h, m, s := w.Start.Clock(); h != 0 || m != 0 || s != 0 {
				t.Errorf("window does not start at local midnight: %02d:%02d:%02d", h, m, s)
			}
		})
	}
}

func TestWeekWindowHonoursWeekStart(t *testing.T) {
	shanghai := mustLoad(t, "Asia/Shanghai")
	// 2026-03-11 is a Wednesday.
	day := mustDate(t, "2026-03-11", shanghai)

	tests := []struct {
		name      string
		weekStart WeekStart
		wantStart string
		wantEnd   string
	}{
		{"monday start", Monday, "2026-03-09", "2026-03-16"},
		{"sunday start", Sunday, "2026-03-08", "2026-03-15"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := Week(day, shanghai, tc.weekStart)
			if w.StartDate != tc.wantStart || w.EndDate != tc.wantEnd {
				t.Errorf("week = [%s, %s), want [%s, %s)", w.StartDate, w.EndDate, tc.wantStart, tc.wantEnd)
			}
			if got, want := w.EndUnix()-w.StartUnix(), int64(7*86400); got != want {
				t.Errorf("week length = %d, want %d", got, want)
			}
		})
	}
}

// A week containing a DST change spans 167 or 169 hours, not 168.
func TestWeekWindowLengthAcrossDST(t *testing.T) {
	ny := mustLoad(t, "America/New_York")
	w := Week(mustDate(t, "2026-03-08", ny), ny, Monday)
	if got, want := w.EndUnix()-w.StartUnix(), int64(167*3600); got != want {
		t.Errorf("week length = %d seconds, want %d", got, want)
	}
}

func TestMonthWindowEdges(t *testing.T) {
	tests := []struct {
		name      string
		zone      string
		date      string
		wantStart string
		wantEnd   string
	}{
		{"31 day month", "Asia/Shanghai", "2026-03-31", "2026-03-01", "2026-04-01"},
		{"december rolls into next year", "Asia/Shanghai", "2026-12-15", "2026-12-01", "2027-01-01"},
		{"february non leap", "Asia/Shanghai", "2026-02-10", "2026-02-01", "2026-03-01"},
		{"february leap", "Asia/Shanghai", "2028-02-10", "2028-02-01", "2028-03-01"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			loc := mustLoad(t, tc.zone)
			w := Month(mustDate(t, tc.date, loc), loc)
			if w.StartDate != tc.wantStart || w.EndDate != tc.wantEnd {
				t.Errorf("month = [%s, %s), want [%s, %s)", w.StartDate, w.EndDate, tc.wantStart, tc.wantEnd)
			}
			if w.Start.Unix() >= w.End.Unix() {
				t.Errorf("month window is empty: %d >= %d", w.Start.Unix(), w.End.Unix())
			}
		})
	}
}

func TestParseDateRejectsImpossibleDates(t *testing.T) {
	loc := mustLoad(t, "Asia/Shanghai")
	for _, bad := range []string{"2026-02-30", "2026-13-01", "2026-00-10", "11-03-2026", "2026/03/11", "", "today"} {
		if _, err := ParseDate(bad, loc); err == nil {
			t.Errorf("ParseDate(%q) succeeded, want an error", bad)
		}
	}
	for _, good := range []string{"2026-03-11", "2028-02-29", "1970-01-01", "2099-12-31"} {
		if _, err := ParseDate(good, loc); err != nil {
			t.Errorf("ParseDate(%q) failed: %v", good, err)
		}
	}
}

// A partial-day window must still cover the calendar day it touches, so that
// all-day events are not silently dropped from a 09:00-17:00 query.
func TestLastTouchedDateForPartialWindows(t *testing.T) {
	shanghai := mustLoad(t, "Asia/Shanghai")

	start := time.Date(2026, 3, 11, 9, 0, 0, 0, shanghai)
	end := time.Date(2026, 3, 11, 17, 0, 0, 0, shanghai)
	if got, want := LastTouchedDate(start, end, shanghai), "2026-03-12"; got != want {
		t.Errorf("partial day LastTouchedDate = %q, want %q", got, want)
	}

	// A window that ends exactly at midnight belongs to the previous day.
	dayStart := time.Date(2026, 3, 11, 0, 0, 0, 0, shanghai)
	dayEnd := time.Date(2026, 3, 12, 0, 0, 0, 0, shanghai)
	if got, want := LastTouchedDate(dayStart, dayEnd, shanghai), "2026-03-12"; got != want {
		t.Errorf("whole day LastTouchedDate = %q, want %q", got, want)
	}
}

func TestCustomWindowValidation(t *testing.T) {
	loc := mustLoad(t, "UTC")
	start := time.Unix(1_000_000, 0)

	if _, err := Custom(start, start, loc, Monday); err == nil {
		t.Error("Custom with equal bounds succeeded, want an error")
	}
	if _, err := Custom(start, start.Add(-time.Second), loc, Monday); err == nil {
		t.Error("Custom with reversed bounds succeeded, want an error")
	}
	w, err := Custom(start, start.Add(time.Hour), loc, Monday)
	if err != nil {
		t.Fatalf("Custom: %v", err)
	}
	if w.Kind != KindCustom {
		t.Errorf("Kind = %q, want %q", w.Kind, KindCustom)
	}
}

func TestParseKindAndWeekStart(t *testing.T) {
	for _, ok := range []string{"day", "week", "month", "DAY", " Month "} {
		if _, err := ParseKind(ok); err != nil {
			t.Errorf("ParseKind(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{"year", "", "days"} {
		if _, err := ParseKind(bad); err == nil {
			t.Errorf("ParseKind(%q) succeeded, want an error", bad)
		}
	}
	for _, ok := range []string{"monday", "sunday", "MONDAY", ""} {
		if _, err := ParseWeekStart(ok); err != nil {
			t.Errorf("ParseWeekStart(%q): %v", ok, err)
		}
	}
	if _, err := ParseWeekStart("tuesday"); err == nil {
		t.Error("ParseWeekStart(tuesday) succeeded, want an error")
	}
}

func TestWindowIntersects(t *testing.T) {
	loc := mustLoad(t, "UTC")
	w := Day(time.Date(2026, 3, 11, 0, 0, 0, 0, loc), loc)

	tests := []struct {
		name       string
		start, end int64
		want       bool
	}{
		{"fully inside", w.StartUnix() + 3600, w.StartUnix() + 7200, true},
		{"starts before ends inside", w.StartUnix() - 3600, w.StartUnix() + 60, true},
		{"starts inside ends after", w.EndUnix() - 60, w.EndUnix() + 3600, true},
		{"spans the whole window", w.StartUnix() - 3600, w.EndUnix() + 3600, true},
		{"ends exactly at window start", w.StartUnix() - 3600, w.StartUnix(), false},
		{"starts exactly at window end", w.EndUnix(), w.EndUnix() + 3600, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := w.Intersects(tc.start, tc.end); got != tc.want {
				t.Errorf("Intersects(%d, %d) = %v, want %v", tc.start, tc.end, got, tc.want)
			}
		})
	}
}
