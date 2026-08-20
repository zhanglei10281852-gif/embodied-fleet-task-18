package apperr

import (
	"time"

	"github.com/benbjohnson/clock"
)

// Clock abstracts time so tests never depend on real wall-clock waits.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
	NewTimer(d time.Duration) Timer
	NewTicker(d time.Duration) Ticker
}

// Timer abstracts time.Timer.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(d time.Duration) bool
}

// Ticker abstracts time.Ticker.
type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type realClock struct{}

// Default returns a Clock backed by wall-clock time.
func Default() Clock { return realClock{} }

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (realClock) NewTimer(d time.Duration) Timer         { return &realTimer{t: time.NewTimer(d)} }
func (realClock) NewTicker(d time.Duration) Ticker       { return &realTicker{t: time.NewTicker(d)} }

type realTimer struct{ t *time.Timer }

func (r *realTimer) C() <-chan time.Time        { return r.t.C }
func (r *realTimer) Stop() bool                 { return r.t.Stop() }
func (r *realTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }

type realTicker struct{ t *time.Ticker }

func (r *realTicker) C() <-chan time.Time { return r.t.C }
func (r *realTicker) Stop()               { r.t.Stop() }

// FakeClock wraps benbjohnson/clock for deterministic tests.
type FakeClock struct {
	mock *clock.Mock
}

// NewFake returns a FakeClock initialised at the given time (or now if zero).
func NewFake(t time.Time) *FakeClock {
	m := clock.NewMock()
	if !t.IsZero() {
		m.Set(t)
	}
	return &FakeClock{mock: m}
}

func (f *FakeClock) Now() time.Time                         { return f.mock.Now() }
func (f *FakeClock) After(d time.Duration) <-chan time.Time { return f.mock.After(d) }
func (f *FakeClock) NewTimer(d time.Duration) Timer         { return &fakeTimer{m: f.mock, d: d} }
func (f *FakeClock) NewTicker(d time.Duration) Ticker       { return &fakeTicker{m: f.mock, d: d} }

// Advance moves the fake clock forward.
func (f *FakeClock) Advance(d time.Duration) { f.mock.Add(d) }

// Set sets the fake clock to a specific time.
func (f *FakeClock) Set(t time.Time) { f.mock.Set(t) }

type fakeTimer struct {
	m       *clock.Mock
	d       time.Duration
	ch      chan time.Time
	stopped bool
}

func (t *fakeTimer) C() <-chan time.Time {
	if t.ch == nil {
		t.ch = make(chan time.Time, 1)
		go func() {
			<-t.m.After(t.d)
			if !t.stopped {
				select {
				case t.ch <- t.m.Now():
				default:
				}
			}
		}()
	}
	return t.ch
}

func (t *fakeTimer) Stop() bool {
	t.stopped = true
	return true
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.d = d
	t.stopped = false
	return true
}

type fakeTicker struct {
	m       *clock.Mock
	d       time.Duration
	ch      chan time.Time
	stopped bool
}

func (tk *fakeTicker) C() <-chan time.Time {
	if tk.ch == nil {
		tk.ch = make(chan time.Time, 1)
		go func() {
			for !tk.stopped {
				<-tk.m.After(tk.d)
				if tk.stopped {
					return
				}
				select {
				case tk.ch <- tk.m.Now():
				default:
				}
			}
		}()
	}
	return tk.ch
}

func (tk *fakeTicker) Stop() { tk.stopped = true }
