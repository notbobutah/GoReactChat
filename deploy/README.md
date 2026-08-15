# Deploying lumi-go

Target: namespace **`dev-next`**, served at **https://rob.expona.ai**.

These manifests are the single source of truth for that deployment. CI applies
them; nothing is `kubectl patch`ed in place, and there is no second copy in the
infrastructure repository to drift away from this one.

## Shape

```
                     rob.expona.ai (nginx ingress, TLS from cert-manager)
                                  │
             ┌────────────────────┴────────────────────┐
             │                                         │
   /lumi.chat.v1.ChatService                          /
   /health                                            │
             │                                         │
       lumi-go-api:8080                        lumi-go-web:3000
       (Go, ConnectRPC/h2c)                    (Next.js standalone)
             │
             └──── Neon Postgres (external, over TLS)
```

One host, split by path. The browser loads the page and then streams from the
Connect endpoint on the **same origin** — no CORS preflight in front of every
turn, one certificate, and a web image with no hostname baked into it.

Two images, published to GHCR by `.github/workflows/publish-image.yml`:

| Image | Built from | Contents |
| --- | --- | --- |
| `ghcr.io/notbobutah/goreactchat/lumid` | `server/Dockerfile`, context = repo root | static Go binary on distroless + the `data/` corpus + `README.md` |
| `ghcr.io/notbobutah/goreactchat/web` | `web/Dockerfile`, context = `web/` | Next.js standalone bundle on node:alpine |

The backend image carries the corpus on purpose. This service answers questions
about documents; a container with no `data/` boots into the generic prompt and
quietly stops being the thing it advertises.

## First-time bootstrap

The namespace, the nginx ingress controller, the `letsencrypt-prod` Issuer and
the `gh-registry-credentials` pull secret already exist in `dev-next` — they are
shared with the other apps there. What is specific to this deployment:

```bash
export KUBECONFIG=/path/to/dev-next.kubeconfig

# 1. Secrets. Never from a file in this repo — the repo is public.
kubectl -n dev-next create secret generic lumi-go-secrets \
  --from-literal=DATABASE_URL="$DATABASE_URL" \
  --from-literal=ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY"

# 2. Schema, once, against the same database the pods will use.
DATABASE_URL="$DATABASE_URL" make migrate-remote      # from the repo root

# 3. Everything else.
kubectl apply -f deploy/configmap.yaml
kubectl apply -f deploy/api.yaml
kubectl apply -f deploy/web.yaml
kubectl apply -f deploy/ingress.yaml
```

Then confirm the certificate was issued before expecting HTTPS to work:

```bash
kubectl -n dev-next get certificate lumi-go-tls          # READY should be True
curl -s https://rob.expona.ai/health                     # {"status":"ok"}
```

DNS is already in place: `rob.expona.ai` resolves to the same ingress address as
the other `dev-next` hosts.

## Continuous deployment

`.github/workflows/deploy-dev.yml` runs after a successful image build on
`main`: it applies these manifests and then restarts both rollouts. The restart
is not optional — both deployments track the `:latest` tag, which never changes,
so `imagePullPolicy: Always` has nothing to react to without it.

It needs one repository secret, `KUBE_CONFIG_DEV`, holding a base64-encoded
kubeconfig for the cluster:

```bash
base64 -w0 /path/to/dev-next.kubeconfig
# GitHub → Settings → Secrets and variables → Actions → New repository secret
```

## Things worth knowing before changing this

**The API runs a single replica, and `strategy: Recreate`.** The rate limiter
and the in-memory half of the token budget are per-process. A second pod would
hand out the full per-minute allowance independently, so the global cap would
silently double; a rolling update does the same thing for the length of the
rollout. The persisted total in Postgres still bounds lifetime spend either way,
but the burst rate is what protects the wallet in the short run. Scaling out
means moving the limiter behind Redis first — the interface is already in
`internal/orchestrator/ratelimit.go`, so it is a substitution, not a rewrite.

**There is no sign-in, deliberately.** It is a résumé; putting a login in front
of it defeats the purpose. The limits in `configmap.yaml` are what stands in for
authentication — total token budget, global requests per minute, per-message
character cap, per-conversation message cap. Treat them as the security
boundary, not as tuning. `AUTH_MODE` is `dev`, which means the bearer token is
trusted as the user id; every visitor therefore shares one scope.

**Retrieval is off here (`RAG=off`).** Embeddings are generated locally through
Ollama, and there is no Ollama in this cluster. With `RAG=on` the service would
spend its boot discovering that and fall back anyway. The fallback inlines the
corpus in the system prompt, which costs tokens rather than answers — what is
actually lost is the `search_go_docs` tool, because the Go documentation is far
too large to inline. Turning it on means running Ollama with `nomic-embed-text`
in-cluster (a Deployment plus a PVC for the model), pointing `OLLAMA_HOST` at
it, and accepting a few minutes of indexing on every boot unless the embedding
cache is given a writable volume.

**The database is external.** Neon, reached over TLS from the pod. Nothing in
the cluster stores state for this app, so a namespace wipe costs nothing but a
re-apply.
