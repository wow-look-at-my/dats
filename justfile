# DATS - Declarative Automated Testing System

export BUILD_DIR := justfile_directory() / "build"

mod vscode 'src/vscode-dats'

_help:
    @just --list --list-submodules

# Build everything (Go binary + VS Code extension)
build output="dats": (_build-go output) (vscode::build)

# Run all tests with coverage
test: _build-go (vscode::test)
    go test -cover ./src/dats/...
    ./dats examples/example.dats examples/
    bats examples/*.gen.bats

[private]
clean:
    git clean -Xdf

# Symlink to ~/.local/bin
install: build
    mkdir -p ~/.local/bin
    ln -sf "$(pwd)/dats" ~/.local/bin/dats

_build-go output="dats":
    go fmt ./src/dats/...
    go vet ./src/dats/...
    go build -o {{output}} ./src/dats
