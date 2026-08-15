package reference

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/expona-ai/lumi-go/server/internal/corpus"
	"github.com/expona-ai/lumi-go/server/internal/orchestrator"
	"github.com/expona-ai/lumi-go/server/internal/rag"
)

// ToolName is the reference-search tool exposed to the model.
const ToolName = "search_go_docs"

type searchInput struct {
	Query string `json:"query"`
	TopK  int    `json:"top_k,omitempty"`
}

// SearchTool exposes the reference index.
//
// Separate from `search_documents` on purpose. One tool per authority keeps the
// citation honest: a passage returned here is what the language does, never
// what the candidate has done. A single blended tool would make it easy for the
// model to answer "do they know goroutines?" with a paragraph from the spec.
func SearchTool(s *rag.Store) orchestrator.ToolDef {
	return orchestrator.ToolDef{
		Name: ToolName,
		Description: strings.TrimSpace(`
Search the official Go documentation: the language specification, Effective Go, the memory model, the FAQ, release notes, and the modules guide.
Use it to answer technical questions about Go itself — semantics, syntax, idiom, concurrency guarantees, version-specific behaviour — and to check a technical claim before making it.
This is documentation about the language. It is never evidence of the candidate's experience; use search_documents for that.
Note the source is tip.golang.org, the in-development documentation, which can be ahead of the current release — say so when a detail is version-sensitive.`),
		Properties: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "What to look up, phrased as it would appear in the documentation.",
			},
			"top_k": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("How many passages to return (default %d).", rag.DefaultTopK),
			},
		},
		Required: []string{"query"},
		Recall:   true,
		Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
			var in searchInput
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &in); err != nil {
					return "", fmt.Errorf("invalid input: %w", err)
				}
			}
			if strings.TrimSpace(in.Query) == "" {
				return "", fmt.Errorf("query is required")
			}
			results, err := s.Search(ctx, in.Query, rag.SearchOptions{TopK: in.TopK, Kind: corpus.KindReference})
			if err != nil {
				return "", err
			}
			return rag.Format(results), nil
		},
	}
}

// PromptSection describes the reference material and how to use it, for
// inclusion in the system prompt.
func PromptSection(c *corpus.Corpus) string {
	if c == nil || len(c.Documents) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("# Go reference documentation\n")
	b.WriteString("Searchable with the `" + ToolName + "` tool:\n")
	for _, d := range c.Documents {
		fmt.Fprintf(&b, "- %s\n", d.Name)
	}
	b.WriteString(strings.TrimSpace(`
Use it when a Go technical question comes up — in an interview answer, a claim about the language, or a comparison against what the role asks for. Prefer looking it up over answering from memory: a wrong claim about Go in an interview costs more than the second it takes to check.

Keep the two authorities separate. This documentation establishes what Go does. Only the résumé establishes what the candidate has done. Never present a passage from the documentation as the candidate's experience.`))
	return b.String()
}
