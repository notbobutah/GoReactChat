package rag

import (
	"context"
	"hash/fnv"
	"strings"
	"testing"

	"github.com/expona-ai/lumi-go/server/internal/corpus"
)

// hashEmbedder is a deterministic bag-of-words embedder: each token is hashed
// into a dimension. It has none of a real model's semantics — "Golang" and "Go"
// are unrelated to it — but it makes retrieval mechanics testable without a
// network call or a model download, which is what these tests are about.
type hashEmbedder struct{ dims int }

func (h hashEmbedder) Model() string { return "test-hash" }

func (h hashEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, h.dims)
		for _, word := range strings.Fields(strings.ToLower(t)) {
			word = strings.Trim(word, ".,:;()[]—·")
			if word == "" {
				continue
			}
			f := fnv.New32a()
			_, _ = f.Write([]byte(word))
			v[int(f.Sum32())%h.dims] += 1
		}
		normalize(v)
		out[i] = v
	}
	return out, nil
}

func testCorpus() *corpus.Corpus {
	return &corpus.Corpus{Documents: []corpus.Document{
		{
			Kind: corpus.KindResume,
			Name: "resume.md",
			Text: `SUMMARY

Software architect with 25 years building distributed systems.

EXPERIENCE

Led a Kubernetes migration for an enterprise SaaS platform, reaching 90% customer adoption.

Built real-time trading infrastructure in Java handling market data feeds.`,
		},
		{
			Kind: corpus.KindJobDescription,
			Name: "role.md",
			Text: `REQUIREMENTS

Senior Golang engineer with production gRPC experience.

Familiarity with vector databases and retrieval augmented generation.`,
		},
	}}
}

func TestChunksPreserveHeadingsAndProvenance(t *testing.T) {
	chunks := Chunks(testCorpus())
	if len(chunks) == 0 {
		t.Fatal("no chunks produced")
	}

	var sawResumeHeading bool
	for _, c := range chunks {
		if c.Text == "" {
			t.Errorf("chunk %s has no text", c.ID)
		}
		if c.DocName == "" || c.Kind == "" {
			t.Errorf("chunk %s lost its provenance: doc=%q kind=%q", c.ID, c.DocName, c.Kind)
		}
		if c.Kind == corpus.KindResume && c.Heading == "EXPERIENCE" {
			sawResumeHeading = true
		}
	}
	// Section headings are the retrieval unit; losing them means a passage can
	// no longer say where in the document it came from.
	if !sawResumeHeading {
		t.Error("expected a chunk under the EXPERIENCE heading")
	}
}

func TestSearchRanksByRelevance(t *testing.T) {
	ctx := context.Background()
	index, err := Index(ctx, testCorpus(), hashEmbedder{dims: 512})
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	results, err := index.Search(ctx, "Kubernetes migration enterprise SaaS adoption", SearchOptions{TopK: 1})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].Chunk.Text, "Kubernetes") {
		t.Errorf("top hit missed the Kubernetes passage:\n%s", results[0].Chunk.Text)
	}
}

func TestSearchKindFilterIsHonoured(t *testing.T) {
	ctx := context.Background()
	index, err := Index(ctx, testCorpus(), hashEmbedder{dims: 512})
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	// A query whose words live in the résumé, restricted to the job description:
	// the filter must win over relevance, or the tool's `document` argument is a
	// lie and the model will cite the wrong source.
	results, err := index.Search(ctx, "Kubernetes migration", SearchOptions{Kind: corpus.KindJobDescription})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, r := range results {
		if r.Chunk.Kind != corpus.KindJobDescription {
			t.Errorf("filter leaked a %s chunk from %s", r.Chunk.Kind, r.Chunk.DocName)
		}
	}
}

func TestSearchEmptyQueryIsRejected(t *testing.T) {
	ctx := context.Background()
	index, err := Index(ctx, testCorpus(), hashEmbedder{dims: 512})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if _, err := index.Search(ctx, "   ", SearchOptions{}); err == nil {
		t.Error("expected an error for a blank query")
	}
}

func TestFormatSaysSoWhenNothingMatched(t *testing.T) {
	out := Format(nil)
	// The model must be able to distinguish "not in the documents" from "search
	// returned junk"; a silent empty string reads as the latter.
	if !strings.Contains(strings.ToLower(out), "no matching passages") {
		t.Errorf("empty result should say so explicitly, got: %q", out)
	}
}

func TestSearchToolValidatesInput(t *testing.T) {
	ctx := context.Background()
	index, err := Index(ctx, testCorpus(), hashEmbedder{dims: 512})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	tool := SearchTool(index)

	if !tool.Recall {
		t.Error("the search tool must be flagged Recall, or the preamble guard rail never fires")
	}
	if _, err := tool.Execute(ctx, []byte(`{}`)); err == nil {
		t.Error("expected an error when query is missing")
	}
	if _, err := tool.Execute(ctx, []byte(`{"query":"go","document":"cover_letter"}`)); err == nil {
		t.Error("expected an error for an unknown document filter")
	}
	out, err := tool.Execute(ctx, []byte(`{"query":"Kubernetes migration","top_k":2}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "resume.md") {
		t.Errorf("expected a cited source in the tool output, got:\n%s", out)
	}
}

func TestSearchBalancesAcrossDocuments(t *testing.T) {
	ctx := context.Background()
	index, err := Index(ctx, testCorpus(), hashEmbedder{dims: 512})
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	// "Golang gRPC" is job-description vocabulary; the résumé never uses those
	// words. Pure relevance ranking would return job-description passages only,
	// and the model would answer "do I match?" having never seen the résumé.
	results, err := index.Search(ctx, "senior golang engineer production grpc", SearchOptions{TopK: 4})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	seen := map[corpus.Kind]int{}
	for _, r := range results {
		seen[r.Chunk.Kind]++
	}
	if seen[corpus.KindResume] == 0 {
		t.Errorf("unfiltered search returned no résumé passages: %v", seen)
	}
	if seen[corpus.KindJobDescription] == 0 {
		t.Errorf("unfiltered search returned no job-description passages: %v", seen)
	}

	// Scores must stay descending after balancing, or the model reads the
	// weakest match first.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results out of score order at %d: %.3f > %.3f", i, results[i].Score, results[i-1].Score)
		}
	}
}

func TestSearchWithKindFilterIsNotBalanced(t *testing.T) {
	ctx := context.Background()
	index, err := Index(ctx, testCorpus(), hashEmbedder{dims: 512})
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	// An explicit filter is an instruction, not a hint: balancing must not
	// smuggle the other document back in.
	results, err := index.Search(ctx, "golang grpc", SearchOptions{TopK: 4, Kind: corpus.KindResume})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected résumé results")
	}
	for _, r := range results {
		if r.Chunk.Kind != corpus.KindResume {
			t.Errorf("filtered search leaked a %s passage", r.Chunk.Kind)
		}
	}
}

func TestBalanceHandlesThreeDocumentKinds(t *testing.T) {
	ctx := context.Background()
	c := testCorpus()
	// The delivered-project README is a third kind. The quota is topK/kinds, so
	// adding one must not silently starve the résumé — the failure mode would be
	// a fit answer that never sees the candidate's actual background.
	c.Documents = append(c.Documents, corpus.Document{
		Kind: corpus.KindProject,
		Name: "README.md",
		Text: `ARCHITECTURE

A Go service exposing gRPC streaming over Connect, with an in-process vector store.

Senior golang engineer production grpc retrieval augmented generation.`,
	})

	index, err := Index(ctx, c, hashEmbedder{dims: 512})
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	results, err := index.Search(ctx, "senior golang engineer production grpc", SearchOptions{TopK: 6})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	seen := map[corpus.Kind]int{}
	for _, r := range results {
		seen[r.Chunk.Kind]++
	}
	for _, want := range []corpus.Kind{corpus.KindResume, corpus.KindJobDescription, corpus.KindProject} {
		if seen[want] == 0 {
			t.Errorf("no %s passages in a three-kind search: %v", want, seen)
		}
	}
}
