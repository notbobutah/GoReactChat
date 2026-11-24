package video

import (
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

const fixtureCatalogJSON = `{
  "` + idA + `": { "title": "Fixture Film", "description": "A fixture description.", "hidden": false },
  "` + idB + `": { "title": "Hidden Film", "description": "Never shown.", "hidden": true },
  "` + idC + `": { "title": "   ", "description": "", "hidden": false }
}`

func fixtureCatalog(t *testing.T) Catalog {
	t.Helper()
	c, err := ParseCatalog([]byte(fixtureCatalogJSON))
	if err != nil {
		t.Fatalf("ParseCatalog: %v", err)
	}
	return c
}

func TestParseCatalog(t *testing.T) {
	c := fixtureCatalog(t)
	if len(c) != 3 {
		t.Fatalf("entries = %d, want 3", len(c))
	}
	want := Entry{Title: "Fixture Film", Description: "A fixture description.", Hidden: false}
	if c[idA] != want {
		t.Errorf("entry = %+v, want %+v", c[idA], want)
	}
	if !c[idB].Hidden {
		t.Error("hidden flag was not parsed")
	}
	if _, err := ParseCatalog([]byte(`{"broken`)); err == nil {
		t.Error("malformed catalog must be an error")
	}
}

// AC3: curated titles, the hidden flag and the dated default.
func TestCatalogAppliedToListing(t *testing.T) {
	e := newTestEnv(t, fixtureCatalog(t),
		mp4("videos/"+idA+"/cinematic.mp4", stubBase),
		mp4("videos/"+idB+"/cinematic.mp4", stubBase),
		mp4("videos/"+idC+"/cinematic.mp4", time.Date(2026, time.August, 5, 23, 59, 0, 0, time.UTC)),
		mp4("videos/"+idD+"/cinematic.mp4", time.Date(2026, time.August, 22, 0, 0, 1, 0, time.UTC)),
	)
	byID := map[string]Video{}
	for _, v := range e.list(t) {
		byID[v.ID] = v
	}

	if got := byID[idA]; got.Title != "Fixture Film" || got.Description != "A fixture description." {
		t.Errorf("catalogued video = %q / %q", got.Title, got.Description)
	}
	if _, ok := byID[idB]; ok {
		t.Error("hidden video is listed")
	}
	if got := byID[idC].Title; got != "Expona film · 5 Aug 2026" {
		t.Errorf("blank-title video title = %q", got)
	}
	got, ok := byID[idD]
	if !ok {
		t.Fatal("video without a catalog entry must still be listed")
	}
	if got.Title != "Expona film · 22 Aug 2026" || got.Description != "" {
		t.Errorf("uncatalogued video = %q / %q", got.Title, got.Description)
	}
	if len(byID) != 3 {
		t.Errorf("listed %d videos, want 3", len(byID))
	}
}

// AC3: without a catalog every video falls back to the default title.
func TestNilCatalogUsesDefaultTitles(t *testing.T) {
	e := newTestEnv(t, nil, mp4("videos/"+idA+"/cinematic.mp4", stubBase))
	videos := e.list(t)
	if !slices.Equal(videoIDs(videos), []string{idA}) {
		t.Fatalf("ids = %v", videoIDs(videos))
	}
	if videos[0].Title != "Expona film · 22 Aug 2026" {
		t.Errorf("title = %q", videos[0].Title)
	}
}

// AC3: the committed catalog is embedded in the binary and parses.
func TestEmbeddedCatalog(t *testing.T) {
	c, err := EmbeddedCatalog()
	if err != nil {
		t.Fatalf("EmbeddedCatalog: %v", err)
	}
	if len(c) != 27 {
		t.Errorf("entries = %d, want 27", len(c))
	}
	uuid := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	for id, entry := range c {
		if !uuid.MatchString(id) {
			t.Errorf("key %q is not a UUID", id)
		}
		if strings.TrimSpace(entry.Title) == "" {
			t.Errorf("entry %s has no title", id)
		}
	}
}
