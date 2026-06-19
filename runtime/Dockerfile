# Standalone runtime image (octo-runtime).
#
# A generic image that runs ANY integration: it carries only the compiled `octo`
# binary and, by default, runs `octo run --config /etc/octo/integrations`. The
# orchestrator deploys one Deployment per integration from this image, mounting
# the integration's YAML into /etc/octo/integrations via a per-deployment
# ConfigMap (octo loads every .yaml/.yml in that directory).
#
# Build context is ./runtime (the whole Go workspace lives there), e.g.:
#   docker build -t octo-runtime:dev -f runtime/Dockerfile ./runtime

# --- Stage 1: build the octo binary from the Go workspace ---------------------
FROM golang:1.25-alpine AS build
RUN apk add --no-cache git
WORKDIR /src
# Copy the whole workspace (go.work + all member modules) so the cli module's
# replace-free workspace build resolves its siblings.
COPY . .
WORKDIR /src/cli
RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /out/octo .

# --- Stage 2: minimal runtime image -------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/octo /usr/local/bin/octo
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/octo"]
# Load every integration YAML mounted into this directory.
CMD ["run", "--config", "/etc/octo/integrations"]
