.PHONY: run build fmt vet test lint tidy migrate-up migrate-down docker-up docker-down docker-build docker-logs
APP_NAME := server
BIN_DIR := bin
MIGRATIONS_DIR := migrations
run: ; go run ./cmd/server
build: ; CGO_ENABLED=0 go build -o $(BIN_DIR)/$(APP_NAME) ./cmd/server
fmt: ; go fmt ./...
vet: ; go vet ./...
test: ; go test ./... -v
lint: ; golangci-lint run
tidy: ; go mod tidy
migrate-up: ; migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up
migrate-down: ; migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1
docker-up: ; docker compose up --build
docker-down: ; docker compose down
docker-build: ; docker compose build app
docker-logs: ; docker compose logs -f app
