# syntax=docker/dockerfile:1
# Multi-stage build for the dev sidecar.
# Build context is the sidecars/ dir (self-contained module, no workspace), so a
# second sidecar builds from the same context with its own -f path.
#
# BIN_SOURCE picks where the binary comes from:
#   compile   (default) build it here. What Cloud Build and release CI use.
#   prebuilt  copy the host-built binary from the `prebuilt` named build context,
#             which task bins:linux fills:
#               task bins:linux
#               docker build --build-arg BIN_SOURCE=prebuilt \
#                 --build-context prebuilt=dist/bin \
#                 -t octo-devsidecar:dev -f sidecars/dev/Dockerfile ./sidecars
#
# A NAMED context and not a path in the one above, because this image's context
# is the module dir: dist/bin sits at the repo root, where a COPY from here
# cannot reach it.
ARG BIN_SOURCE=compile

# --- Binary source: compile from the module -----------------------------------
FROM golang:1.27-alpine AS bin-compile
WORKDIR /src

# No go.sum, because the module has no dependencies — deliberately. This process
# runs beside every dev run and holds a token to an integration's secrets, so it is
# worth keeping free of a dependency tree; everything it needs is in the standard
# library. Add go.sum to this COPY if that ever stops being true.
COPY go.mod ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -o /out/dev-sidecar ./dev

# --- Binary source: copy the host-built binary --------------------------------
# TARGETARCH keys the directory so one form serves a native build and a
# --platform/multi-arch one alike.
FROM scratch AS bin-prebuilt
ARG TARGETARCH
COPY --from=prebuilt linux_${TARGETARCH}/dev-sidecar /out/dev-sidecar

FROM bin-${BIN_SOURCE} AS bin

# Minimal runtime image.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=bin /out/dev-sidecar /dev-sidecar
EXPOSE 8099
# The same nonroot uid the runtime image runs as (runtime/Dockerfile). That match
# is what lets both containers read and write the emptyDir they share — the sidecar
# writes the workspace, the runtime watches it.
USER nonroot:nonroot
ENTRYPOINT ["/dev-sidecar"]
