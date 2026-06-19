# Bundled editor + runtime image.
#
# The editor is a Next.js app that shells out to the `octo` runtime binary via
# OCTO_BIN_PATH (see editor/app/api/run/session.ts), so a single image carries
# both: the compiled Go binary plus the Next.js standalone server.
#
# IMPORTANT: build context must be the repo ROOT (not editor/) so this Dockerfile
# can reach both runtime/ and editor/. devspace.yaml sets context accordingly.

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

# --- Stage 2: build the Next.js editor (standalone output) --------------------
FROM node:22-alpine AS editor-build
WORKDIR /app
COPY editor/package.json editor/package-lock.json* ./
RUN npm ci
COPY editor/ ./
RUN npm run build

# --- Stage 3: minimal runtime image -------------------------------------------
FROM node:22-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    PORT=3000 \
    HOSTNAME=0.0.0.0 \
    OCTO_BIN_PATH=/app/bin/octo \
    OCTO_RUN_DIR=/app/.octo-run

# Next.js standalone server + its static assets and public files.
COPY --from=editor-build /app/.next/standalone ./
COPY --from=editor-build /app/.next/static ./.next/static
COPY --from=editor-build /app/public ./public
# The octo runtime the editor's RUN feature spawns.
COPY --from=runtime-build /out/octo /app/bin/octo

# OCTO_RUN_DIR must be writable at runtime; back it with an emptyDir in k8s.
RUN mkdir -p /app/.octo-run

EXPOSE 3000
# server.js is the entrypoint emitted by Next.js standalone output.
CMD ["node", "server.js"]
