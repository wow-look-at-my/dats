# DATS - Declarative Automated Testing System

# Default recipe
default: build

# Build the dats binary
build:
    go build -o dats ./src/dats

# Run tests
test: build
    cd examples && bats *.gen.bats

# Generate example tests
examples: build
    ./dats examples/example.dats examples/ --runtime-dir=runtime

# Clean build artifacts (ignored files only)
clean:
    git clean -Xdf

# Install to /usr/local/bin
install: build
    cp dats /usr/local/bin/

# Format Go code
fmt:
    go fmt ./...

# Vet Go code
vet:
    go vet ./...

# Build VS Code extension
vscode-build:
    cd src/vscode-dats && npm run build

# Install VS Code extension dependencies
vscode-install:
    cd src/vscode-dats && npm install

# Update VS Code extension dependencies
vscode-update:
    cd src/vscode-dats && npx npm-check-updates -u && npm install
