# Build context is `web/` — the Go service is a separate image with a separate
# lifecycle, and keeping the contexts apart means a backend change does not
# invalidate this image's layers.
#
#   docker build -f web/Dockerfile -t ghcr.io/notbobutah/goreactchat/web web

# --- dependencies -----------------------------------------------------------
# Split from the build stage so a source-only change reuses the install layer.
FROM node:24-alpine AS deps

WORKDIR /app

COPY package.json package-lock.json ./
# `npm ci` installs exactly the lockfile, failing rather than silently resolving
# a newer transitive version — the reproducibility the lockfile is there for.
RUN npm ci

# --- build ------------------------------------------------------------------
FROM node:24-alpine AS build

WORKDIR /app

COPY --from=deps /app/node_modules ./node_modules
COPY . .

# Deliberately NOT passing NEXT_PUBLIC_LUMI_URL. Next.js inlines NEXT_PUBLIC_*
# into the client bundle at build time, so setting it here would bake one
# hostname into the artifact and force a rebuild per environment. Unset, the
# client falls back to a same-origin base — correct wherever one ingress host
# serves both the page and the API.
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# --- runtime ----------------------------------------------------------------
FROM node:24-alpine AS runtime

WORKDIR /app

ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0

LABEL org.opencontainers.image.source="https://github.com/notbobutah/GoReactChat"
LABEL org.opencontainers.image.description="lumi-go chat web client (Next.js)"

# The standalone bundle carries its own minimal server plus only the traced
# node_modules. `public/` and `.next/static` are excluded from it by design (the
# docs assume a CDN fronts them); nothing fronts this deployment, so they are
# copied in explicitly or the page loads with no JS and no images.
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public

# node:alpine ships an unprivileged `node` user (uid 1000). Nothing here is
# written at runtime, so the whole tree can stay root-owned and read-only.
USER node

EXPOSE 3000

# server.js is emitted by the standalone build, not written by hand.
CMD ["node", "server.js"]
