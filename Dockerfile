# syntax=docker/dockerfile:1
# ==========================================
# Stage 1: Build the SPA (Vite + React, Bun)
# ==========================================
FROM --platform=$BUILDPLATFORM oven/bun:1.4-alpine AS frontend-builder
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bun x vite build --outDir /out --emptyOutDir

# ==========================================
# Stage 2: Build Backend (Go static binary)
# ==========================================
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS backend-builder
WORKDIR /src/backend

# Install CA certificates and timezone database
RUN apk --no-cache add ca-certificates tzdata

# Create unprivileged non-root user (UID 10001) and storage directory
RUN adduser -D -u 10001 -g "" -s /sbin/nologin warpstash && \
    mkdir -p /data/storage && \
    chown -R 10001:10001 /data

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
COPY --from=frontend-builder /out ./web/dist

# Build fully static binary (zero CGO) with embedded version and native cross-compilation
ARG TARGETOS
ARG TARGETARCH
ARG WARPSTASH_VERSION=v1.0.0
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X warpstash/internal/config.Version=${WARPSTASH_VERSION}" -o /app/warpstash ./cmd/warpstash

# ==========================================
# Stage 3: Ultra-Minimal Production Runner
# ==========================================
FROM scratch AS runner

WORKDIR /app

# System certificates and timezone data
COPY --from=backend-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=backend-builder /usr/share/zoneinfo /usr/share/zoneinfo

# Non-root user and pre-configured data directory
COPY --from=backend-builder /etc/passwd /etc/passwd
COPY --from=backend-builder --chown=10001:10001 /data /data

# Binary
COPY --from=backend-builder /app/warpstash /app/warpstash

# Run as non-root user
USER 10001:10001

# Default Environment Variables
ENV WARPSTASH_PORT=8080 \
    WARPSTASH_BASE_URL=http://localhost:8080 \
    WARPSTASH_STORAGE_PATH=/data/storage \
    WARPSTASH_DB_PATH=/data/warpstash.db \
    WARPSTASH_MAX_FILE_SIZE_MB=1024 \
    WARPSTASH_MAX_FILES=10 \
    WARPSTASH_MAX_TOTAL_STORAGE_GB=0 \
    WARPSTASH_DEFAULT_EXPIRY=24h \
    WARPSTASH_ALLOWED_EXPIRIES=1h,12h,24h,72h,burn \
    WARPSTASH_TRUST_PROXY=false \
    WARPSTASH_LOG_FORMAT=json \
    WARPSTASH_LOG_LEVEL=info

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/app/warpstash"]
