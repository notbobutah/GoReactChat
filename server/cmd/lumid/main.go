// Command lumid is the lumi-go chat backend.
//
// It serves one Connect handler that speaks gRPC, gRPC-Web and the Connect
// protocol on a single port: the browser talks Connect (server streaming over
// fetch, no proxy), other backend services can dial it as native gRPC.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/expona-ai/lumi-go/server/gen/lumi/chat/v1/chatv1connect"
	"github.com/expona-ai/lumi-go/server/internal/auth"
	"github.com/expona-ai/lumi-go/server/internal/budget"
	"github.com/expona-ai/lumi-go/server/internal/chat"
	"github.com/expona-ai/lumi-go/server/internal/config"
	"github.com/expona-ai/lumi-go/server/internal/corpus"
	"github.com/expona-ai/lumi-go/server/internal/model"
	"github.com/expona-ai/lumi-go/server/internal/orchestrator"
	"github.com/expona-ai/lumi-go/server/internal/rag"
	"github.com/expona-ai/lumi-go/server/internal/reference"
	"github.com/expona-ai/lumi-go/server/internal/service"
	"github.com/expona-ai/lumi-go/server/internal/store"
)

// version is stamped at build time (-ldflags "-X main.version=…") and logged at
// boot, so a running container can be traced back to the commit that built it.
var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- store ---------------------------------------------------------------
	var st store.Store
	switch cfg.StoreKind {
	case "memory":
		logger.Warn("STORE=memory — conversations are lost on restart; not a production configuration")
		st = store.NewMemoryStore()
	default:
		pg, err := store.NewPostgresStore(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		st = pg
	}
	defer st.Close()

	// --- model client --------------------------------------------------------
	modelConfig := orchestrator.ModelConfig{Strong: cfg.StrongModel, Fast: cfg.FastModel}
	var client orchestrator.StreamingClient
	if cfg.ModelClient == "echo" {
		logger.Warn("MODEL_CLIENT=echo — replies are canned; no model is called")
		client = &model.EchoClient{Delay: 25 * time.Millisecond}
	} else {
		client = model.NewAnthropicClient(cfg.AnthropicKey, cfg.Effort)
	}

	// Two limiters: one per caller so a single visitor cannot take the whole
	// allowance, and one shared so several visitors together still cannot. With
	// one or two readers the global limit is the one that binds.
	rateLimiter := orchestrator.NewCompositeRateLimiter(
		orchestrator.NewWindowRateLimiter(cfg.RateLimit, cfg.RateLimitWin),
		orchestrator.NewFixedKeyRateLimiter(
			orchestrator.NewWindowRateLimiter(cfg.GlobalRateLimit, cfg.RateLimitWin)),
	)

	// Service-wide spend ceiling. Rate limiting bounds how fast tokens go; this
	// bounds how many exist. Persisted through the store when there is one, so a
	// restart cannot hand out a fresh budget.
	var budgetStore budget.Store
	if pg, ok := st.(*store.PostgresStore); ok {
		budgetStore = pg
	}
	tokens, err := budget.New(ctx, cfg.TokenBudget, budgetStore, logger)
	if err != nil {
		return err
	}
	if used, limit := tokens.Used(); limit > 0 {
		logger.Info("token budget", "used", used, "limit", limit,
			"remaining", limit-used, "persisted", budgetStore != nil)
	} else {
		logger.Warn("TOKEN_BUDGET is unset — total spend is uncapped")
	}

	// --- grounding documents -------------------------------------------------
	// The résumé and job description are small and relevant to every turn, so
	// they are inlined in the system prompt rather than fetched by a tool. A
	// missing data directory is not fatal — the service falls back to the
	// generic prompt so an offline run and a container with nothing mounted
	// both still work.
	docs, skipped, err := corpus.Load(cfg.DataDir)
	if err != nil {
		return err
	}
	for _, s := range skipped {
		logger.Warn("skipped document", "detail", s, "dir", cfg.DataDir)
	}
	// The project README is evidence of delivered work, indexed with the
	// candidate's documents rather than with the external reference material:
	// unlike the Go docs, it describes something this candidate actually built.
	if doc, err := corpus.LoadFile(cfg.ProjectDoc, corpus.KindProject,
		"lumi-go README (codebase the candidate designed and shipped)"); err != nil {
		logger.Warn("project document unreadable — continuing without it",
			"path", cfg.ProjectDoc, "error", err)
	} else if doc != nil {
		docs.Add(*doc)
	}

	if docs.Empty() {
		logger.Warn("no grounding documents loaded — using the generic assistant prompt",
			"data_dir", cfg.DataDir)
	} else {
		logger.Info("grounding documents loaded", "data_dir", cfg.DataDir, "documents", docs.Summary())
	}

	// --- retrieval -----------------------------------------------------------
	// Documents are chunked, embedded locally via Ollama, and searched in
	// process. Nothing leaves the machine: the résumé is personal data, and a
	// local embedder keeps it off a vendor's servers.
	//
	// If the embedder is unreachable, fall back to inlining the documents in the
	// system prompt rather than starting with a retrieval tool that errors on
	// every call. The corpus is small enough that inlining is a legitimate
	// fallback, not a degraded one — it costs tokens, not answers.
	systemPrompt := docs.SystemPrompt()
	var tools []orchestrator.ToolDef

	if cfg.RAGEnabled && !docs.Empty() {
		ollama := rag.NewOllamaEmbedder(cfg.OllamaHost, cfg.EmbedModel)
		// Vectors are cached on disk between runs. The Go documentation is a
		// thousand-odd passages that never change between restarts; without
		// this, every boot re-embeds them for nothing.
		cache := rag.LoadEmbeddingCache(cfg.EmbedCacheDir, ollama.Model())
		embedder := &rag.CachedEmbedder{Inner: ollama, Cache: cache}
		defer func() {
			if err := cache.Save(); err != nil {
				logger.Warn("embedding cache not saved — next boot will re-embed", "error", err)
			}
		}()

		if err := ollama.Available(ctx); err != nil {
			logger.Warn("retrieval disabled — falling back to inlining the documents in the prompt",
				"error", err, "hint", "start `ollama serve` and run `ollama pull "+ollama.Model()+"`")
		} else {
			indexCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			index, err := rag.Index(indexCtx, docs, embedder)
			cancel()
			switch {
			case err != nil:
				logger.Warn("indexing failed — falling back to inlining the documents in the prompt",
					"error", err)
			case index.Empty():
				logger.Warn("index is empty — falling back to inlining the documents in the prompt")
			default:
				tools = append(tools, rag.SearchTool(index))

				// External reference material (the Go documentation) gets its
				// own index and its own tool. Keeping it out of the candidate's
				// index is what stops a passage from the spec being cited as
				// the candidate's experience.
				refPrompt := ""
				if refs, err := reference.Load(cfg.DataDir); err != nil {
					logger.Warn("reference documents unreadable — continuing without them", "error", err)
				} else if !refs.Empty() {
					refIndex, err := indexReference(ctx, refs, embedder)
					if err != nil {
						logger.Warn("reference indexing failed — continuing without it", "error", err)
					} else {
						tools = append(tools, reference.SearchTool(refIndex))
						refPrompt = reference.PromptSection(refs)
						logger.Info("reference index built",
							"chunks", refIndex.Len(), "documents", reference.Summary(refs))
					}
				} else {
					logger.Info("no reference documents cached — run `make refs` to enable Go documentation search",
						"dir", reference.Dir(cfg.DataDir))
				}

				systemPrompt = rag.SystemPrompt(docs, index, refPrompt)
				logger.Info("retrieval index built",
					"chunks", index.Len(), "embed_model", ollama.Model(),
					"host", cfg.OllamaHost, "cached_vectors", cache.Len())
			}
		}
	}

	deps := chat.Deps{
		Store:        st,
		Client:       client,
		RateLimiter:  rateLimiter,
		ModelConfig:  modelConfig,
		Budget:       tokens,
		SystemPrompt: systemPrompt,
		Tools:        tools,
		Logger:       logger,
	}
	// The echo client would "title" a conversation with its own canned reply.
	// Leaving the generator unwired falls back to the trimmed first message,
	// which is what the rail should show in a dev run.
	if cfg.ModelClient != "echo" {
		deps.TitleGenerator = chat.NewTitleGenerator(client, rateLimiter, modelConfig)
	}
	session := chat.NewSession(deps)

	// --- auth ----------------------------------------------------------------
	// Dev verifier: the bearer token IS the user id. The seam is what matters —
	// the real verifier drops in here without touching anything downstream.
	logger.Warn("AUTH_MODE=dev — the bearer token is trusted as the user id; never run this outside local development")
	verifier := &auth.DevBearerVerifier{DefaultWorkspaceID: cfg.DefaultWorkspaceID}

	// --- http ----------------------------------------------------------------
	mux := http.NewServeMux()
	path, handler := chatv1connect.NewChatServiceHandler(
		service.NewChatService(st, session, logger),
		connect.WithInterceptors(auth.NewInterceptor(verifier)),
	)
	mux.Handle(path, handler)

	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{
		Addr: cfg.Addr(),
		// h2c so a local gRPC client can reach us over plaintext HTTP/2 while
		// the browser uses HTTP/1.1. In production TLS terminates upstream.
		Handler:           h2c.NewHandler(withCORS(mux, cfg.AllowedOrigins), &http2.Server{}),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: a turn is a long-lived stream and would be cut off.
		IdleTimeout: 120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening",
			"version", version, "addr", cfg.Addr(), "rpc_path", path,
			"store", cfg.StoreKind, "model_client", cfg.ModelClient,
			"rate_limit_per_user", cfg.RateLimit, "rate_limit_global", cfg.GlobalRateLimit,
			"strong_model", cfg.StrongModel, "effort", cfg.Effort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// withCORS lets the Next.js dev server (a different origin) call the service
// from the browser. Connect sends its own protocol headers, so the allow-list
// has to name them explicitly; Grpc-Status/Grpc-Message must be exposed or the
// client can't read an error.
func withCORS(next http.Handler, origins []string) http.Handler {
	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[o] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if _, ok := allowed[origin]; ok {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			h.Set("Access-Control-Allow-Headers", strings.Join([]string{
				"Content-Type", "Authorization",
				"Connect-Protocol-Version", "Connect-Timeout-Ms", "Connect-Accept-Encoding", "Connect-Content-Encoding",
				"Grpc-Timeout", "X-Grpc-Web", "X-User-Agent",
				"X-Workspace-Id", "X-Lumi-Surface",
			}, ", "))
			h.Set("Access-Control-Expose-Headers", strings.Join([]string{
				"Grpc-Status", "Grpc-Message", "Grpc-Status-Details-Bin",
				"Connect-Content-Encoding", "Connect-Accept-Encoding",
			}, ", "))
			h.Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// indexReference embeds the reference corpus. It gets a longer budget than the
// candidate's documents: the Go specification alone is a few hundred kilobytes,
// so this is hundreds of passages rather than dozens.
func indexReference(ctx context.Context, refs *corpus.Corpus, e rag.Embedder) (*rag.Store, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	return rag.Index(ctx, refs, e)
}
