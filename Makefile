.PHONY: build test clean

# Build the native CLI used by ./toyo.
build:
	go build -o build/toyo ./cmd/toyo

# Run the Go CLI and Python runtime test suites.
test:
	go test ./...
	uv run pytest

# Remove compiled binaries, preserving the generated catalog and other data.
clean:
	rm -f build/toyo build/toyo-linux-arm64 build/toyo-windows-amd64.exe
