package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/expona-ai/lumi-go/server/internal/corpus"
	"github.com/expona-ai/lumi-go/server/internal/orchestrator"
)

// ToolName is the single retrieval tool exposed to the model.
const ToolName = "search_documents"

type searchInput struct {
	Query string `json:"query"`
	// "resume" | "job_description" | "project" | "" (all)
	Document string `json:"document,omitempty"`
	TopK     int    `json:"top_k,omitempty"`
}

// SearchTool exposes the index to the model.
//
// Recall is true, which is load-bearing: it marks this as a silent context
// loader, so the orchestrator drops the "let me look that up…" preamble the
// model streams before calling it (and preserves a substantial one as a
// thinking block). Without the flag the user would read the preamble and then
// the answer that restates it.
func SearchTool(s *Store) orchestrator.ToolDef {
	return orchestrator.ToolDef{
		Name: ToolName,
		Description: strings.TrimSpace(`
Search Robert's documents: his résumé, the job description under consideration, and documentation of systems he designed and shipped.
Call this before answering anything about his background, about what the role requires, or about what he has delivered — you do not have the documents in context, and answering from memory is how fabrications happen.
Search with the words that would appear in the document, not the user's phrasing: for "am I senior enough?", search for titles and years of experience. Call it several times with different wording when one search is thin.`),
		Properties: map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "What to look for, phrased as it would appear in the document.",
			},
			"document": map[string]any{
				"type":        "string",
				"enum":        []string{"resume", "job_description", "project"},
				"description": "Restrict the search to one document: the résumé, the job description, or the delivered project. Omit to search all of them.",
			},
			"top_k": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("How many passages to return (default %d).", DefaultTopK),
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

			var kind corpus.Kind
			switch in.Document {
			case "resume":
				kind = corpus.KindResume
			case "job_description":
				kind = corpus.KindJobDescription
			case "project":
				kind = corpus.KindProject
			case "", "any", "both":
				kind = ""
			default:
				return "", fmt.Errorf("unknown document %q: use \"resume\", \"job_description\", or \"project\"", in.Document)
			}

			results, err := s.Search(ctx, in.Query, SearchOptions{TopK: in.TopK, Kind: kind})
			if err != nil {
				return "", err
			}
			return Format(results), nil
		},
	}
}

// SystemPrompt is the retrieval-mode prompt: the same behavioural rules as the
// inlined mode, plus the protocol for getting the documents, plus a manifest of
// what exists. The manifest matters — without it the model cannot tell the
// difference between "the résumé does not mention Kubernetes" and "there is no
// résumé loaded".
// extras are appended verbatim after the manifest — used for reference material
// that lives in its own index and its own tool (see package reference).
func SystemPrompt(c *corpus.Corpus, s *Store, extras ...string) string {
	var b strings.Builder
	b.WriteString(corpus.Role)
	b.WriteString("\n\nYou do NOT have the documents in context. They are indexed and retrievable with the `")
	b.WriteString(ToolName)
	b.WriteString("` tool.\n\n")

	b.WriteString("Retrieval protocol:\n")
	b.WriteString("- Search before answering any question about Robert's background or the role's requirements. Never answer either from memory or from earlier turns alone.\n")
	b.WriteString("- A fit question needs both sides: search the résumé (and his delivered work) as well as the job description before answering.\n")
	b.WriteString("- Retrieval returns the closest passages, not everything. If the passages look thin or off-target, search again with different words before concluding the documents do not cover it.\n")
	b.WriteString("- Cite the source filename and section when you use a passage.\n")
	b.WriteString("- Technical questions about Go itself are a separate lookup — see the reference section below if one is listed.\n")
	b.WriteString("- `project` documents describe systems Robert designed and shipped, and the reader can go and read the code. Cite them as delivered work — the strongest evidence available, because it is verifiable. Keep them distinct from the résumé, which is the authority on dates, employers and titles.\n\n")

	b.WriteString(corpus.GroundingRules)

	b.WriteString("\n\n# Indexed documents\n")
	if c.Empty() {
		b.WriteString("None. Say so if asked about Robert or the role.\n")
		return b.String()
	}
	for _, d := range c.Documents {
		fmt.Fprintf(&b, "- %s — %s (%d characters)\n", d.Name, d.Kind, len(d.Text))
	}
	fmt.Fprintf(&b, "\nIndexed as %d passages.\n", s.Len())

	for _, extra := range extras {
		if strings.TrimSpace(extra) != "" {
			b.WriteString("\n")
			b.WriteString(strings.TrimSpace(extra))
			b.WriteString("\n")
		}
	}
	return b.String()
}
