VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -s -w -X warpstash/internal/config.Version=$(VERSION)

.PHONY: all build-frontend build-backend build test run dev-web clean

all: build

# Vite writes straight into backend/web/dist, which the Go binary embeds.
build-frontend:
	cd web && bun install --frozen-lockfile && bun run build

build-backend:
	cd backend && CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o ../warpstash ./cmd/warpstash

build: build-frontend build-backend

test:
	cd backend && go test ./...
	cd web && bun run test

run: build
	./warpstash

# Vite dev server on :5173, proxying /api, /f/ and /upload to a backend on :8080.
dev-web:
	cd web && bun run dev

clean:
	rm -rf warpstash data/
	find backend/web/dist -mindepth 1 ! -name .gitkeep -delete
