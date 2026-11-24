// Package video serves a read-only portfolio of the films in an object store
// bucket. The bucket is the source of truth; video bytes never pass through
// this process.
package video

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	refreshTimeout = 15 * time.Second
	// failureTTL is how long a failed refresh is remembered, so an outage does
	// not cause a listing per request.
	failureTTL = 10 * time.Second
	maxHeads   = 8
)

// Options configures a Service.
type Options struct {
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string // with a trailing slash
	AccessKey string
	SecretKey string
	CacheTTL  time.Duration
	Catalog   Catalog
	Logger    *slog.Logger
	Now       func() time.Time // nil means time.Now
}

// Video is one film in the portfolio.
type Video struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	URL          string    `json:"url"`
	SizeBytes    int64     `json:"sizeBytes"`
	LastModified time.Time `json:"lastModified"`

	key string
}

// Service lists the portfolio and caches the result in memory.
type Service struct {
	store    objectStore // nil when misconfigured
	prefix   string
	cacheTTL time.Duration
	catalog  Catalog
	logger   *slog.Logger
	now      func() time.Time

	mu      sync.Mutex
	videos  []Video
	err     error     // outcome of the last refresh
	expires time.Time // when that outcome stops being served
	pending *refresh  // non-nil while a refresh runs

	skippedMu sync.Mutex
	skipped   map[string]struct{} // keys already logged as skipped
}

// refresh is one listing shared by every caller that arrives while it runs.
// The results are written before done is closed.
type refresh struct {
	done   chan struct{}
	videos []Video
	err    error
}

var errNotConfigured = errors.New("video store is not configured")

// New never fails and never contacts the store. A misconfigured service logs a
// warning and answers every List with an error.
func New(opts Options) *Service {
	s := &Service{
		prefix:   opts.Prefix,
		cacheTTL: opts.CacheTTL,
		catalog:  opts.Catalog,
		logger:   opts.Logger,
		now:      opts.Now,
		skipped:  map[string]struct{}{},
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	st, err := newS3Store(opts)
	if err != nil {
		s.logger.Warn("video portfolio disabled — /api/videos will return 503", "reason", err)
		return s
	}
	s.store = st
	return s
}

// List returns the portfolio, newest first, from the cache when it is fresh.
func (s *Service) List(ctx context.Context) ([]Video, error) {
	if s.store == nil {
		return nil, errNotConfigured
	}

	s.mu.Lock()
	if s.now().Before(s.expires) {
		defer s.mu.Unlock()
		return slices.Clone(s.videos), s.err
	}
	r := s.pending
	if r == nil {
		r = &refresh{done: make(chan struct{})}
		s.pending = r
		// Not the caller's context: a visitor who disconnects must not fail the
		// refresh for everyone else waiting on it.
		go s.run(r)
	}
	s.mu.Unlock()

	select {
	case <-r.done:
		return slices.Clone(r.videos), r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) run(r *refresh) {
	ctx, cancel := context.WithTimeout(context.Background(), refreshTimeout)
	defer cancel()
	r.videos, r.err = s.build(ctx)

	ttl := s.cacheTTL
	if r.err != nil {
		ttl = failureTTL
		s.logger.Error("video listing failed", "error", r.err)
	}

	s.mu.Lock()
	s.videos, s.err = r.videos, r.err
	s.expires = s.now().Add(ttl)
	s.pending = nil
	s.mu.Unlock()
	close(r.done)
}

// build lists the bucket. It returns everything or an error, never a partial
// portfolio.
func (s *Service) build(ctx context.Context) ([]Video, error) {
	objects, err := s.store.list(ctx, s.prefix)
	if err != nil {
		return nil, err
	}
	candidates := slices.DeleteFunc(objects, func(o object) bool {
		id, file, ok := s.splitKey(o.key)
		return !ok || !hasVideoExtension(file) || s.catalog[id].Hidden
	})
	real, err := s.filterByType(ctx, candidates)
	if err != nil {
		return nil, err
	}

	best := map[string]object{}
	for _, o := range real {
		id, _, _ := s.splitKey(o.key)
		if cur, ok := best[id]; !ok || preferred(o, cur) {
			best[id] = o
		}
	}

	videos := make([]Video, 0, len(best))
	for id, o := range best {
		title, description := s.catalog.describe(id, o.lastModified)
		videos = append(videos, Video{
			ID:           id,
			Title:        title,
			Description:  description,
			URL:          s.store.publicURL(o.key),
			SizeBytes:    o.size,
			LastModified: o.lastModified.UTC(),
			key:          o.key,
		})
	}
	slices.SortFunc(videos, func(a, b Video) int {
		return cmp.Or(b.LastModified.Compare(a.LastModified), cmp.Compare(a.ID, b.ID))
	})
	return videos, nil
}

// filterByType keeps the objects whose stored Content-Type is video/*.
func (s *Service) filterByType(ctx context.Context, objects []object) ([]object, error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	keep := make([]bool, len(objects))
	next := make(chan int)
	var wg sync.WaitGroup
	for range min(maxHeads, len(objects)) {
		wg.Go(func() {
			for i := range next {
				ok, err := s.isVideo(ctx, objects[i].key)
				if err != nil {
					cancel(err) // the first cause wins
					return
				}
				keep[i] = ok
			}
		})
	}
feed:
	for i := range objects {
		select {
		case next <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(next)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}

	var out []object
	for i, o := range objects {
		if keep[i] {
			out = append(out, o)
		}
	}
	return out, nil
}

func (s *Service) isVideo(ctx context.Context, key string) (bool, error) {
	contentType, err := s.store.stat(ctx, key)
	switch {
	case errors.Is(err, errObjectUnreadable):
		s.logSkipped(key, err.Error())
		return false, nil
	case err != nil:
		return false, err
	case !strings.HasPrefix(strings.ToLower(contentType), "video/"):
		s.logSkipped(key, "content type is "+contentType)
		return false, nil
	}
	return true, nil
}

// logSkipped reports a rejected object once per process, not once per refresh.
func (s *Service) logSkipped(key, reason string) {
	s.skippedMu.Lock()
	_, seen := s.skipped[key]
	s.skipped[key] = struct{}{}
	s.skippedMu.Unlock()
	if !seen {
		s.logger.Warn("skipped object in the video bucket", "key", key, "reason", reason)
	}
}

// splitKey accepts only <prefix><uuid>/<file>.
func (s *Service) splitKey(key string) (id, file string, ok bool) {
	rest, ok := strings.CutPrefix(key, s.prefix)
	if !ok {
		return "", "", false
	}
	id, file, ok = strings.Cut(rest, "/")
	if !ok || !validID(id) || strings.Contains(file, "/") {
		return "", "", false
	}
	return id, file, true
}

// validID accepts only the canonical 36-character form; the length check rules
// out the URN, braced and undashed forms uuid.Validate also allows.
func validID(id string) bool {
	return len(id) == 36 && uuid.Validate(id) == nil
}

func hasVideoExtension(file string) bool {
	switch strings.ToLower(path.Ext(file)) {
	case ".mp4", ".webm", ".mov":
		return true
	}
	return false
}

// preferred reports whether a should represent a job folder instead of b.
func preferred(a, b object) bool {
	return cmp.Or(
		cmp.Compare(rank(a.key), rank(b.key)),
		b.lastModified.Compare(a.lastModified),
		cmp.Compare(a.key, b.key),
	) < 0
}

func rank(key string) int {
	switch path.Base(key) {
	case "cinematic-graded.mp4":
		return 0
	case "cinematic.mp4":
		return 1
	}
	return 2
}
