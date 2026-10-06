.PHONY: all build-frontend build-backend build test run clean docker-build docker-up

all: build

build-frontend:
	cd web && pnpm install && pnpm build

build-backend:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o warpstash ./cmd/warpstash

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
