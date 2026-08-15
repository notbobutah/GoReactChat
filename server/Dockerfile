# Build context is the REPOSITORY ROOT, not `server/`.
#
# The context was `server/` while the image only had to carry a binary. It no
# longer does: this service answers questions about documents, and a deployed
# container with no `data/` boots into the generic prompt and quietly stops
# being the thing it advertises. The corpus is part of the artifact, so it has
# to be inside the build context. `.dockerignore` at the root keeps `web/` and
# its node_modules out, so the context stays small.
#
#   docker build -f server/Dockerfile -t ghcr.io/notbobutah/goreactchat/lumid .

# --- build ------------------------------------------------------------------
# Pinned to the toolchain in go.mod. --platform=$BUILDPLATFORM keeps the
# compiler running natively while cross-compiling for TARGETARCH: Go does this
# natively, so a multi-arch build needs no QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies first, so a source-only change reuses this layer.
COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server/ ./

ARG TARGETOS
ARG TARGETARCH
# VERSION is stamped into the binary and logged at boot, so a running container
# can be traced back to the commit that built it.
ARG VERSION=dev

# CGO off gives a static binary that runs on a distroless base with no libc.
# -s -w drop the symbol table and DWARF: smaller image, and a stack trace still
# carries file/line.
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/lumid ./cmd/lumid

# --- runtime ----------------------------------------------------------------
# distroless/static: no shell, no package manager, nothing to exec into. It
# ships the CA bundle, which the Anthropic client needs for TLS.
FROM gcr.io/distroless/static-debian12:nonroot

# CI overrides the full label set via docker/metadata-action; this one is here
# so a locally built image still links to the repository on GHCR.
LABEL org.opencontainers.image.source="https://github.com/notbobutah/GoReactChat"
LABEL org.opencontainers.image.description="lumi-go chat backend (gRPC / Connect)"

COPY --from=build /out/lumid /usr/local/bin/lumid

# The grounding corpus and the README that stands as evidence of delivered work.
# Both are read once at boot and never written, which is why they can live on a
# read-only root filesystem. `data/.embeddings` is excluded by .dockerignore:
# the vector cache is only useful next to a reachable embedder, and there is no
# Ollama in the cluster (see deploy/README.md).
COPY data/ /app/data/
COPY README.md /app/README.md

# The server binds 127.0.0.1 by default — correct for a local run, unreachable
# from outside a container. Bind all interfaces here instead; the container
# boundary is the thing limiting exposure.
#
# DATA_DIR and PROJECT_DOC default to `../data` and `../README.md`, which are
# relative to running out of `server/`. Point them at the absolute paths above
# so the corpus is found no matter what the working directory is.
ENV HOST=0.0.0.0 \
    PORT=8080 \
    DATA_DIR=/app/data \
    PROJECT_DOC=/app/README.md

EXPOSE 8080

# nonroot (uid 65532) is inherited from the base image.
USER nonroot:nonroot

# No HEALTHCHECK: there is no shell or curl in the image to run one. Point the
# orchestrator's probe at GET /health, which returns {"status":"ok"}.
ENTRYPOINT ["/usr/local/bin/lumid"]
