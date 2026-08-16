package newsagent

import (
	"fmt"
	"strings"
	"time"
)

// prompt is the whole "framework". With the loop on the far side, the only
// levers left are what to ask for and what shape the answer must take — so the
// instructions carry the parts a schema cannot express: what counts as news,
// what to do when there is nothing, and the standing rule that every claim
// needs a link somebody can open.
func (a *Agent) prompt() string {
	days := int(a.window().Hours() / 24)

	var b strings.Builder
	fmt.Fprintf(&b, `You are a release watcher for a Go engineering team. Find what has actually changed in the last %d days across these projects:

- go: the Go programming language and toolchain (go.dev, the Go blog, golang/go)
- grpc: gRPC (grpc.io, grpc/grpc-go and the other gRPC repositories)
- protobuf: Protocol Buffers (protocolbuffers/protobuf, protobuf-go, buf)

What counts as news, in descending order of interest: releases and release
candidates, security advisories, accepted proposals and language or wire-format
changes, deprecations, and significant tooling changes. What does not: tutorials,
opinion posts, conference announcements, job listings, and "top 10" articles.

Rules:
- Search for each topic. Do not answer from memory — versions and dates are
  exactly what memory gets wrong, and a confidently stated wrong version number
  is worse than no answer.
- Every item must carry the URL of a page you actually opened. If you cannot
  produce a real link for something, leave it out.
- Prefer the primary source (release notes, the advisory, the commit) over an
  article describing it.
- Report at most %d items and at most 3 per topic, newest first.
- "published" is the date the source gives, in YYYY-MM-DD form. If the source
  gives no date, use the release or tag date. Do not guess.
- "summary" is one or two sentences on what changed and why an engineer would
  care. No marketing language.
- "source" is the human-readable publisher, like "go.dev" or "GitHub —
  protocolbuffers/protobuf".
- Fewer, well-sourced items beat a padded list. If a topic genuinely had no
  news in the window, return nothing for it rather than reaching further back.

Today is %s.`, days, a.maxItems(), time.Now().UTC().Format("2006-01-02"))

	return b.String()
}

// schema is the response contract. `strict` makes the provider enforce it, so
// the digest arrives parseable or not at all — no prose to strip, no partial
// JSON to repair, and no place for the model to answer in a shape the UI
// cannot render.
func (a *Agent) schema() jsonSchemaFormat {
	topics := make([]any, 0, len(Topics))
	for _, t := range Topics {
		topics = append(topics, t)
	}

	return jsonSchemaFormat{
		Type:   "json_schema",
		Name:   "news_digest",
		Strict: true,
		Schema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"items"},
			"properties": map[string]any{
				"items": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"required": []string{
							"topic", "headline", "summary", "url", "source", "published",
						},
						"properties": map[string]any{
							"topic":     map[string]any{"type": "string", "enum": topics},
							"headline":  map[string]any{"type": "string"},
							"summary":   map[string]any{"type": "string"},
							"url":       map[string]any{"type": "string"},
							"source":    map[string]any{"type": "string"},
							"published": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	}
}

func (a *Agent) window() time.Duration {
	if a.Window > 0 {
		return a.Window
	}
	return 30 * 24 * time.Hour
}
