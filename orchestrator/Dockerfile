# Multi-stage build for the orchestrator API.
# Build context is the orchestrator/ dir (self-contained module, no workspace).
#
# BIN_SOURCE picks where the binary comes from:
#   compile   (default) build it here. What Cloud Build and release CI use.
#   prebuilt  copy the host-built binary from the `prebuilt` named build context,
#             which task bins:linux fills:
#               task bins:linux
#               docker build --build-arg BIN_SOURCE=prebuilt \
#                 --build-context prebuilt=dist/bin \
#                 -t octo-orchestrator:dev -f orchestrator/Dockerfile ./orchestrator
#
# A NAMED context and not a path in the one above, because this image's context
# is the module dir: dist/bin sits at the repo root, where a COPY from here
# cannot reach it.
ARG BIN_SOURCE=compile

# --- Binary source: compile from the module -----------------------------------
FROM golang:1.25-alpine AS bin-compile
WORKDIR /src

# Cache deps separately from source.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -o /out/orchestrator .

# --- Binary source: copy the host-built binary --------------------------------
# TARGETARCH keys the directory so one form serves a native build and a
# --platform/multi-arch one alike.
FROM scratch AS bin-prebuilt
ARG TARGETARCH
COPY --from=prebuilt linux_${TARGETARCH}/orchestrator /out/orchestrator

FROM bin-${BIN_SOURCE} AS bin

# Minimal runtime image.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=bin /out/orchestrator /orchestrator
EXPOSE 8090
USER nonroot:nonroot
ENTRYPOINT ["/orchestrator"]
