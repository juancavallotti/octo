# Bundled platform app + runtime image (octo-editor image).
#
# apps/platform is a Next.js app that shells out to the `octo` runtime binary via
# OCTO_BIN_PATH (see apps/platform/app/api/run/session.ts), so a single image
# carries both: the compiled Go binary plus the Next.js standalone server.
#
# IMPORTANT: build context must be the repo ROOT (not apps/platform/) so this
# Dockerfile can reach runtime/ (to build octo) and the whole pnpm workspace.
# devspace.yaml / cloudbuild.yaml set the context accordingly.

# --- Stage 1: build the octo runtime binary from the Go workspace -------------
FROM golang:1.25-alpine AS runtime-build
RUN apk add --no-cache git
WORKDIR /src
# Copy the whole runtime workspace (go.work + all member modules).
COPY runtime/ ./runtime/
WORKDIR /src/runtime/cli
RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /out/octo .

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
    OCTO_RUN_DIR=/app/.octo-run

# Next.js standalone bundle (server + hoisted node_modules) plus static assets
# and public files. The monorepo standalone nests under apps/platform/.
COPY --from=editor-build /app/apps/platform/.next/standalone ./
COPY --from=editor-build /app/apps/platform/.next/static ./apps/platform/.next/static
COPY --from=editor-build /app/apps/platform/public ./apps/platform/public
# The octo runtime the RUN feature spawns. Kept OUTSIDE /app so the dev image's
# DevSpace source-sync can never delete it.
COPY --from=runtime-build /out/octo /usr/local/bin/octo

# OCTO_RUN_DIR must be writable at runtime; back it with an emptyDir in k8s.
RUN mkdir -p /app/.octo-run

EXPOSE 3000
# server.js is the entrypoint emitted by Next.js standalone output.
CMD ["node", "apps/platform/server.js"]
