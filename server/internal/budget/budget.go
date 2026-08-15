// Package budget caps total model spend for the whole service.
//
// This is not a per-user quota — it is the ceiling on what the deployment can
// cost in total, because the application is a public link funded by one
// person's API key. Rate limiting bounds how fast tokens can be spent; this
// bounds how many there are.
//
// The counter is authoritative in Postgres rather than in memory, so a restart
// (or a crash loop, which is when you least want the meter reset) cannot hand
// out a fresh budget. A cached total keeps the hot path to one in-memory read.
package budget

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// ErrExhausted is returned by Allow when the cap has been reached.
var ErrExhausted = errors.New("token budget exhausted")

// Tracker is the seam the orchestrator checks before each model call.
type Tracker interface {
	// Allow reports whether another model call may start. It returns
	// ErrExhausted when the budget is spent.
	Allow(ctx context.Context) error
	// Record adds the usage of a completed call.
	Record(ctx context.Context, inputTokens, outputTokens int64)
	// Used returns tokens consumed so far and the configured cap.
	Used() (used, limit int64)
}

// Unlimited is the no-op tracker, used when no cap is configured.
type Unlimited struct{}

func (Unlimited) Allow(context.Context) error          { return nil }
func (Unlimited) Record(context.Context, int64, int64) {}
func (Unlimited) Used() (int64, int64)                 { return 0, 0 }

// Store persists cumulative usage. Implemented by the Postgres store; nil for
// in-memory deployments.
type Store interface {
	// TotalTokens returns everything recorded so far.
	TotalTokens(ctx context.Context) (int64, error)
	// RecordTokens appends usage for one model call.
	RecordTokens(ctx context.Context, inputTokens, outputTokens int64) error
}

// Counter caps spend at a fixed number of tokens.
//
// A call is admitted whenever the running total is under the cap — the check
// cannot know a call's cost before making it, so the final total can overshoot
// by at most one call's usage. That is the honest bound; treat the cap as
// "stop starting new work", not an exact stop.
type Counter struct {
	limit int64
	store Store
	log   *slog.Logger

	mu   sync.Mutex
	used int64
}

// New builds a Counter. A limit of zero or less returns Unlimited, so an unset
// configuration is explicit rather than a silent cap of nothing.
//
// When a store is supplied, the persisted total is loaded now: a restart
// resumes the meter where it stopped. A failed load is fatal by design — the
// alternative is booting with a zeroed budget, which is exactly the failure
// this package exists to prevent.
func New(ctx context.Context, limit int64, store Store, log *slog.Logger) (Tracker, error) {
	if limit <= 0 {
		return Unlimited{}, nil
	}
	if log == nil {
		log = slog.Default()
	}

	c := &Counter{limit: limit, store: store, log: log}
	if store != nil {
		used, err := store.TotalTokens(ctx)
		if err != nil {
			return nil, fmt.Errorf("load token usage: %w", err)
		}
		c.used = used
	}
	return c, nil
}

func (c *Counter) Allow(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used >= c.limit {
		return fmt.Errorf("%w: %d of %d tokens used", ErrExhausted, c.used, c.limit)
	}
	return nil
}

// Record updates the in-memory total first so a concurrent Allow sees the spend
// immediately, then persists. A failed write is logged rather than returned:
// the turn already happened, and losing the record must not also fail the
// user's response. The in-memory total still counts it for this process.
func (c *Counter) Record(ctx context.Context, inputTokens, outputTokens int64) {
	total := inputTokens + outputTokens
	if total <= 0 {
		return
	}

	c.mu.Lock()
	c.used += total
	used := c.used
	c.mu.Unlock()

	if c.store != nil {
		if err := c.store.RecordTokens(ctx, inputTokens, outputTokens); err != nil {
			c.log.Warn("token usage not persisted — the budget will under-count after a restart",
				"error", err, "input_tokens", inputTokens, "output_tokens", outputTokens)
		}
	}

	if used >= c.limit {
		c.log.Warn("token budget exhausted — no further model calls will be started",
			"used", used, "limit", c.limit)
	}
}

func (c *Counter) Used() (int64, int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.used, c.limit
}
