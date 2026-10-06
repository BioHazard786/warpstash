# Stage 1: Build Frontend (Astro 5 + Vite)
# ==========================================
FROM node:22-alpine AS frontend-builder
WORKDIR /app/web

# Copy package.json and install dependencies
COPY web/package.json ./
RUN npm install

# Copy source and root environment files
WORKDIR /app
COPY web/ ./web/
COPY .env* ./

# Build arguments with fallback defaults
ARG WARPSTASH_MAX_FILE_SIZE_MB=1024
ARG WARPSTASH_MAX_FILES=10
ENV WARPSTASH_MAX_FILE_SIZE_MB=$WARPSTASH_MAX_FILE_SIZE_MB
ENV WARPSTASH_MAX_FILES=$WARPSTASH_MAX_FILES

# Build frontend static bundle
WORKDIR /app/web
RUN npm run build

# ==========================================
# Stage 2: Build Backend (Go 1.26 Static Binary)
# ==========================================
FROM golang:1.26-alpine AS backend-builder
WORKDIR /app

# Install CA certificates and timezone database
RUN apk --no-cache add ca-certificates tzdata

# Create unprivileged non-root user (UID 10001) and storage directory
RUN adduser -D -u 10001 -g "" -s /sbin/nologin warpstash && \
    mkdir -p /data/storage && \
    chown -R 10001:10001 /data

# Download Go module dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code and compiled static frontend from Stage 1
COPY . .
COPY --from=frontend-builder /app/web/dist ./web/dist

# Build fully static binary (zero CGO)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/warpstash ./cmd/warpstash

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
