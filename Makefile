.PHONY: build test clean

# Build the native CLI used by ./toyo.
build:
	go build -o build/toyo ./cmd/toyo

# Run application and transport tests.
test:
	go test ./...

# Remove compiled binaries, preserving the generated catalog and other data.
clean:
	rm -f build/toyo build/toyo-linux-arm64 build/toyo-windows-amd64.exe
