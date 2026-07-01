# syntax=docker/dockerfile:1

# ---- Build stage ---------------------------------------------------------
# A single build stage compiles every binary in the repo; the final image is
# chosen per-service via the docker-compose `command:` (gateway vs mockservice).
FROM golang:1.25-alpine AS build

WORKDIR /src

# Cache dependencies first for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static, stripped binaries.
ENV CGO_ENABLED=0
RUN go build -trimpath -ldflags="-s -w" -o /out/gateway     ./cmd/gateway && \
    go build -trimpath -ldflags="-s -w" -o /out/mockservice ./cmd/mockservice

# ---- Runtime stage -------------------------------------------------------
FROM alpine:3.20

# Non-root user and CA certs (for outbound TLS / OTLP).
RUN apk add --no-cache ca-certificates wget && \
    adduser -D -u 10001 gateway

WORKDIR /app
COPY --from=build /out/gateway     /app/gateway
COPY --from=build /out/mockservice /app/mockservice
COPY configs/ /app/configs/

USER gateway
EXPOSE 8080

# Default to the gateway; mock services override `command` in compose.
ENTRYPOINT ["/app/gateway"]
CMD ["-config", "/app/configs/config.docker.yaml"]
