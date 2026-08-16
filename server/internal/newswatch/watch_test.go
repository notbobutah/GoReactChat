package newswatch

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/expona-ai/lumi-go/server/internal/newsagent"
)

type fakeScanner struct {
	calls   atomic.Int32
	delay   time.Duration
	err     error
	release chan struct{}
}

func (f *fakeScanner) Scan(ctx context.Context) (*newsagent.Digest, error) {
	f.calls.Add(1)
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.err != nil {
		return nil, f.err
	}
	return &newsagent.Digest{
		ID:          "d1",
		GeneratedAt: time.Now().UTC(),
		Items:       []newsagent.Item{{ID: "a", Topic: "go", Headline: "Go 1.99", URL: "https://go.dev/x"}},
		ToolCalls:   11,
	}, nil
}

type fakeStore struct {
	mu      sync.Mutex
	loaded  *newsagent.Digest
	saved   *newsagent.Digest
	saveErr error
	loadErr error
}

func (f *fakeStore) LoadDigest(context.Context) (*newsagent.Digest, error) {
	return f.loaded, f.loadErr
}

func (f *fakeStore) SaveDigest(_ context.Context, d *newsagent.Digest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = d
	return f.saveErr
}

// waitFor polls until cond holds, so a test never depends on a fixed sleep.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSubscriberGetsSnapshotImmediately(t *testing.T) {
	// A client that connects long after a scan must render at once. Without
	// this the page is blank until something changes, which for a four-hour
	// interval means blank.
	w := New(context.Background(), Options{
		Scanner: &fakeScanner{},
		Store:   &fakeStore{loaded: &newsagent.Digest{ID: "restored", GeneratedAt: time.Now()}},
	})

	events, cancel := w.Subscribe()
	defer cancel()

	select {
	case ev := <-events:
		if ev.Snapshot == nil {
			t.Fatalf("first event was not a snapshot: %+v", ev)
		}
		if ev.Snapshot.Digest == nil || ev.Snapshot.Digest.ID != "restored" {
			t.Errorf("snapshot did not carry the restored digest: %+v", ev.Snapshot.Digest)
		}
	case <-time.After(time.Second):
		t.Fatal("no snapshot on subscribe")
	}
}

func TestRestoredDigestSuppressesImmediateRescan(t *testing.T) {
	// The reason the digest is persisted at all: a restart must not spend money
	// re-deriving what the previous process already paid for. A crash loop
	// would otherwise be a scan loop.
	sc := &fakeScanner{}
	w := New(context.Background(), Options{
		Scanner:  sc,
		Store:    &fakeStore{loaded: &newsagent.Digest{ID: "fresh", GeneratedAt: time.Now()}},
		Interval: time.Hour,
	})

	if err := w.MaybeScan(context.Background()); !errors.Is(err, ErrTooSoon) {
		t.Fatalf("want ErrTooSoon with a fresh restored digest, got %v", err)
	}
	if n := sc.calls.Load(); n != 0 {
		t.Errorf("scanner ran %d times after restoring a fresh digest; want 0", n)
	}
}

func TestScanRunsWhenStale(t *testing.T) {
	sc := &fakeScanner{}
	st := &fakeStore{loaded: &newsagent.Digest{ID: "old", GeneratedAt: time.Now().Add(-2 * time.Hour)}}
	w := New(context.Background(), Options{Scanner: sc, Store: st, Interval: time.Hour})

	events, cancel := w.Subscribe()
	defer cancel()
	<-events // snapshot

	if err := w.MaybeScan(context.Background()); err != nil {
		t.Fatalf("stale digest should permit a scan: %v", err)
	}
	waitFor(t, "the scan to run", func() bool { return sc.calls.Load() == 1 })
	waitFor(t, "the digest to be persisted", func() bool {
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.saved != nil
	})

	var sawScanning, sawDigest bool
	for i := 0; i < 4 && !(sawScanning && sawDigest); i++ {
		select {
		case ev := <-events:
			if ev.State != nil && *ev.State == StateScanning {
				sawScanning = true
			}
			if ev.Digest != nil {
				sawDigest = true
			}
		case <-time.After(time.Second):
			i = 4
		}
	}
	if !sawScanning {
		t.Error("subscriber never saw the scanning state — the UI could not show work in progress")
	}
	if !sawDigest {
		t.Error("subscriber never received the digest")
	}
}

func TestConcurrentSubscribersTriggerOneScan(t *testing.T) {
	// Ten visitors arriving together must cost one scan, not ten. This is the
	// difference between a demo and a bill.
	sc := &fakeScanner{release: make(chan struct{})}
	w := New(context.Background(), Options{Scanner: sc, Interval: time.Hour})

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, cancel := w.Subscribe()
			defer cancel()
			_ = w.MaybeScan(context.Background())
		}()
	}
	wg.Wait()

	close(sc.release)
	waitFor(t, "the scan to finish", func() bool {
		return w.Snapshot().State == StateIdle && w.Snapshot().Digest != nil
	})

	if n := sc.calls.Load(); n != 1 {
		t.Errorf("10 concurrent subscribers caused %d scans; want exactly 1", n)
	}
}

func TestFailedScanKeepsPreviousDigestAndStartsTheClock(t *testing.T) {
	// A failing provider must not be retried on every page load. Restarting the
	// interval only on success is the expensive way to discover an outage.
	sc := &fakeScanner{err: errors.New("upstream down")}
	w := New(context.Background(), Options{
		Scanner:  sc,
		Store:    &fakeStore{loaded: &newsagent.Digest{ID: "kept", GeneratedAt: time.Now().Add(-2 * time.Hour)}},
		Interval: time.Hour,
	})

	events, cancel := w.Subscribe()
	defer cancel()
	<-events

	if err := w.MaybeScan(context.Background()); err != nil {
		t.Fatalf("scan should have been permitted: %v", err)
	}
	waitFor(t, "the failed scan to settle", func() bool { return w.Snapshot().State == StateIdle })

	snap := w.Snapshot()
	if snap.Digest == nil || snap.Digest.ID != "kept" {
		t.Errorf("a failed scan discarded the previous digest: %+v", snap.Digest)
	}
	if err := w.MaybeScan(context.Background()); !errors.Is(err, ErrTooSoon) {
		t.Errorf("a failed scan did not start the interval — the provider would be retried on every visit, got %v", err)
	}
	if n := sc.calls.Load(); n != 1 {
		t.Errorf("scanner ran %d times; want 1", n)
	}
}

func TestSlowSubscriberDoesNotBlockOthers(t *testing.T) {
	// One client that stops reading must not stall the fan-out. It loses
	// intermediate events and re-syncs from the next snapshot; everyone else is
	// unaffected.
	w := New(context.Background(), Options{Scanner: &fakeScanner{}, Interval: time.Hour})

	_, cancelSlow := w.Subscribe() // never drained, fills its buffer
	defer cancelSlow()

	fast, cancelFast := w.Subscribe()
	defer cancelFast()
	<-fast // snapshot

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			w.broadcastState(StateScanning)
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("broadcast blocked on a subscriber that stopped reading")
	}
}

func TestStoreLoadFailureIsNotFatal(t *testing.T) {
	// An unreadable history means a stale-looking watcher, not a broken one.
	w := New(context.Background(), Options{
		Scanner: &fakeScanner{},
		Store:   &fakeStore{loadErr: errors.New("db down")},
	})
	if snap := w.Snapshot(); snap.Digest != nil {
		t.Errorf("expected no digest after a failed load, got %+v", snap.Digest)
	}
	if err := w.MaybeScan(context.Background()); err != nil {
		t.Errorf("a cold watcher should permit a scan: %v", err)
	}
}
