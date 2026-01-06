#!/usr/bin/env bash

# Navigate to the project root directory (assuming script is run from scripts/)
cd "$(dirname "$0")/.."

# Create a temporary file for coverage output
COVERAGE_FILE="coverage.out"

# Run all tests with coverage profile
echo "Running tests..."
go test -coverprofile="$COVERAGE_FILE" ./...
TEST_RESULT=$?

# Check if tests passed (exit code 0 means success)
if [ $TEST_RESULT -eq 0 ]; then
    echo "All tests passed!"
    echo "Generating coverage summary..."
    go tool cover -func="$COVERAGE_FILE"
else
    echo "Tests failed. Coverage summary not generated."
fi

# Clean up the coverage file
rm -f "$COVERAGE_FILE"

exit $TEST_RESULT
