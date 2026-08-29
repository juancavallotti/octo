# syntax=docker/dockerfile:1
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

# BIN_SOURCE picks where the octo binary comes from. This file is the one place
# where that choice also has to say WHICH octo, because the providers are
# mutually exclusive build tags and all of them are built from this Dockerfile:
#
#   compile              build it here from source, honouring GOTAGS (default k8s).
#                        The default, and what Cloud Build and release CI use.
#   prebuilt-k8s         copy the host-built k8s binary.
#   prebuilt-standalone  copy the host-built standalone binary.
#   prebuilt-api         copy the host-built api binary — the flavour that
#                        delegates every platform capability to an
#                        operator-implemented HTTP API. Build it here instead
#                        with GOTAGS=api.
#
# The variant is folded into BIN_SOURCE rather than left to a second argument
# beside GOTAGS on purpose: two arguments that each name a provider is two ways
# to disagree, and the disagreement would be silent — an octo-devruntime image
# holding the k8s binary looks fine until a dev run exits for want of
# RUNTIME_SERVICES_MODULE. One value, no wrong-but-valid combination.
#
# The prebuilt arms read from the `prebuilt` named build context, which
# task bins:linux fills:
#   task bins:linux
#   docker build --build-arg BIN_SOURCE=prebuilt-k8s \
#     --build-context prebuilt=dist/bin -t octo-runtime:dev -f runtime/Dockerfile .
ARG BIN_SOURCE=compile

# --- Binary source: compile from the Go module --------------------------------
FROM golang:1.27-alpine AS bin-compile
# GOTAGS selects the runtime-services provider compiled in (k8s for this image).
ARG GOTAGS=k8s
RUN apk add --no-cache git
WORKDIR /src
# The module manifest lives at the repo root; every Go package lives under runtime/.
COPY go.mod go.sum ./
COPY runtime/ ./runtime/
# -s -w strip the symbol table and DWARF debug info for a leaner binary.
# The cache mounts are what keep this arm bearable when it is taken: without
# them every build recompiles the standard library and the whole dependency
# tree from an empty GOCACHE.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build \
    -trimpath \
    -tags "${GOTAGS}" \
    -ldflags "-s -w -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /out/octo ./runtime/octo

# --- Binary source: copy a host-built binary ----------------------------------
# TARGETARCH keys the directory so one form serves a native build and a
# --platform/multi-arch one alike.
FROM scratch AS bin-prebuilt-k8s
ARG TARGETARCH
COPY --from=prebuilt linux_${TARGETARCH}/octo-k8s /out/octo

FROM scratch AS bin-prebuilt-standalone
ARG TARGETARCH
COPY --from=prebuilt linux_${TARGETARCH}/octo /out/octo

FROM scratch AS bin-prebuilt-api
ARG TARGETARCH
COPY --from=prebuilt linux_${TARGETARCH}/octo-api /out/octo

FROM bin-${BIN_SOURCE} AS bin

# --- Final stage: minimal runtime image ---------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=bin /out/octo /usr/local/bin/octo
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/octo"]
# Load every integration YAML mounted into this directory.
CMD ["run", "--config", "/etc/octo/integrations"]
