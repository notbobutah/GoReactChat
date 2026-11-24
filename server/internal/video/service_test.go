package video

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"
)

// AC1: continuation tokens are followed, under the prefix, newest first.
func TestListFollowsPagination(t *testing.T) {
	stub := newS3Stub(t,
		mp4("videos/"+idA+"/cinematic.mp4", stubBase.Add(1*time.Hour)),
		mp4("videos/"+idB+"/cinematic.mp4", stubBase.Add(5*time.Hour)),
		mp4("videos/"+idC+"/cinematic.mp4", stubBase.Add(3*time.Hour)),
		mp4("videos/"+idD+"/cinematic.mp4", stubBase.Add(4*time.Hour)),
		mp4("videos/"+idE+"/cinematic.mp4", stubBase.Add(2*time.Hour)),
		mp4("images/"+idUnknown+"/cinematic.mp4", stubBase.Add(9*time.Hour)),
	)
	stub.pageSize = 2
	e := newTestEnvWith(t, stub, nil)

	got := videoIDs(e.list(t))
	if want := []string{idB, idD, idC, idE, idA}; !slices.Equal(got, want) {
		t.Errorf("ids = %v, want newest first %v", got, want)
	}
	want := []stubListCall{
		{"2", "videos/", ""},
		{"2", "videos/", "page-token-2"},
		{"2", "videos/", "page-token-4"},
	}
	if calls := stub.listCalls(); !slices.Equal(calls, want) {
		t.Errorf("list calls = %+v, want %+v", calls, want)
	}
}

func TestListHonoursConfiguredPrefix(t *testing.T) {
	stub := newS3Stub(t,
		mp4("films/"+idA+"/cinematic.mp4", stubBase),
		mp4("videos/"+idB+"/cinematic.mp4", stubBase),
	)
	e := newTestEnvWith(t, stub, func(o *Options) { o.Prefix = "films/" })
	if got := videoIDs(e.list(t)); !slices.Equal(got, []string{idA}) {
		t.Errorf("ids = %v, want only the configured prefix", got)
	}
	if calls := stub.listCalls(); len(calls) != 1 || calls[0].prefix != "films/" {
		t.Errorf("list calls = %+v", calls)
	}
}

// AC1: every field, including the escaped public URL.
func TestListFields(t *testing.T) {
	key := "videos/" + idA + "/final cut.webm"
	e := newTestEnv(t, Catalog{idA: {Title: "T", Description: "D"}},
		stubObject{key: key, contentType: "video/webm", size: 123456789, modified: stubBase})
	videos := e.list(t)
	if len(videos) != 1 {
		t.Fatalf("videos = %+v", videos)
	}
	v := videos[0]
	if v.ID != idA || v.Title != "T" || v.Description != "D" || v.SizeBytes != 123456789 {
		t.Errorf("video = %+v", v)
	}
	if !v.LastModified.Equal(stubBase) {
		t.Errorf("lastModified = %v, want %v", v.LastModified, stubBase)
	}
	if want := e.stub.publicURL("videos/" + idA + "/final%20cut.webm"); v.URL != want {
		t.Errorf("url = %q, want %q", v.URL, want)
	}
}

func TestListTieBreaksByID(t *testing.T) {
	e := newTestEnv(t, nil,
		mp4("videos/"+idC+"/cinematic.mp4", stubBase),
		mp4("videos/"+idA+"/cinematic.mp4", stubBase),
		mp4("videos/"+idB+"/cinematic.mp4", stubBase),
	)
	if got, want := videoIDs(e.list(t)), []string{idA, idB, idC}; !slices.Equal(got, want) {
		t.Errorf("ids = %v, want %v", got, want)
	}
}

// AC2: accepted extensions, in any case, need a stored video/* type.
func TestListTypeFilter(t *testing.T) {
	obj := func(id, file, contentType string) stubObject {
		return stubObject{key: "videos/" + id + "/" + file, contentType: contentType, size: 1, modified: stubBase}
	}
	tests := []struct {
		name string
		obj  stubObject
		want bool
	}{
		{"mp4", obj(idA, "a.mp4", "video/mp4"), true},
		{"webm", obj(idA, "a.webm", "video/webm"), true},
		{"mov", obj(idA, "a.mov", "video/quicktime"), true},
		{"upper-case extension", obj(idA, "A.MP4", "video/mp4"), true},
		{"type with parameters", obj(idA, "a.mp4", `video/mp4; codecs="avc1"`), true},
		{"PNG named mp4", obj(idA, "product-demo.mp4", "image/png"), false},
		{"octet-stream", obj(idA, "a.mp4", "application/octet-stream"), false},
		{"video type but image extension", obj(idA, "a.png", "video/mp4"), false},
		{"other video extension", obj(idA, "a.mkv", "video/x-matroska"), false},
		{"extension not at the end", obj(idA, "a.mp4.json", "video/mp4"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t, nil, tt.obj)
			if got := len(e.list(t)) == 1; got != tt.want {
				t.Errorf("listed = %v, want %v", got, tt.want)
			}
		})
	}
}

// AC2: failing objects are skipped, and logged once however often we refresh.
func TestListSkipsAndLogsBadObjectsOnce(t *testing.T) {
	png := "videos/" + idB + "/product-demo.mp4"
	gone := "videos/" + idC + "/cinematic.mp4"
	denied := "videos/" + idD + "/cinematic.mp4"
	e := newTestEnv(t, nil,
		mp4("videos/"+idA+"/cinematic.mp4", stubBase),
		stubObject{key: png, contentType: "image/png", size: 1, modified: stubBase},
		stubObject{key: gone, modified: stubBase, headStatus: http.StatusNotFound},
		stubObject{key: denied, modified: stubBase, headStatus: http.StatusForbidden},
	)
	for i := range 3 {
		if got := videoIDs(e.list(t)); !slices.Equal(got, []string{idA}) {
			t.Fatalf("refresh %d: ids = %v, want only %s", i, got, idA)
		}
		e.clock.Advance(testTTL + time.Second)
	}
	if n := len(e.stub.listCalls()); n != 3 {
		t.Fatalf("lists = %d, want 3 refreshes", n)
	}
	for _, key := range []string{png, gone, denied} {
		if n := e.logs.linesContaining(key); n != 1 {
			t.Errorf("%s logged %d times, want once", key, n)
		}
	}
}

// Only <prefix><uuid>/<file> counts; anything else is not even a HEAD candidate.
func TestListIgnoresKeysOutsideTheLayout(t *testing.T) {
	ignored := []string{
		"videos/loose.mp4",
		"videos/not-a-uuid/cinematic.mp4",
		"videos/" + idB + "/nested/cinematic.mp4",
		"videos/" + idC + "x/cinematic.mp4",
		"videos/{" + idD + "}/cinematic.mp4",
		"videos/" + idA + "/poster.jpg",
	}
	objects := []stubObject{mp4("videos/"+idA+"/cinematic.mp4", stubBase)}
	for _, k := range ignored {
		objects = append(objects, mp4(k, stubBase.Add(time.Hour)))
	}
	e := newTestEnv(t, nil, objects...)
	if got := videoIDs(e.list(t)); !slices.Equal(got, []string{idA}) {
		t.Errorf("ids = %v, want only %s", got, idA)
	}
	for _, k := range ignored {
		if n := e.stub.headCount(k); n != 0 {
			t.Errorf("%s got %d HEAD requests, want none", k, n)
		}
	}
}

// AC2: one video per folder, preferred after the type filter.
func TestListFolderPreference(t *testing.T) {
	f := func(file string, age time.Duration) stubObject {
		return mp4("videos/"+idA+"/"+file, stubBase.Add(-age))
	}
	png := func(file string, age time.Duration) stubObject {
		o := f(file, age)
		o.contentType = "image/png"
		return o
	}
	tests := []struct {
		name    string
		objects []stubObject
		want    string
	}{
		{"graded beats cinematic and newer files",
			[]stubObject{f("cinematic-graded.mp4", 3*time.Hour), f("cinematic.mp4", 2*time.Hour), f("zz.mp4", 0)},
			"cinematic-graded.mp4"},
		{"cinematic beats newer files",
			[]stubObject{f("cinematic.mp4", 2*time.Hour), f("aa.mp4", 0), f("zz.mov", time.Hour)},
			"cinematic.mp4"},
		{"otherwise the newest",
			[]stubObject{f("aa.mp4", 2*time.Hour), f("mm.mp4", 0), f("zz.mp4", time.Hour)},
			"mm.mp4"},
		{"bad graded falls back to cinematic",
			[]stubObject{png("cinematic-graded.mp4", 0), f("cinematic.mp4", 2*time.Hour), f("zz.mp4", time.Hour)},
			"cinematic.mp4"},
		{"bad newest falls back to the newest real video",
			[]stubObject{png("zz.mp4", 0), f("aa.mp4", 2*time.Hour), f("mm.mp4", time.Hour)},
			"mm.mp4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t, nil, tt.objects...)
			videos := e.list(t)
			if len(videos) != 1 {
				t.Fatalf("videos = %+v, want one per folder", videos)
			}
			if want := e.stub.publicURL("videos/" + idA + "/" + tt.want); videos[0].URL != want {
				t.Errorf("url = %q, want %q", videos[0].URL, want)
			}
		})
	}
}

func TestListBoundsHeadConcurrency(t *testing.T) {
	stub := newS3Stub(t)
	for i := range 40 {
		stub.put(mp4(fmt.Sprintf("videos/%08d-1111-4111-8111-111111111111/cinematic.mp4", i), stubBase))
	}
	stub.headDelay = 10 * time.Millisecond
	e := newTestEnvWith(t, stub, nil)
	if n := len(e.list(t)); n != 40 {
		t.Errorf("videos = %d, want 40", n)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.maxFlight > 8 {
		t.Errorf("concurrent HEADs = %d, want at most 8", stub.maxFlight)
	}
}

// A refresh that cannot see everything fails; it never returns a partial list.
func TestListFailsRatherThanReturningPartialResults(t *testing.T) {
	t.Run("second page fails", func(t *testing.T) {
		stub := newS3Stub(t,
			mp4("videos/"+idA+"/cinematic.mp4", stubBase),
			mp4("videos/"+idB+"/cinematic.mp4", stubBase),
		)
		stub.pageSize = 1
		stub.failLists(http.StatusInternalServerError, 2)
		e := newTestEnvWith(t, stub, nil)
		if videos, err := e.svc.List(t.Context()); err == nil {
			t.Errorf("List = %v, want an error", videoIDs(videos))
		}
	})
	t.Run("HEAD server error", func(t *testing.T) {
		e := newTestEnv(t, nil,
			mp4("videos/"+idA+"/cinematic.mp4", stubBase),
			stubObject{key: "videos/" + idB + "/cinematic.mp4", modified: stubBase, headStatus: http.StatusInternalServerError},
		)
		if videos, err := e.svc.List(t.Context()); err == nil {
			t.Errorf("List = %v, want an error", videoIDs(videos))
		}
	})
}

// AC5: nothing reaches the store within the TTL; expiry lists again.
func TestListCache(t *testing.T) {
	e := newTestEnv(t, nil, mp4("videos/"+idA+"/cinematic.mp4", stubBase))
	if n := e.stub.requests(); n != 0 {
		t.Fatalf("New made %d store requests, want none", n)
	}
	e.list(t)
	after := e.stub.requests()

	e.stub.put(mp4("videos/"+idB+"/cinematic.mp4", stubBase.Add(time.Hour)))
	e.clock.Advance(testTTL - time.Second)
	if got := videoIDs(e.list(t)); !slices.Equal(got, []string{idA}) {
		t.Errorf("within TTL ids = %v, want the cached list", got)
	}
	if n := e.stub.requests(); n != after {
		t.Errorf("store requests within TTL: %d more, want none", n-after)
	}

	e.clock.Advance(2 * time.Second)
	if got := videoIDs(e.list(t)); !slices.Equal(got, []string{idB, idA}) {
		t.Errorf("after expiry ids = %v, want a fresh list", got)
	}
	if n := len(e.stub.listCalls()); n != 2 {
		t.Errorf("lists = %d, want 2", n)
	}
}

// AC5: an empty bucket is a valid, cacheable result.
func TestListCachesEmptyResult(t *testing.T) {
	e := newTestEnv(t, nil)
	for range 3 {
		if n := len(e.list(t)); n != 0 {
			t.Fatalf("videos = %d, want 0", n)
		}
	}
	if n := len(e.stub.listCalls()); n != 1 {
		t.Errorf("lists = %d, want 1", n)
	}
}

// AC5: after expiry a failure is an error, never stale data; it is remembered
// briefly so an outage does not cause a listing per request.
func TestListFailureAfterExpiry(t *testing.T) {
	e := newTestEnv(t, nil, mp4("videos/"+idA+"/cinematic.mp4", stubBase))
	e.list(t)

	e.stub.failLists(http.StatusInternalServerError, 0)
	e.clock.Advance(testTTL + time.Second)
	if videos, err := e.svc.List(t.Context()); err == nil {
		t.Fatalf("List = %v, want an error instead of stale data", videoIDs(videos))
	}

	after := e.stub.requests()
	e.clock.Advance(9 * time.Second)
	if _, err := e.svc.List(t.Context()); err == nil {
		t.Error("List within the failure window succeeded, want an error")
	}
	if n := e.stub.requests(); n != after {
		t.Errorf("failure window made %d store requests, want none", n-after)
	}

	e.stub.failLists(0, 0)
	e.clock.Advance(2 * time.Second)
	if got := videoIDs(e.list(t)); !slices.Equal(got, []string{idA}) {
		t.Errorf("after recovery ids = %v", got)
	}
}

// AC5: concurrent misses share one refresh.
func TestListConcurrentMissesShareOneRefresh(t *testing.T) {
	e := newTestEnv(t, nil, mp4("videos/"+idA+"/cinematic.mp4", stubBase))
	started, release := e.stub.gateLists(t)

	const callers = 10
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			videos, err := e.svc.List(t.Context())
			if err == nil && !slices.Equal(videoIDs(videos), []string{idA}) {
				err = fmt.Errorf("ids = %v", videoIDs(videos))
			}
			errs <- err
		})
	}
	waitStarted(t, started)
	time.Sleep(100 * time.Millisecond) // let the other callers reach the cache
	release()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("caller: %v", err)
		}
	}
	if n := len(e.stub.listCalls()); n != 1 {
		t.Errorf("lists = %d, want 1 shared refresh", n)
	}
}

// AC5: a visitor who disconnects cannot fail the refresh for the others.
func TestListCallerCancellationDoesNotFailOthers(t *testing.T) {
	e := newTestEnv(t, nil, mp4("videos/"+idA+"/cinematic.mp4", stubBase))
	started, release := e.stub.gateLists(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var wg sync.WaitGroup
	wg.Go(func() { _, _ = e.svc.List(ctx) })
	waitStarted(t, started)

	var videos []Video
	var err error
	wg.Go(func() { videos, err = e.svc.List(t.Context()) })
	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(50 * time.Millisecond)
	release()
	wg.Wait()

	if err != nil {
		t.Fatalf("second caller: %v", err)
	}
	if !slices.Equal(videoIDs(videos), []string{idA}) {
		t.Errorf("ids = %v", videoIDs(videos))
	}
	if n := len(e.stub.listCalls()); n != 1 {
		t.Errorf("lists = %d, want the one refresh to survive", n)
	}
}

func waitStarted(t *testing.T, started <-chan struct{}) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("no list request reached the store")
	}
}
