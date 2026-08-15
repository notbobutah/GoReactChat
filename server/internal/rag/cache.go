package rag

import (
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// EmbeddingCache persists vectors between runs.
//
// Embedding the Go documentation takes ~15s for a thousand-odd passages, and
// the documentation does not change between restarts — so without a cache that
// cost is paid on every boot for nothing. Entries are keyed by the content hash
// AND the model name: vectors from two different models are not comparable, so
// changing EMBED_MODEL must miss rather than silently mix vector spaces.
type EmbeddingCache struct {
	path  string
	model string

	mu      sync.Mutex
	vectors map[string][]float32
	dirty   bool
}

// cacheFile is the on-disk shape. Versioned so a format change invalidates
// rather than mis-parses.
type cacheFile struct {
	Version int
	Model   string
	Vectors map[string][]float32
}

const cacheVersion = 1

// LoadEmbeddingCache reads the cache for a model, returning an empty one when
// the file is absent, unreadable, or written by a different model or version.
// A cache is an optimisation: a bad one is discarded, never fatal.
func LoadEmbeddingCache(dir, model string) *EmbeddingCache {
	c := &EmbeddingCache{
		path:    filepath.Join(dir, fmt.Sprintf("embeddings-%s.gob", sanitize(model))),
		model:   model,
		vectors: map[string][]float32{},
	}

	f, err := os.Open(c.path)
	if err != nil {
		return c
	}
	defer f.Close()

	var stored cacheFile
	if err := gob.NewDecoder(f).Decode(&stored); err != nil {
		return c
	}
	if stored.Version != cacheVersion || stored.Model != model {
		return c
	}
	if stored.Vectors != nil {
		c.vectors = stored.Vectors
	}
	return c
}

// Len is the number of cached vectors.
func (c *EmbeddingCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.vectors)
}

// Save writes the cache if anything changed. Errors are returned for logging,
// not for failing a boot — the process runs fine with a cold cache.
func (c *EmbeddingCache) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	// Write-then-rename so a crash mid-write cannot leave a torn cache that the
	// next boot would have to detect.
	tmp := c.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := gob.NewEncoder(f).Encode(cacheFile{
		Version: cacheVersion, Model: c.model, Vectors: c.vectors,
	}); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		return err
	}
	c.dirty = false
	return nil
}

func (c *EmbeddingCache) get(text string) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.vectors[key(c.model, text)]
	return v, ok
}

func (c *EmbeddingCache) put(text string, v []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.vectors[key(c.model, text)] = v
	c.dirty = true
}

func key(model, text string) string {
	h := sha256.Sum256([]byte(model + "\x00" + text))
	return hex.EncodeToString(h[:])
}

func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}

// CachedEmbedder wraps an Embedder with a persistent cache. Cache misses go to
// the wrapped embedder in one batch, so a partially-warm cache still makes a
// single request rather than one per miss.
type CachedEmbedder struct {
	Inner Embedder
	Cache *EmbeddingCache
}

var _ Embedder = (*CachedEmbedder)(nil)

func (c *CachedEmbedder) Model() string { return c.Inner.Model() }

func (c *CachedEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	var missIdx []int
	var missTexts []string

	for i, t := range texts {
		if v, ok := c.Cache.get(t); ok {
			out[i] = v
			continue
		}
		missIdx = append(missIdx, i)
		missTexts = append(missTexts, t)
	}

	if len(missTexts) > 0 {
		vecs, err := c.Inner.Embed(ctx, missTexts)
		if err != nil {
			return nil, err
		}
		if len(vecs) != len(missTexts) {
			return nil, fmt.Errorf("embedder returned %d vectors for %d inputs", len(vecs), len(missTexts))
		}
		for j, v := range vecs {
			out[missIdx[j]] = v
			c.Cache.put(missTexts[j], v)
		}
	}
	return out, nil
}

// Misses reports how many of these texts are not cached — used to log whether a
// boot is warm or cold.
func (c *CachedEmbedder) Misses(texts []string) int {
	n := 0
	for _, t := range texts {
		if _, ok := c.Cache.get(t); !ok {
			n++
		}
	}
	return n
}
