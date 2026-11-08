.PHONY: build test clean

# Build the native CLI used by ./toyota.
build:
	go build -o build/toyota ./cmd/toyota

# Run the Go CLI and Python runtime test suites.
test:
	go test ./...
	uv run pytest

# Remove compiled binaries, preserving the generated catalog and other data.
clean:
	rm -f build/toyota build/toyota-linux-arm64 build/toyota-windows-amd64.exe
