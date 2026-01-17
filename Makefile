# DATS - Declarative Automated Testing System

# Go source files
GO_SRC := $(shell find src/dats -name '*.go')

# DATS source files
DATS_FILES := $(wildcard examples/*.dats)
BATS_FILES := $(DATS_FILES:.dats=.gen.bats)

# Default target
all: dats

# Build the dats binary
dats: $(GO_SRC) go.mod go.sum
	go build -o $@ ./src/dats

# Generate BATS files from DATS
examples/%.gen.bats: examples/%.dats dats
	./dats $< examples/ --runtime-dir=runtime

# Generate all examples
examples: $(BATS_FILES)

# Run tests
test: $(BATS_FILES)
	bats examples/*.gen.bats

# Clean build artifacts
clean:
	rm -f dats
	rm -f examples/*.gen.bats examples/*.gen.bats.d
	rm -rf examples/fixtures/

# Install to /usr/local/bin
install: dats
	cp dats /usr/local/bin/

# Format Go code
fmt: $(GO_SRC)
	go fmt ./...

# Vet Go code
vet: $(GO_SRC)
	go vet ./...

# Include generated dependency files
-include $(BATS_FILES:.bats=.bats.d)
