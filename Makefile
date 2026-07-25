GO ?= go
BIN := bin/folicular

# Host port published by `docker compose` (the container always listens on
# 8080). Override when 8080 is taken: make compose-up FOLICULAR_HOST_PORT=18080
FOLICULAR_HOST_PORT ?= 8080

.PHONY: build run test vet tidy sqlc ci smoke compose-up compose-down compose-logs

build:
	$(GO) build -o $(BIN) ./cmd/server

run:
	$(GO) run ./cmd/server

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

# Requires sqlc (https://sqlc.dev): go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
sqlc:
	sqlc generate

ci: vet test build

# Starts the server and exercises the core API flow (register, push, pull, predictions).
smoke: build
	./scripts/smoke.sh

# Docker Compose dev / real-device-trial backend (see README "Running with
# Docker Compose"). The container listens on 8080 and is published on
# $(FOLICULAR_HOST_PORT).
compose-up:
	FOLICULAR_HOST_PORT=$(FOLICULAR_HOST_PORT) docker compose up -d --build

compose-down:
	docker compose down

compose-logs:
	docker compose logs -f folicular
