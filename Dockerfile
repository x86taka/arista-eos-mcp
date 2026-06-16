# syntax=docker/dockerfile:1

# ---- build stage ----
FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Build the static binary.
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/arista-eos-mcp ./cmd/server

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/arista-eos-mcp /usr/local/bin/arista-eos-mcp

# HTTP transport listens here by default (override with MCP_HTTP_ADDR).
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/arista-eos-mcp"]
