#!/bin/bash

# BadSMTP Test Runner
# This script runs all tests for the BadSMTP server

set -e

# Check for FAST_TESTS environment variable
FAST_TESTS=${FAST_TESTS:-false}

echo "🧪 Running BadSMTP Test Suite"
echo "=============================="

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print status
print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if Go is installed
if ! command -v go &> /dev/null; then
    print_error "Go is not installed or not in PATH"
    exit 1
fi

# Check Go version
GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
print_status "Go version: $GO_VERSION"

# Clean previous test results
print_status "Cleaning previous test artifacts..."
rm -rf coverage.out
rm -rf test_results.xml

# Run go mod tidy to ensure dependencies are clean
print_status "Tidying Go modules..."
go mod tidy

# Run tests by package
print_status "Running unit tests..."

# Test auth package
print_status "Testing auth package..."
go test -v ./auth/... -coverprofile=auth_coverage.out

# Test smtp package  
print_status "Testing smtp package..."
go test -v ./smtp/... -coverprofile=smtp_coverage.out

# Test storage package
print_status "Testing storage package..."
go test -v ./storage/... -coverprofile=storage_coverage.out

# Determine which server tests to run based on FAST_TESTS
if [ "$FAST_TESTS" = "true" ]; then
    print_status "Testing server package (fast tests only)..."
    go test -v -tags=fasttests ./server/... -coverprofile=server_coverage.out
else
    print_status "Testing server package..."
    go test -v ./server/... -coverprofile=server_coverage.out
fi

# Run integration tests - only run one basic test in fast mode
if [ "$FAST_TESTS" = "true" ]; then
    print_status "Running basic integration test (fast mode)..."
    go test -v -run TestSMTPIntegration/BasicSMTPSession . -coverprofile=integration_coverage.out
else
    print_status "Running integration tests..."
    go test -v . -run TestSMTPIntegration -coverprofile=integration_coverage.out
fi

# Run TLS tests - always run in both modes since they're not slow
print_status "Running TLS tests..."
go test -v ./server -run Test.*TLS -coverprofile=tls_coverage.out

# Combine coverage reports
print_status "Combining coverage reports..."
echo "mode: atomic" > combined_coverage.out
tail -n +2 auth_coverage.out >> combined_coverage.out
tail -n +2 smtp_coverage.out >> combined_coverage.out
tail -n +2 storage_coverage.out >> combined_coverage.out
tail -n +2 server_coverage.out >> combined_coverage.out
tail -n +2 integration_coverage.out >> combined_coverage.out
tail -n +2 tls_coverage.out >> combined_coverage.out

# Generate coverage report
print_status "Generating coverage report..."
go tool cover -html=combined_coverage.out -o coverage.html
go tool cover -func=combined_coverage.out > coverage.txt

# Display coverage summary
print_status "Coverage Summary:"
echo "==================="
tail -n 1 coverage.txt

# Run benchmarks only in full test mode
if [ "$FAST_TESTS" != "true" ]; then
    print_status "Running benchmarks..."
    go test -bench=. -benchmem ./... > benchmark_results.txt
fi

# Run race condition tests only in full test mode
if [ "$FAST_TESTS" != "true" ]; then
    print_status "Running race condition tests..."
    go test -race ./...
fi

# Run tests with different build tags only in full test mode
if [ "$FAST_TESTS" != "true" ]; then
    print_status "Running tests with different build constraints..."
    go test -tags=integration ./...
fi

# Cleanup temporary files
print_status "Cleaning up temporary files..."
rm -f *_coverage.out

print_status "Test suite completed successfully!"
print_status "Coverage report generated: coverage.html"
print_status "Detailed coverage: coverage.txt"

if [ "$FAST_TESTS" != "true" ]; then
    print_status "Benchmark results: benchmark_results.txt"
fi

echo ""
echo "📊 Test Results Summary"
echo "======================"
echo "✅ Unit tests: PASSED"
echo "✅ Integration tests: PASSED"
echo "✅ TLS tests: PASSED"

if [ "$FAST_TESTS" != "true" ]; then
    echo "✅ Race condition tests: PASSED"
    echo "✅ Benchmarks: COMPLETED"
fi

echo ""
echo "🎉 All tests passed!"