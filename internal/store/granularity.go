package store

import (
	"fmt"
	"time"
)

// Granularity is the width of one aggregated chart point.
type Granularity string

const (
	Minute         Granularity = "1m"
	FiveMinutes    Granularity = "5m"
	FifteenMinutes Granularity = "15m"
	Hour           Granularity = "1h"
	Day            Granularity = "1d"
	Week           Granularity = "1w"
	Month          Granularity = "1mo"
)

var fixed = map[Granularity]time.Duration{
	Minute:         time.Minute,
	FiveMinutes:    5 * time.Minute,
	FifteenMinutes: 15 * time.Minute,
	Hour:           time.Hour,
}

func ParseGranularity(s string) (Granularity, error) {
	g := Granularity(s)
	if _, ok := fixed[g]; ok || g == Day || g == Week || g == Month {
		return g, nil
	}
	return "", fmt.Errorf("unknown granularity %q", s)
}

// Floor returns the start of the point containing t, in t's location.
// Weeks start on Monday.
func (g Granularity) Floor(t time.Time) time.Time {
	y, mo, d := t.Date()
	loc := t.Location()
	switch g {
	case Day:
		return time.Date(y, mo, d, 0, 0, 0, 0, loc)
	case Week:
		daysSinceMonday := (int(t.Weekday()) + 6) % 7
		return time.Date(y, mo, d-daysSinceMonday, 0, 0, 0, 0, loc)
	case Month:
		return time.Date(y, mo, 1, 0, 0, 0, 0, loc)
	}
	// Floor on the local wall clock, so hours and quarters line up with local
	// time even in zones with non-hour UTC offsets.
	step := int(fixed[g] / time.Minute)
	minutes := t.Hour()*60 + t.Minute()
	minutes -= minutes % step
	return time.Date(y, mo, d, minutes/60, minutes%60, 0, 0, loc)
}

// Next returns the start of the point after the one starting at t. Calendar
// granularities follow the wall clock, so a day can be 23 or 25 hours long
// across a DST change.
func (g Granularity) Next(t time.Time) time.Time {
	switch g {
	case Day:
		return t.AddDate(0, 0, 1)
	case Week:
		return t.AddDate(0, 0, 7)
	case Month:
		return t.AddDate(0, 1, 0)
	}
	return t.Add(fixed[g])
}
