# Makefile for GoTodo
# Usage: run `make <target>`

.PHONY: help tidy build run run-api test lint air install-air docker-build docker-run fmt clean kill restart seed

## help: Show this help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## ' Makefile | sort | awk 'BEGIN {FS":.*?## "}; {printf "\033[36m%-18s\033[0m %s\n", $$1, $$2}'

## tidy: Download and tidy modules
tidy:
	go mod tidy

## build: Build API binary
build: tidy
	go build -o bin/gotodo ./cmd/api

## run: Run API directly (no hot reload)
run:
	go run ./cmd/api

## run-api: Alias for run
run-api: run ## Run the API server

## kill: Kill any running Go processes on port 8080
kill:
	@echo "Killing any processes on port 8080..."
	@lsof -ti:8080 | xargs kill -9 2>/dev/null || echo "No processes found on port 8080"

## restart: Kill existing server and run fresh
restart: kill run ## Kill server and restart

## test: Run tests (if any)
test:
	go test ./...

## lint: Run basic static checks (go vet)
lint:
	go vet ./...

## fmt: Format code
fmt:
	go fmt ./...



## install-air: Install Air live-reload tool
install-air:
	go install github.com/air-verse/air@latest

## air: Run with hot-reload (recommended for development)
air:
	$$(go env GOPATH)/bin/air -c .air.toml

## docker-build: Build Docker image
docker-build:
	docker build -t gotodo:latest .

## docker-run: Run Docker container on port 8080
docker-run:
	docker run --rm -p 8080:8080 --env-file .env gotodo:latest

## clean: Remove build artifacts
clean:
	rm -rf bin

## seed: Seed the database with persona poems and likes (run once from project root)
seed:
	go run ./cmd/seed

## db-reset: Drop the chat_db database to start fresh
db-reset:
	mongosh chat_db --eval "db.dropDatabase()"

## ngrok: Start ngrok tunnel
ngrok:
	ngrok http 8080