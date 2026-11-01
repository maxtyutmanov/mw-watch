// Package tracker samples the probe on a fixed tick and records, per 1-minute
// bucket, whether there was user input and whether the screen was on.
package tracker

import (
	"context"
	"log"
	"time"

	"github.com/maxtyutmanov/mw-watch/internal/probe"
)

const (
	// BucketSeconds is the size of one stored bucket.
	BucketSeconds = 60
	// TickInterval divides BucketSeconds evenly, so every tick window lies inside one bucket.
	TickInterval = 10 * time.Second
	// maxGap is the longest tick window taken at face value. Longer gaps come
	// from sleep or a stalled process and are treated as a single tick, so
	// input after resume is not credited to the time spent asleep.
	maxGap = 60 * time.Second
	// idleSlack absorbs the jitter between the scheduled tick and the actual sample.
	idleSlack = 0.5
)

// Sink stores bucket flags. Implementations must merge with OR semantics:
// a flag that was once set for a bucket stays set.
type Sink interface {
	Mark(bucketStart int64, active, screenOn bool) error
}

// Tracker turns probe samples into bucket flags.
type Tracker struct {
	probe probe.Probe
	sink  Sink
	log   *log.Logger

	prev     time.Time
	bucket   int64
	active   bool
	screenOn bool
}

func New(p probe.Probe, sink Sink, logger *log.Logger) *Tracker {
	return &Tracker{probe: p, sink: sink, log: logger}
}

// BucketStart returns the start (Unix seconds) of the bucket that the tick
// window ending at tick belongs to.
func BucketStart(tick time.Time) int64 {
	s := tick.Add(-time.Second).Unix()
	return s - s%BucketSeconds
}

// Run samples on wall-clock aligned ticks until ctx is cancelled.
func (t *Tracker) Run(ctx context.Context) {
	for {
		next := time.Now().Truncate(TickInterval).Add(TickInterval)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s, err := t.probe.Sample()
		if err != nil {
			t.log.Printf("sample: %v", err)
			continue
		}
		// Round to the scheduled tick; this also drops the monotonic reading so
		// the gap check below sees wall-clock time across sleep.
		if err := t.Step(time.Now().Round(TickInterval), s); err != nil {
			t.log.Printf("store: %v", err)
		}
	}
}

// Step records one sample taken at the tick time tick.
func (t *Tracker) Step(tick time.Time, s probe.State) error {
	window := tick.Sub(t.prev)
	if t.prev.IsZero() || window <= 0 || window > maxGap {
		window = TickInterval
	}
	t.prev = tick

	bucket := BucketStart(tick)
	if bucket != t.bucket {
		t.bucket, t.active, t.screenOn = bucket, false, false
	}

	active := t.active || s.IdleSeconds < window.Seconds()+idleSlack
	screenOn := t.screenOn || s.ScreenOn()
	if active == t.active && screenOn == t.screenOn {
		return nil
	}
	if err := t.sink.Mark(bucket, active, screenOn); err != nil {
		return err
	}
	t.active, t.screenOn = active, screenOn
	return nil
}
