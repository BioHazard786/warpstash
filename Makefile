VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS = -s -w -X warpstash/internal/config.Version=$(VERSION)

.PHONY: all build-frontend build-backend build test run clean docker-build docker-up

all: build

build-frontend:
	cd web && pnpm install && pnpm build

build-backend:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o warpstash ./cmd/warpstash

build: build-frontend build-backend

test:
	go test -v ./...

run: build
	./warpstash

docker-build:
	docker compose build

docker-up:
	docker compose up -d

clean:
	rm -rf warpstash web/dist data/
