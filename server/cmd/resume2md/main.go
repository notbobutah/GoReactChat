// Command resume2md converts the résumé's HTML source into the markdown the
// service uses for grounding.
//
// It replaces a lossy path with a faithful one. The markdown used to be
// produced by cmd/pdf2md, reading the rendered PDF — but a PDF has no
// structure, only positioned text, so every role's bullets collapsed into one
// run-on paragraph and attached themselves to whatever heading came last. The
// result filed four employers' accomplishments, including "gRPC in Go", under
// "EARLIER CAREER" — a heading that in the document means 1999–2005. The
// service answered from that.
//
// The HTML has the structure the PDF lost: a section label, a job block per
// role, and a bullet list inside it. Converting from the source rather than
// from the artifact keeps each accomplishment attached to the role and the
// dates it belongs to.
//
//	go run ./cmd/resume2md ../data/resume-src/Resume.html > ../data/Resume.md
package main

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/net/html"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: resume2md <resume.html> [> out.md]")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer f.Close()

	doc, err := html.Parse(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: parse html:", err)
		os.Exit(1)
	}

	var b strings.Builder
	convert(doc, &b)
	fmt.Print(tidy(b.String()))
}

// convert walks the document, emitting markdown for the classes the résumé
// template uses. Anything unrecognized is descended into rather than dropped,
// so a new block in the template degrades to its text instead of vanishing.
func convert(n *html.Node, b *strings.Builder) {
	if n.Type == html.ElementNode {
		switch class(n) {
		case "name":
			fmt.Fprintf(b, "# %s\n\n", text(n))
			return
		case "role-line":
			fmt.Fprintf(b, "%s\n\n", text(n))
			return
		case "contact":
			fmt.Fprintf(b, "%s\n\n", text(n))
			return
		case "sec-label":
			fmt.Fprintf(b, "## %s\n\n", strings.ToUpper(text(n)))
			return
		case "summary", "earlier", "kv":
			fmt.Fprintf(b, "%s\n\n", text(n))
			return
		case "skill-row":
			// The label is a <b> inside the row; the rest is the content.
			label, rest := splitLeadingBold(n)
			if label != "" {
				fmt.Fprintf(b, "### %s\n\n%s\n\n", strings.ToUpper(label), rest)
			} else {
				fmt.Fprintf(b, "%s\n\n", text(n))
			}
			return
		case "job":
			writeJob(n, b)
			return
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		convert(c, b)
	}
}

// writeJob emits one role: heading, dates, intro, then its bullets as a list.
//
// The bullets stay a list on purpose. Flattening them into a paragraph is
// exactly what the PDF path did, and it is what let one role's work drift onto
// another's — a list item cannot silently reattach itself to the wrong heading.
func writeJob(n *html.Node, b *strings.Builder) {
	title := firstByClass(n, "job-title")
	dates := firstByClass(n, "job-dates")
	intro := firstByClass(n, "job-intro")

	if title != nil {
		fmt.Fprintf(b, "### %s\n\n", text(title))
	}
	if dates != nil {
		fmt.Fprintf(b, "*%s*\n\n", text(dates))
	}
	if intro != nil {
		fmt.Fprintf(b, "%s\n\n", text(intro))
	}
	if list := firstByTag(n, "ul"); list != nil {
		for li := list.FirstChild; li != nil; li = li.NextSibling {
			if li.Type == html.ElementNode && li.Data == "li" {
				if t := text(li); t != "" {
					fmt.Fprintf(b, "- %s\n", t)
				}
			}
		}
		b.WriteString("\n")
	}
}

// splitLeadingBold returns the text of a leading <b> and everything after it.
func splitLeadingBold(n *html.Node) (label, rest string) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if label == "" && c.Type == html.ElementNode && c.Data == "b" {
			label = text(c)
			continue
		}
		rest += text(c) + " "
	}
	return strings.TrimSpace(label), strings.TrimSpace(rest)
}

func class(n *html.Node) string {
	for _, a := range n.Attr {
		if a.Key == "class" {
			return a.Val
		}
	}
	return ""
}

func firstByClass(n *html.Node, want string) *html.Node {
	if n.Type == html.ElementNode && class(n) == want {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := firstByClass(c, want); found != nil {
			return found
		}
	}
	return nil
}

func firstByTag(n *html.Node, tag string) *html.Node {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			return c
		}
		if found := firstByTag(c, tag); found != nil {
			return found
		}
	}
	return nil
}

// text flattens an element to a single spaced line. Inline emphasis is dropped
// rather than translated: these documents are read by a retrieval index, and
// bold markers in the middle of a sentence only add tokens.
func text(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
			// A separator between inline elements, so "…platform" and "Users
			// create…" do not fuse into one word.
			if c.Type == html.ElementNode {
				b.WriteString(" ")
			}
		}
	}
	walk(n)
	out := strings.Join(strings.Fields(strings.ReplaceAll(b.String(), " ", " ")), " ")
	return fixSpacing(out)
}

// fixSpacing removes the separator inserted after an inline element when the
// next character is punctuation. `<span>deploy/</span>,` would otherwise render
// as "deploy/ ," — not merely ugly: the corpus is checked against the PDF's
// text layer to prove the two agree, and a stray space makes an identical
// sentence look like a mismatch.
func fixSpacing(s string) string {
	for _, p := range []string{",", ".", ";", ":", "!", "?", ")", "%"} {
		s = strings.ReplaceAll(s, " "+p, p)
	}
	return strings.ReplaceAll(s, "( ", "(")
}

// tidy collapses runs of blank lines left by skipped nodes.
func tidy(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s) + "\n"
}
