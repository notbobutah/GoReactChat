GO       ?= go
BUF      ?= buf
# Defaults to the local docker Postgres, but an exported DATABASE_URL wins — so
# pointing at a hosted database (Neon, RDS) needs no edit here and no credential
# in the repository.
DB_URL   ?= $(if $(DATABASE_URL),$(DATABASE_URL),postgres://lumi:lumi@localhost:5433/lumi?sslmode=disable)
SERVER   := server
WEB      := web

# Container image. Override IMAGE to push somewhere else:
#   make image-push IMAGE=ghcr.io/acme/goreactchat/lumid TAG=v0.1.0
REGISTRY ?= ghcr.io
# ghcr.io rejects uppercase in image names, so this is the lowercased form of
# notbobutah/GoReactChat. CI derives the same name from ${GITHUB_REPOSITORY,,}.
IMAGE    ?= $(REGISTRY)/notbobutah/goreactchat/lumid
WEB_IMAGE ?= $(REGISTRY)/notbobutah/goreactchat/web
VERSION  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
TAG      ?= $(VERSION)

.DEFAULT_GOAL := help

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'

## generate: regenerate Go + TypeScript from proto/
generate:
	$(BUF) generate

## lint-proto: lint the proto module
lint-proto:
	$(BUF) lint

## build: compile the Go server
build:
	cd $(SERVER) && $(GO) build ./...

## test: run Go tests
test:
	cd $(SERVER) && $(GO) test ./...

## vet: go vet the server
vet:
	cd $(SERVER) && $(GO) vet ./...

## db-up: start the local Postgres
db-up:
	docker compose up -d postgres

## db-down: stop the local Postgres
db-down:
	docker compose down

## migrate: apply every migration to the local Postgres, in order
# Applied in filename order and each one is idempotent (IF NOT EXISTS), so
# re-running is safe. Hardcoding a single file meant a new migration silently
# never ran.
migrate:
	@for f in $$(ls migrations/*.sql | sort); do \
		echo "applying $$f"; \
		docker compose exec -T postgres psql -q -v ON_ERROR_STOP=1 -U lumi -d lumi < $$f || exit 1; \
	done

## dev-server: run the backend against local Postgres
dev-server:
	cd $(SERVER) && DATABASE_URL="$(DB_URL)" $(GO) run ./cmd/lumid

## migrate-remote: apply every migration to $DATABASE_URL (hosted Postgres)
# `migrate` talks to the local container; this one uses a local psql client
# against whatever DB_URL points at, so it works for Neon and friends.
#
# Schema changes go to the DIRECT endpoint, not the pooled one. Neon's pooled
# host runs PgBouncer in transaction mode, which does not preserve session
# state — the thing migrations, LISTEN/NOTIFY and advisory locks rely on. The
# migrations here are plain CREATE/ALTER and have applied cleanly through the
# pooler, so this is latent rather than broken; it is the next migration
# needing a session that would fail, confusingly and in production.
#
# The direct host is the pooled one with `-pooler` removed, which is Neon's own
# convention. MIGRATE_URL overrides the derivation for a provider that spells
# it differently.
MIGRATE_URL ?= $(subst -pooler,,$(DB_URL))
migrate-remote:
	@test -n "$(DB_URL)" || { echo "set DATABASE_URL first"; exit 1; }
	@echo "applying to $$(echo '$(MIGRATE_URL)' | sed -E 's#.*@([^/]*)/.*#\1#')"
	@for f in $$(ls migrations/*.sql | sort); do \
		echo "applying $$f"; \
		psql "$(MIGRATE_URL)" -q -v ON_ERROR_STOP=1 -f $$f || exit 1; \
	done

## dev-offline: run the backend with no database and no model calls
dev-offline:
	cd $(SERVER) && STORE=memory MODEL_CLIENT=echo $(GO) run ./cmd/lumid

## dev-web: run the Next.js chat client
dev-web:
	cd $(WEB) && npm run dev

## web-build: production build of the client
web-build:
	cd $(WEB) && npm run build

## refs: fetch the Go documentation from tip.golang.org into data/reference
refs:
	cd $(SERVER) && $(GO) run ./cmd/fetchrefs -data ../data

## resume-md: regenerate the markdown résumé from the HTML source
# From the HTML, NOT from the PDF. A PDF has no structure — only positioned
# text — so converting the artifact collapsed every role's bullets into one
# paragraph that attached to whatever heading came last, filing four employers'
# accomplishments (including "gRPC in Go") under EARLIER CAREER. The service
# answers from this file, so it was answering from that.
#
# cmd/pdf2md still exists for a résumé that arrives as a PDF and nothing else,
# but it is the fallback now, not the path.
resume-md:
	cd $(SERVER) && $(GO) run ./cmd/resume2md "../$(RESUME_HTML)" > "../$(RESUME_MD)"
	@echo "wrote $(RESUME_MD) from $(RESUME_HTML)"

## resume-pdf: re-render the résumé PDF from its HTML source
# Headless Chrome is what produced the original: rendering the untouched source
# reproduces it with an identical text layer and a 26-byte size difference, so
# this is the same pipeline rather than a lookalike. Edit the HTML, run this,
# then resume-public.
RESUME_HTML ?= data/resume-src/MacKay-Resume-2026-Comprehensive.html
CHROME ?= google-chrome
resume-pdf:
	$(CHROME) --headless --disable-gpu --no-sandbox --no-pdf-header-footer \
		--print-to-pdf="$(abspath $(RESUME_PDF))" "file://$(abspath $(RESUME_HTML))"
	@echo "rendered $(RESUME_HTML) -> $(RESUME_PDF); check the page count before shipping"

## resume-public: refresh the downloadable PDF in web/public from data/
# data/ is the source of truth — the corpus the model answers from is loaded
# from there, and web/public/ only holds the copy the browser serves. Without a
# target for this the two drift apart silently, and the file a recruiter
# downloads stops matching the answers they were just given.
# PDF is deliberately not reused here: it is relative to $(SERVER), because
# resume-md cds there before running the converter. This target runs from the
# repository root, so it needs its own root-relative path.
RESUME_PDF ?= data/MacKay-Resume-2026-Comprehensive.pdf
RESUME_MD  ?= data/MacKay-Resume-2026-Comprehensive.md
resume-public:
	cp "$(RESUME_PDF)" $(WEB)/public/Robert-MacKay-Resume.pdf
	@echo "copied $(RESUME_PDF) -> $(WEB)/public/Robert-MacKay-Resume.pdf"

## rag-check: print what retrieval returns for sample questions (needs Ollama)
rag-check:
	cd $(SERVER) && $(GO) test ./internal/rag -run TestLiveRetrieval -v

## image: build the backend container image for this host's architecture
# Context is the repository root, not $(SERVER): the image ships data/ next to
# the binary, so the corpus has to be inside the build context. .dockerignore
# keeps web/ and its node_modules out.
image:
	docker build -f $(SERVER)/Dockerfile --build-arg VERSION=$(VERSION) -t $(IMAGE):$(TAG) .

## image-web: build the Next.js client image
image-web:
	docker build -f $(WEB)/Dockerfile -t $(WEB_IMAGE):$(TAG) $(WEB)

## image-run: run the built image locally (in-memory store, canned replies)
image-run:
	docker run --rm -p 8080:8080 -e STORE=memory -e MODEL_CLIENT=echo -e RAG=off $(IMAGE):$(TAG)

## image-web-run: run the built client image locally on :3000
# Read-only root plus a writable cache mirrors deploy/web.yaml — next/image
# optimizes on first request and writes the result under .next/cache.
image-web-run:
	docker run --rm --read-only --tmpfs /app/.next/cache --tmpfs /tmp \
		-p 3000:3000 $(WEB_IMAGE):$(TAG)

## ghcr-login: log docker in to ghcr.io using the gh CLI's token
ghcr-login:
	gh auth token | docker login $(REGISTRY) -u $$(gh api user -q .login) --password-stdin

## image-push: push $(IMAGE):$(TAG) (single-arch; CI publishes multi-arch)
image-push: image
	docker push $(IMAGE):$(TAG)

## image-push-multi: build and push linux/amd64 + linux/arm64 in one manifest
image-push-multi:
	docker buildx build -f $(SERVER)/Dockerfile --build-arg VERSION=$(VERSION) \
		--platform linux/amd64,linux/arm64 -t $(IMAGE):$(TAG) --push .

## deploy-dry-run: validate deploy/ against the cluster without applying
# Server-side dry run, so it checks against the real API server (CRDs, admission
# webhooks) rather than just parsing YAML. Needs KUBECONFIG pointed at dev-next.
deploy-dry-run:
	kubectl apply --dry-run=server -f deploy/configmap.yaml -f deploy/api.yaml \
		-f deploy/web.yaml -f deploy/ingress.yaml

## deploy: apply deploy/ to dev-next and restart both rollouts
# CI does this on every green build of main (.github/workflows/deploy-dev.yml);
# this target is the same sequence by hand. The restart is what makes
# imagePullPolicy: Always pick up a freshly published :latest.
deploy:
	kubectl apply -f deploy/configmap.yaml
	kubectl apply -f deploy/api.yaml
	kubectl apply -f deploy/web.yaml
	kubectl apply -f deploy/ingress.yaml
	kubectl -n dev-next rollout restart deployment/lumi-go-api
	kubectl -n dev-next rollout restart deployment/lumi-go-web
	kubectl -n dev-next rollout status deployment/lumi-go-api --timeout=5m
	kubectl -n dev-next rollout status deployment/lumi-go-web --timeout=5m

.PHONY: help generate lint-proto build test vet db-up db-down migrate dev-server dev-offline dev-web web-build \
	refs resume-md resume-pdf resume-public rag-check \
	image image-web image-run image-web-run ghcr-login image-push image-push-multi \
	deploy-dry-run deploy
