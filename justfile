# DATS - Declarative Automated Testing System

mod vscode 'src/vscode-dats'

_help:
    @just --list --list-submodules

# Build everything (Go binary + VS Code extension)
build: _build-go (vscode::build)

# Run all tests with coverage
test: _build-go
    go test -cover ./src/dats/...
    cd src/vscode-dats && npm test
    ./dats examples/example.dats examples/
    bats examples/*.gen.bats

[private]
clean:
    git clean -Xdf

# Symlink to ~/.local/bin
install: build
    mkdir -p ~/.local/bin
    ln -sf "$(pwd)/dats" ~/.local/bin/dats

_build-go:
    go fmt ./src/dats/...
    go vet ./src/dats/...
    go build -o dats ./src/dats
