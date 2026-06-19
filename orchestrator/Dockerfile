# Multi-stage build for the orchestrator API.
# Build context is the orchestrator/ dir (self-contained module, no workspace).
FROM golang:1.25-alpine AS build
WORKDIR /src

# Cache deps separately from source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/orchestrator .

# Minimal runtime image.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/orchestrator /orchestrator
EXPOSE 8090
USER nonroot:nonroot
ENTRYPOINT ["/orchestrator"]
