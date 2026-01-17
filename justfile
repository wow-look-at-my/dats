# DATS - Declarative Automated Testing System

# Default recipe
default: build

# Build the dats binary
build:
    go fmt ./src/dats/...
    go vet ./src/dats/...
    go build -o dats ./src/dats

# Generate example tests (uses make for dependencies)
examples:
    make examples/example.gen.bats

# Run tests
test: examples
    bats examples/*.gen.bats

# Clean build artifacts (ignored files only)
clean:
    git clean -Xdf

# Symlink to ~/.local/bin
install: build
    mkdir -p ~/.local/bin
    ln -sf "$(pwd)/dats" ~/.local/bin/dats

# Build VS Code extension
vscode-build:
    cd src/vscode-dats && npm run build

# Install VS Code extension dependencies
vscode-install:
    cd src/vscode-dats && npm install

# Update VS Code extension dependencies
vscode-update:
    cd src/vscode-dats && npx npm-check-updates -u && npm install
