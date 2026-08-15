package rag

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/expona-ai/lumi-go/server/internal/corpus"
)

// TestLiveRetrieval exercises the real path — actual documents, actual Ollama,
// actual embeddings — and prints what comes back for a set of questions a
// candidate would genuinely ask. It skips unless both are present, so `go test
// ./...` stays green on a machine with neither.
//
// Run it when tuning chunking or swapping the embedding model:
//
//	go test ./internal/rag -run TestLiveRetrieval -v
func TestLiveRetrieval(t *testing.T) {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "../../../data"
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Skipf("no data directory at %s", dataDir)
	}

	ctx := context.Background()
	embedder := NewOllamaEmbedder(os.Getenv("OLLAMA_HOST"), os.Getenv("EMBED_MODEL"))
	if err := embedder.Available(ctx); err != nil {
		t.Skipf("ollama unavailable: %v", err)
	}

	docs, skipped, err := corpus.Load(dataDir)
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	for _, s := range skipped {
		t.Logf("skipped: %s", s)
	}
	// Mirror what the server indexes: the project README is loaded from the repo
	// root, not from the data directory. Without it this check would pass while
	// the running service retrieved a different corpus.
	if doc, err := corpus.LoadFile(filepath.Join(dataDir, "..", "README.md"),
		corpus.KindProject, "lumi-go README (delivered project)"); err != nil {
		t.Logf("project document: %v", err)
	} else if doc != nil {
		docs.Add(*doc)
	}

	if docs.Empty() {
		t.Skip("no documents loaded")
	}

	index, err := Index(ctx, docs, embedder)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	t.Logf("indexed %d chunks with %s", index.Len(), embedder.Model())

	for _, q := range []string{
		"Go and Golang production experience",
		"what does this role require",
		"agentic AI, LLM orchestration, vector databases",
		"years of experience and seniority",
		"contract length, rate, and location",
		"what has the candidate built in Go",
		"gRPC streaming service architecture",
	} {
		results, err := index.Search(ctx, q, SearchOptions{TopK: 3})
		if err != nil {
			t.Fatalf("search %q: %v", q, err)
		}
		if len(results) == 0 {
			t.Errorf("no results for %q — retrieval is not usable", q)
			continue
		}
		t.Logf("\nQUERY: %s", q)
		for _, r := range results {
			t.Logf("  %.3f  %-34s  %-24s  %s",
				r.Score, truncate(r.Chunk.DocName, 34), truncate(r.Chunk.Heading, 24),
				truncate(strings.ReplaceAll(r.Chunk.Text, "\n", " "), 70))
		}
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
