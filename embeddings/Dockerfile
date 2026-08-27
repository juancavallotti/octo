# syntax=docker/dockerfile:1
# The embedding server (octo-embeddings).
#
# The standalone octo binary plus one small app, baked in. It is an image rather
# than the generic runtime image with a mounted ConfigMap because this is a
# platform component, like the log aggregator beside it, and every other
# component here ships as an image with its config compiled in. There is nothing
# per-installation about the app itself — what varies is four environment
# variables.
#
# STANDALONE, not the k8s build. This server needs none of the cluster services:
# no KV, no leases, no cluster queues, no orchestrator to register with. The k8s
# binary would refuse to start without RUNTIME_SERVICES_MODULE and the
# deployment identity behind it, which is a dependency on the orchestrator that
# would make the embedding server unavailable exactly when the orchestrator is
# the thing being diagnosed.
#
# Build context is the repo ROOT (the Go module is rooted there):
#   docker build -t octo-embeddings:dev -f embeddings/Dockerfile .
ARG BIN_SOURCE=compile

# --- Binary source: compile from the Go module --------------------------------
FROM golang:1.27-alpine AS bin-compile
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
COPY runtime/ ./runtime/
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /out/octo ./runtime/octo

# --- Binary source: copy a host-built binary ----------------------------------
# The untagged binary, which is the standalone one — see runtime/Dockerfile for
# why the provider is folded into BIN_SOURCE rather than left to a second arg.
FROM scratch AS bin-prebuilt
ARG TARGETARCH
COPY --from=prebuilt linux_${TARGETARCH}/octo /out/octo

FROM bin-${BIN_SOURCE} AS bin

# --- Final stage --------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=bin /out/octo /usr/local/bin/octo
COPY embeddings/config.yaml /etc/octo/embeddings/config.yaml
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/octo"]
CMD ["run", "--config", "/etc/octo/embeddings"]
