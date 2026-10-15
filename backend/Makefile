.PHONY: run build test migrate-up migrate-down docker-up docker-down lint clean setup env deps dev install-tools fmt vet coverage

# Default variables
BINARY_NAME=safearound-backend
MAIN_PATH=cmd/api/main.go

# --- Environment Setup Commands ---

# Run initial project setup (Copy .env, download dependencies, start Postgres and Redis)
setup: env deps docker-up

# Generate .env from .env.example
env:
	@echo "Copying .env.example to .env..."
	@cp -n .env.example .env || echo ".env already exists"

# Install Go dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod tidy
	@go mod download

# Start development server with hot reload (Air)
dev:
	@echo "Starting hot-reload server with Air..."
	@air -c .air.toml

# --- Quality & Tooling ---

# Install tools (Air for hot-reload and Golangci for Linting)
install-tools:
	@echo "Installing Go tools..."
	@go install github.com/cosmtrek/air@latest
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Format code
fmt:
	@echo "Formatting Go files..."
	@go fmt ./...

# Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...

# Run linters (install with `make install-tools`)
lint:
	@echo "Running golangci-lint..."
	@golangci-lint run ./...

# Run tests and print coverage per function
coverage:
	@echo "Running tests with coverage..."
	@ENV=testing go test -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out

# --- Standard Commands ---

# Start development server (standard)
run:
	@echo "Starting development server..."
	go run $(MAIN_PATH)

# Build binary
build:
	@echo "Building application..."
	go build -o bin/$(BINARY_NAME) $(MAIN_PATH)

# Run standard tests (no coverage gate)
test:
	@echo "Running tests..."
	go test -v ./...

# Run database migrations
migrate-up:
	@echo "Running migrations UP..."
	@bash scripts/migrate.sh up

# Rollback migrations
migrate-down:
	@echo "Rolling back migrations DOWN..."
	@bash scripts/migrate.sh down

# Start Docker containers
docker-up:
	@echo "Starting Docker containers..."
	docker-compose up -d

# Stop Docker containers
docker-down:
	@echo "Stopping Docker containers..."
	docker-compose down

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -rf tmp/
	rm -rf coverage.out
