package budget

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/expona-ai/lumi-go/server/internal/orchestrator"
)

type fakeStore struct {
	mu       sync.Mutex
	total    int64
	writes   int
	loadErr  error
	writeErr error
}

func (f *fakeStore) TotalTokens(context.Context) (int64, error) {
	if f.loadErr != nil {
		return 0, f.loadErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total, nil
}

func (f *fakeStore) RecordTokens(_ context.Context, u orchestrator.Usage) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.total += u.BillableInputTokens() + u.OutputTokens
	f.writes++
	return nil
}

// plain builds usage for a call that did not touch the cache.
func plain(in, out int64) orchestrator.Usage {
	return orchestrator.Usage{InputTokens: in, OutputTokens: out}
}

func TestZeroLimitMeansUnlimited(t *testing.T) {
	// An unset budget must be an explicit "no cap", never a cap of zero that
	// silently refuses every call.
	tr, err := New(context.Background(), 0, nil, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, ok := tr.(Unlimited); !ok {
		t.Fatalf("want Unlimited, got %T", tr)
	}
	if err := tr.Allow(context.Background()); err != nil {
		t.Errorf("unlimited tracker denied a call: %v", err)
	}
}

func TestAllowsUntilLimitReached(t *testing.T) {
	ctx := context.Background()
	tr, err := New(ctx, 100, nil, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	if err := tr.Allow(ctx); err != nil {
		t.Fatalf("first call denied: %v", err)
	}
	tr.Record(ctx, plain(40, 30)) // 70 of 100

	if err := tr.Allow(ctx); err != nil {
		t.Fatalf("call denied while under budget: %v", err)
	}
	tr.Record(ctx, plain(20, 20)) // 110 — over

	err = tr.Allow(ctx)
	if !errors.Is(err, ErrExhausted) {
		t.Errorf("want ErrExhausted once over budget, got %v", err)
	}
}

func TestUsageResumesFromStore(t *testing.T) {
	// The whole point of persistence: a restart must not hand out a fresh
	// budget. This is the check that a crash loop cannot spend without bound.
	ctx := context.Background()
	st := &fakeStore{total: 950}

	tr, err := New(ctx, 1000, st, nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	used, limit := tr.Used()
	if used != 950 || limit != 1000 {
		t.Fatalf("want 950/1000 restored, got %d/%d", used, limit)
	}

	tr.Record(ctx, plain(30, 40)) // 1020
	if err := tr.Allow(ctx); !errors.Is(err, ErrExhausted) {
		t.Errorf("want exhausted after restored usage plus spend, got %v", err)
	}
}

func TestRecordPersists(t *testing.T) {
	ctx := context.Background()
	st := &fakeStore{}
	tr, _ := New(ctx, 1000, st, nil)

	tr.Record(ctx, plain(10, 5))
	tr.Record(ctx, plain(0, 0)) // no-op: nothing to record

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.writes != 1 {
		t.Errorf("want 1 persisted write, got %d", st.writes)
	}
	if st.total != 15 {
		t.Errorf("want 15 tokens persisted, got %d", st.total)
	}
}

func TestPersistFailureStillCountsInMemory(t *testing.T) {
	// A failed write must not lose the spend for this process, or a database
	// blip becomes an unmetered budget.
	ctx := context.Background()
	tr, _ := New(ctx, 100, &fakeStore{writeErr: errors.New("db down")}, nil)

	tr.Record(ctx, plain(60, 60))
	if err := tr.Allow(ctx); !errors.Is(err, ErrExhausted) {
		t.Errorf("want exhausted despite the failed write, got %v", err)
	}
}

func TestLoadFailureIsFatal(t *testing.T) {
	// Booting with a zeroed budget is precisely the failure this package
	// exists to prevent, so an unreadable total must stop the boot.
	_, err := New(context.Background(), 100, &fakeStore{loadErr: errors.New("db down")}, nil)
	if err == nil {
		t.Error("expected New to fail when the persisted total cannot be read")
	}
}

func TestConcurrentRecordsAreCounted(t *testing.T) {
	ctx := context.Background()
	tr, _ := New(ctx, 10_000, &fakeStore{}, nil)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr.Record(ctx, plain(1, 1))
		}()
	}
	wg.Wait()

	if used, _ := tr.Used(); used != 100 {
		t.Errorf("want 100 tokens counted under concurrency, got %d", used)
	}
}

func TestCachedPrefixIsCountedAtItsBilledRate(t *testing.T) {
	// The whole point of wiring caching through the budget. The provider
	// excludes cached tokens from InputTokens, so a tracker that added only
	// input+output would score this call at 200 — under-counting a 100k-token
	// prompt by three orders of magnitude and turning the cap into scenery.
	ctx := context.Background()
	st := &fakeStore{}
	tr, _ := New(ctx, 1_000_000, st, nil)

	tr.Record(ctx, orchestrator.Usage{
		InputTokens:          100,
		OutputTokens:         100,
		CacheReadInputTokens: 100_000,
	})

	used, _ := tr.Used()
	const want = 100 + 100 + 10_000 // the cached read at a tenth
	if used != want {
		t.Errorf("want %d billable tokens counted, got %d", want, used)
	}
	if used <= 200 {
		t.Error("cached tokens were not counted at all — the cap would be effectively unbounded")
	}
}

func TestCacheWriteCostsAPremium(t *testing.T) {
	// A cache write is more expensive than processing the same tokens once, so
	// the first turn of a conversation must not be scored as though it were
	// cheaper than an uncached one.
	ctx := context.Background()
	tr, _ := New(ctx, 1_000_000, &fakeStore{}, nil)

	tr.Record(ctx, orchestrator.Usage{CacheCreationInputTokens: 10_000})

	used, _ := tr.Used()
	if used != 12_500 {
		t.Errorf("want a 1.25x write premium (12500), got %d", used)
	}
}

func TestBillableIsLessThanRawWhenCacheHits(t *testing.T) {
	// The saving is the reason the feature exists; assert it is real rather
	// than assumed.
	u := orchestrator.Usage{InputTokens: 500, OutputTokens: 500, CacheReadInputTokens: 26_000}
	billable := u.BillableInputTokens() + u.OutputTokens
	if billable >= u.TotalRawTokens() {
		t.Errorf("a cache hit did not reduce the billable total: billable=%d raw=%d",
			billable, u.TotalRawTokens())
	}
}
