package corpus

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

// extract turns a file into plain text, dispatching on extension.
func extract(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".md", ".markdown":
		b, err := os.ReadFile(path)
		return string(b), err
	case ".pdf":
		return extractPDF(path)
	case ".eml":
		return extractEML(path)
	default:
		return "", fmt.Errorf("unsupported extension")
	}
}

// extractPDF pulls the text layer out of a PDF. Pure Go, so it survives the
// distroless image — but it means a scanned (image-only) PDF yields nothing
// and gets skipped with a reason rather than silently loading as empty.
func extractPDF(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("open pdf: %w", err)
	}
	defer f.Close()

	reader, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("extract pdf text: %w", err)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, reader); err != nil {
		return "", fmt.Errorf("read pdf text: %w", err)
	}
	return buf.String(), nil
}

// extractEML reads a saved email — the usual way a job description arrives.
// It prefers the text/plain alternative and falls back to stripped HTML;
// inline images and attachments are ignored.
func extractEML(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	msg, err := mail.ReadMessage(f)
	if err != nil {
		return "", fmt.Errorf("parse email: %w", err)
	}

	body, err := readPart(msg.Header.Get("Content-Type"), msg.Header.Get("Content-Transfer-Encoding"), msg.Body)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("no text part")
	}

	// The subject usually carries the role title, which the body often assumes
	// rather than restates.
	var b strings.Builder
	if subject := decodeHeader(msg.Header.Get("Subject")); subject != "" {
		fmt.Fprintf(&b, "Subject: %s\n", subject)
	}
	if from := decodeHeader(msg.Header.Get("From")); from != "" {
		fmt.Fprintf(&b, "From: %s\n", from)
	}
	if date := msg.Header.Get("Date"); date != "" {
		fmt.Fprintf(&b, "Date: %s\n", date)
	}
	b.WriteString("\n")
	b.WriteString(body)
	return b.String(), nil
}

// readPart walks a MIME tree and returns the best text it can find. Depth is
// bounded by the recursion following the actual part structure; a multipart
// with no text part returns "" rather than an error so the caller can decide.
func readPart(contentType, encoding string, body io.Reader) (string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// No or malformed Content-Type: treat the body as plain text, which is
		// what a bare RFC 822 message is.
		b, readErr := io.ReadAll(body)
		return string(b), readErr
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return "", fmt.Errorf("multipart without boundary")
		}
		mr := multipart.NewReader(body, boundary)

		var plain, html string
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", fmt.Errorf("read mime part: %w", err)
			}
			text, err := readPart(
				part.Header.Get("Content-Type"),
				part.Header.Get("Content-Transfer-Encoding"),
				part,
			)
			part.Close()
			if err != nil || strings.TrimSpace(text) == "" {
				continue
			}
			// Prefer the first text/plain; keep an HTML fallback in case the
			// sender shipped HTML only.
			ct, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			switch {
			case strings.HasPrefix(ct, "text/plain") && plain == "":
				plain = text
			case strings.HasPrefix(ct, "text/html") && html == "":
				html = text
			case strings.HasPrefix(ct, "multipart/") && plain == "":
				plain = text
			}
		}
		if plain != "" {
			return plain, nil
		}
		return html, nil
	}

	if !strings.HasPrefix(mediaType, "text/") {
		return "", nil // images, attachments — not context
	}

	decoded, err := decodeBody(body, encoding)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(mediaType, "text/html") {
		decoded = stripHTML(decoded)
	}
	return decoded, nil
}

func decodeBody(r io.Reader, encoding string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		b, err := io.ReadAll(quotedprintable.NewReader(r))
		return string(b), err
	case "base64":
		// mime/multipart decodes base64 parts transparently; a top-level
		// base64 body is rare enough that reading it raw is the honest
		// behaviour — it will produce no usable text and be skipped.
		b, err := io.ReadAll(r)
		return string(b), err
	default:
		b, err := io.ReadAll(r)
		return string(b), err
	}
}

func decodeHeader(v string) string {
	dec := new(mime.WordDecoder)
	if out, err := dec.DecodeHeader(v); err == nil {
		return out
	}
	return v
}

var (
	// Two patterns rather than one with a backreference: Go's RE2 has no \1.
	htmlScript    = regexp.MustCompile(`(?is)<script\b.*?</script>`)
	htmlStyle     = regexp.MustCompile(`(?is)<style\b.*?</style>`)
	htmlBreak     = regexp.MustCompile(`(?i)<(br\s*/?|/p|/div|/tr|/li|/h[1-6])>`)
	htmlTag       = regexp.MustCompile(`(?s)<[^>]*>`)
	blankRuns     = regexp.MustCompile(`\n{3,}`)
	trailingSpace = regexp.MustCompile(`[ \t]+\n`)
)

// stripHTML is a fallback for HTML-only email. It is not a parser — it drops
// script/style, turns block ends into newlines, removes tags, and unescapes the
// handful of entities that actually show up in mail.
func stripHTML(s string) string {
	s = htmlScript.ReplaceAllString(s, "")
	s = htmlStyle.ReplaceAllString(s, "")
	s = htmlBreak.ReplaceAllString(s, "\n")
	s = htmlTag.ReplaceAllString(s, "")
	r := strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&#39;", "'", "&rsquo;", "'", "&ldquo;", `"`, "&rdquo;", `"`,
	)
	return r.Replace(s)
}

// normalize collapses the whitespace noise that PDF extraction and email
// quoting leave behind, so the prompt carries content rather than layout.
func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, " ", " ")
	s = stripBoilerplate(s)
	s = trailingSpace.ReplaceAllString(s, "\n")
	s = blankRuns.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

var (
	// Inline-image placeholders and tracking/unsubscribe links from an email
	// signature. These carry no meaning but embed as text and compete with real
	// content at retrieval time.
	cidRef      = regexp.MustCompile(`(?i)\[cid:[^\]]*\]`)
	spacerRef   = regexp.MustCompile(`(?i)\[spacer[^\]]*\]`)
	bracketURL  = regexp.MustCompile(`<https?://[^>]*>`)
	bareURLLine = regexp.MustCompile(`(?m)^\s*https?://\S+\s*$`)
)

// boilerplatePrefixes marks lines that begin a legal or marketing footer.
// Matching is case-insensitive and prefix-based. Kept deliberately short —
// over-eager filtering would strip the recruiter's actual instructions, which
// are part of the job context.
var boilerplatePrefixes = []string{
	"all referenced trademarks are the property",
	"© 20", "(c) 20",
	"robert half inc. is an equal opportunity employer",
	"this message and any attachments",
	"if you are not the intended recipient",
	"please consider the environment",
	"unsubscribe",
}

// stripBoilerplate removes email chrome: inline-image placeholders, tracking
// links, and legal footers. Retrieval quality is largely a function of what is
// NOT in the index — a trademark notice that outranks a job requirement is a
// retrieval bug, not a formatting nit.
func stripBoilerplate(s string) string {
	s = cidRef.ReplaceAllString(s, "")
	s = spacerRef.ReplaceAllString(s, "")
	s = bracketURL.ReplaceAllString(s, "")
	s = bareURLLine.ReplaceAllString(s, "")

	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		l := strings.ToLower(strings.TrimSpace(line))
		drop := false
		for _, p := range boilerplatePrefixes {
			if strings.HasPrefix(l, p) {
				drop = true
				break
			}
		}
		if !drop {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}
