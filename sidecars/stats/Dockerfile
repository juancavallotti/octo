# syntax=docker/dockerfile:1
# Multi-stage build for the stats sidecar.
# Build context is the sidecars/ dir, the same as the dev sidecar's, so both
# build from one context with their own -f path.
#
# BIN_SOURCE picks where the binary comes from:
#   compile   (default) build it here. What Cloud Build and release CI use.
#   prebuilt  copy the host-built binary from the `prebuilt` named build context,
#             which task bins:linux fills:
#               task bins:linux
#               docker build --build-arg BIN_SOURCE=prebuilt \
#                 --build-context prebuilt=dist/bin \
#                 -t octo-statssidecar:dev -f sidecars/stats/Dockerfile ./sidecars
#
# A NAMED context and not a path in the one above, because this image's context
# is the module dir: dist/bin sits at the repo root, where a COPY from here
# cannot reach it.
ARG BIN_SOURCE=compile

# --- Binary source: compile from the module -----------------------------------
FROM golang:1.27-alpine AS bin-compile
WORKDIR /src

# go.sum as well as go.mod, because this sidecar has dependencies: a Redis
# client and a Prometheus exposition parser. Both are load-bearing — it writes
# to Redis, and it has to know a counter from a gauge to collapse a bucket
# correctly, which only the TYPE lines say.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -o /out/stats-sidecar ./stats

# --- Binary source: copy the host-built binary --------------------------------
# TARGETARCH keys the directory so one form serves a native build and a
# --platform/multi-arch one alike.
FROM scratch AS bin-prebuilt
ARG TARGETARCH
COPY --from=prebuilt linux_${TARGETARCH}/stats-sidecar /out/stats-sidecar

FROM bin-${BIN_SOURCE} AS bin

# Minimal runtime image.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=bin /out/stats-sidecar /stats-sidecar
EXPOSE 8098
# The same nonroot uid the runtime image runs as (runtime/Dockerfile). Nothing
# is shared between the two containers here — this one holds no volume and
# writes no files — but running as the same unprivileged user as its neighbour
# is the property worth keeping uniform across the images.
USER nonroot:nonroot
ENTRYPOINT ["/stats-sidecar"]
