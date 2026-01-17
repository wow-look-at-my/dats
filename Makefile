# DATS - Declarative Automated Testing System
# File-producing targets only. Use `just` for commands.

# Go source files
GO_SRC := $(shell find src/dats -name '*.go')

# DATS source files
DATS_FILES := $(wildcard examples/*.dats)
BATS_FILES := $(DATS_FILES:.dats=.gen.bats)

# Build the dats binary
dats: $(GO_SRC) go.mod go.sum
	go build -o $@ ./src/dats

# Generate BATS files from DATS
examples/%.gen.bats: examples/%.dats dats
	./dats $< examples/ --runtime-dir=runtime

# Include generated dependency files
-include $(BATS_FILES:.bats=.bats.d)
