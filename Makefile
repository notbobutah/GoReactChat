GO       ?= go
BUF      ?= buf
DB_URL   ?= postgres://lumi:lumi@localhost:5433/lumi?sslmode=disable
SERVER   := server
WEB      := web

# Container image. Override IMAGE to push somewhere else:
#   make image-push IMAGE=ghcr.io/acme/goreactchat/lumid TAG=v0.1.0
REGISTRY ?= ghcr.io
# ghcr.io rejects uppercase in image names, so this is the lowercased form of
# notbobutah/GoReactChat. CI derives the same name from ${GITHUB_REPOSITORY,,}.
IMAGE    ?= $(REGISTRY)/notbobutah/goreactchat/lumid
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

## migrate: apply migrations to the local Postgres
migrate:
	docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U lumi -d lumi < migrations/0001_init.sql

## dev-server: run the backend against local Postgres
dev-server:
	cd $(SERVER) && DATABASE_URL="$(DB_URL)" $(GO) run ./cmd/lumid

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

## resume-md: regenerate the markdown résumé from the source PDF
## Usage: make resume-md PDF=../data/Your-Resume.pdf
PDF ?= ../data/MacKay-Resume-2026-Comprehensive.pdf
resume-md:
	cd $(SERVER) && $(GO) run ./cmd/pdf2md "$(PDF)" > "$(basename $(PDF)).md"
	@echo "wrote $(basename $(PDF)).md — review it; the converter is best-effort"

## rag-check: print what retrieval returns for sample questions (needs Ollama)
rag-check:
	cd $(SERVER) && $(GO) test ./internal/rag -run TestLiveRetrieval -v

## image: build the backend container image for this host's architecture
image:
	docker build -f $(SERVER)/Dockerfile --build-arg VERSION=$(VERSION) -t $(IMAGE):$(TAG) $(SERVER)

## image-run: run the built image locally (in-memory store, canned replies)
image-run:
	docker run --rm -p 8080:8080 -e STORE=memory -e MODEL_CLIENT=echo $(IMAGE):$(TAG)

## ghcr-login: log docker in to ghcr.io using the gh CLI's token
ghcr-login:
	gh auth token | docker login $(REGISTRY) -u $$(gh api user -q .login) --password-stdin

## image-push: push $(IMAGE):$(TAG) (single-arch; CI publishes multi-arch)
image-push: image
	docker push $(IMAGE):$(TAG)

## image-push-multi: build and push linux/amd64 + linux/arm64 in one manifest
image-push-multi:
	docker buildx build -f $(SERVER)/Dockerfile --build-arg VERSION=$(VERSION) \
		--platform linux/amd64,linux/arm64 -t $(IMAGE):$(TAG) --push $(SERVER)

.PHONY: help generate lint-proto build test vet db-up db-down migrate dev-server dev-offline dev-web web-build \
	refs resume-md rag-check \
	image image-run ghcr-login image-push image-push-multi
