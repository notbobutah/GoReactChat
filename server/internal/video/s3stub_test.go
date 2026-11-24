package video

// Shared fixtures for the package tests: an httptest S3 stub, a fake clock and a
// log capture. Nothing here ever talks to the real bucket.
//
// The tests drive the package through this small API:
//
//	type Entry struct { Title, Description string; Hidden bool }
//	type Catalog map[string]Entry            // keyed by video id
//	func ParseCatalog(data []byte) (Catalog, error)
//	func EmbeddedCatalog() (Catalog, error)  // the go:embed'ed catalog.json
//
//	type Options struct {
//		Endpoint, Region, Bucket, Prefix, AccessKey, SecretKey string
//		CacheTTL time.Duration
//		Catalog  Catalog
//		Logger   *slog.Logger
//		Now      func() time.Time // nil means time.Now
//	}
//	// New never fails and never contacts the store. With an unset bucket,
//	// missing keys or an invalid endpoint it logs a warning and the service
//	// answers every List with an error (503 over HTTP).
//	func New(opts Options) *Service
//	func (s *Service) List(ctx context.Context) ([]Video, error)
//	func (s *Service) Register(mux *http.ServeMux)
//
//	type Video struct {
//		ID, Title, Description, URL string
//		SizeBytes    int64
//		LastModified time.Time
//		// unexported fields are fine
//	}

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	stubBucket    = "expona-images"
	stubRegion    = "us-lax-1"
	stubAccessKey = "AKIDSTUBACCESSKEY"
	stubSecretKey = "stub-secret-key-never-log-me"

	idA = "aaaaaaaa-1111-4111-8111-111111111111"
	idB = "bbbbbbbb-2222-4222-8222-222222222222"
	idC = "cccccccc-3333-4333-8333-333333333333"
	idD = "dddddddd-4444-4444-8444-444444444444"
	idE = "eeeeeeee-5555-4555-8555-555555555555"
	// idUnknown is a valid UUID that no fixture puts in the bucket.
	idUnknown = "ffffffff-6666-4666-8666-666666666666"
)

var stubBase = time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)

type stubObject struct {
	key         string
	contentType string
	size        int64
	modified    time.Time
	headStatus  int // 0 means 200
}

func mp4(key string, modified time.Time) stubObject {
	return stubObject{key: key, contentType: "video/mp4", size: 1000, modified: modified}
}

type stubListCall struct {
	listType, prefix, token string
}

// s3Stub implements the read-only slice of S3 the package may use: paged
// ListObjectsV2 and HEAD. Every other request is recorded as a violation and
// fails the test when it ends.
type s3Stub struct {
	srv *httptest.Server

	mu         sync.Mutex
	objects    map[string]stubObject
	pageSize   int
	listStatus int // non-zero fails list requests
	failPage   int // first page (1-based) listStatus applies to; 0 means all
	headDelay  time.Duration
	lists      []stubListCall
	heads      map[string]int
	inFlight   int
	maxFlight  int
	violations []string

	listStarted chan struct{} // receives one value per list request when gated
	listGate    chan struct{} // when set, list requests wait until it is closed
}

func newS3Stub(t *testing.T, objects ...stubObject) *s3Stub {
	t.Helper()
	s := &s3Stub{objects: map[string]stubObject{}, heads: map[string]int{}, pageSize: 1000}
	s.put(objects...)
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(func() {
		s.srv.Close()
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, v := range s.violations {
			t.Errorf("request outside the read-only contract: %s", v)
		}
	})
	return s
}

func (s *s3Stub) put(objects ...stubObject) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range objects {
		s.objects[o.key] = o
	}
}

func (s *s3Stub) failLists(status, fromPage int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listStatus, s.failPage = status, fromPage
}

// gateLists makes list requests block until release is called.
func (s *s3Stub) gateLists(t *testing.T) (started <-chan struct{}, release func()) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listStarted = make(chan struct{}, 100)
	gate := make(chan struct{})
	s.listGate = gate
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release) // runs before the server's Close
	return s.listStarted, release
}

func (s *s3Stub) listCalls() []stubListCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]stubListCall(nil), s.lists...)
}

func (s *s3Stub) headCount(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.heads[key]
}

// requests is the total number of store requests seen so far.
func (s *s3Stub) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.lists) + len(s.violations)
	for _, c := range s.heads {
		n += c
	}
	return n
}

func (s *s3Stub) violate(r *http.Request, why string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.violations = append(s.violations, fmt.Sprintf("%s %s (%s)", r.Method, r.URL.Path, why))
}

func (s *s3Stub) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(r.Header.Get("Authorization"), "/"+stubRegion+"/s3/aws4_request") {
		s.violate(r, "not signed for the configured region")
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/"+stubBucket)
	key := strings.TrimPrefix(rest, "/")
	switch {
	case !ok:
		s.violate(r, "wrong bucket or not path style")
		w.WriteHeader(http.StatusBadRequest)
	case r.Method == http.MethodGet && key == "" && r.URL.Query().Get("list-type") == "2":
		s.serveList(w, r)
	case r.Method == http.MethodHead && key != "":
		s.serveHead(w, key)
	default:
		s.violate(r, "only ListObjectsV2 and HEAD object are allowed")
		w.WriteHeader(http.StatusBadRequest)
	}
}

type stubListContent struct {
	Key          string
	LastModified string
	ETag         string
	Size         int64
	StorageClass string
}

type stubListResult struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	Name                  string
	Prefix                string
	KeyCount              int
	MaxKeys               int
	IsTruncated           bool
	NextContinuationToken string `xml:",omitempty"`
	Contents              []stubListContent
}

func (s *s3Stub) serveList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	prefix, token := q.Get("prefix"), q.Get("continuation-token")

	s.mu.Lock()
	s.lists = append(s.lists, stubListCall{q.Get("list-type"), prefix, token})
	started, gate := s.listStarted, s.listGate
	s.mu.Unlock()
	if gate != nil {
		started <- struct{}{}
		<-gate
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	start := 0
	if token != "" {
		n, err := strconv.Atoi(strings.TrimPrefix(token, "page-token-"))
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		start = n
	}
	page := start/s.pageSize + 1
	if s.listStatus != 0 && page >= s.failPage {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(s.listStatus)
		_, _ = w.Write([]byte(`<Error><Code>InternalError</Code><Message>stub failure</Message></Error>`))
		return
	}

	var keys []string
	for k := range s.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	res := stubListResult{Name: stubBucket, Prefix: prefix, MaxKeys: s.pageSize}
	end := min(start+s.pageSize, len(keys))
	for _, k := range keys[min(start, end):end] {
		o := s.objects[k]
		res.Contents = append(res.Contents, stubListContent{
			Key: k, Size: o.size, ETag: `"etag"`, StorageClass: "STANDARD",
			LastModified: o.modified.UTC().Format("2006-01-02T15:04:05.000Z"),
		})
	}
	res.KeyCount = len(res.Contents)
	if end < len(keys) {
		res.IsTruncated = true
		res.NextContinuationToken = fmt.Sprintf("page-token-%d", end)
	}
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(res)
}

func (s *s3Stub) serveHead(w http.ResponseWriter, key string) {
	s.mu.Lock()
	s.heads[key]++
	s.inFlight++
	s.maxFlight = max(s.maxFlight, s.inFlight)
	o, ok := s.objects[key]
	delay := s.headDelay
	s.mu.Unlock()
	time.Sleep(delay)
	defer func() {
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
	}()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if o.headStatus != 0 {
		w.WriteHeader(o.headStatus)
		return
	}
	h := w.Header()
	h.Set("Content-Type", o.contentType)
	h.Set("Content-Length", strconv.FormatInt(o.size, 10))
	h.Set("Last-Modified", o.modified.UTC().Format(http.TimeFormat))
	h.Set("ETag", `"etag"`)
	w.WriteHeader(http.StatusOK)
}

// fakeClock is an injectable, goroutine-safe clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// logCapture collects slog output at every level.
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logCapture) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logCapture) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func (l *logCapture) linesContaining(sub string) int {
	n := 0
	for _, line := range strings.Split(l.String(), "\n") {
		if strings.Contains(line, sub) {
			n++
		}
	}
	return n
}

// testEnv is one service wired to a stub.
type testEnv struct {
	svc   *Service
	stub  *s3Stub
	clock *fakeClock
	logs  *logCapture
}

const testTTL = 5 * time.Minute

func newTestEnv(t *testing.T, catalog Catalog, objects ...stubObject) *testEnv {
	t.Helper()
	stub := newS3Stub(t, objects...)
	return newTestEnvWith(t, stub, func(o *Options) { o.Catalog = catalog })
}

func newTestEnvWith(t *testing.T, stub *s3Stub, edit func(*Options)) *testEnv {
	t.Helper()
	e := &testEnv{
		stub:  stub,
		clock: &fakeClock{now: stubBase.Add(24 * time.Hour)},
		logs:  &logCapture{},
	}
	opts := Options{
		Endpoint:  stub.srv.URL,
		Region:    stubRegion,
		Bucket:    stubBucket,
		Prefix:    "videos/",
		AccessKey: stubAccessKey,
		SecretKey: stubSecretKey,
		CacheTTL:  testTTL,
		Catalog:   Catalog{},
		Logger:    slog.New(slog.NewJSONHandler(e.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Now:       e.clock.Now,
	}
	if edit != nil {
		edit(&opts)
	}
	e.svc = New(opts)
	if e.svc == nil {
		t.Fatal("New returned nil; it must always return a usable service")
	}
	return e
}

func (e *testEnv) list(t *testing.T) []Video {
	t.Helper()
	videos, err := e.svc.List(t.Context())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return videos
}

// publicURL is the expected virtual-hosted URL for an already escaped key.
func (s *s3Stub) publicURL(escapedKey string) string {
	return "http://" + stubBucket + "." + strings.TrimPrefix(s.srv.URL, "http://") + "/" + escapedKey
}

func videoIDs(videos []Video) []string {
	ids := make([]string, 0, len(videos))
	for _, v := range videos {
		ids = append(ids, v.ID)
	}
	return ids
}
