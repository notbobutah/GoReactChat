package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"
)

// Embedder turns text into vectors. The interface exists so the store, the
// tool, and the tests never depend on which model produced the numbers — the
// only requirement is that queries and documents go through the same one.
type Embedder interface {
	// Embed returns one vector per input, in order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Model identifies the embedding model, for the boot log and for a future
	// persisted index that must refuse to load vectors from another model.
	Model() string
}

// OllamaEmbedder calls a local Ollama server. Nothing leaves the machine and
// there is no API key: the résumé is personal data, and this keeps it on disk
// under the user's control rather than in a vendor's request logs.
type OllamaEmbedder struct {
	Host string
	Name string
	HTTP *http.Client
}

// DefaultOllamaHost is where `ollama serve` listens.
const DefaultOllamaHost = "http://localhost:11434"

// DefaultEmbedModel is a small, strong retrieval model (768 dims, ~274MB).
const DefaultEmbedModel = "nomic-embed-text"

func NewOllamaEmbedder(host, model string) *OllamaEmbedder {
	if host == "" {
		host = DefaultOllamaHost
	}
	if model == "" {
		model = DefaultEmbedModel
	}
	return &OllamaEmbedder{
		Host: host,
		Name: model,
		// Embedding a whole corpus is one call; a cold model load can take a
		// few seconds, so this is generous rather than snappy.
		HTTP: &http.Client{Timeout: 120 * time.Second},
	}
}

func (e *OllamaEmbedder) Model() string { return e.Name }

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error,omitempty"`
}

func (e *OllamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(ollamaEmbedRequest{Model: e.Name, Input: texts})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Host+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()

	var out ollamaEmbedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("ollama embed: decode response (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := out.Error
		if msg == "" {
			msg = resp.Status
		}
		// The common case is a model that was never pulled; say so plainly.
		return nil, fmt.Errorf("ollama embed: %s (is `ollama pull %s` done?)", msg, e.Name)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embed: got %d vectors for %d inputs", len(out.Embeddings), len(texts))
	}

	for i := range out.Embeddings {
		normalize(out.Embeddings[i])
	}
	return out.Embeddings, nil
}

// Available reports whether the embedder can actually be used, so boot can fall
// back to inlining the documents instead of failing or, worse, starting with a
// retrieval tool that errors on every call.
func (e *OllamaEmbedder) Available(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.Host+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("ollama unreachable at %s: %w", e.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama at %s returned %s", e.Host, resp.Status)
	}
	return nil
}

// normalize scales a vector to unit length in place, so cosine similarity
// reduces to a dot product at query time.
func normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(sum))
	for i := range v {
		v[i] *= inv
	}
}
