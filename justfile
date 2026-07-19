# DATS - Declarative Automated Testing System

export BUILD_DIR := justfile_directory() / "build"
export COVER_DIR := justfile_directory() / "coverage"

_help:
    @just --list

# Build the binary
build output="$BUILD_DIR/dats":
    go fmt ./...
    go vet ./...
    go build -o {{output}} .

# Build with coverage instrumentation
build-cover output="$BUILD_DIR/dats":
    go fmt ./...
    go vet ./...
    go build -cover -o {{output}} .

# Run all tests with coverage
test: build
    go test -cover ./...
    "$BUILD_DIR/dats" examples/example.dats

# Run all tests and collect binary coverage data
test-cover: build-cover
    go test -cover ./...
    rm -rf "$COVER_DIR" && mkdir -p "$COVER_DIR"
    GOCOVERDIR="$COVER_DIR" "$BUILD_DIR/dats" examples/example.dats
    go tool covdata percent -i="$COVER_DIR"
    go tool covdata textfmt -i="$COVER_DIR" -o="$COVER_DIR/coverage.txt"
    @echo "Coverage profile: $COVER_DIR/coverage.txt"

[private]
clean:
    git clean -Xdf

# Symlink to ~/.local/bin
install: build
    mkdir -p ~/.local/bin
    ln -sf "$BUILD_DIR/dats" ~/.local/bin/dats
