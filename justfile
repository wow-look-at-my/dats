# DATS - Declarative Automated Testing System

export BUILD_DIR := justfile_directory() / "build"

mod dats 'src/dats'
mod vscode 'src/vscode-dats'

_help:
    @just --list --list-submodules

# Build everything (Go binary + VS Code extension)
build: (dats::build) (vscode::build)

# Run all tests with coverage
test: (dats::build) (vscode::build) && (dats::test)
    "$BUILD_DIR/dats" examples/example.dats examples/
    bats examples/*.gen.bats

[private]
clean:
    git clean -Xdf

# Symlink to ~/.local/bin
install: build
    mkdir -p ~/.local/bin
    ln -sf "$BUILD_DIR/dats" ~/.local/bin/dats
