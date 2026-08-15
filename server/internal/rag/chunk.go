// Package rag turns the loaded documents into an embedded, searchable index
// and exposes it to the model as a recall tool.
//
// The store is in-process: chunks and their vectors live in a slice, and search
// is a cosine scan. At this corpus size (two documents, tens of chunks) an
// approximate index would be slower than the scan it replaces, and the whole
// thing rebuilds from the data directory at boot — so there is nothing to
// persist, migrate, or keep in sync.
package rag

import (
	"fmt"
	"strings"

	"github.com/expona-ai/lumi-go/server/internal/corpus"
)

// Chunking targets.
//
// Sized small on purpose. An embedding is an average of everything in the
// passage, so a large chunk answers every query mediocrely and none well — a
// whole résumé in one vector loses to any focused paragraph. These sizes keep a
// passage to roughly one subsection: a job requirement block, or one role's
// bullets.
const (
	targetChunkChars = 600
	maxChunkChars    = 900
	overlapChars     = 120
)

// Chunk is one retrievable passage.
type Chunk struct {
	ID      string
	DocName string
	Kind    corpus.Kind
	Index   int
	Text    string
	Vector  []float32
	// Heading is the nearest preceding section heading, kept so a retrieved
	// passage can say where in the document it came from.
	Heading string
}

// Chunks splits a corpus into passages.
func Chunks(c *corpus.Corpus) []Chunk {
	var out []Chunk
	for _, doc := range c.Documents {
		for i, p := range splitDocument(doc.Text) {
			out = append(out, Chunk{
				ID:      fmt.Sprintf("%s#%d", doc.Name, i),
				DocName: doc.Name,
				Kind:    doc.Kind,
				Index:   i,
				Text:    p.text,
				Heading: p.heading,
			})
		}
	}
	return out
}

type passage struct {
	text    string
	heading string
}

// splitDocument works line by line rather than paragraph by paragraph.
//
// That is not a stylistic choice: PDF text extraction emits one newline per
// visual line and no blank lines at all, so paragraph splitting returns the
// entire document as a single unit. Line-based accumulation gives the same
// result as paragraph splitting on documents that do have blank lines (a blank
// line just forces a soft boundary) and works on the ones that do not.
func splitDocument(text string) []passage {
	lines := strings.Split(text, "\n")

	var (
		out     []passage
		buf     strings.Builder
		heading string // heading in force now
		current string // heading in force for the buffer being built
	)

	flush := func() {
		if strings.TrimSpace(buf.String()) == "" {
			buf.Reset()
			return
		}
		out = append(out, passage{text: strings.TrimSpace(buf.String()), heading: current})
		buf.Reset()
		current = heading
	}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			// A blank line is a soft boundary: break here if the buffer is
			// already big enough to stand on its own.
			if buf.Len() >= targetChunkChars {
				flush()
			}
			continue
		}

		if h, ok := asHeading(line); ok {
			// Sections are the natural retrieval unit in both a résumé and a
			// job posting, so a heading always starts a new passage.
			flush()
			heading = h
			current = h
			buf.WriteString(line)
			buf.WriteString("\n")
			continue
		}

		if buf.Len() > 0 && buf.Len()+len(line) > targetChunkChars {
			tail := overlapTail(buf.String())
			flush()
			// Carry a little of the previous passage so a sentence split across
			// a boundary stays retrievable from either side.
			if tail != "" {
				buf.WriteString(tail)
				buf.WriteString("\n")
			}
		}

		buf.WriteString(line)
		buf.WriteString("\n")

		if buf.Len() >= maxChunkChars {
			flush()
		}
	}
	flush()

	return out
}

// asHeading recognises the shapes that actually appear in these documents: an
// ALL-CAPS section label (résumé sections, and the "Must-Have Skills" style
// labels in a posting) and a markdown-style "# " heading.
func asHeading(line string) (string, bool) {
	if len(line) == 0 || len(line) > 60 {
		return "", false
	}
	if strings.HasPrefix(line, "#") {
		return strings.TrimSpace(strings.TrimLeft(line, "# ")), true
	}
	// A bullet is never a heading, however it is punctuated.
	if strings.HasPrefix(line, "*") || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "•") {
		return "", false
	}

	var letters, upper int
	for _, r := range line {
		switch {
		case r >= 'a' && r <= 'z':
			letters++
		case r >= 'A' && r <= 'Z':
			letters++
			upper++
		}
	}
	if letters >= 3 && upper == letters {
		return line, true
	}
	return "", false
}

// overlapTail returns the trailing text up to overlapChars, cut at a line or
// sentence boundary so the carried fragment is readable on its own.
func overlapTail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= overlapChars {
		return s
	}
	tail := s[len(s)-overlapChars:]
	if i := strings.Index(tail, "\n"); i >= 0 {
		tail = tail[i+1:]
	} else if i := strings.Index(tail, ". "); i >= 0 {
		tail = tail[i+2:]
	}
	return strings.TrimSpace(tail)
}
