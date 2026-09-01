# Define the default target to be executed when 'make' is called without specifying a target.
.DEFAULT_GOAL := start

BINARY    := ./cmd/zest.o
GOPATH    := $(shell go env GOPATH)

.PHONY: build clean start run fmt vet lint test tidy validate-push docker-build docker-clean stub e2e

# Build the zest binary.
build:
	@echo "Building zest..."
	go build -o $(BINARY) ./cmd

# Clean up the built application.
clean:
	@echo "Cleaning zest..."
	rm -f $(BINARY)

# Start the previously built binary. Configuration comes from the environment.
start:
	@echo "Starting zest..."
	$(BINARY)

# Build and run in one step.
run:
	@echo "Running zest..."
	go run ./cmd

# Format the code.
fmt:
	@echo "Formatting..."
	gofmt -l -w .

# Report suspicious constructs.
vet:
	@echo "Vetting..."
	go vet ./...

# Run linters using golangci-lint.
lint:
	golangci-lint run

# Run the test suite.
test:
	@echo "Running tests..."
	go test ./... -race -count=1

# Run the stub Slack/Zendesk upstreams on :8777 (no credentials needed).
stub:
	go run ./scripts/stub

# Exercise all four flows end to end against the stubs.
e2e:
	./scripts/e2e.sh

# Tidy the module graph.
tidy:
	go mod tidy

# Clean unused Docker images.
docker-clean:
	@echo "Cleaning unused images..."
	docker image prune -f

# Run various validation tasks before pushing code changes.
validate-push:
	@echo ""
	@echo "========== Step 1: Tidy Go modules =========="
	go mod tidy
	@echo ""
	@echo ""
	@echo ""

	@echo "========== Step 2: Formatting the code =========="
	make fmt
	@echo ""
	@echo ""
	@echo ""

	@echo "========== Step 3: Vetting the code =========="
	make vet
	@echo ""
	@echo ""
	@echo ""

	@echo "========== Step 4: Linting the code =========="
	make lint
	@echo ""
	@echo ""
	@echo ""

	@echo "========== Step 5: Running tests =========="
	make test
	@echo ""
	@echo ""
	@echo ""

	@echo "========== Step 6: Build the project =========="
	make build
	@echo ""
	@echo ""
	@echo ""

	@echo "========== Step 7: Cleanup build artifacts =========="
	make clean
	@echo ""
	@echo ""
	@echo ""

	@echo "✅ All validation steps completed successfully."
