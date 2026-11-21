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

# Storage directory, owned by the runtime user (UID 10001, kept from earlier images so existing
# volumes stay writable). A new named volume inherits this ownership from the image.
RUN mkdir -p /data/storage && chown -R 10001:10001 /data

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
COPY --from=frontend-builder /out ./web/dist

# Build fully static binary (zero CGO) with embedded version and native cross-compilation
ARG TARGETOS
ARG TARGETARCH
ARG WARPSTASH_VERSION=dev
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w -X warpstash/internal/config.Version=${WARPSTASH_VERSION}" -o /app/warpstash ./cmd/warpstash

# ==========================================
# Stage 3: Minimal Production Runner (distroless: CA certs, tzdata, no shell)
# ==========================================
FROM gcr.io/distroless/static:nonroot AS runner

WORKDIR /app

COPY --from=backend-builder --chown=10001:10001 /data /data
COPY --from=backend-builder /app/warpstash /app/warpstash

# Numeric user: no passwd entry needed. Not distroless' default 65532, so existing volumes keep working.
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
    WARPSTASH_ALLOWED_EXPIRIES=1h,24h,7d,30d,burn \
    WARPSTASH_MAX_EXPIRY=30d \
    WARPSTASH_TRUST_PROXY=false \
    WARPSTASH_LOG_FORMAT=json \
    WARPSTASH_LOG_LEVEL=info

VOLUME ["/data"]
EXPOSE 8080

# No curl in the image; the binary probes its own /healthz.
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 CMD ["/app/warpstash", "-healthcheck"]

ENTRYPOINT ["/app/warpstash"]
