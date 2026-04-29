.DEFAULT_GOAL := help

PORT       ?= 8080
IMAGE      ?= pack-calculator:latest
DB_PATH    ?= ./data/pack-calculator.db

.PHONY: help
help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?##' $(MAKEFILE_LIST) | awk -F':.*##' '{printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## Resolve Go module dependencies
	go mod tidy

.PHONY: test
test: ## Run all unit tests with race detection
	go test -race ./...

.PHONY: cover
cover: ## Generate coverage report (./coverage.out, then ./coverage.html)
	go test -race -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "open coverage.html"

.PHONY: build
build: ## Build the binary into ./bin/pack-calculator
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/pack-calculator ./cmd/server

.PHONY: run
run: ## Run the server locally (DB_PATH=./data/...)
	mkdir -p $(dir $(DB_PATH))
	DB_PATH=$(DB_PATH) PORT=$(PORT) go run ./cmd/server

.PHONY: docker-build
docker-build: ## Build the Docker image
	docker build -t $(IMAGE) .

.PHONY: docker-run
docker-run: ## Run the Docker image with a named volume for persistence
	docker run --rm -it \
		-p $(PORT):8080 \
		-v pack-calculator-data:/data \
		--name pack-calculator \
		$(IMAGE)

.PHONY: docker-smoke
docker-smoke: docker-build ## Build, start, hit the API, tear down
	./scripts/docker-smoke.sh

.PHONY: clean
clean: ## Remove build artifacts and local DB state
	rm -rf bin coverage.out coverage.html data
