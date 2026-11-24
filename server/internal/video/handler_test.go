package video

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// mux mirrors main.go: the video routes next to /health and the chat service.
func (e *testEnv) mux() *http.ServeMux {
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	mux.HandleFunc("/lumi.chat.v1.ChatService/", ok)
	mux.HandleFunc("GET /health", ok)
	e.svc.Register(mux)
	return mux
}

func (e *testEnv) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func downloadPath(id string) string { return "/api/videos/" + id + "/download" }

func assertJSONError(t *testing.T, rec *httptest.ResponseRecorder, status int) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body, err)
	}
	if body.Error == "" {
		t.Errorf("body %s has no error message", rec.Body)
	}
}

func assertNoSecrets(t *testing.T, where, s string) {
	t.Helper()
	for _, secret := range []string{stubAccessKey, stubSecretKey, "X-Amz-Signature"} {
		if strings.Contains(s, secret) {
			t.Errorf("%s contains %q", where, secret)
		}
	}
}

// AC1: the JSON contract the web app reads.
func TestHandleListJSON(t *testing.T) {
	e := newTestEnv(t, Catalog{idA: {Title: "T", Description: "D"}},
		stubObject{key: "videos/" + idA + "/cinematic.mp4", contentType: "video/mp4", size: 42, modified: stubBase},
		mp4("videos/"+idB+"/cinematic.mp4", stubBase.Add(time.Hour)),
	)
	rec := e.get(t, "/api/videos")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q", ct)
	}
	var body struct {
		Videos []map[string]any `json:"videos"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Videos) != 2 || body.Videos[0]["id"] != idB {
		t.Fatalf("videos = %v, want two, newest first", body.Videos)
	}
	want := map[string]any{
		"id":           idA,
		"title":        "T",
		"description":  "D",
		"url":          e.stub.publicURL("videos/" + idA + "/cinematic.mp4"),
		"sizeBytes":    float64(42),
		"lastModified": "2026-08-22T12:00:00Z",
	}
	got := body.Videos[1]
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("fields = %v, want exactly %v", got, want)
	}
	// The description is present even when empty.
	if d, ok := body.Videos[0]["description"]; !ok || d != "" {
		t.Errorf("uncatalogued description = %v (present %v), want an empty string", d, ok)
	}
}

func TestHandleListEmptyIsAnArray(t *testing.T) {
	e := newTestEnv(t, nil)
	rec := e.get(t, "/api/videos")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"videos":[]}` {
		t.Errorf("body = %s, want an empty array", got)
	}
}

// AC5: page views and downloads are served from the cached listing.
func TestHandlersUseTheCache(t *testing.T) {
	e := newTestEnv(t, nil, mp4("videos/"+idA+"/cinematic.mp4", stubBase))
	e.get(t, "/api/videos")
	after := e.stub.requests()
	e.get(t, "/api/videos")
	if rec := e.get(t, downloadPath(idA)); rec.Code != http.StatusFound {
		t.Errorf("download status = %d", rec.Code)
	}
	if n := e.stub.requests(); n != after {
		t.Errorf("%d extra store requests, want none", n-after)
	}
}

// AC4: 302 to a 15-minute presigned GET of the listed key, as an attachment.
// The stub fails the test on any object GET, so no bytes pass through the API.
func TestHandleDownloadRedirect(t *testing.T) {
	for _, file := range []string{"cinematic-graded.mp4", "clip.webm"} {
		t.Run(file, func(t *testing.T) {
			e := newTestEnv(t, nil, stubObject{
				key: "videos/" + idA + "/" + file, contentType: "video/mp4", size: 1, modified: stubBase,
			})
			rec := e.get(t, downloadPath(idA)) // cold cache: the lookup itself lists
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302 (body %s)", rec.Code, rec.Body)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("cache-control = %q, want no-store", cc)
			}
			loc, err := url.Parse(rec.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			if got := loc.Scheme + "://" + loc.Host; got != e.stub.srv.URL {
				t.Errorf("redirects to %s, want the store %s", got, e.stub.srv.URL)
			}
			if want := "/" + stubBucket + "/videos/" + idA + "/" + file; loc.Path != want {
				t.Errorf("path = %q, want %q", loc.Path, want)
			}
			q := loc.Query()
			if got := q.Get("X-Amz-Expires"); got != "900" {
				t.Errorf("X-Amz-Expires = %q, want 900", got)
			}
			if want := `attachment; filename="expona-` + idA + `.mp4"`; q.Get("response-content-disposition") != want {
				t.Errorf("disposition = %q, want %q", q.Get("response-content-disposition"), want)
			}
			if q.Get("X-Amz-Signature") == "" {
				t.Error("redirect is not presigned")
			}
			if got := q.Get("X-Amz-Credential"); !strings.Contains(got, "/"+stubRegion+"/s3/aws4_request") {
				t.Errorf("credential scope = %q", got)
			}
			if strings.Contains(loc.String(), stubSecretKey) {
				t.Error("redirect leaks the secret key")
			}
			assertNoSecrets(t, "log", e.logs.String())
		})
	}
}

// AC4: ids outside the current listing are 404, including hidden ones.
func TestHandleDownloadNotFound(t *testing.T) {
	e := newTestEnv(t, Catalog{idB: {Title: "Hidden", Hidden: true}},
		mp4("videos/"+idA+"/cinematic.mp4", stubBase),
		mp4("videos/"+idB+"/cinematic.mp4", stubBase),
		stubObject{key: "videos/" + idC + "/product-demo.mp4", contentType: "image/png", modified: stubBase},
	)
	for name, id := range map[string]string{"unknown": idUnknown, "hidden": idB, "not a real video": idC} {
		t.Run(name, func(t *testing.T) {
			rec := e.get(t, downloadPath(id))
			assertJSONError(t, rec, http.StatusNotFound)
			if loc := rec.Header().Get("Location"); loc != "" {
				t.Errorf("location = %q, want none", loc)
			}
		})
	}
}

// AC4: a non-UUID id is 400 and never reaches the store.
func TestHandleDownloadBadID(t *testing.T) {
	e := newTestEnv(t, nil, mp4("videos/"+idA+"/cinematic.mp4", stubBase))
	for _, id := range []string{
		"not-a-uuid",
		"cinematic.mp4",
		strings.ReplaceAll(idA, "-", ""),
		"urn:uuid:" + idA,
		"%7B" + idA + "%7D",
		idA + "x",
		idA[:35],
		"g" + idA[1:],
		strings.Replace(idA, "-", "a", 1),
		"%2e%2e%2f%2e%2e%2fimages",
		idA + "%2fcinematic.mp4",
		"..%2fvideos%2f" + idA,
	} {
		t.Run(id, func(t *testing.T) {
			assertJSONError(t, e.get(t, downloadPath(id)), http.StatusBadRequest)
		})
	}
	if n := e.stub.requests(); n != 0 {
		t.Errorf("bad ids caused %d store requests, want none", n)
	}
}

// AC5: every store failure is a 503 with a generic JSON error, the keys never
// leak, and the rest of the server keeps working.
func TestStoreFailureIs503(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	down.Close()

	tests := []struct {
		name       string
		listStatus int
		edit       func(*Options)
		disabled   bool // misconfigured: warns at New and never calls the store
	}{
		{"store error", http.StatusInternalServerError, nil, false},
		{"access denied", http.StatusForbidden, nil, false},
		{"unreachable", 0, func(o *Options) { o.Endpoint = down.URL }, false},
		{"bucket unset", 0, func(o *Options) { o.Bucket = "" }, true},
		{"access key missing", 0, func(o *Options) { o.AccessKey = "" }, true},
		{"secret key missing", 0, func(o *Options) { o.SecretKey = "" }, true},
		{"endpoint missing", 0, func(o *Options) { o.Endpoint = "" }, true},
		{"endpoint invalid", 0, func(o *Options) { o.Endpoint = "://nope" }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newS3Stub(t, mp4("videos/"+idA+"/cinematic.mp4", stubBase))
			stub.failLists(tt.listStatus, 0)
			e := newTestEnvWith(t, stub, tt.edit)
			if tt.disabled && e.logs.linesContaining(`"level":"WARN"`) == 0 {
				t.Error("misconfiguration was not logged as a warning")
			}

			for _, path := range []string{"/api/videos", downloadPath(idA)} {
				rec := e.get(t, path)
				assertJSONError(t, rec, http.StatusServiceUnavailable)
				assertNoSecrets(t, path+" body", rec.Body.String())
				for _, detail := range []string{"127.0.0.1", "stub failure", "InternalError"} {
					if strings.Contains(rec.Body.String(), detail) {
						t.Errorf("%s body leaks %q: %s", path, detail, rec.Body)
					}
				}
			}
			for _, path := range []string{"/health", "/lumi.chat.v1.ChatService/Chat"} {
				if rec := e.get(t, path); rec.Code != http.StatusOK {
					t.Errorf("%s = %d, want 200", path, rec.Code)
				}
			}
			if tt.disabled && stub.requests() != 0 {
				t.Errorf("misconfigured service made %d store requests", stub.requests())
			}
			assertNoSecrets(t, "log", e.logs.String())
		})
	}
}
