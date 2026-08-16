// Package newswatch owns when a scan runs and who hears about it.
//
// The agent's shape forces this design rather than the other way around: a scan
// takes about a minute and costs real money per run, so it cannot be done
// inside a request and cannot be done on a short timer. What is left is a
// watcher that holds the last result, refreshes it rarely, and pushes to
// whoever is listening when it changes.
//
// Three rules keep an autonomous, billable agent from being a liability on a
// public page with no sign-in:
//
//   - It only scans when somebody is watching. An empty room costs nothing.
//   - It scans at most once per interval, and the interval survives restarts
//     because the last digest is persisted — a crash loop cannot turn into a
//     scan loop.
//   - Exactly one scan runs at a time, however many people are watching.
package newswatch

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/expona-ai/lumi-go/server/internal/newsagent"
)

// State is what the watcher is doing.
type State int

const (
	StateIdle State = iota
	StateScanning
)

// Event is one notification to a subscriber.
type Event struct {
	// Snapshot is set only on the first event of a subscription.
	Snapshot *Snapshot
	// State is set when a scan starts or finishes.
	State *State
	// Digest is set when a scan produced a new result.
	Digest *newsagent.Digest
	// Err is set when a scan failed. The previous digest remains valid.
	Err error
}

// Snapshot is the current state, sent to every new subscriber so it can render
// immediately instead of waiting for the next change.
type Snapshot struct {
	State             State
	Digest            *newsagent.Digest
	NextScanAllowedAt time.Time
}

// Scanner is the agent seam. Production wires *newsagent.Agent; tests wire a
// fake, so none of the scheduling logic below needs a network to be tested.
type Scanner interface {
	Scan(ctx context.Context) (*newsagent.Digest, error)
}

// Store persists the last digest across restarts. Optional: without one the
// watcher still works, it just starts every boot with an empty digest and an
// immediately-available scan.
type Store interface {
	LoadDigest(ctx context.Context) (*newsagent.Digest, error)
	SaveDigest(ctx context.Context, d *newsagent.Digest) error
}

// Options configure a Watcher.
type Options struct {
	Scanner Scanner
	Store   Store
	// Interval is the minimum time between scans.
	Interval time.Duration
	// Timeout bounds a single scan.
	Timeout time.Duration
	Logger  *slog.Logger
}

// Watcher fans scan results out to subscribers.
type Watcher struct {
	scanner  Scanner
	store    Store
	interval time.Duration
	timeout  time.Duration
	log      *slog.Logger

	mu          sync.Mutex
	state       State
	digest      *newsagent.Digest
	lastScanAt  time.Time
	subscribers map[int]chan Event
	nextSubID   int
	scanning    bool
}

// New builds a Watcher and restores the last digest if a store is configured.
// A failure to read the store is not fatal — an unavailable history means a
// stale-looking watcher, not a broken one.
func New(ctx context.Context, opts Options) *Watcher {
	w := &Watcher{
		scanner:     opts.Scanner,
		store:       opts.Store,
		interval:    opts.Interval,
		timeout:     opts.Timeout,
		log:         opts.Logger,
		subscribers: make(map[int]chan Event),
	}
	if w.log == nil {
		w.log = slog.Default()
	}
	if w.interval <= 0 {
		w.interval = 6 * time.Hour
	}
	if w.timeout <= 0 {
		w.timeout = 4 * time.Minute
	}

	if w.store != nil {
		if d, err := w.store.LoadDigest(ctx); err != nil {
			w.log.Warn("news: last digest unreadable — starting cold", "error", err)
		} else if d != nil {
			w.digest = d
			w.lastScanAt = d.GeneratedAt
			w.log.Info("news: restored digest", "items", len(d.Items), "generated_at", d.GeneratedAt)
		}
	}
	return w
}

// Subscribe registers a listener. The returned channel receives a snapshot
// immediately and every subsequent change; cancel unregisters it and closes the
// channel. Subscribing is also what makes the watcher consider scanning — see
// MaybeScan.
func (w *Watcher) Subscribe() (<-chan Event, func()) {
	// Buffered: a subscriber that stops reading must not be able to stall a
	// scan for everyone else. Sends are non-blocking, so a full channel drops
	// intermediate events rather than wedging the watcher — the next snapshot
	// re-syncs it anyway.
	ch := make(chan Event, 8)

	w.mu.Lock()
	id := w.nextSubID
	w.nextSubID++
	w.subscribers[id] = ch
	snap := w.snapshotLocked()
	w.mu.Unlock()

	ch <- Event{Snapshot: &snap}

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			w.mu.Lock()
			delete(w.subscribers, id)
			w.mu.Unlock()
			close(ch)
		})
	}
}

// ErrTooSoon is returned when a scan is refused because the interval has not
// elapsed. Distinct from a failure: the caller should show the existing digest,
// not an error.
var ErrTooSoon = errors.New("news: scan interval has not elapsed")

// MaybeScan starts a scan if one is due and none is running. It returns
// immediately; the result arrives on subscriber channels.
//
// Called when a subscriber connects, which is what ties spend to attention: the
// watcher does no work for an empty room, and a page nobody opens costs nothing.
func (w *Watcher) MaybeScan(ctx context.Context) error {
	w.mu.Lock()
	switch {
	case w.scanner == nil:
		w.mu.Unlock()
		return errors.New("news: no scanner configured")
	case w.scanning:
		w.mu.Unlock()
		// Not an error: someone else's scan is already producing the result
		// this caller wants.
		return nil
	case !w.dueLocked():
		w.mu.Unlock()
		return ErrTooSoon
	}
	w.scanning = true
	w.state = StateScanning
	w.mu.Unlock()

	w.broadcastState(StateScanning)

	go w.runScan(context.WithoutCancel(ctx))
	return nil
}

// runScan performs the scan and publishes the outcome. It deliberately does not
// inherit the subscriber's context: the visitor who triggered the scan will
// often close the tab before a minute is out, and a scan already being billed
// for should finish and be saved rather than be abandoned half-paid.
func (w *Watcher) runScan(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	started := time.Now()
	digest, err := w.scanner.Scan(ctx)

	w.mu.Lock()
	w.scanning = false
	w.state = StateIdle
	// The clock starts on completion, success or failure. Restarting it only on
	// success would let a persistently failing provider be retried without
	// limit, which is the expensive way to discover an outage.
	w.lastScanAt = time.Now()
	if err == nil {
		w.digest = digest
	}
	w.mu.Unlock()

	if err != nil {
		w.log.Warn("news: scan failed", "error", err, "elapsed", time.Since(started).Round(time.Second))
		w.broadcast(Event{Err: err})
		w.broadcastState(StateIdle)
		return
	}

	w.log.Info("news: scan complete",
		"items", len(digest.Items),
		"tool_calls", digest.ToolCalls,
		"tokens", digest.TotalTokens,
		"cost_ticks", digest.CostTicks,
		"elapsed", time.Since(started).Round(time.Second))

	if w.store != nil {
		if err := w.store.SaveDigest(ctx, digest); err != nil {
			// The digest is still served from memory, so this costs durability
			// across a restart, not the result itself.
			w.log.Warn("news: digest not persisted — a restart will rescan", "error", err)
		}
	}

	w.broadcast(Event{Digest: digest})
	w.broadcastState(StateIdle)
}

// Snapshot returns the current state.
func (w *Watcher) Snapshot() Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.snapshotLocked()
}

func (w *Watcher) snapshotLocked() Snapshot {
	return Snapshot{
		State:             w.state,
		Digest:            w.digest,
		NextScanAllowedAt: w.nextScanAtLocked(),
	}
}

func (w *Watcher) dueLocked() bool {
	return w.lastScanAt.IsZero() || time.Since(w.lastScanAt) >= w.interval
}

func (w *Watcher) nextScanAtLocked() time.Time {
	if w.lastScanAt.IsZero() {
		return time.Time{}
	}
	return w.lastScanAt.Add(w.interval)
}

func (w *Watcher) broadcastState(s State) {
	w.broadcast(Event{State: &s})
}

func (w *Watcher) broadcast(ev Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, ch := range w.subscribers {
		select {
		case ch <- ev:
		default:
			// Slow subscriber. Dropping is correct: every subscription opens
			// with a snapshot, so a client that misses an event re-syncs on
			// reconnect rather than being held in a stale state forever.
		}
	}
}
