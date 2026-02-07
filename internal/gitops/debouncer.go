package gitops

import (
	"sync"
	"time"
)

// Debouncer calls a function after a quiet period.
// Each call to Trigger resets the timer.
type Debouncer struct {
	delay   time.Duration
	fn      func()
	timer   *time.Timer
	mu      sync.Mutex
	running sync.Mutex // serializes fn() execution
}

func NewDebouncer(delay time.Duration, fn func()) *Debouncer {
	return &Debouncer{
		delay: delay,
		fn:    fn,
	}
}

func (d *Debouncer) fire() {
	d.running.Lock()
	defer d.running.Unlock()
	d.fn()
}

func (d *Debouncer) Trigger() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.delay, d.fire)
}

func (d *Debouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
}

// Flush fires the function immediately if a trigger is pending.
func (d *Debouncer) Flush() {
	d.mu.Lock()
	pending := d.timer != nil
	if pending {
		d.timer.Stop()
		d.timer = nil
	}
	d.mu.Unlock()

	if pending {
		d.fire()
	}
}
