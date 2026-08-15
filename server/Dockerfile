# Build context is `server/` — everything the binary needs (go.mod, gen/,
# internal/, cmd/) lives there, so the client and its node_modules never enter
# the build context.
#
#   docker build -f server/Dockerfile -t ghcr.io/notbobutah/goreactchat/lumid server

# --- build ------------------------------------------------------------------
# Pinned to the toolchain in go.mod. --platform=$BUILDPLATFORM keeps the
# compiler running natively while cross-compiling for TARGETARCH: Go does this
# natively, so a multi-arch build needs no QEMU emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies first, so a source-only change reuses this layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

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

# The server binds 127.0.0.1 by default — correct for a local run, unreachable
# from outside a container. Bind all interfaces here instead; the container
# boundary is the thing limiting exposure.
ENV HOST=0.0.0.0 \
    PORT=8080

EXPOSE 8080

# nonroot (uid 65532) is inherited from the base image.
USER nonroot:nonroot

# No HEALTHCHECK: there is no shell or curl in the image to run one. Point the
# orchestrator's probe at GET /health, which returns {"status":"ok"}.
ENTRYPOINT ["/usr/local/bin/lumid"]
