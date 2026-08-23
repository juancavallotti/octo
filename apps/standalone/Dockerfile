# syntax=docker/dockerfile:1
# Self-contained public "try Octo" image: the standalone editor + the octo
# runtime binary, with a local-disk flow store. No orchestrator, auth, or DB.
#
#   docker run -p 3000:3000 -v "$PWD:/work" juancavallotti/octo
#
# Flows are read/written under /work (mount a host dir there); RUN spawns the
# bundled octo binary and the Testing tab spawns the bundled dolphin. Build context
# must be the repo ROOT so this Dockerfile can reach runtime/ (to build both CLIs)
# and the pnpm workspace (apps/standalone + packages/*).

# BIN_SOURCE picks where the two Go binaries come from:
#   compile   (default) build them here from source. What Cloud Build and
#             release CI use, and what makes this file work on its own.
#   prebuilt  copy them from the `prebuilt` named build context, which
#             task bins:linux fills:
#               task bins:linux
#               docker build --build-arg BIN_SOURCE=prebuilt \
#                 --build-context prebuilt=dist/bin \
#                 -t juancavallotti/octo -f apps/standalone/Dockerfile .
ARG BIN_SOURCE=compile

# --- Stage 1: the octo and dolphin binaries -----------------------------------
FROM golang:1.25-alpine AS bin-compile
RUN apk add --no-cache git
WORKDIR /src
# The Go module is rooted at the repo root; all its packages live under runtime/.
COPY go.mod go.sum ./
COPY runtime/ ./runtime/
# The cache mounts are what keep this arm bearable when it is taken: without them
# every build recompiles the standard library and the whole dependency tree from
# an empty GOCACHE. They are shared by both RUNs below, so dolphin pays only for
# the packages octo did not already build.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /out/octo ./runtime/octo
# dolphin, the test runner the Testing tab drives. Built in this stage so it comes
# from the same module at the same commit as the octo above it — the two are one
# release train, and a mismatched pair is the one skew this image cannot have.
# The prebuilt arm below keeps that guarantee the same way: task bins:linux builds
# the pair from one tree in one pass.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /out/dolphin ./runtime/dolphin

# Or copy the host-built pair. TARGETARCH keys the directory so one form serves a
# native build and a --platform/multi-arch one alike.
FROM scratch AS bin-prebuilt
ARG TARGETARCH
COPY --from=prebuilt linux_${TARGETARCH}/octo    /out/octo
COPY --from=prebuilt linux_${TARGETARCH}/dolphin /out/dolphin

FROM bin-${BIN_SOURCE} AS bin

# --- Stage 2: build the Next.js standalone app (standalone output) ------------
FROM node:22-alpine AS app-build
RUN corepack enable && corepack prepare pnpm@10.32.1 --activate
WORKDIR /app
# Install deps from the workspace lockfile; manifests first for layer caching.
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json ./
COPY apps/standalone/package.json ./apps/standalone/
COPY packages/editor/package.json ./packages/editor/
COPY packages/events/package.json ./packages/events/
COPY packages/http/package.json ./packages/http/
COPY packages/mcp/package.json ./packages/mcp/
COPY packages/run-host/package.json ./packages/run-host/
RUN pnpm install --frozen-lockfile
# Shared base tsconfig + workspace source (the app consumes @octo/editor,
# @octo/events, @octo/http, @octo/mcp and @octo/run-host as TS source via
# transpilePackages), then build.
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
    DOLPHIN_BIN_PATH=/usr/local/bin/dolphin \
    OCTO_RUN_DIR=/app/.octo-run \
    OCTO_FS_DIR=/work

# Next.js standalone bundle (server + hoisted node_modules) and static assets.
# The monorepo standalone nests under apps/standalone/.
COPY --from=app-build /app/apps/standalone/.next/standalone ./
COPY --from=app-build /app/apps/standalone/.next/static ./apps/standalone/.next/static
COPY --from=app-build /app/apps/standalone/public ./apps/standalone/public
# The octo runtime the RUN feature spawns, and the dolphin the Testing tab does.
COPY --from=bin /out/octo /usr/local/bin/octo
COPY --from=bin /out/dolphin /usr/local/bin/dolphin

# OCTO_RUN_DIR holds rendered run configs; /work is the (mountable) flow store.
RUN mkdir -p /app/.octo-run /work

EXPOSE 3000
VOLUME ["/work"]
# server.js is the entrypoint emitted by Next.js standalone output.
CMD ["node", "apps/standalone/server.js"]
