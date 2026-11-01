package store

import (
	"path/filepath"
	"testing"
	"time"

	_ "time/tzdata"
)

func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	// Tests use fixed dates, so put "now" after all of them.
	s.now = func() time.Time { return time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC) }
	return s
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// markRange marks every minute in [from, to).
func markRange(t *testing.T, s *Store, from, to time.Time, active, screenOn bool) {
	t.Helper()
	for m := from; m.Before(to); m = m.Add(time.Minute) {
		if err := s.Mark(m.Unix(), active, screenOn); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMarkNeverClearsFlags(t *testing.T) {
	s := openTemp(t)
	start := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	s.Mark(start.Unix(), true, false)
	s.Mark(start.Unix(), false, true)
	s.Mark(start.Unix(), false, false)

	pts, err := s.Aggregate(start, start.Add(time.Minute), Minute, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 || pts[0].ActiveMin != 1 || pts[0].ScreenMin != 1 {
		t.Fatalf("got %+v", pts)
	}
}

func TestAggregateSkipsMinuteInProgress(t *testing.T) {
	s := openTemp(t)
	hour := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	markRange(t, s, hour, hour.Add(5*time.Minute), true, true)
	// 10:04 is still running: screen on, no input seen yet.
	s.Mark(hour.Add(4*time.Minute).Unix(), false, true)
	s.now = func() time.Time { return hour.Add(4*time.Minute + 20*time.Second) }

	pts, err := s.Aggregate(hour, hour.Add(time.Hour), Hour, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 1 || pts[0].ActiveMin != 4 || pts[0].ScreenMin != 4 {
		t.Fatalf("got %+v, want 4 active and 4 screen-on minutes", pts)
	}
}

func TestAggregateHourlyZeroFill(t *testing.T) {
	s := openTemp(t)
	loc := mustLoc(t, "Europe/Berlin")
	day := time.Date(2026, 11, 2, 0, 0, 0, 0, loc)
	markRange(t, s, day.Add(9*time.Hour+30*time.Minute), day.Add(10*time.Hour+15*time.Minute), true, true)

	pts, err := s.Aggregate(day, day.AddDate(0, 0, 1), Hour, loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 24 {
		t.Fatalf("len = %d, want 24", len(pts))
	}
	if pts[9].ActiveMin != 30 || pts[10].ActiveMin != 15 || pts[11].ActiveMin != 0 {
		t.Fatalf("hours 9-11 = %+v %+v %+v", pts[9], pts[10], pts[11])
	}
}

func TestAggregateDSTDay(t *testing.T) {
	s := openTemp(t)
	loc := mustLoc(t, "Europe/Berlin")
	// Clocks go forward at 02:00 on 2026-03-29, so the day has 23 hours.
	day := time.Date(2026, 3, 29, 0, 0, 0, 0, loc)
	next := day.AddDate(0, 0, 1)
	markRange(t, s, day, next, true, false)

	hours, err := s.Aggregate(day, next, Hour, loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 23 {
		t.Fatalf("hourly points = %d, want 23", len(hours))
	}
	days, err := s.Aggregate(day.AddDate(0, 0, -1), next.AddDate(0, 0, 1), Day, loc)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 3 || days[1].ActiveMin != 23*60 || days[0].ActiveMin != 0 || days[2].ActiveMin != 0 {
		t.Fatalf("days = %+v", days)
	}
}

func TestAggregateWeeksStartMonday(t *testing.T) {
	s := openTemp(t)
	// 2026-11-01 is a Sunday; its week starts on Monday 2026-10-26.
	sunday := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	monday := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	s.Mark(sunday.Unix(), true, true)
	s.Mark(monday.Unix(), true, true)

	from := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	pts, err := s.Aggregate(from, from.AddDate(0, 0, 10), Week, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(pts), pts)
	}
	if !pts[0].Start.Equal(time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)) || pts[0].ActiveMin != 1 || pts[1].ActiveMin != 1 {
		t.Fatalf("weeks = %+v", pts)
	}
}

func TestAggregateMonthsClipToRange(t *testing.T) {
	s := openTemp(t)
	s.Mark(time.Date(2026, 1, 31, 23, 59, 0, 0, time.UTC).Unix(), true, false)
	s.Mark(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC).Unix(), true, false)
	s.Mark(time.Date(2026, 2, 28, 23, 59, 0, 0, time.UTC).Unix(), true, false)
	s.Mark(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Unix(), true, false)

	// The range starts mid-January, so January only counts from the 15th on.
	from := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	pts, err := s.Aggregate(from, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Month, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 || pts[0].ActiveMin != 1 || pts[1].ActiveMin != 2 {
		t.Fatalf("months = %+v", pts)
	}
}

func TestAggregateTooManyPoints(t *testing.T) {
	s := openTemp(t)
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.Aggregate(from, from.AddDate(0, 0, 7), Minute, time.UTC); err == nil {
		t.Fatal("expected an error for 10080 points")
	}
}

func TestFloorNonHourOffset(t *testing.T) {
	loc := mustLoc(t, "Asia/Kolkata") // UTC+05:30
	got := Hour.Floor(time.Date(2026, 11, 1, 14, 47, 0, 0, loc))
	if want := time.Date(2026, 11, 1, 14, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("Floor = %v, want %v", got, want)
	}
}
