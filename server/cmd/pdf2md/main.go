// Command pdf2md converts a résumé PDF into structured markdown.
//
// Why this exists: PDF text extraction emits one line per *visual* line, and
// splits again at every styled span — a bold "25+ years" mid-sentence becomes
// its own line. The result has no paragraphs and no heading structure, which
// makes it poor input for chunking: the whole document embeds as one blob.
// Markdown restores the structure, so retrieval works on sections instead.
//
// This is a best-effort structural reflow, not a faithful renderer. Treat the
// output as a starting point: it is written once, hand-corrected if needed, and
// then IT becomes the canonical document (the loader prefers .md over a .pdf of
// the same name). Re-run it when the source PDF changes.
//
//	go run ./cmd/pdf2md ../data/Resume.pdf > ../data/Resume.md
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

// topLevelSections are the headings that become `##`. Any other ALL-CAPS line
// is treated as a subsection (`###`) — in a résumé those are the skill-group
// and role labels nested inside a section.
var topLevelSections = map[string]bool{
	"SUMMARY": true, "PROFILE": true, "OBJECTIVE": true,
	"CORE EXPERTISE": true, "SKILLS": true, "TECHNICAL SKILLS": true,
	"EXPERIENCE": true, "PROFESSIONAL EXPERIENCE": true, "EMPLOYMENT": true,
	"EDUCATION": true, "CERTIFICATIONS": true, "PROJECTS": true,
	"PUBLICATIONS": true, "AWARDS": true, "VOLUNTEER": true, "INTERESTS": true,
}

var (
	// "Chief Technology Officer —" : a role line whose employer is on the next
	// line and dates on the one after.
	roleLine = regexp.MustCompile(`^(.+?)\s+[—–]\s*$`)
	// "Jul 2023 – Present", "Aug 2023 – Nov 2024 · concurrent"
	dateLine = regexp.MustCompile(`^([A-Z][a-z]{2}\s+)?\d{4}\s*[–—-]\s*(Present|([A-Z][a-z]{2}\s+)?\d{4})`)
	multiSpc = regexp.MustCompile(`[ \t]{2,}`)
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: pdf2md <file.pdf> [> out.md]")
		os.Exit(2)
	}

	text, err := extractPDF(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Print(toMarkdown(text))
}

func extractPDF(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	reader, err := r.GetPlainText()
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func toMarkdown(text string) string {
	lines := splitLines(text)
	if len(lines) == 0 {
		return ""
	}

	var out strings.Builder
	// The first line is the name; everything until the first section heading is
	// the contact block, which the extractor scatters across lines separated by
	// bullet characters.
	fmt.Fprintf(&out, "# %s\n\n", lines[0])

	i := 1
	var contact []string
	for ; i < len(lines); i++ {
		if _, ok := heading(lines[i], prevOf(lines, i)); ok {
			break
		}
		if isBulletSeparator(lines[i]) {
			continue
		}
		contact = append(contact, lines[i])
	}
	if len(contact) > 0 {
		fmt.Fprintf(&out, "%s\n\n", strings.Join(contact, " · "))
	}

	// Body: accumulate prose into paragraphs, emitting on structural boundaries.
	var para []string
	flush := func() {
		if len(para) == 0 {
			return
		}
		fmt.Fprintf(&out, "%s\n\n", reflow(para))
		para = nil
	}

	for ; i < len(lines); i++ {
		line := lines[i]

		if h, ok := heading(line, prevOf(lines, i)); ok {
			flush()
			level := "###"
			if topLevelSections[h] {
				level = "##"
			}
			fmt.Fprintf(&out, "%s %s\n\n", level, h)
			continue
		}

		// A role header spans up to three lines: title, employer, dates.
		if m := roleLine.FindStringSubmatch(line); m != nil && i+1 < len(lines) &&
			len(strings.TrimSpace(m[1])) <= 55 && len(lines[i+1]) <= 60 {
			flush()
			title, employer := strings.TrimSpace(m[1]), lines[i+1]
			i++
			fmt.Fprintf(&out, "### %s — %s\n\n", title, employer)
			if i+1 < len(lines) && dateLine.MatchString(lines[i+1]) {
				i++
				fmt.Fprintf(&out, "*%s*\n\n", lines[i])
			}
			continue
		}

		if dateLine.MatchString(line) {
			flush()
			fmt.Fprintf(&out, "*%s*\n\n", line)
			continue
		}

		if item, ok := bulletItem(line); ok {
			flush()
			fmt.Fprintf(&out, "- %s\n", item)
			continue
		}

		para = append(para, line)
	}
	flush()

	// Collapse the blank-line runs the emitters above can leave behind.
	return regexp.MustCompile(`\n{3,}`).ReplaceAllString(out.String(), "\n\n")
}

func splitLines(text string) []string {
	var out []string
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(strings.ReplaceAll(raw, " ", " "))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// heading reports an ALL-CAPS line (letters only — "AI & DATA" and
// "CLOUD & DEVOPS" must qualify) that is short enough to be a label.
func heading(line, prev string) (string, bool) {
	if len(line) == 0 || len(line) > 60 {
		return "", false
	}
	// Rune-aware: "·" and "•" are multi-byte, so a byte-slice prefix test would
	// silently miss them.
	for _, p := range []string{"•", "·", "-", ",", ".", ";", ":", "&", "/"} {
		if strings.HasPrefix(line, p) {
			return "", false
		}
	}
	// A fragment that closes or continues the previous line is not a heading,
	// however capitalised it looks: "…SageMaker," / "TAMR)" is one sentence the
	// extractor split at a style boundary.
	if strings.HasSuffix(line, ")") || strings.HasSuffix(line, ",") ||
		strings.HasSuffix(line, "&") || strings.HasSuffix(line, "·") {
		return "", false
	}
	if p := strings.TrimSpace(prev); p != "" {
		if strings.HasSuffix(p, ",") || strings.HasSuffix(p, "·") || strings.HasSuffix(p, "-") {
			return "", false
		}
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
	// An identifier is not a heading: "US PCT/US2001/003087" is all caps and all
	// noise. Requiring letters to outnumber digits keeps real labels ("AI & DATA")
	// and drops reference numbers.
	var digits int
	for _, r := range line {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	if letters >= 3 && upper == letters && letters > digits {
		return line, true
	}
	return "", false
}

func isBulletSeparator(line string) bool {
	t := strings.TrimSpace(line)
	return t == "•" || t == "·" || t == "|"
}

func bulletItem(line string) (string, bool) {
	for _, p := range []string{"• ", "* ", "- ", "▪ "} {
		if strings.HasPrefix(line, p) {
			return strings.TrimSpace(strings.TrimPrefix(line, p)), true
		}
	}
	return "", false
}

// reflow joins the fragments of one paragraph back into a sentence.
//
// The extractor splits mid-sentence at style changes, leaving fragments that
// begin or end mid-word-boundary (" turning emerging"). Joining with a single
// space and collapsing runs restores readable prose; punctuation that ended up
// on its own is re-attached rather than left floating.
func reflow(lines []string) string {
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			prev := lines[i-1]
			// "event-" + "driven systems" is one hyphenated word, not two.
			if strings.HasSuffix(prev, "-") && startsLower(line) {
				b.WriteString(line)
				continue
			}
			needSpace := !strings.HasSuffix(prev, " ") && !strings.HasPrefix(line, " ")
			// Do not put a space before punctuation that was split off.
			if strings.HasPrefix(strings.TrimSpace(line), ".") ||
				strings.HasPrefix(strings.TrimSpace(line), ",") ||
				strings.HasPrefix(strings.TrimSpace(line), ";") {
				needSpace = false
				line = strings.TrimSpace(line)
			}
			if needSpace {
				b.WriteString(" ")
			}
		}
		b.WriteString(line)
	}
	return strings.TrimSpace(multiSpc.ReplaceAllString(b.String(), " "))
}

// prevOf returns the line before i, or "" at the start.
func prevOf(lines []string, i int) string {
	if i == 0 {
		return ""
	}
	return lines[i-1]
}

// startsLower reports whether a line begins with a lowercase letter, which
// marks it as the continuation of a word or sentence rather than a new one.
func startsLower(s string) bool {
	for _, r := range s {
		return r >= 'a' && r <= 'z'
	}
	return false
}
