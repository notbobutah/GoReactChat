--
-- PostgreSQL database dump
--

\restrict 0OynXYkmaGU8YACx24r1WmbpNtadHnQL9u2aC9ot5nH8RAetqj0XQSkcvxhieiw

-- Dumped from database version 17.11 (c4ba6b8)
-- Dumped by pg_dump version 17.11 (Debian 17.11-1.pgdg13+2)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: lumi; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA lumi;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: conversations; Type: TABLE; Schema: lumi; Owner: -
--

CREATE TABLE lumi.conversations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id text NOT NULL,
    workspace_id text NOT NULL,
    project_id text,
    title text,
    status text DEFAULT 'active'::text NOT NULL,
    persona_mode boolean DEFAULT false NOT NULL,
    message_count integer DEFAULT 0 NOT NULL,
    last_message_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: messages; Type: TABLE; Schema: lumi; Owner: -
--

CREATE TABLE lumi.messages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    conversation_id uuid NOT NULL,
    role text NOT NULL,
    content text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT messages_role_check CHECK ((role = ANY (ARRAY['user'::text, 'assistant'::text])))
);


--
-- Name: news_digest; Type: TABLE; Schema: lumi; Owner: -
--

CREATE TABLE lumi.news_digest (
    id smallint DEFAULT 1 NOT NULL,
    digest_id text NOT NULL,
    generated_at timestamp with time zone NOT NULL,
    tool_calls integer DEFAULT 0 NOT NULL,
    total_tokens bigint DEFAULT 0 NOT NULL,
    items jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT news_digest_id_check CHECK ((id = 1))
);


--
-- Name: token_usage; Type: TABLE; Schema: lumi; Owner: -
--

CREATE TABLE lumi.token_usage (
    id bigint NOT NULL,
    input_tokens bigint NOT NULL,
    output_tokens bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    cache_creation_input_tokens bigint DEFAULT 0 NOT NULL,
    cache_read_input_tokens bigint DEFAULT 0 NOT NULL,
    CONSTRAINT token_usage_input_tokens_check CHECK ((input_tokens >= 0)),
    CONSTRAINT token_usage_output_tokens_check CHECK ((output_tokens >= 0))
);


--
-- Name: token_usage_id_seq; Type: SEQUENCE; Schema: lumi; Owner: -
--

CREATE SEQUENCE lumi.token_usage_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: token_usage_id_seq; Type: SEQUENCE OWNED BY; Schema: lumi; Owner: -
--

ALTER SEQUENCE lumi.token_usage_id_seq OWNED BY lumi.token_usage.id;


--
-- Name: token_usage id; Type: DEFAULT; Schema: lumi; Owner: -
--

ALTER TABLE ONLY lumi.token_usage ALTER COLUMN id SET DEFAULT nextval('lumi.token_usage_id_seq'::regclass);


--
-- Data for Name: conversations; Type: TABLE DATA; Schema: lumi; Owner: -
--

COPY lumi.conversations (id, user_id, workspace_id, project_id, title, status, persona_mode, message_count, last_message_at, created_at, updated_at) FROM stdin;
81d918af-30c6-44fd-9341-0289b6d2c31c	dev-user	local-workspace	\N	Does he have production Go experience? Be…	active	f	2	2026-09-22 21:15:31.183384+00	2026-09-22 21:15:21.747006+00	2026-09-22 21:15:31.183384+00
cd4ba2f0-955e-4884-b92d-ff6fc118fbd2	dev-user	local-workspace	\N	First, and in detail: the Ecosystem watch panel…	active	f	2	2026-09-22 22:32:03.920835+00	2026-09-22 22:31:20.261296+00	2026-09-22 22:32:03.920835+00
\.


--
-- Data for Name: messages; Type: TABLE DATA; Schema: lumi; Owner: -
--

COPY lumi.messages (id, conversation_id, role, content, metadata, created_at) FROM stdin;
fe7975d4-8758-44b0-8feb-41866f5736ea	81d918af-30c6-44fd-9341-0289b6d2c31c	user	Does he have production Go experience? Be specific about where and when.	{}	2026-09-22 21:15:21.853725+00
31275e3a-1384-4821-92a3-a1c17999091b	81d918af-30c6-44fd-9341-0289b6d2c31c	assistant	Yes, in two places — one commercial, one public and readable.\n\n**Commercial (résumé, Omadi, Inc., Jul 2018 – Jul 2019):** as a Senior Software Engineer he "developed backend services ranging from gRPC in Go and Java for high-performance inter-cluster communication," deployed on AWS Kubernetes with Jenkins, GitFlow and Bitbucket pipelines. That is the only Go entry on the résumé's employment history.\n\n**Public and current (GoReactChat, 2026):** a complete Go service — Connect/gRPC streaming handler, hand-written Anthropic tool-use loop, Postgres via pgx, local-embedding retrieval, containerised and running on Kubernetes behind TLS at rob.expona.ai. The code and the `deploy/` manifests are public, so this is the artifact to read for a current assessment of his Go depth rather than a résumé line.\n\nOne caveat the GitHub portfolio document states plainly: before GoReactChat, the only public Go on his account was `on-xml-proxy` (2019), about 10 KB of SOAP/XML examples. His Go work at Expona AI is not part of the picture either — that estate is TypeScript (20 repos) and Python (9 repos) per the portfolio summary. So the Go is Omadi in 2018–19 plus this service; the intervening architecture roles were C#, TypeScript and Python.	{}	2026-09-22 21:15:31.183384+00
be0cadba-1e7f-471c-8a63-3666507e677b	cd4ba2f0-955e-4884-b92d-ff6fc118fbd2	user	First, and in detail: the Ecosystem watch panel on this page is produced by an agent whose tool loop executes inside xAI, not in this application. Explain that — the single Responses API request, what runs server-side, and why there is no local agent loop or extra service. Only after that, cover the hand-written Go loop behind this chat and his wider agent work.	{}	2026-09-22 22:31:20.363035+00
abdb0121-f9ff-4a88-8e3c-f7d2e5b60dce	cd4ba2f0-955e-4884-b92d-ff6fc118fbd2	assistant	## The Ecosystem watch panel — the loop runs at xAI\n\n**What the application sends.** One HTTP request to xAI's Responses API, `POST {GROK_BASE_URL}/v1/responses` (default `https://api.x.ai`, model `grok-4.6`). That single request declares which tools the model may use, carries the prompt, and carries a `strict` JSON schema for the answer. That is the entire outbound surface of the agent — there is no second call. (lumi-go README §11, §7.)\n\n**What happens after that, and where.** The model does the agentic part on xAI's infrastructure: it searches, reads what it finds, decides what to search next, and iterates. A real scan measured on this deployment ran **~15 web searches, ~128k tokens, about a minute of wall clock** — and this process dispatched none of those searches. No tool-call events come back to be executed here, no results are fed back in, no rounds are counted here. `NEWS_MAX_TURNS` (12) bounds the server-side iteration; `NEWS_TIMEOUT_SECONDS` (240) bounds the whole thing. (README §11.)\n\n**So there is no local agent loop, and no extra service.** The conventional shape for a research agent is a loop you own: call the model, read the tool calls it asks for, execute the searches, feed the results back, repeat — plus retries, partial state, and a runaway-iteration limit. The README is explicit that this loop "is most of the code and all of the operational risk," and that moving it server-side means that whole apparatus stops being ours to get right. What remains in `internal/newsagent` is three things: the prompt (`prompt.go` — what counts as news, primary sources over articles, and a standing rule that every claim carries an openable link), the enforced response schema, and the request itself. The package's stated non-responsibility is "Run a loop. That is the point — the loop executes on xAI." (README §4 package table, §11.)\n\n**Why the result is pushed, not returned.** A scan takes roughly a minute, which is far too long to hold a request open for. So the digest arrives over `rpc WatchNews(WatchNewsRequest) returns (stream NewsEvent)` — a server-streaming RPC the browser subscribes to. The first event on any stream is always a snapshot of current state, so a client connecting mid-scan or hours later renders immediately rather than waiting for the next change; subsequent events mark a scan starting, the digest landing, or a failure. The browser panel reconnects with capped backoff, because the ingress closes an idle connection at ten minutes — stream termination is the normal case here, and without reconnection the panel would silently stop updating while looking fine. (README §11, §12.)\n\n**What moving the loop away actually costs, and how that is handled.** The README is precise that this is a trade, not a free win, and names both consequences:\n\n1. *Latency stops being controllable* — hence the push-over-stream design above.\n2. *Billing changes shape* — it bills **per server-side tool call**, not per token, so the service-wide `TOKEN_BUDGET` (§8) does not bound it. The tokens barely move that number. Spend is therefore capped by **frequency** instead: it scans only while somebody is subscribed (an unopened page costs nothing); at most once per `NEWS_INTERVAL_MINUTES` (four hours in production, so at most six scans a day regardless of traffic); exactly one scan at a time however many people are watching; the last digest is persisted so a restart restores instead of rescanning (without that, a crash loop is a scan loop); and a *failed* scan starts the interval too, because retrying a broken provider on every page load is the expensive way to discover an outage.\n\nThe digest reports its own `tool_calls` and token count in the UI, on the stated principle that "an autonomous agent whose cost is invisible is one nobody notices running away." (README §11.)\n\n**It is optional and isolated.** `GROK_API_KEY` enables it. With no key there is no watcher, `WatchNews` reports `Unimplemented`, the panel renders nothing, and nothing else in the service is affected. In the extension-seam table, `newswatch.Scanner` has `newsagent.Agent` in production and a fake scanner in tests, with "another hosted agent provider" listed as the next thing that could slot in. (README §7, §4.)\n\n---\n\n## The hand-written Go loop behind this chat\n\nThe other agent in the same binary is built the opposite way round, and the README treats the contrast as the design decision worth reading rather than either half on its own.\n\n`internal/orchestrator/loop.go` is a streaming tool-use loop written by hand against the Anthropic Go SDK (`anthropic-sdk-go` v1.63.1). Per round: rate-limit gate → stream from the model, accumulating text and tool calls as they arrive mid-stream → if there are no tool calls, the turn is done; otherwise execute the tools, emit results, and loop. A `maxToolRounds` cap of 8 stops runaway iteration. (README §4.)\n\nThe stated reason it is hand-written is that **the exact sequencing is the product**. The clearest case is the recall guard rail: when the model calls a retrieval tool it typically streams a "let me check…" preamble and then restates the same content once the tool returns. The loop emits a `discard_buffer` event at one specific point — between a tool returning and the next round streaming — the client drops its buffer, and the session drops it from what gets persisted, so the reader sees the answer rather than the narration. A substantial preamble (≥20 chars) is preserved as a `thinking` block first, so reasoning is visible rather than merely deleted. The README notes that point "is not a configuration option." Alongside it: persist-only-on-a-clean-end (an errored turn writes no assistant row), and tenant scope threaded through every call — scope comes from the verified auth identity, never from the request, so a conversation id from another tenant reads as absent and writes affect zero rows. (README §4.)\n\nThe loop is also the innermost package and imports nothing internal: it *defines* the `StreamingClient` interface, and `internal/model` implements it, so the loop never imports a provider SDK. `model.EchoClient` is the second implementation, which is why tests need no network. Adding a capability means adding a `ToolDef`, not adopting a runtime. (README §4.)\n\n**The rule that separates the two agents**, as the README states it: ownership of the sequence. Where the order of operations *is* the product, the loop stays local. Where the work is open-ended retrieval and only the finished answer matters, running the loop is undifferentiated effort. It also names what it would reach for if the local side outgrows a `for` loop — `anthropic-sdk-go/toolrunner`, `cloudwego/eino`, or Anthropic Managed Agents — and notes that MCP maps onto the existing `ToolDef` seam without a framework at all.\n\n**Tools the chat loop actually calls:** `search_documents` (the grounding corpus — résumé, job description, and this README as a project document) and `search_go_docs` (a separate index over ~1,130 passages of Go documentation from tip.golang.org). Two indexes and two tools on purpose: blending them would let a passage about goroutines surface as though it were evidence of experience with them. Retrieval runs over locally generated embeddings (Ollama, `nomic-embed-text`) with an in-process cosine scan, and results are balanced per document kind so one long document cannot crowd out the rest. (README §5.)\n\n---\n\n## His wider agent work\n\n**Expona AI, CTO, Jul 2023 – present** (résumé). He architected and shipped a 20-service, 30-repository AI-native marketing-intelligence platform on Kubernetes across dev/stage/prod, operated end-to-end by a 3-engineer team. Directly relevant to agent orchestration:\n\n- **20+ CrewAI agent crews** generating buyer personas, competitive intelligence and campaign content on demand, with **dynamic selection of the right crew per request** — that is orchestration and routing across agents, not a single-agent integration.\n- **Semantic retrieval via a ChromaDB-backed Knowledge Service**, so agents reason against embedded, workspace-scoped context rather than raw prompts — context management as a service the agents call.\n- A **provenance and "composition-receipt" layer** making every AI-generated claim traceable to its sources, with field-level provenance and anti-clobber locks protecting human edits from automated regeneration.\n- **Workspace-scoped multi-tenant isolation on every read and write.**\n- A **dedicated TypeScript agent runtime** as its own service alongside the Python CrewAI generation services.\n\nThe aggregate estate summary corroborates the shape: 36 private repositories over ~21.5 months, TypeScript (20) and Python (9) as the two-language core, with named functional groupings for "agent and generation services," "retrieval," and an "assistant / agent runtime" including a clean-slate rebuild of the earlier implementation. It is a self-reported summary of a private organisation and cannot be verified from outside — the document says so itself, and offers the repositories for review under NDA.\n\nOne boundary worth stating plainly: **the Expona agent work is TypeScript and Python, not Go.** The Go agent work is this service, which is public and readable. The résumé's earlier Go is professional rather than public — gRPC services in Go and Java for high-performance inter-cluster communication at Omadi (Jul 2018 – Jul 2019), deployed on AWS Kubernetes.	{}	2026-09-22 22:32:03.920835+00
\.


--
-- Data for Name: news_digest; Type: TABLE DATA; Schema: lumi; Owner: -
--

COPY lumi.news_digest (id, digest_id, generated_at, tool_calls, total_tokens, items, updated_at) FROM stdin;
1	564bb0c0c7884570	2026-09-30 22:51:44.186865+00	13	135244	[{"url": "https://protobuf.dev/news/2026-09-22/", "topic": "protobuf", "source": "protobuf.dev", "summary": "v38 (expected 2027 Q1) will drop Bazel 8/WORKSPACE, Python 3.10, and PHP 8.2; disable generic services by default; deprecate Objective-C after the 37.x LTS; and remove several C++ reflection APIs. Engineers should plan Bazel Bzlmod and language-runtime upgrades before the bump.", "headline": "Protocol Buffers announces planned v38 breaking changes", "published": "2026-09-22"}, {"url": "https://github.com/grpc/grpc-go/releases/tag/v1.84.0", "topic": "grpc", "source": "GitHub — grpc/grpc-go", "summary": "Replaces grpc.lb.pick_first OpenTelemetry metrics with grpc.subchannel metrics (gRFC A94), adds xDS contains_match header matchers, and fixes a ClientConn that could stay stuck IDLE after a canceled stream plus several xDS/RBAC panics and fail-open DENY policies.", "headline": "grpc-go v1.84.0", "published": "2026-09-17"}, {"url": "https://github.com/protocolbuffers/protobuf/releases/tag/v36.2", "topic": "protobuf", "source": "GitHub — protocolbuffers/protobuf", "summary": "Patch release focused on Rust codegen: canonical type paths, public per-file modules, and crate-root re-exports for cross-file symbol collisions. The protobuf crate README now points users at google-protobuf.", "headline": "Protocol Buffers v36.2", "published": "2026-09-17"}, {"url": "https://github.com/bufbuild/buf/releases/tag/v1.73.0", "topic": "protobuf", "source": "GitHub — bufbuild/buf", "summary": "Fixes buf format dropping comments and non-idempotent trailing comments, import-public symbol resolution, LSP go-to-definition/references across dependencies, and managed mode wrongly setting java_multiple_files on Edition 2024 files. Built-in well-known types update to Protobuf v35.1.", "headline": "Buf CLI v1.73.0", "published": "2026-09-11"}, {"url": "https://go.dev/doc/devel/release", "topic": "go", "source": "go.dev", "summary": "Minor revisions: 1.27.1 fixes cgo, the compiler, runtime, go fix, and packages including encoding/json, net/http, os, and simd; 1.26.8 fixes cgo, compiler, runtime, debug/elf, and os. Neither notes security fixes.", "headline": "Go 1.27.1 and 1.26.8", "published": "2026-09-01"}]	2026-09-30 22:51:44.637946+00
\.


--
-- Data for Name: token_usage; Type: TABLE DATA; Schema: lumi; Owner: -
--

COPY lumi.token_usage (id, input_tokens, output_tokens, created_at, cache_creation_input_tokens, cache_read_input_tokens) FROM stdin;
1	23	480	2026-09-22 21:15:31.127216+00	32304	0
2	115	3304	2026-09-22 22:32:03.868885+00	32304	0
\.


--
-- Name: token_usage_id_seq; Type: SEQUENCE SET; Schema: lumi; Owner: -
--

SELECT pg_catalog.setval('lumi.token_usage_id_seq', 2, true);


--
-- Name: conversations conversations_pkey; Type: CONSTRAINT; Schema: lumi; Owner: -
--

ALTER TABLE ONLY lumi.conversations
    ADD CONSTRAINT conversations_pkey PRIMARY KEY (id);


--
-- Name: messages messages_pkey; Type: CONSTRAINT; Schema: lumi; Owner: -
--

ALTER TABLE ONLY lumi.messages
    ADD CONSTRAINT messages_pkey PRIMARY KEY (id);


--
-- Name: news_digest news_digest_pkey; Type: CONSTRAINT; Schema: lumi; Owner: -
--

ALTER TABLE ONLY lumi.news_digest
    ADD CONSTRAINT news_digest_pkey PRIMARY KEY (id);


--
-- Name: token_usage token_usage_pkey; Type: CONSTRAINT; Schema: lumi; Owner: -
--

ALTER TABLE ONLY lumi.token_usage
    ADD CONSTRAINT token_usage_pkey PRIMARY KEY (id);


--
-- Name: conversations_project_idx; Type: INDEX; Schema: lumi; Owner: -
--

CREATE INDEX conversations_project_idx ON lumi.conversations USING btree (workspace_id, project_id) WHERE (project_id IS NOT NULL);


--
-- Name: conversations_scope_recent_idx; Type: INDEX; Schema: lumi; Owner: -
--

CREATE INDEX conversations_scope_recent_idx ON lumi.conversations USING btree (user_id, workspace_id, last_message_at DESC NULLS LAST);


--
-- Name: messages_conversation_idx; Type: INDEX; Schema: lumi; Owner: -
--

CREATE INDEX messages_conversation_idx ON lumi.messages USING btree (conversation_id, created_at);


--
-- Name: token_usage_created_idx; Type: INDEX; Schema: lumi; Owner: -
--

CREATE INDEX token_usage_created_idx ON lumi.token_usage USING btree (created_at);


--
-- Name: messages messages_conversation_id_fkey; Type: FK CONSTRAINT; Schema: lumi; Owner: -
--

ALTER TABLE ONLY lumi.messages
    ADD CONSTRAINT messages_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES lumi.conversations(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

\unrestrict 0OynXYkmaGU8YACx24r1WmbpNtadHnQL9u2aC9ot5nH8RAetqj0XQSkcvxhieiw

