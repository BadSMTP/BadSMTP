# BadSMTP SMTP Test Server Makefile

BINARY_NAME=badsmtp
GO_VERSION=1.25.5
PLATFORMS=linux/amd64 linux/arm64 linux/riscv64 darwin/amd64 darwin/arm64 windows/amd64

# Default target
all: build

# Initialise Go module and download dependencies
init:
	go mod init badsmtp
	go get github.com/joho/godotenv

# Build for current platform
# Respects GOOS and GOARCH environment variables for cross-compilation
build:
	# Build the main module in the current directory
	go build -o ${BINARY_NAME} .

# Build for all platforms
build-all:
	@for platform in $(PLATFORMS); do \
		GOOS=$$(echo $$platform | cut -d'/' -f1); \
		GOARCH=$$(echo $$platform | cut -d'/' -f2); \
		OUT=${BINARY_NAME}-$${GOOS}-$${GOARCH}; \
		if [ "$$GOOS" = "windows" ]; then OUT=$${OUT}.exe; fi; \
		echo "Building for $$GOOS/$$GOARCH..."; \
		GOOS=$$GOOS GOARCH=$$GOARCH go build -o $$OUT .; \
	done

# Run the server
run: build
	./${BINARY_NAME}

# Run with custom port
run-port:
	./${BINARY_NAME} -port=2525

# Run tests
test:
	go test ./...

# Run fast tests only
test-fast:
	FAST_TESTS=true ./run_tests.sh

# Clean build artifacts
clean:
	rm -f ${BINARY_NAME}*
	rm -rf mailbox/

# Install dependencies
deps:
	go mod tidy
	go mod download

# Create a Docker image
docker:
	docker build -t badsmtp:latest .

# Format code
fmt:
	goreturns -w ./...

# Lint code
lint:
	golangci-lint run

# Show help
help:
	@echo "Available targets:"
	@echo "  init       - Initialise Go module and download dependencies"
	@echo "  build      - Build for current platform"
	@echo "  build-all  - Build for all platforms"
	@echo "  run        - Build and run the server"
	@echo "  run-port   - Run with custom port"
	@echo "  test       - Run tests"
	@echo "  test-fast  - Run fast tests only"
	@echo "  clean      - Clean build artifacts"
	@echo "  deps       - Install dependencies"
	@echo "  docker     - Build Docker image"
	@echo "  fmt        - Format code"
	@echo "  lint       - Lint code"
	@echo "  help       - Show this help"

.PHONY: all init build build-all run run-port test test-fast clean deps docker fmt lint help
