package video

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// catalogJSON is curated by the owner; the code only ever reads it.
//
//go:embed catalog.json
var catalogJSON []byte

// Entry is the curated text for one video.
type Entry struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Hidden      bool   `json:"hidden"`
}

// Catalog maps a video id to its curated entry.
type Catalog map[string]Entry

// ParseCatalog decodes a catalog document.
func ParseCatalog(data []byte) (Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse video catalog: %w", err)
	}
	return c, nil
}

// EmbeddedCatalog returns the catalog compiled into the binary.
func EmbeddedCatalog() (Catalog, error) { return ParseCatalog(catalogJSON) }

// describe returns the title and description for a video. Without a curated
// title it falls back to a dated one.
func (c Catalog) describe(id string, modified time.Time) (title, description string) {
	e := c[id]
	title = strings.TrimSpace(e.Title)
	if title == "" {
		title = "Expona film · " + modified.UTC().Format("2 Jan 2006")
	}
	return title, e.Description
}
