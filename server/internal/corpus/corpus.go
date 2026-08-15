// Package corpus loads the grounding documents for a conversation — a résumé
// and the job description it is being discussed against — and composes them
// into the system prompt.
//
// The documents are small (a few thousand tokens together) and relevant to
// every turn, so they are inlined in the system prompt rather than fetched by a
// lookup tool. That costs one cached prefix instead of a tool round-trip per
// question, and it means the model can never answer a résumé question without
// the résumé in front of it.
package corpus

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Kind is how a document is used in the prompt. It is inferred from the
// filename so dropping a file into the data directory is the whole workflow.
type Kind string

const (
	KindResume         Kind = "resume"
	KindJobDescription Kind = "job_description"
	KindSupporting     Kind = "supporting"
	// KindProject is a document describing work the candidate actually
	// delivered — the codebase of this application. Unlike the résumé it is a
	// primary artifact the reader can go and check.
	KindProject Kind = "project"
	// KindReference is external documentation (the Go docs). Authority on the
	// subject matter, never evidence of the candidate's experience.
	KindReference Kind = "reference"
)

// Document is one loaded file.
type Document struct {
	Kind Kind
	// Name is the source filename, kept so the model can cite where a claim
	// came from and so the boot log says what was loaded.
	Name string
	Path string
	Text string
}

// Corpus is everything loaded from the data directory.
type Corpus struct {
	Documents []Document
	Dir       string
}

// Empty reports whether anything was loaded. An empty corpus is not an error:
// the server falls back to its generic prompt so `make dev-offline` and a
// container with no mounted data still run.
func (c *Corpus) Empty() bool { return c == nil || len(c.Documents) == 0 }

// Load reads every supported file in dir. Unsupported extensions and files
// that yield no text are skipped with a reason rather than failing the boot —
// one unreadable attachment should not take the service down.
func Load(dir string) (*Corpus, []string, error) {
	var skipped []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return &Corpus{Dir: dir}, nil, nil
		}
		return nil, nil, fmt.Errorf("read data dir: %w", err)
	}

	// When the same document exists in several formats — a résumé PDF and the
	// markdown converted from it — only the preferred one is loaded. Indexing
	// both would double every passage and let a retrieval return the same
	// content twice under two names.
	preferred := preferredFormats(entries)

	c := &Corpus{Dir: dir}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if want, ok := preferred[stem(e.Name())]; ok && want != e.Name() {
			skipped = append(skipped, fmt.Sprintf("%s (superseded by %s)", e.Name(), want))
			continue
		}
		path := filepath.Join(dir, e.Name())

		text, err := extract(path)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s (%v)", e.Name(), err))
			continue
		}
		text = normalize(text)
		if strings.TrimSpace(text) == "" {
			skipped = append(skipped, fmt.Sprintf("%s (no extractable text)", e.Name()))
			continue
		}

		c.Documents = append(c.Documents, Document{
			Kind: classify(e.Name()),
			Name: e.Name(),
			Path: path,
			Text: text,
		})
	}

	// Deterministic order: résumé, then job description, then supporting docs,
	// alphabetical within each. A stable system prompt is a cacheable one.
	sort.SliceStable(c.Documents, func(i, j int) bool {
		oi, oj := kindOrder(c.Documents[i].Kind), kindOrder(c.Documents[j].Kind)
		if oi != oj {
			return oi < oj
		}
		return c.Documents[i].Name < c.Documents[j].Name
	})

	return c, skipped, nil
}

func kindOrder(k Kind) int {
	switch k {
	case KindResume:
		return 0
	case KindJobDescription:
		return 1
	case KindProject:
		return 2
	default:
		return 3
	}
}

// classify infers a document's role from its filename. The rule is deliberately
// dumb and documented, so the outcome is predictable: anything naming a résumé
// is one, anything that looks like a posting or an email from a recruiter is a
// job description, everything else is supporting context.
func classify(name string) Kind {
	l := strings.ToLower(name)
	switch {
	case strings.Contains(l, "readme"):
		return KindProject
	case strings.Contains(l, "resume"), strings.Contains(l, "résumé"), strings.Contains(l, "cv"):
		return KindResume
	case strings.HasSuffix(l, ".eml"),
		strings.Contains(l, "job"), strings.Contains(l, "role"),
		strings.Contains(l, "position"), strings.Contains(l, "posting"),
		strings.Contains(l, "jd"):
		return KindJobDescription
	default:
		return KindSupporting
	}
}

// Documents(kind) returns the loaded documents of one kind.
func (c *Corpus) Of(kind Kind) []Document {
	var out []Document
	for _, d := range c.Documents {
		if d.Kind == kind {
			out = append(out, d)
		}
	}
	return out
}

// Summary is a one-line description for the boot log.
func (c *Corpus) Summary() string {
	if c.Empty() {
		return "no documents"
	}
	parts := make([]string, 0, len(c.Documents))
	for _, d := range c.Documents {
		parts = append(parts, fmt.Sprintf("%s=%s (%d chars)", d.Kind, d.Name, len(d.Text)))
	}
	return strings.Join(parts, ", ")
}

// formatRank orders the formats a document can arrive in, best first. Markdown
// wins because it is the curated one: `cmd/pdf2md` writes it from the PDF, and
// a human can then fix what the converter got wrong. Extracting from the PDF
// every boot would throw those corrections away.
var formatRank = map[string]int{
	".md": 0, ".markdown": 0,
	".txt": 1,
	".eml": 2,
	".pdf": 3,
}

func rankOf(name string) int {
	if r, ok := formatRank[strings.ToLower(filepath.Ext(name))]; ok {
		return r
	}
	return 99
}

// stem is a filename without its extension — the key that identifies "the same
// document in another format".
func stem(name string) string {
	return strings.TrimSuffix(name, filepath.Ext(name))
}

// preferredFormats maps each document stem to the single filename that should
// be loaded for it.
func preferredFormats(entries []os.DirEntry) map[string]string {
	best := make(map[string]string)
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		s := stem(e.Name())
		if cur, ok := best[s]; !ok || rankOf(e.Name()) < rankOf(cur) {
			best[s] = e.Name()
		}
	}
	return best
}

// LoadFile reads a single document from outside the data directory, for
// material that lives with the source rather than with the candidate's
// documents — the project README is loaded this way, so the delivered codebase
// is citable without keeping a stale copy in data/.
//
// A missing file returns (nil, nil): the assistant simply has no project
// document to cite, which is a smaller problem than refusing to boot.
func LoadFile(path string, kind Kind, name string) (*Document, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	text, err := extract(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	text = normalize(text)
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("%s: no extractable text", filepath.Base(path))
	}

	if name == "" {
		name = filepath.Base(path)
	}
	return &Document{Kind: kind, Name: name, Path: path, Text: text}, nil
}

// Add appends a document and restores the sort order the prompt depends on.
func (c *Corpus) Add(d Document) {
	c.Documents = append(c.Documents, d)
	sort.SliceStable(c.Documents, func(i, j int) bool {
		oi, oj := kindOrder(c.Documents[i].Kind), kindOrder(c.Documents[j].Kind)
		if oi != oj {
			return oi < oj
		}
		return c.Documents[i].Name < c.Documents[j].Name
	})
}
