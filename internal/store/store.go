// Package store keeps 1-minute activity buckets in a local SQLite database
// and aggregates them into chart points.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS buckets (
	start     INTEGER PRIMARY KEY, -- unix seconds, multiple of 60
	active    INTEGER NOT NULL DEFAULT 0,
	screen_on INTEGER NOT NULL DEFAULT 0
);`

// Store is a SQLite-backed bucket store. It is safe for concurrent use.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection keeps the pragmas below in effect and serializes writes.
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		schema,
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("init %s: %w", path, err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Mark sets the flags of the bucket starting at bucketStart. Flags are never cleared.
func (s *Store) Mark(bucketStart int64, active, screenOn bool) error {
	_, err := s.db.Exec(`
		INSERT INTO buckets (start, active, screen_on) VALUES (?, ?, ?)
		ON CONFLICT(start) DO UPDATE SET
			active    = MAX(active, excluded.active),
			screen_on = MAX(screen_on, excluded.screen_on)`,
		bucketStart, boolInt(active), boolInt(screenOn))
	return err
}

// Point is one aggregated chart bar. Minutes count flagged 1-minute buckets.
type Point struct {
	Start     time.Time `json:"start"`
	ActiveMin int       `json:"activeMin"`
	ScreenMin int       `json:"screenMin"`
}

// MaxPoints limits how many points one Aggregate call may return.
const MaxPoints = 3000

// Aggregate groups the buckets in [from, to) into points of granularity g,
// with boundaries in loc. Every point in the range is returned, including empty ones.
func (s *Store) Aggregate(from, to time.Time, g Granularity, loc *time.Location) ([]Point, error) {
	if !to.After(from) {
		return nil, fmt.Errorf("empty range")
	}
	var points []Point
	for b := g.Floor(from.In(loc)); b.Before(to); b = g.Next(b) {
		if len(points) == MaxPoints {
			return nil, fmt.Errorf("range has more than %d points at granularity %s", MaxPoints, g)
		}
		points = append(points, Point{Start: b})
	}

	rows, err := s.db.Query(`SELECT start, active, screen_on FROM buckets
		WHERE start >= ? AND start < ? ORDER BY start`, from.Unix(), to.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	i := 0
	for rows.Next() {
		var start int64
		var active, screenOn int
		if err := rows.Scan(&start, &active, &screenOn); err != nil {
			return nil, err
		}
		t := time.Unix(start, 0)
		for i+1 < len(points) && !t.Before(points[i+1].Start) {
			i++
		}
		points[i].ActiveMin += active
		points[i].ScreenMin += screenOn
	}
	return points, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
