# Stage 1: Builder
FROM golang:1.22-alpine AS builder

ARG VERSION=dev

WORKDIR /build

RUN apk add --no-cache git ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
    -ldflags="-s -w \
      -X github.com/myorg/docker-cleanup-agent/internal/version.Version=${VERSION} \
      -X github.com/myorg/docker-cleanup-agent/internal/version.BuildTime=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /docker-cleanup-agent \
    ./cmd/agent

# Stage 2: Runtime (scratch = minimal attack surface)
FROM scratch

# CA certs for HTTPS (public IP lookup + SMTP TLS)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy the statically-linked binary
COPY --from=builder /docker-cleanup-agent /docker-cleanup-agent

# Run as nobody (UID 65534)
# Note: if log truncation fails due to permissions, either:
#   (a) run docker-compose with user: "0:0", or
#   (b) grant group-write on /var/lib/docker/containers on the host
USER 65534:65534

EXPOSE 8080

ENTRYPOINT ["/docker-cleanup-agent"]
