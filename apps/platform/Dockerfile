# syntax=docker/dockerfile:1
# Bundled platform app + runtime image (octo-editor image).
#
# apps/platform is a Next.js app that shells out to the `octo` runtime binary via
# OCTO_BIN_PATH (see apps/platform/app/api/run/session.ts) and to `dolphin` via
# DOLPHIN_BIN_PATH for the editor's Testing tab, so a single image carries all
# three: the two compiled Go binaries plus the Next.js standalone server.
#
# IMPORTANT: build context must be the repo ROOT (not apps/platform/) so this
# Dockerfile can reach runtime/ (to build the CLIs) and the whole pnpm workspace.
# devspace.yaml / cloudbuild.yaml set the context accordingly.

# BIN_SOURCE picks where the two Go binaries come from:
#   compile   (default) build them here from source. What Cloud Build and
#             release CI use, and what makes this file work on its own.
#   prebuilt  copy them from the `prebuilt` named build context, which
#             task bins:linux fills:
#               task bins:linux
#               docker build --build-arg BIN_SOURCE=prebuilt \
#                 --build-context prebuilt=dist/bin \
#                 -t octo-platform:dev -f apps/platform/Dockerfile .
ARG BIN_SOURCE=compile

# --- Stage 1: the octo and dolphin binaries -----------------------------------
FROM golang:1.27-alpine AS bin-compile
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

# --- Stage 2: build the Next.js platform app (standalone output) ---------------
FROM node:22-alpine AS editor-build
RUN corepack enable && corepack prepare pnpm@10.32.1 --activate
WORKDIR /app
# Install deps from the workspace lockfile; manifests first for layer caching.
COPY pnpm-lock.yaml pnpm-workspace.yaml package.json ./
COPY apps/platform/package.json ./apps/platform/
COPY packages/editor/package.json ./packages/editor/
COPY packages/events/package.json ./packages/events/
COPY packages/http/package.json ./packages/http/
COPY packages/mcp/package.json ./packages/mcp/
COPY packages/run-host/package.json ./packages/run-host/
RUN pnpm install --frozen-lockfile
# Shared base tsconfig + workspace source (the app consumes @octo/editor,
# @octo/events, @octo/http, @octo/mcp and @octo/run-host as TS source via
# transpilePackages), then build. The standalone
# output traces the workspace root, so the server lands at
# apps/platform/.next/standalone/apps/platform/server.js.
COPY tsconfig.base.json ./
COPY packages/ ./packages/
COPY apps/platform/ ./apps/platform/
RUN pnpm --filter platform build

# --- Stage 3: minimal runtime image -------------------------------------------
FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    PORT=3000 \
    HOSTNAME=0.0.0.0 \
    OCTO_BIN_PATH=/usr/local/bin/octo \
    DOLPHIN_BIN_PATH=/usr/local/bin/dolphin \
    OCTO_RUN_DIR=/app/.octo-run

# Next.js standalone bundle (server + hoisted node_modules) plus static assets
# and public files. The monorepo standalone nests under apps/platform/.
COPY --from=editor-build /app/apps/platform/.next/standalone ./
COPY --from=editor-build /app/apps/platform/.next/static ./apps/platform/.next/static
COPY --from=editor-build /app/apps/platform/public ./apps/platform/public
# The octo runtime the RUN feature spawns, and the dolphin the Testing tab does.
# Kept OUTSIDE /app so the dev image's DevSpace source-sync can never delete them.
COPY --from=bin /out/octo /usr/local/bin/octo
COPY --from=bin /out/dolphin /usr/local/bin/dolphin

# OCTO_RUN_DIR must be writable at runtime; back it with an emptyDir in k8s.
# Owned by the unprivileged user below, which is what lets the container write
# there without running as root. Same for /tmp, which Next.js uses for its
# response cache.
RUN mkdir -p /app/.octo-run && chown -R node:node /app/.octo-run /tmp

# Run unprivileged. node:22-alpine ships a `node` user (uid/gid 1000) but does
# not select it, so without this the editor runs as root — which clusters with
# a restricted PodSecurity policy reject, and which the Helm chart would
# otherwise have to special-case for this one component while every Go service
# already runs nonroot from distroless.
#
# Nothing here needs privileges: the server binds 3000 (>1024), and the two Go
# binaries it spawns are read+execute for everyone.
# Numeric, not `node`. With runAsNonRoot set, the kubelet must decide before
# start whether the user is root, and it cannot resolve a username from the
# image to do it — a named USER means the pod is rejected unless the chart also
# sets runAsUser. 1000 is what node:22-alpine's `node` user already is, so this
# is the same identity stated in a form Kubernetes can check on its own.
USER 1000:1000

EXPOSE 3000
# server.js is the entrypoint emitted by Next.js standalone output.
CMD ["node", "apps/platform/server.js"]
