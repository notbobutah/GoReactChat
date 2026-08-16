// Command fetchrefs downloads the reference documentation and caches it as
// plain text under <data>/reference.
//
// Fetching is an explicit step, not something the server does at boot: a chat
// service should not depend on six external HTTP calls to start, and the
// content changes on the order of weeks. Re-run it to refresh:
//
//	go run ./cmd/fetchrefs            # into ../data/reference
//	go run ./cmd/fetchrefs -data ../data
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/expona-ai/lumi-go/server/internal/reference"
)

func main() {
	dataDir := flag.String("data", "../data", "data directory; documents are cached under <data>/reference")
	timeout := flag.Duration("timeout", 60*time.Second, "per-request timeout")
	flag.Parse()

	dir := reference.Dir(*dataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: *timeout}
	failed := 0
	for _, src := range reference.DefaultSources {
		text, err := fetch(client, src.URL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %-52s FAILED: %v\n", src.URL, err)
			failed++
			continue
		}
		out := filepath.Join(dir, src.File)
		if err := os.WriteFile(out, []byte(text), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "  %-52s WRITE FAILED: %v\n", src.URL, err)
			failed++
			continue
		}
		fmt.Printf("  %-52s → %-28s %7d chars\n", src.URL, src.File, len(text))
	}

	if failed > 0 {
		// Partial success still leaves a usable cache; exit non-zero so a CI
		// step or a Makefile notices.
		fmt.Fprintf(os.Stderr, "%d of %d sources failed\n", failed, len(reference.DefaultSources))
		os.Exit(1)
	}
}

func fetch(client *http.Client, url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	// Identify the fetcher rather than pretending to be a browser.
	req.Header.Set("User-Agent", "lumi-go-fetchrefs/1.0 (+https://github.com/notbobutah/GoReactChat)")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %s", resp.Status)
	}

	doc, err := html.Parse(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}
	text := dropBreadcrumbs(joinOrphanBullets(extractText(doc)))
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("no text extracted")
	}
	return fmt.Sprintf("Source: %s\n\n%s", url, text), nil
}

// joinOrphanBullets reattaches a list marker to the text it introduces.
//
// walk writes "- " when it enters an <li>, but a list item whose first child is
// a block element then writes a newline before any text — leaving the marker
// alone on its line and the content on the next. Harmless to read, and not
// harmless to index: a chunk boundary can fall between them, and every orphan
// is a line of pure noise in a corpus that is inlined or embedded by the token.
func joinOrphanBullets(s string) string {
	return orphanBullet.ReplaceAllString(s, "\n- ")
}

var orphanBullet = regexp.MustCompile(`\n-[ \t]*\n[ \t]*`)

// dropBreadcrumbs removes the trail of links these pages put above the title.
//
// <nav> is already skipped, but go.dev renders breadcrumbs as a plain list
// inside <main>, so they survive and arrive as "Documentation / Tutorials /
// <page title>" ahead of the first heading. That is chrome indexed as content:
// a retrieval hit on it returns a chunk that says nothing, attributed to no
// section.
//
// Everything before the first H1 goes. On these pages there is nothing else up
// there — and if a page has no H1 at all the text is returned untouched, so a
// differently-structured page loses nothing.
func dropBreadcrumbs(s string) string {
	i := strings.Index(s, "\n# ")
	if i < 0 {
		if strings.HasPrefix(s, "# ") {
			return s
		}
		return s
	}
	return strings.TrimLeft(s[i+1:], "\n")
}

// extractText pulls the readable body out of a documentation page.
//
// A real HTML parse rather than tag-stripping regexes: these pages carry
// navigation, sidebars, and script blocks that would otherwise be indexed and
// compete with content at retrieval time. Heading tags are re-emitted as
// markdown so the chunker's section detection has something to work with.
func extractText(doc *html.Node) string {
	root := findMain(doc)
	if root == nil {
		root = doc
	}
	var b strings.Builder
	walk(root, &b)
	return collapse(b.String())
}

// findMain locates the primary content container, preferring <main>, then
// <article>, then a div carrying a documentation class.
func findMain(n *html.Node) *html.Node {
	var found *html.Node
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Main, atom.Article:
				found = n
				return
			}
			for _, a := range n.Attr {
				if a.Key == "class" && (strings.Contains(a.Val, "Documentation") || strings.Contains(a.Val, "article")) {
					found = n
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(n)
	return found
}

// skipped elements contribute no readable content.
var skipped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Nav: true,
	atom.Header: true, atom.Footer: true, atom.Aside: true,
	atom.Noscript: true, atom.Svg: true, atom.Button: true, atom.Form: true,
}

func walk(n *html.Node, b *strings.Builder) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
		return
	case html.ElementNode:
		if skipped[n.DataAtom] {
			return
		}
		switch n.DataAtom {
		case atom.H1:
			b.WriteString("\n\n# ")
		case atom.H2:
			b.WriteString("\n\n## ")
		case atom.H3:
			b.WriteString("\n\n### ")
		case atom.H4, atom.H5, atom.H6:
			b.WriteString("\n\n#### ")
		case atom.Li:
			b.WriteString("\n- ")
		case atom.P, atom.Div, atom.Pre, atom.Tr, atom.Br, atom.Section:
			b.WriteString("\n")
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, b)
	}

	if n.Type == html.ElementNode {
		switch n.DataAtom {
		case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.P, atom.Pre, atom.Li:
			b.WriteString("\n")
		}
	}
}

// collapse normalises the whitespace an HTML walk leaves behind: trailing
// spaces, runs of blank lines, and lines that are pure indentation.
func collapse(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(strings.ReplaceAll(line, " ", " "), " \t")
		if strings.TrimSpace(line) == "" {
			blank++
			if blank > 1 {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
