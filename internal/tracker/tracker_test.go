package tracker

import (
	"testing"
	"time"

	"github.com/maxtyutmanov/mw-watch/internal/probe"
)

type mark struct {
	bucket           int64
	active, screenOn bool
}

type fakeSink struct{ marks []mark }

func (f *fakeSink) Mark(bucket int64, active, screenOn bool) error {
	f.marks = append(f.marks, mark{bucket, active, screenOn})
	return nil
}

var base = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func at(sec int) time.Time { return base.Add(time.Duration(sec) * time.Second) }

func on(idle float64) probe.State  { return probe.State{IdleSeconds: idle, DisplayOn: true} }
func off(idle float64) probe.State { return probe.State{IdleSeconds: idle} }

func TestBucketStart(t *testing.T) {
	cases := map[int]int64{
		10: base.Unix(),      // window (12:00:00, 12:00:10]
		60: base.Unix(),      // window (12:00:50, 12:01:00] still belongs to 12:00
		70: base.Unix() + 60, // first window of 12:01
	}
	for sec, want := range cases {
		if got := BucketStart(at(sec)); got != want {
			t.Errorf("BucketStart(+%ds) = %d, want %d", sec, got, want)
		}
	}
}

func TestStepMarksActiveAndScreen(t *testing.T) {
	sink := &fakeSink{}
	tr := New(nil, sink, nil)

	tr.Step(at(10), on(100)) // screen on, idle
	tr.Step(at(20), on(3))   // input 3 s ago
	tr.Step(at(30), on(1))   // nothing new to write
	tr.Step(at(70), off(500))
	tr.Step(at(80), off(2)) // input with the display off

	want := []mark{
		{base.Unix(), false, true},
		{base.Unix(), true, true},
		{base.Unix() + 60, true, false},
	}
	assertMarks(t, sink.marks, want)
}

func TestStepInputJustBeforeBoundary(t *testing.T) {
	sink := &fakeSink{}
	tr := New(nil, sink, nil)
	tr.Step(at(50), off(100))
	// Input at 12:00:59 is seen at the 12:01:00 tick and belongs to the 12:00 bucket.
	tr.Step(at(60), off(1))
	tr.Step(at(70), off(11)) // same input, now older than the window
	assertMarks(t, sink.marks, []mark{{base.Unix(), true, false}})
}

func TestStepLockedScreenIsNotScreenOn(t *testing.T) {
	sink := &fakeSink{}
	tr := New(nil, sink, nil)
	tr.Step(at(10), probe.State{IdleSeconds: 100, DisplayOn: true, Locked: true})
	if len(sink.marks) != 0 {
		t.Fatalf("locked screen recorded: %v", sink.marks)
	}
}

func TestStepAfterSleepDoesNotBackfill(t *testing.T) {
	sink := &fakeSink{}
	tr := New(nil, sink, nil)
	tr.Step(at(10), on(100))
	// Woke up two hours later; the last input was 30 minutes ago (before sleep),
	// which a two-hour window would wrongly count as activity.
	tr.Step(at(7210), on(1800))
	assertMarks(t, sink.marks, []mark{
		{base.Unix(), false, true},
		{BucketStart(at(7210)), false, true},
	})
}

func assertMarks(t *testing.T, got, want []mark) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("marks = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("marks = %v, want %v", got, want)
		}
	}
}
