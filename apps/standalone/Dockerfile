# Self-contained public "try Octo" image: the standalone editor + the octo
# runtime binary, with a local-disk flow store. No orchestrator, auth, or DB.
#
#   docker run -p 3000:3000 -v "$PWD:/work" juancavallotti/octo
#
# Flows are read/written under /work (mount a host dir there); RUN spawns the
# bundled octo binary. Build context must be the repo ROOT so this Dockerfile can
# reach runtime/ (to build octo) and the pnpm workspace (apps/standalone + packages/*).

# --- Stage 1: build the octo runtime binary from the Go workspace -------------
FROM golang:1.25-alpine AS runtime-build
RUN apk add --no-cache git
WORKDIR /src
COPY runtime/ ./runtime/
WORKDIR /src/runtime/cli
RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /out/octo .

# --- Stage 2: build the Next.js standalone app (standalone output) ------------
FROM node:22-alpine AS app-build
RUN corepack enable && corepack prepare pnpm@10.32.1 --activate
WORKDIR /app
# Install deps from the workspace lockfile; manifests first for layer caching.
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json ./
COPY apps/standalone/package.json ./apps/standalone/
COPY packages/editor/package.json ./packages/editor/
COPY packages/run-host/package.json ./packages/run-host/
RUN pnpm install --frozen-lockfile
# Shared base tsconfig + workspace source (the app consumes @octo/editor and
# @octo/run-host as TS source via transpilePackages), then build.
COPY tsconfig.base.json ./
COPY packages/ ./packages/
COPY apps/standalone/ ./apps/standalone/
RUN pnpm --filter standalone build

# --- Stage 3: minimal runtime image -------------------------------------------
FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    PORT=3000 \
    HOSTNAME=0.0.0.0 \
    OCTO_BIN_PATH=/usr/local/bin/octo \
    OCTO_RUN_DIR=/app/.octo-run \
    OCTO_FS_DIR=/work

# Next.js standalone bundle (server + hoisted node_modules) and static assets.
# The monorepo standalone nests under apps/standalone/.
COPY --from=app-build /app/apps/standalone/.next/standalone ./
COPY --from=app-build /app/apps/standalone/.next/static ./apps/standalone/.next/static
COPY --from=app-build /app/apps/standalone/public ./apps/standalone/public
# The octo runtime the RUN feature spawns.
COPY --from=runtime-build /out/octo /usr/local/bin/octo

# OCTO_RUN_DIR holds rendered run configs; /work is the (mountable) flow store.
RUN mkdir -p /app/.octo-run /work

EXPOSE 3000
VOLUME ["/work"]
# server.js is the entrypoint emitted by Next.js standalone output.
CMD ["node", "apps/standalone/server.js"]
