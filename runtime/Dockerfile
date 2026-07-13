# Cluster runtime image (octo-runtime).
#
# A generic image that runs ANY integration: it carries only the compiled `octo`
# binary and, by default, runs `octo run --config /etc/octo/integrations`. The
# orchestrator deploys one Deployment per integration from this image, mounting
# the integration's YAML into /etc/octo/integrations via a per-deployment
# ConfigMap (octo loads every .yaml/.yml in that directory).
#
# Built with -tags k8s so the binary carries the cluster services provider
# (Lease-based leader election, orchestrator KV, NATS queues) and not the
# standalone one. The standalone "try Octo" image builds the default (untagged)
# binary from apps/standalone/Dockerfile.
#
# Build context is the repo ROOT (the Go module is rooted there), e.g.:
#   docker build -t octo-runtime:dev -f runtime/Dockerfile .

# --- Stage 1: build the octo binary from the Go module ------------------------
FROM golang:1.25-alpine AS build
# GOTAGS selects the runtime-services provider compiled in (k8s for this image).
ARG GOTAGS=k8s
RUN apk add --no-cache git
WORKDIR /src
# The module manifest lives at the repo root; every Go package lives under runtime/.
COPY go.mod go.sum ./
COPY runtime/ ./runtime/
# -s -w strip the symbol table and DWARF debug info for a leaner binary.
RUN CGO_ENABLED=0 go build \
    -tags "${GOTAGS}" \
    -ldflags "-s -w -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /out/octo ./runtime/octo

# --- Stage 2: minimal runtime image -------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/octo /usr/local/bin/octo
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/octo"]
# Load every integration YAML mounted into this directory.
CMD ["run", "--config", "/etc/octo/integrations"]
