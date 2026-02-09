# DATS - Declarative Automated Testing System

export BUILD_DIR := justfile_directory() / "build"

_help:
    @just --list

# Generate Go code from XSD schema
generate:
    go generate ./internal/schema/...

# Build the binary
build output="$BUILD_DIR/dats":
    go fmt ./...
    go vet ./...
    go build -o {{output}} .

# Run all tests with coverage
test: build
    go test -cover ./...
    "$BUILD_DIR/dats" examples/example.dats

[private]
clean:
    git clean -Xdf

# Symlink to ~/.local/bin
install: build
    mkdir -p ~/.local/bin
    ln -sf "$BUILD_DIR/dats" ~/.local/bin/dats
