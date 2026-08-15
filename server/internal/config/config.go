// Package config is the single source of truth for runtime configuration.
// Load fails fast with a list of what's missing rather than surfacing a
// half-configured process on the first chat turn.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type AuthMode string

const (
	// AuthModeDev treats the bearer token as the user id. Local only.
	AuthModeDev AuthMode = "dev"
	// AuthModeProd verifies real Expona-minted tokens. Not yet implemented —
	// the verifier seam is in place (internal/auth), the verifier itself lands
	// with the token contract.
	AuthModeProd AuthMode = "prod"
)

type Config struct {
	Host string
	Port int

	AuthMode           AuthMode
	DefaultWorkspaceID string

	// "postgres" (default) or "memory".
	StoreKind   string
	DatabaseURL string

	// Directory of grounding documents (résumé, job description) loaded at boot.
	// Absent or empty → the generic prompt.
	DataDir string

	// Retrieval. When enabled, documents are chunked, embedded via a local
	// Ollama instance and searched in process; the model reaches them through a
	// tool instead of reading them inline. Falls back to inlining when the
	// embedder is unreachable.
	RAGEnabled bool
	OllamaHost string
	EmbedModel string
	// Where embedding vectors are cached between runs. Set to "" to disable.
	EmbedCacheDir string

	// A README describing work the candidate delivered, indexed alongside their
	// documents so the codebase is citable evidence. Empty or missing → skipped.
	ProjectDoc string

	// "anthropic" (default) or "echo".
	ModelClient    string
	AnthropicKey   string
	StrongModel    string
	FastModel      string
	Effort         string
	RateLimit      int
	RateLimitWin   time.Duration
	AllowedOrigins []string
}

// Load reads the environment. It does not read .env files — use `make dev`,
// direnv, or your process manager to inject them, so there's exactly one place
// configuration can come from in production.
func (c *Config) validate() error {
	var missing []string

	if c.StoreKind == "postgres" && c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL (required when STORE=postgres)")
	}
	if c.ModelClient == "anthropic" && c.AnthropicKey == "" {
		missing = append(missing, "ANTHROPIC_API_KEY (required when MODEL_CLIENT=anthropic)")
	}
	if c.AuthMode == AuthModeProd {
		return fmt.Errorf("AUTH_MODE=prod is not implemented yet — the production verifier lands with the token contract; use AUTH_MODE=dev locally")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return nil
}

func Load() (*Config, error) {
	c := &Config{
		Host:               env("HOST", "127.0.0.1"),
		Port:               envInt("PORT", 8080),
		AuthMode:           AuthMode(env("AUTH_MODE", string(AuthModeDev))),
		DefaultWorkspaceID: env("DEFAULT_WORKSPACE_ID", "local-workspace"),
		StoreKind:          env("STORE", "postgres"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		DataDir:            env("DATA_DIR", "../data"),
		RAGEnabled:         env("RAG", "on") != "off",
		OllamaHost:         env("OLLAMA_HOST", "http://localhost:11434"),
		EmbedModel:         env("EMBED_MODEL", "nomic-embed-text"),
		EmbedCacheDir:      env("EMBED_CACHE_DIR", "../data/.embeddings"),
		ProjectDoc:         env("PROJECT_DOC", "../README.md"),
		ModelClient:        env("MODEL_CLIENT", "anthropic"),
		AnthropicKey:       os.Getenv("ANTHROPIC_API_KEY"),
		StrongModel:        env("STRONG_MODEL", "claude-opus-5"),
		FastModel:          env("FAST_MODEL", "claude-haiku-4-5"),
		Effort:             env("EFFORT", "medium"),
		RateLimit:          envInt("RATE_LIMIT", 60),
		RateLimitWin:       time.Duration(envInt("RATE_LIMIT_WINDOW_SECONDS", 60)) * time.Second,
		AllowedOrigins:     splitList(env("ALLOWED_ORIGINS", "http://localhost:3000")),
	}

	switch c.AuthMode {
	case AuthModeDev, AuthModeProd:
	default:
		return nil, fmt.Errorf("AUTH_MODE must be 'dev' or 'prod', got %q", c.AuthMode)
	}
	switch c.StoreKind {
	case "postgres", "memory":
	default:
		return nil, fmt.Errorf("STORE must be 'postgres' or 'memory', got %q", c.StoreKind)
	}
	switch c.ModelClient {
	case "anthropic", "echo":
	default:
		return nil, fmt.Errorf("MODEL_CLIENT must be 'anthropic' or 'echo', got %q", c.ModelClient)
	}
	if c.Port <= 0 || c.Port > 65535 {
		return nil, fmt.Errorf("PORT must be in [1, 65535], got %d", c.Port)
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) Addr() string { return fmt.Sprintf("%s:%d", c.Host, c.Port) }

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
