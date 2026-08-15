package reference

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/expona-ai/lumi-go/server/internal/rag"
)

// TestLiveReferenceRetrieval checks that the Go documentation index answers Go
// questions — the reason it is indexed at all. Skips without a cache or Ollama.
func TestLiveReferenceRetrieval(t *testing.T) {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "../../../data"
	}

	ctx := context.Background()
	embedder := rag.NewOllamaEmbedder(os.Getenv("OLLAMA_HOST"), os.Getenv("EMBED_MODEL"))
	if err := embedder.Available(ctx); err != nil {
		t.Skipf("ollama unavailable: %v", err)
	}

	refs, err := Load(dataDir)
	if err != nil {
		t.Fatalf("load reference: %v", err)
	}
	if refs.Empty() {
		t.Skip("no reference cache — run `make refs`")
	}

	// Use the same on-disk cache the server uses, so this test is fast after the
	// first run rather than re-embedding a thousand passages every time.
	cached := &rag.CachedEmbedder{
		Inner: embedder,
		Cache: rag.LoadEmbeddingCache(filepath.Join(dataDir, ".embeddings"), embedder.Model()),
	}
	index, err := rag.Index(ctx, refs, cached)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if err := cached.Cache.Save(); err != nil {
		t.Logf("cache save: %v", err)
	}
	t.Logf("indexed %d reference passages", index.Len())

	for _, q := range []string{
		"how do channels behave when closed",
		"happens before guarantees for goroutines",
		"error handling idiom wrapping errors",
		"generics type parameter constraints",
	} {
		results, err := index.Search(ctx, q, rag.SearchOptions{TopK: 2})
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		if len(results) == 0 {
			t.Errorf("no results for %q", q)
			continue
		}
		t.Logf("\nQUERY: %s", q)
		for _, r := range results {
			t.Logf("  %.3f  %-44s  %s",
				r.Score, truncate(r.Chunk.DocName, 44),
				truncate(strings.ReplaceAll(r.Chunk.Text, "\n", " "), 76))
		}
	}
}

// truncate shortens a string for log output without splitting a rune.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
