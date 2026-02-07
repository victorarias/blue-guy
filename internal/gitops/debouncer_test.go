package gitops_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/victorarias/blue-guy/internal/gitops"
)

func TestDebouncer_FiresAfterQuietPeriod(t *testing.T) {
	var called atomic.Int32
	d := gitops.NewDebouncer(50*time.Millisecond, func() {
		called.Add(1)
	})

	d.Trigger()
	time.Sleep(100 * time.Millisecond)

	if got := called.Load(); got != 1 {
		t.Errorf("expected 1 call, got %d", got)
	}
}

func TestDebouncer_ResetsOnRetrigger(t *testing.T) {
	var called atomic.Int32
	d := gitops.NewDebouncer(50*time.Millisecond, func() {
		called.Add(1)
	})

	d.Trigger()
	time.Sleep(30 * time.Millisecond)
	d.Trigger() // reset the timer
	time.Sleep(30 * time.Millisecond)

	// Should not have fired yet (only 30ms since last trigger)
	if got := called.Load(); got != 0 {
		t.Errorf("expected 0 calls at this point, got %d", got)
	}

	time.Sleep(40 * time.Millisecond)
	if got := called.Load(); got != 1 {
		t.Errorf("expected 1 call after full delay, got %d", got)
	}
}

func TestDebouncer_Flush(t *testing.T) {
	var called atomic.Int32
	d := gitops.NewDebouncer(1*time.Second, func() {
		called.Add(1)
	})

	d.Trigger()
	d.Flush() // should fire immediately

	if got := called.Load(); got != 1 {
		t.Errorf("expected 1 call after flush, got %d", got)
	}
}

func TestDebouncer_StopPreventsExecution(t *testing.T) {
	var called atomic.Int32
	d := gitops.NewDebouncer(50*time.Millisecond, func() {
		called.Add(1)
	})

	d.Trigger()
	d.Stop()
	time.Sleep(100 * time.Millisecond)

	if got := called.Load(); got != 0 {
		t.Errorf("expected 0 calls after stop, got %d", got)
	}
}
