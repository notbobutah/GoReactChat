// Package reference makes external documentation searchable alongside the
// candidate's own documents — today, the Go documentation at tip.golang.org.
//
// It is deliberately a separate corpus, a separate index, and a separate tool
// from the résumé and job description. The reason is the grounding rule the
// whole assistant rests on: the résumé is the ONLY authority on what the
// candidate has done. Go documentation is authority on what Go does. Mixing
// them into one index would let a passage about goroutines surface as though it
// were evidence of the candidate's experience with them.
package reference

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/expona-ai/lumi-go/server/internal/corpus"
)

// Source is one document to fetch and index.
type Source struct {
	// URL is fetched by `cmd/fetchrefs`.
	URL string
	// File is the cache filename under the reference directory.
	File string
	// Title is what the model sees as the source name in a citation.
	Title string
}

// DefaultSources is the curated set: the documents that answer "what does Go
// actually do here?" rather than tutorials or blog posts. The specification and
// memory model are normative; Effective Go and the FAQ carry the idiom and the
// rationale; the release notes carry what changed in the current version.
var DefaultSources = []Source{
	{URL: "https://tip.golang.org/ref/spec", File: "go-spec.txt", Title: "Go Language Specification (tip.golang.org/ref/spec)"},
	{URL: "https://tip.golang.org/doc/effective_go", File: "effective-go.txt", Title: "Effective Go (tip.golang.org/doc/effective_go)"},
	{URL: "https://tip.golang.org/ref/mem", File: "go-memory-model.txt", Title: "The Go Memory Model (tip.golang.org/ref/mem)"},
	{URL: "https://tip.golang.org/doc/faq", File: "go-faq.txt", Title: "Go FAQ (tip.golang.org/doc/faq)"},
	{URL: "https://tip.golang.org/doc/go1.26", File: "go1.26-release-notes.txt", Title: "Go 1.26 Release Notes (tip.golang.org/doc/go1.26)"},
	{URL: "https://tip.golang.org/doc/modules/developing", File: "go-modules.txt", Title: "Developing Go Modules (tip.golang.org/doc/modules/developing)"},
	// Added after a retrieval check: "error wrapping idiom" returned unrelated
	// spec sections, because that idiom lives in the errors package docs — and
	// tip.golang.org/pkg/* redirects off-site to pkg.go.dev. This tutorial is
	// the on-site coverage of it.
	{URL: "https://tip.golang.org/doc/tutorial/handle-errors", File: "go-error-handling.txt", Title: "Go: Returning and Handling Errors (tip.golang.org/doc/tutorial/handle-errors)"},
}

// DirName is the subdirectory of the data directory holding the cache. The
// corpus loader skips directories, so cached reference text never leaks into
// the candidate's document set by accident.
const DirName = "reference"

// Dir returns the cache directory for a given data directory.
func Dir(dataDir string) string { return filepath.Join(dataDir, DirName) }

// Load reads the cached reference documents. A missing directory is not an
// error: the reference tool is simply not offered, and the assistant says it
// cannot look something up rather than inventing an answer.
func Load(dataDir string) (*corpus.Corpus, error) {
	dir := Dir(dataDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &corpus.Corpus{Dir: dir}, nil
		}
		return nil, fmt.Errorf("read reference dir: %w", err)
	}

	titles := make(map[string]string, len(DefaultSources))
	for _, s := range DefaultSources {
		titles[s.File] = s.Title
	}

	c := &corpus.Corpus{Dir: dir}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		text := strings.TrimSpace(string(b))
		if text == "" {
			continue
		}

		name := titles[e.Name()]
		if name == "" {
			// A file dropped in by hand still indexes; it just cites by filename.
			name = e.Name()
		}
		c.Documents = append(c.Documents, corpus.Document{
			Kind: corpus.KindReference,
			Name: name,
			Path: path,
			Text: text,
		})
	}
	return c, nil
}

// Summary is a one-line description for the boot log.
func Summary(c *corpus.Corpus) string {
	if c == nil || len(c.Documents) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(c.Documents))
	for _, d := range c.Documents {
		parts = append(parts, fmt.Sprintf("%s (%d chars)", filepath.Base(d.Path), len(d.Text)))
	}
	return strings.Join(parts, ", ")
}
