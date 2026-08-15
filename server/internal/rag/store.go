package rag

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/expona-ai/lumi-go/server/internal/corpus"
)

// Store is an in-process vector index over the corpus.
//
// Search is a linear cosine scan. For a few dozen chunks that is microseconds
// and exact; an ANN index would add a dependency, a build step, and recall
// error in exchange for nothing measurable at this size. The shape to watch is
// corpus growth — past a few thousand chunks, swap the scan for a real index
// behind the same Search signature.
type Store struct {
	mu       sync.RWMutex
	chunks   []Chunk
	embedder Embedder
}

// Result is one retrieved passage and its similarity to the query.
type Result struct {
	Chunk Chunk
	Score float32
}

// Index embeds every chunk of the corpus and returns a searchable store. The
// whole corpus is embedded in one call — batching matters here because a cold
// model load dominates the cost of the first request.
func Index(ctx context.Context, c *corpus.Corpus, e Embedder) (*Store, error) {
	chunks := Chunks(c)
	if len(chunks) == 0 {
		return &Store{embedder: e}, nil
	}

	texts := make([]string, len(chunks))
	for i, ch := range chunks {
		// Prefix the passage with its provenance so the embedding reflects
		// which document and section it came from, not just the words in it.
		texts[i] = fmt.Sprintf("%s — %s\n\n%s", ch.DocName, ch.Heading, ch.Text)
	}

	vectors, err := e.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed corpus: %w", err)
	}
	for i := range chunks {
		chunks[i].Vector = vectors[i]
	}

	return &Store{chunks: chunks, embedder: e}, nil
}

// Len is the number of indexed chunks.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.chunks)
}

// Empty reports whether there is anything to search.
func (s *Store) Empty() bool { return s == nil || s.Len() == 0 }

// SearchOptions narrows a query.
type SearchOptions struct {
	// TopK results to return. Zero means DefaultTopK.
	TopK int
	// Kind restricts the search to one document role ("resume",
	// "job_description"). Empty searches everything.
	Kind corpus.Kind
}

// DefaultTopK is a deliberate compromise: enough passages that a cross-section
// question ("does my experience match their requirements") sees both sides,
// few enough that the model isn't handed the whole corpus and left to skim.
const DefaultTopK = 6

// Search embeds the query and returns the closest passages.
func (s *Store) Search(ctx context.Context, query string, opts SearchOptions) ([]Result, error) {
	if s.Empty() {
		return nil, nil
	}
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is empty")
	}

	topK := opts.TopK
	if topK <= 0 {
		topK = DefaultTopK
	}

	vecs, err := s.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("embed query: got %d vectors", len(vecs))
	}
	q := vecs[0]

	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]Result, 0, len(s.chunks))
	for _, ch := range s.chunks {
		if opts.Kind != "" && ch.Kind != opts.Kind {
			continue
		}
		results = append(results, Result{Chunk: ch, Score: dot(q, ch.Vector)})
	}

	sort.SliceStable(results, func(i, j int) bool { return results[i].Score > results[j].Score })

	if opts.Kind == "" {
		return balance(results, topK), nil
	}
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

// balance guarantees each document kind a share of an unfiltered result set.
//
// Without this, one document wins every slot on shared vocabulary: a job
// posting saturated with "Go" beats a résumé that lists Go once, so the
// question "do I have the Go experience they want?" retrieves only the demand
// side and never the evidence. Pure relevance ranking is the wrong objective
// when the user's question spans two documents — the comparison needs both
// halves in front of the model, not the six best-matching passages.
//
// Each kind present gets an equal quota of the top-k, filled in score order;
// any quota a kind cannot fill is redistributed to the rest by score.
func balance(ranked []Result, topK int) []Result {
	kinds := make(map[corpus.Kind]bool)
	for _, r := range ranked {
		kinds[r.Chunk.Kind] = true
	}
	if len(kinds) < 2 || len(ranked) <= topK {
		if len(ranked) > topK {
			return ranked[:topK]
		}
		return ranked
	}

	quota := topK / len(kinds)
	if quota < 1 {
		quota = 1
	}

	taken := make(map[corpus.Kind]int, len(kinds))
	chosen := make([]Result, 0, topK)
	picked := make(map[string]bool, topK)

	for _, r := range ranked { // ranked is already score-ordered
		if len(chosen) == topK {
			break
		}
		if taken[r.Chunk.Kind] < quota {
			chosen = append(chosen, r)
			picked[r.Chunk.ID] = true
			taken[r.Chunk.Kind]++
		}
	}
	// Fill whatever the quotas left over with the best remaining passages,
	// whichever document they come from.
	for _, r := range ranked {
		if len(chosen) == topK {
			break
		}
		if !picked[r.Chunk.ID] {
			chosen = append(chosen, r)
			picked[r.Chunk.ID] = true
		}
	}

	sort.SliceStable(chosen, func(i, j int) bool { return chosen[i].Score > chosen[j].Score })
	return chosen
}

// dot is cosine similarity for unit vectors (both sides are normalized on
// creation). Mismatched dimensions score zero rather than panicking — that only
// happens if the index and the query were embedded by different models.
func dot(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var sum float32
	for i := range a {
		sum += a[i] * b[i]
	}
	return sum
}

// Format renders results for the model: provenance first, then the passage, so
// a citation is available for every claim built on it.
func Format(results []Result) string {
	if len(results) == 0 {
		return "No matching passages. The documents may not cover this — say so rather than guessing."
	}
	var b strings.Builder
	for i, r := range results {
		heading := r.Chunk.Heading
		if heading == "" {
			heading = "(no section heading)"
		}
		fmt.Fprintf(&b, "[%d] %s · %s · relevance %.2f\n%s\n\n",
			i+1, r.Chunk.DocName, heading, r.Score, r.Chunk.Text)
	}
	return strings.TrimSpace(b.String())
}
