SHELL := /bin/bash
.DEFAULT_GOAL := help

MODULE := github.com/mmk31585/updater-service
ENTRY_DIR := cmd/entry
WORKER_DIR := cmd/worker
MIGRATIONS_DIR := migrations

GO := go
GOTEST := $(GO) test
GOFMT := gofmt
GOOSE := go run github.com/pressly/goose/v3/cmd/goose

DOCKER_COMPOSE := docker compose

ifneq ($(wildcard .env),)
  include .env
  export $(shell sed 's/=.*//' .env)
endif

.PHONY: help build-entry build-worker build run-entry run-worker run test test-cover lint fmt fmt-check vet tidy deps clean migrate-up migrate-down migrate-status migrate-create docker-up docker-down docker-logs docker-up-entry docker-up-worker dev

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build-entry: ## Build the entry service binary
	@echo "Building entry service..."
	@$(GO) build -ldflags="-s -w" -o bin/entry ./$(ENTRY_DIR)

build-worker: ## Build the worker service binary
	@echo "Building worker service..."
	@$(GO) build -ldflags="-s -w" -o bin/worker ./$(WORKER_DIR)

build: build-entry build-worker ## Build both entry and worker services

run-entry: ## Run the entry service locally
	@echo "Running entry service..."
	@$(GO) run ./$(ENTRY_DIR)

run-worker: ## Run the worker service locally
	@echo "Running worker service..."
	@$(GO) run ./$(WORKER_DIR)

run: run-entry ## Run the entry service (default)

test: ## Run all tests
	@echo "Running tests..."
	@$(GOTEST) -v ./...

test-cover: ## Run tests with coverage report
	@echo "Running tests with coverage..."
	@$(GOTEST) -coverprofile=coverage.out ./...
	@$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

lint: ## Run golangci-lint
	@echo "Running linter..."
	@golangci-lint run ./...

fmt: ## Format Go source files
	@echo "Formatting..."
	@$(GOFMT) -w -s .

fmt-check: ## Check formatting without modifying files
	@echo "Checking formatting..."
	@test -z $$($(GOFMT) -d -s . | tee /dev/stderr)

vet: ## Run go vet
	@echo "Running go vet..."
	@$(GO) vet ./...

tidy: ## Tidy go modules
	@echo "Tidying modules..."
	@$(GO) mod tidy

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	@$(GO) mod download

clean: ## Remove build artifacts
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -f coverage.out coverage.html
	@echo "Clean complete."

migrate-up: ## Run database migrations up
	@echo "Running migrations up..."
	@if [ ! -d "$(MIGRATIONS_DIR)" ]; then echo "Migrations directory not found"; exit 1; fi
	@$(GOOSE) -dir $(MIGRATIONS_DIR) mysql "$(DB_USER):$(DB_PASSWORD)@tcp($(DB_HOST):$(DB_PORT))/$(DB_NAME)" up

migrate-down: ## Run database migrations down (all)
	@echo "Running migrations down..."
	@$(GOOSE) -dir $(MIGRATIONS_DIR) mysql "$(DB_USER):$(DB_PASSWORD)@tcp($(DB_HOST):$(DB_PORT))/$(DB_NAME)" down

migrate-status: ## Check migration status
	@echo "Checking migration status..."
	@$(GOOSE) -dir $(MIGRATIONS_DIR) mysql "$(DB_USER):$(DB_PASSWORD)@tcp($(DB_HOST):$(DB_PORT))/$(DB_NAME)" status

migrate-create: ## Create a new migration file (usage: make migrate-create name=migration_name)
	@if [ -z "$(name)" ]; then echo "Usage: make migrate-create name=<migration_name>"; exit 1; fi
	@echo "Creating migration: $(name)"
	@$(GOOSE) -dir $(MIGRATIONS_DIR) create $(name) sql

docker-up: ## Start services with docker-compose
	@echo "Starting services..."
	@$(DOCKER_COMPOSE) up -d
	@echo "Services started:"
	@echo "  - NATS:              localhost:4222"
	@echo "  - NATS Monitor:      http://localhost:8222"
	@echo "  - MariaDB:           localhost:3306"
	@echo "  - Entry:             localhost:8080"
	@echo "  - Worker:            (logs via docker logs update-worker)"

docker-up-entry: ## Start entry service only
	@echo "Starting entry service..."
	@$(DOCKER_COMPOSE) up -d --build entry
	@echo "Entry service started: localhost:8080"

docker-up-worker: ## Start worker service only
	@echo "Starting worker service..."
	@$(DOCKER_COMPOSE) up -d --build worker
	@echo "Worker service started"

docker-entry-logs: ## Tail logs from entry service
	@$(DOCKER_COMPOSE) logs -f update-entry

docker-worker-logs: ## Tail logs from worker service
	@$(DOCKER_COMPOSE) logs -f update-worker

docker-down: ## Stop all services
	@echo "Stopping services..."
	@$(DOCKER_COMPOSE) down

docker-logs: ## Tail logs from all services
	@$(DOCKER_COMPOSE) logs -f

dev: fmt vet test build ## Run full dev pipeline (fmt, vet, test, build)
