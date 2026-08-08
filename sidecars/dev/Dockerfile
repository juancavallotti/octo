# Multi-stage build for the dev sidecar.
# Build context is the sidecars/ dir (self-contained module, no workspace), so a
# second sidecar builds from the same context with its own -f path.
FROM golang:1.25-alpine AS build
WORKDIR /src

# No go.sum, because the module has no dependencies — deliberately. This process
# runs beside every dev run and holds a token to an integration's secrets, so it is
# worth keeping free of a dependency tree; everything it needs is in the standard
# library. Add go.sum to this COPY if that ever stops being true.
COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/dev-sidecar ./dev

# Minimal runtime image.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dev-sidecar /dev-sidecar
EXPOSE 8099
# The same nonroot uid the runtime image runs as (runtime/Dockerfile). That match
# is what lets both containers read and write the emptyDir they share — the sidecar
# writes the workspace, the runtime watches it.
USER nonroot:nonroot
ENTRYPOINT ["/dev-sidecar"]
