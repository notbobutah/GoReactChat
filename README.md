# lumi-go

A derivative of [`lumi-neo`](../lumi-neo) that keeps the chat behaviour and
replaces the transport and the runtime: **Go backend, gRPC streaming, React
(Next.js) client**.

The difference that motivates the project: in the Next-based stack the chat API
is integrated into the Next app and written in TypeScript. Here the Next app is
a pure client — every byte of chat logic lives in the Go service, and the
browser reaches it over the contract in `proto/`.

---

## 1. Build environment

### Toolchain

| Tool | Version here | Why it's needed |
|---|---|---|
| Go | 1.26.6 | The backend. Installed at `~/.local/go` (no root needed). |
| buf | 1.72.0 | Compiles and lints protos, drives codegen. Replaces `protoc` — no C++ toolchain. |
| protoc-gen-go | latest | Generates Go message types. |
| protoc-gen-connect-go | latest | Generates the Connect handler + client. |
| protoc-gen-es | 2.14 (npm) | Generates TypeScript types **and** the service descriptor. |
| Node | 24.16.0 | Builds and serves the Next client. |
| npm | 11.13.0 | Client dependency management. |
| Docker | 28.2.2 | Local Postgres via `docker compose`. |
| psql | 16.13 | Applies migrations (through the container). |

Go tools live in `~/go/bin`, the Go SDK in `~/.local/go/bin`. Both are on
`PATH` via `~/.bashrc`:

```bash
export PATH=$PATH:/usr/local/go/bin:$HOME/.local/go/bin:$GOPATH/bin:/snap/bin
```

### Installing from scratch

```bash
# Go SDK (no sudo required)
curl -sSLO https://go.dev/dl/go1.26.6.linux-amd64.tar.gz
tar -C ~/.local -xzf go1.26.6.linux-amd64.tar.gz
export PATH=$PATH:$HOME/.local/go/bin:$HOME/go/bin

# codegen toolchain
go install github.com/bufbuild/buf/cmd/buf@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install connectrpc.com/connect/cmd/protoc-gen-connect-go@latest

# backend + client dependencies
cd server && go mod download && cd ..
cd web && npm install && cd ..
```

Verify the environment before touching code:

```bash
buf lint          # proto contract
make build        # go build ./...
make vet          # go vet ./...
make web-build    # next build (includes a full TypeScript check)
```

### Runtime dependencies

| Dependency | Version | Role |
|---|---|---|
| `connectrpc.com/connect` | v1.20.0 | Serves gRPC + gRPC-Web + Connect from one handler. |
| `github.com/anthropics/anthropic-sdk-go` | v1.63.1 | Streaming model calls. |
| `github.com/jackc/pgx/v5` | v5.10.0 | Postgres driver + pool. |
| `google.golang.org/protobuf` | v1.36.12 | Generated message runtime. |
| `golang.org/x/net` | v0.58.0 | `h2c` — plaintext HTTP/2 for local gRPC clients. |
| `@connectrpc/connect-web` | 2.1.2 | Browser transport (streaming over `fetch`). |
| `@bufbuild/protobuf` | 2.14 | Generated TypeScript runtime. |
| Next.js / React | 16.3.1 / 19.2.8 | Client app (App Router, Tailwind v4). |
| Postgres | 16-alpine | Conversation + message storage. |

### Ports

| Port | Process | Notes |
|---|---|---|
| 8080 | Go service | RPC + `GET /health`. Configurable via `PORT`. |
| 3000 | Next dev server | Origin must appear in `ALLOWED_ORIGINS`. |
| 5433 | Postgres (Docker) | Deliberately not 5432, so it can't collide with a local Postgres. |

---

## 2. Code generation

`proto/lumi/chat/v1/chat.proto` is the single source of truth. `buf.gen.yaml`
fans it out to both languages in one pass:

```
proto/lumi/chat/v1/chat.proto
        │
        ├── protoc-gen-go          ──▶ server/gen/lumi/chat/v1/chat.pb.go
        ├── protoc-gen-connect-go  ──▶ server/gen/lumi/chat/v1/chatv1connect/chat.connect.go
        └── protoc-gen-es          ──▶ web/src/gen/lumi/chat/v1/chat_pb.ts
```

```bash
make generate     # regenerate both sides
make lint-proto   # buf lint
```

Generated code is **committed**, so a fresh clone builds without the codegen
toolchain installed. Never hand-edit anything under `gen/` — change the proto
and regenerate. Because both sides come from the same file, a contract change
that breaks the client is a compile error rather than a runtime surprise.

Connect-ES v2 consumes the protobuf-es output directly, so there is no separate
TypeScript service generator — `ChatService` in `chat_pb.ts` is both the type
and the descriptor `createClient` needs.

`buf.yaml` disables one lint rule, `RPC_RESPONSE_STANDARD_NAME`: `SendMessage`
streams a union of chat events, and naming that `SendMessageResponse` would
describe the RPC shape at the cost of describing what the client receives.

---

## 3. Repository layout

```
lumi-go/
├── proto/lumi/chat/v1/chat.proto   the wire contract
├── buf.yaml  buf.gen.yaml          codegen + lint config
├── data/                           résumé + job description (grounding, §5)
│   └── reference/                  fetched Go docs (gitignored, `make refs`)
├── migrations/0001_init.sql        lumi.conversations, lumi.messages
├── docker-compose.yml              local Postgres on :5433
├── Makefile                        every workflow command
├── .github/workflows/
│   └── publish-image.yml           build + push to ghcr.io
├── server/
│   ├── Dockerfile  .dockerignore   backend image (build context is server/)
│   ├── cmd/lumid/main.go           composition root
│   ├── cmd/pdf2md/main.go          PDF résumé → structured markdown
│   ├── cmd/fetchrefs/main.go       tip.golang.org → cached plain text
│   ├── gen/                        generated (Go)
│   └── internal/
│       ├── config/                 env parsing, fail-fast validation
│       ├── auth/                   Verifier seam + Connect interceptor
│       ├── service/                generated-handler adapter
│       ├── corpus/                 document loading (pdf, eml, md) + prompt
│       ├── rag/                    chunking, embedding, vector search, cache
│       ├── reference/              Go documentation corpus + its own tool
│       ├── chat/                   one turn: session.go, title.go
│       ├── orchestrator/           loop.go, types.go, models.go, ratelimit.go
│       ├── model/                  anthropic.go, echo.go
│       └── store/                  store.go, postgres.go, memory.go
└── web/
    └── src/
        ├── gen/                    generated (TypeScript)
        ├── lib/chat-client.ts      transport, auth headers, conversation id
        ├── components/chat.tsx     the chat surface
        └── app/page.tsx
```

---

## 4. Architecture

### Shape

```
browser (Next.js)                          Go service (:8080)
┌──────────────────────┐                  ┌──────────────────────────────────┐
│ components/chat.tsx  │                  │ h2c ▸ CORS ▸ connect handler     │
│ lib/chat-client.ts   │ ── Connect ────▶ │   └─ auth.Interceptor            │
│  @connectrpc/        │   HTTP/1.1       │        └─ service.ChatService    │
│  connect-web         │   or HTTP/2      │             └─ chat.Session      │
└──────────────────────┘                  │                  ├─ orchestrator │
                                          │                  ├─ model        │
   other Go services ── native gRPC ────▶ │                  └─ store        │
                                          └──────────────────────────────────┘
```

One `http.Handler` speaks **gRPC, gRPC-Web and Connect** at once. The browser
uses the Connect protocol, which gives real server streaming over `fetch` with
no proxy and no sidecar; a backend service can dial the same address as
ordinary gRPC. `h2c` is wired so a local gRPC client works over plaintext
HTTP/2 — in production TLS terminates upstream.

### Dependency direction

Dependencies point inward, and every arrow crosses an interface:

```
cmd/lumid ─▶ config, auth, service, chat, orchestrator, model, store
service   ─▶ chat, store, auth, gen            (translation only)
chat      ─▶ orchestrator, store
orchestrator ─▶ (nothing internal)             defines StreamingClient, Store is not visible here
model     ─▶ orchestrator                      implements StreamingClient
store     ─▶ (nothing internal)
```

`orchestrator` is the innermost package: it defines the `StreamingClient`
interface and `model` implements it, so the loop never imports a provider SDK.
Swapping Anthropic for another provider is one new file in `model/`.

### Package responsibilities

| Package | Owns | Deliberately does not |
|---|---|---|
| `cmd/lumid` | Composition: read config, build store/client/session, mount the handler, graceful shutdown. | Contain logic — if it isn't wiring, it belongs in a package. |
| `internal/config` | Env parsing and fail-fast validation with a list of what's missing. | Read `.env` files — injection is the process manager's job, so config has one source in production. |
| `internal/auth` | `Verifier` seam, `DevBearerVerifier`, the Connect interceptor, identity-on-context. | Authorize resources — that's the store's scoped SQL. |
| `internal/service` | Proto ⇄ domain translation, argument validation, error→Connect-code mapping. | Business logic. |
| `internal/chat` | One turn end to end, plus auto-titling. | Know about proto or HTTP. |
| `internal/orchestrator` | The streaming tool-use loop, model-client seam, tier→model resolution, rate limiting. | Know about persistence or surfaces. |
| `internal/model` | Provider adaptation: SDK params in, normalized `StreamEvent`s out. | Decide anything about a turn. |
| `internal/store` | Conversation + message persistence, tenant scoping, idempotent creates. | Interpret content. |

### The lifecycle of one turn

`SendMessage` is a server-streaming RPC. Start to finish:

1. **Interceptor** (`auth`) reads `Authorization` (+ `X-Workspace-Id`), verifies it, and puts an `Identity` on the context. Failure → `unauthenticated`, no handler runs.
2. **Handler** (`service`) pulls the verified scope off the context, validates `conversation_id` and "text or attachments", and calls the session with a callback that writes each event to the stream.
3. **Session** (`chat`) loads the conversation or creates it under the verified scope, snapshots whether this is the first message on an untitled thread, then **appends the user message first** so it survives a failed model call.
4. **Auto-titling** starts in a goroutine on the fast tier if this is a first message. The fallback (trimmed first message) always succeeds, so the row is never blank.
5. **History** is loaded and projected into model messages — persisted canvas blocks are display state and are dropped here.
6. **Loop** (`orchestrator`) runs rounds: rate-limit gate → stream from the model, accumulating text and tool calls → no tool calls means done; otherwise execute the tools, emit results, and loop. A `maxToolRounds` cap (8) stops runaway loops.
7. **Translation** turns loop events into surface events: `text_delta`→`token`, `thinking`→`block`, `discard_buffer` passes through, `message_end`→`done`, `error`→`error`. Tool events are silent at the surface.
8. **Title** is awaited just before `done`, so `conversation_titled` and the turn's final paint land together.
9. **Persist** the assistant text plus any blocks — **skipped when the turn errored**, so a blocked or partial response never enters canonical history.

### Wire contract

| RPC | Kind | Purpose |
|---|---|---|
| `SendMessage` | server streaming | One turn. |
| `ListConversations` | unary | Left-rail list, scoped and recency-ordered. |
| `GetMessages` | unary | History replay. |
| `RenameConversation` / `ArchiveConversation` / `DeleteConversation` | unary | Conversation management. |

`ChatEvent` is a `oneof` — a closed union mirroring lumi-neo's `SurfaceEvent`:

| Variant | Client behaviour |
|---|---|
| `token` | Append to the current buffer. |
| `block` | Render a completed canvas block (`kind` + `rendered` markdown + free-form `data`). |
| `discard_buffer` | Drop the current buffer (see the recall guard rail below). |
| `conversation_titled` | Update the rail row's label in place. |
| `error` | Show and stop. |
| `done` | Commit the buffer as a message. |

`BlockEvent.data` is a `google.protobuf.Struct`, so a new block kind ships
without a proto change; only the renderer needs to learn about it.

### Invariants worth protecting

- **Scope comes from auth, never from the request.** `SendMessageRequest` has no user or workspace field by design. Every store query carries the verified triple in its `WHERE`, so a conversation id from another tenant reads as absent and writes affect zero rows. `ErrNotFound` covers "doesn't exist" and "belongs to someone else" identically — the difference is not observable to a caller.
- **The recall guard rail.** When the model calls a recall tool it usually streams a "let me check…" preamble and then restates the same content after the tool returns. The loop emits `discard_buffer`; the client drops its buffer and the session drops it from what gets persisted. A non-trivial preamble (≥20 chars) is preserved as a `thinking` block first, so the reasoning is visible rather than merely deleted.
- **Persist only on a clean end.** An errored turn writes no assistant row.
- **Idempotent conversation creation.** `INSERT … ON CONFLICT DO NOTHING` plus a scoped re-select, so a double-send is harmless and a foreign id can't be probed.
- **Counters can't drift.** `AppendMessage` bumps `message_count`/`last_message_at` and inserts in one transaction; the scoped `UPDATE` returning zero rows *is* the authorization check.
- **The stream has no write timeout.** A turn is long-lived; `ReadHeaderTimeout` and `IdleTimeout` are set, `WriteTimeout` deliberately is not.

### Extension seams

Each is an interface with a real second implementation, so tests never need the network or a database:

| Seam | Production | Alternate | Add next |
|---|---|---|---|
| `auth.Verifier` | — | `DevBearerVerifier` (insecure stub) | Expona-minted panel token verifier. |
| `orchestrator.StreamingClient` | `model.AnthropicClient` | `model.EchoClient` | Another provider. |
| `store.Store` | `PostgresStore` | `MemoryStore` | — |
| `orchestrator.ToolDef` | (none registered) | — | Capability tools; `Recall`/`Writes` flags already drive the guard rails. |

### Why there is no agent framework

`loop.go` is the agent loop, and it stays hand-written on purpose. The parts
that matter here — the recall guard rail, thinking-block emission,
persist-only-on-clean-end, tenant scope threaded through every call — are
product behaviour, not generic plumbing, and a framework would either duplicate
the loop or force those semantics through someone else's abstraction. Adding
tools means adding `ToolDef`s, not adopting a runtime.

Revisit when the control flow genuinely outgrows a `for` loop: multi-agent
fan-out, delegation, or branching pipelines. At that point the options worth
weighing are `anthropic-sdk-go/toolrunner` (first-party tool loop),
`cloudwego/eino` (typed graph orchestration), or Managed Agents (Anthropic runs
the loop and hosts the sandbox — `client.Beta.Agents`/`Sessions`). For tool
interop specifically, an MCP client maps onto the existing `ToolDef` seam
without a framework at all.

---

## 5. Grounding: documents and retrieval

The chat is grounded in two documents in `data/` — a résumé and the job
description it is being discussed against — so the conversation is about the
candidate's actual experience against a specific role, not generic advice.

### Ingest

| Format | How it is read |
|---|---|
| `.md` / `.txt` | Read as-is. **Preferred** when several formats share a filename. |
| `.pdf` | Pure-Go text layer extraction (`ledongthuc/pdf`). No system dependency, so it works in the distroless image; a scanned PDF yields nothing and is skipped with a reason. |
| `.eml` | MIME walk preferring `text/plain`, falling back to stripped HTML. Subject/From/Date are kept — the subject usually carries the role title. |

A document's role is inferred from its filename: `resume`/`cv` → résumé,
`.eml` or `job`/`role`/`position`/`posting` → job description, anything else →
supporting context. Deliberately dumb and documented, so the outcome is
predictable.

**Format precedence matters.** `data/` holds both the source PDF and the
markdown converted from it; only the markdown loads, and the PDF is logged as
superseded. Markdown wins because it is the curated copy — the converter writes
it once and a human fixes what the converter got wrong.

```bash
make resume-md PDF=../data/Your-Resume.pdf   # PDF → markdown
```

`cmd/pdf2md` exists because PDF extraction emits one line per *visual* line and
splits again at every styled span — a bold "25+ years" mid-sentence becomes its
own line. The output has no paragraphs and no headings, which chunks terribly.
The converter reflows prose, rebuilds heading levels, and rejoins hyphenated
line breaks. It is **best-effort**: review the output, because a PDF's visual
layout does not always say which bullets belong to which role.

### Retrieval

Documents are chunked (~600 chars, section-aware), embedded by a **local
Ollama** instance, and searched in process. The model reaches them through one
recall tool, `search_documents`.

```bash
ollama serve && ollama pull nomic-embed-text   # once
make rag-check                                  # see what retrieval returns
```

| Decision | Reason |
|---|---|
| In-process vector store, not Chroma | ChromaDB has no embedded mode outside Python; from Go it would mean a container. At tens of chunks a linear cosine scan is exact and microseconds — an ANN index would add a dependency and recall error for nothing. Swap the scan behind `Search` if the corpus ever grows past a few thousand chunks. |
| Local embeddings (Ollama) | The résumé is personal data. Local embedding keeps it off a vendor's servers and needs no API key. |
| Chunks ~600 chars | An embedding averages everything in the passage, so a big chunk answers every query mediocrely. The whole 8.8k-char résumé as one chunk lost every query to the job description; splitting it fixed that. |
| `Recall: true` on the tool | Marks it a silent context loader, so the orchestrator drops the "let me look that up…" preamble and keeps a substantial one as a thinking block (§4). |
| Results balanced across documents | Pure relevance ranking is the wrong objective for a comparison. A posting saturated with "Go" beats a résumé that lists Go once, so "do I have the Go experience they want?" would retrieve only the demand side. Each document gets a quota of the top-k; an explicit `document` filter overrides it. |
| Boilerplate stripped at ingest | A trademark footer that outranks a job requirement is a retrieval bug. Inline-image placeholders, tracking links and legal footers are dropped. |

Retrieval degrades rather than fails. If Ollama is unreachable, the boot log
says so and the full documents are inlined in the system prompt instead — at
this corpus size that is a legitimate fallback, costing tokens rather than
answers. `RAG=off` forces it.

### The project as evidence

This README is itself indexed, as a `project` document alongside the résumé and
job description — so "what has this candidate actually built in Go?" is
answerable from a primary artifact rather than from a résumé bullet. The
codebase it describes is public; a reader can go and check it.

It is loaded from the repo root (`PROJECT_DOC`, default `../README.md`), not
copied into `data/`, so it can never drift from the code it describes.

The distinction the prompt enforces: the résumé is authority on **dates,
employers and titles**; the project document is authority on **what was built
and which decisions were made**. Neither substitutes for the other, and the Go
documentation (below) is authority on neither — it describes the language, not
the candidate.

This also changes retrieval balance: three document kinds now share the top-k
quota, so a fit question sees the résumé, the role, and the delivered work
rather than whichever one happens to share the most vocabulary with the query.

### Go reference documentation

The Go documentation from **tip.golang.org** is fetched, cached, chunked and
embedded into a *second* index, searchable with its own tool, `search_go_docs`:

```bash
make refs        # fetch into data/reference/ (~480 KB of text, 7 documents)
```

| Source | Why it is in the set |
|---|---|
| Language Specification | Normative answer to "what does Go do here?" |
| The Go Memory Model | Concurrency guarantees — the questions that are easiest to get wrong from memory |
| Effective Go | Idiom and rationale |
| Go FAQ | Design reasoning, the "why is it like that" questions |
| Go 1.26 Release Notes | Version-specific behaviour |
| Developing Go Modules | Module and versioning semantics |
| Returning and Handling Errors | Added after a retrieval check: "error wrapping idiom" returned unrelated spec sections, because that idiom lives in the `errors` package docs — and `tip.golang.org/pkg/*` redirects off-site to pkg.go.dev |

**Two indexes, two tools, on purpose.** The reference material is deliberately
*not* in the same index as the résumé. The grounding rule the assistant rests on
is that the résumé is the only authority on what the candidate has done; the Go
docs are authority on what Go does. One blended index would let a passage about
goroutines surface as though it were evidence of experience with them. The tool
description says so explicitly, and so does the prompt.

Fetching is a separate step, not something the server does at boot — a chat
service should not need six external HTTP calls to start, and the content
changes on the order of weeks. `data/reference/` is gitignored: it is fetched
content, reproducible with one command.

> `tip.golang.org` is the **in-development** documentation and can be ahead of
> the released toolchain (the spec currently reports go1.27 while the release
> notes are go1.26). The tool description tells the model to flag that when a
> detail is version-sensitive.

### Embedding cache

Embedding ~1,130 reference passages takes about 15 seconds, and the Go
documentation does not change between restarts — so vectors are cached to
`data/.embeddings/` keyed by content hash **and** model name. Changing
`EMBED_MODEL` misses rather than silently mixing two vector spaces.

| Boot | Time to `listening` |
|---|---|
| Cold (empty cache) | ~15 s |
| Warm | **0.07 s** |

The cache is an optimisation, never load-bearing: an unreadable, stale, or
wrong-model cache file is discarded and re-embedded.

### The prompt is the product

`corpus.GroundingRules` is the part that decides whether this is useful or
dangerous. It is the same principle as the guard rails inherited from lumi-neo:
that codebase blocks an assistant claiming "saved!" with no write behind it,
and this one blocks an assistant claiming experience with no résumé line behind
it. **A fabricated bullet point is worse than a missing one, because the
candidate has to defend it in an interview.** The rules require citing the
source section, forbid inventing employers/titles/dates/metrics, and require
naming real gaps rather than papering over them.

> **Privacy.** `data/` holds a real résumé (name, email, phone) and a real
> recruiter's email. It is committed to the repo, so a public GitHub repo would
> publish both. The container image does **not** include it — the build context
> is `server/` — so mount it at runtime (`-v ./data:/data -e DATA_DIR=/data`)
> rather than baking personal data into a registry.

---

## 6. Data model

```sql
lumi.conversations (id, user_id, workspace_id, project_id, title, status,
                    persona_mode, message_count, last_message_at,
                    created_at, updated_at)
lumi.messages      (id, conversation_id → conversations ON DELETE CASCADE,
                    role, content, metadata jsonb, created_at)
```

Its own schema rather than lumi-neo's `assist.*`: both services can run side by
side without a bug in one writer corrupting the other's live data. The column
shape tracks `assist.assist_conversations` closely enough that a later backfill
is a straight `INSERT … SELECT`. `metadata.blocks` holds the canvas blocks
emitted during a turn, so a reload reproduces the layout the user first saw.

Indexes match the two real queries: `(user_id, workspace_id, last_message_at DESC)`
for the rail, `(conversation_id, created_at)` for replay.

---

## 7. Configuration

Read from the environment only — see `.env.example`.

| Variable | Default | Notes |
|---|---|---|
| `HOST` / `PORT` | `127.0.0.1` / `8080` | Bind address. |
| `ALLOWED_ORIGINS` | `http://localhost:3000` | CORS allow-list for the browser client. |
| `AUTH_MODE` | `dev` | **Dev only.** `prod` refuses to boot until the real verifier lands. |
| `DEFAULT_WORKSPACE_ID` | `local-workspace` | Used when a dev request sends no workspace header. |
| `STORE` | `postgres` | `memory` for a throwaway run. |
| `DATABASE_URL` | — | Required when `STORE=postgres`. |
| `MODEL_CLIENT` | `anthropic` | `echo` streams a canned reply; no API key needed. |
| `ANTHROPIC_API_KEY` | — | Required when `MODEL_CLIENT=anthropic`. |
| `STRONG_MODEL` | `claude-opus-5` | Main reasoning tier. |
| `FAST_MODEL` | `claude-haiku-4-5` | Auto-titling and other helpers. |
| `EFFORT` | `medium` | Thinking depth / token spend. Raise to `high`/`xhigh` for harder work. |
| `RATE_LIMIT` / `RATE_LIMIT_WINDOW_SECONDS` | `6` / `60` | Per caller (user × workspace × tier). |
| `GLOBAL_RATE_LIMIT` | `10` | Per window across **all** callers. |
| `TOKEN_BUDGET` | `2000000` | Total tokens the deployment may ever spend. `0` disables. |
| `GROK_API_KEY` / `GROK_BASE_URL` / `GROK_MODEL` | — | xAI credentials, carried in `.env` for a future provider client. **Nothing reads them yet.** |
| `DATA_DIR` | `../data` | Grounding documents (§5). |
| `PROJECT_DOC` | `../README.md` | Delivered-work document indexed as evidence (§5). Empty disables it. |
| `RAG` | `on` | `off` inlines the documents instead of retrieving them. |
| `OLLAMA_HOST` / `EMBED_MODEL` | `http://localhost:11434` / `nomic-embed-text` | Local embedder for retrieval. |
| `EMBED_CACHE_DIR` | `../data/.embeddings` | Persisted vectors. Empty disables caching. |

`.env` holds live credentials and is gitignored — keep it that way, and keep
real values out of `.env.example`.

`AUTH_MODE=dev` is an insecure stub: the bearer token **is** the user id. It
exists so the chat path is runnable before the real verifier lands, and the
service logs a warning at boot.

---

## 8. Limits

The application is a public link funded by one API key, so two independent
controls bound it: rate limiting caps how *fast* tokens can be spent, and the
token budget caps how *many* exist at all.

### Rate limiting

Two limiters, composed — a request must pass both:

| Limiter | Default | Bounds |
|---|---|---|
| Per caller | 6 / minute | One visitor cannot take the whole allowance |
| Global (all callers pooled) | 10 / minute | Several visitors together still cannot |

With one or two readers the global limit is the one that binds. The per-caller
limiter only starts mattering once real per-visitor identities exist — today
every visitor shares one dev identity, so they share one bucket.

### Token budget

`TOKEN_BUDGET` is a hard ceiling on input + output tokens for the whole
deployment, across every conversation, for all time.

- **Checked per model call, not per turn.** A tool-using turn makes several
  calls, and the cap should stop the next call rather than only the next turn.
- **Persisted in Postgres** (`lumi.token_usage`, one row per call). A restart —
  or a crash loop, which is when you least want the meter reset — resumes where
  it stopped. Boot fails rather than starting with a zeroed budget if the total
  cannot be read.
- **Counted from the provider's own numbers**, taken off the `message_delta`
  event's cumulative usage, not estimated.
- **Overshoots by at most one call.** The check cannot know a call's cost
  before making it, so treat the cap as "stop starting new work", not an exact
  stop.
- When exhausted, a turn ends with an `error` event carrying
  `budget_exhausted` — distinct from `rate_limited`, because waiting will not
  help.

A failed usage write is logged, not returned: the turn already happened, and
losing the record must not also fail the reader's response. The in-memory total
still counts it for the life of the process.

---

## 9. Running

No database, no API key:

```bash
make dev-offline    # backend on :8080, in-memory store, canned replies
make dev-web        # client on :3000
```

Full stack:

```bash
cp .env.example .env                       # fill in ANTHROPIC_API_KEY
cp web/.env.local.example web/.env.local
make db-up && make migrate                 # Postgres on :5433
set -a; source .env; set +a
make dev-server
make dev-web
```

### Make targets

| Target | Does |
|---|---|
| `generate` / `lint-proto` | Regenerate both languages / lint the proto module. |
| `build` / `vet` / `test` | Go build, vet, tests. |
| `db-up` / `db-down` / `migrate` | Local Postgres lifecycle and schema. |
| `dev-server` / `dev-offline` / `dev-web` | Run the backend (Postgres / in-memory) and the client. |
| `web-build` | Production client build, including the TypeScript check. |
| `refs` | Fetch the Go documentation into `data/reference/`. |
| `resume-md` / `rag-check` | PDF → markdown; print what retrieval returns for sample questions. |
| `image` / `image-run` / `image-push` / `image-push-multi` | Container build, local run, and registry push (§10). |

### Calling it by hand

```bash
# streaming RPC — Connect framing is a 1-byte flag + 4-byte big-endian length
python3 - > /tmp/frame.bin <<'PY'
import json, struct, sys
b = json.dumps({"conversationId": "11111111-1111-1111-1111-111111111111", "text": "hello"}).encode()
sys.stdout.buffer.write(b"\x00" + struct.pack(">I", len(b)) + b)
PY

curl -sN -X POST localhost:8080/lumi.chat.v1.ChatService/SendMessage \
  -H "Content-Type: application/connect+json" \
  -H "Connect-Protocol-Version: 1" \
  -H "Authorization: Bearer rob:acme" \
  --data-binary @/tmp/frame.bin

# unary RPCs take plain JSON
curl -s -X POST localhost:8080/lumi.chat.v1.ChatService/ListConversations \
  -H "Content-Type: application/json" -H "Connect-Protocol-Version: 1" \
  -H "Authorization: Bearer rob:acme" -d '{"limit":10}'
```

---

## 10. Container image

`server/Dockerfile` builds the backend. The build context is `server/`, so the
client and its `node_modules` never enter it.

```bash
make image                       # build for this host's architecture
make image-run                   # run it: in-memory store, canned replies, :8080
make image-push TAG=v0.1.0       # single-arch push
make image-push-multi TAG=v0.1.0 # linux/amd64 + linux/arm64 in one manifest
```

Default name: `ghcr.io/notbobutah/goreactchat/lumid` — lowercased, because
ghcr.io rejects uppercase in image names. Override with
`IMAGE=…`. `TAG` defaults to the short commit SHA.

**Build decisions and why:**

| Decision | Reason |
|---|---|
| Multi-stage, `golang:1.26-alpine` → `distroless/static-debian12:nonroot` | ~20 MB image with no shell, no package manager, and nothing to exec into. |
| `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w"` | Static binary that runs on a libc-less base; no absolute build paths in it. |
| `--platform=$BUILDPLATFORM` + `GOARCH=$TARGETARCH` | Go cross-compiles, so an arm64 image costs a second compile rather than a QEMU-emulated build. |
| `ENV HOST=0.0.0.0` | The server binds `127.0.0.1` by default — right for a local run, unreachable inside a container. |
| `USER nonroot` (uid 65532) | No root in the container; nothing in the image is writable by it. |
| No `HEALTHCHECK` | There is no shell or curl to run one. Point the orchestrator's probe at `GET /health`. |
| `-X main.version` | The boot log carries the version, so a running container traces back to its commit. |

Configuration is entirely environment-driven (§7), so the same image runs in
every environment:

```bash
docker run --rm -p 8080:8080 \
  -e DATABASE_URL="postgres://…" \
  -e ANTHROPIC_API_KEY="sk-ant-…" \
  -e ALLOWED_ORIGINS="https://app.example.com" \
  ghcr.io/notbobutah/goreactchat/lumid:v0.1.0
```

### Publishing to GitHub Container Registry

`.github/workflows/publish-image.yml` builds and pushes on every push to
`main` and every `v*` tag. It authenticates with the built-in `GITHUB_TOKEN`
(`packages: write`) — no PAT to create or rotate.

| Trigger | Tags produced |
|---|---|
| push to `main` | `main`, `sha-<commit>`, `latest` |
| tag `v1.2.3` | `1.2.3`, `1.2`, `sha-<commit>` |
| pull request | builds only — **never pushes** |

Pull requests deliberately don't push: a fork's token can't write packages, and
an unreviewed branch shouldn't be able to publish a tag someone might pull. The
workflow also runs `go build`, `go vet` and `go test` before building the image,
and attaches a signed provenance attestation to each published digest:

```bash
gh attestation verify oci://ghcr.io/notbobutah/goreactchat/lumid:latest \
  --repo notbobutah/GoReactChat
```

**First publish:** the package is created private and inherits the repository's
permissions. To let other machines pull it anonymously, set the package to
public once under *Packages → lumid → Package settings* on GitHub.

Pushing by hand (CI is the normal path):

```bash
make ghcr-login                  # uses the gh CLI's token
# or: echo $CR_PAT | docker login ghcr.io -u <user> --password-stdin
#     with a classic PAT carrying write:packages
make image-push-multi TAG=v0.1.0
```

The web client has no image — it's a static/Node build deployed on its own. Add
a second Dockerfile under `web/` and a matching `IMAGE_NAME` if that changes.

---

## 11. Relationship to lumi-neo

| lumi-neo (TypeScript) | lumi-go (Go) |
|---|---|
| WebSocket frames (`send`, `rename_conversation`, …) | RPCs on `ChatService` |
| `SurfaceEvent` union | `ChatEvent` oneof |
| `src/orchestrator/streaming-loop.ts` | `internal/orchestrator/loop.go` |
| `src/surface/session.ts` | `internal/chat/session.go` |
| `src/memory/conversation-store.ts` | `internal/store` |
| `src/gateway/auth.ts` | `internal/auth` |
| `assist.assist_conversations` | `lumi.conversations` |

**Deliberately absent for now**, with their seams already in place: capability
tools, memory recall and post-turn extraction, entity context, canvas emission
tools (`emit_brief`, `emit_picker`), generation callbacks, and the activity
rail. Real token verification replaces `DevBearerVerifier`; Go tests for the
loop's guard rails and the store's tenant isolation are the next thing to add.
