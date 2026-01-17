# Example DATS Makefile
DATS := ../dats
RUNTIME_DIR := ../runtime

DATS_FILES := $(wildcard *.dats)
BATS_FILES := $(DATS_FILES:.dats=.gen.bats)

# Default target
all: $(BATS_FILES)

# Generate BATS from DATS
%.gen.bats: %.dats $(DATS)
	$(DATS) $< . --runtime-dir=$(RUNTIME_DIR)

# Run tests
test: $(BATS_FILES)
	bats *.gen.bats

# Include generated dependency files
-include $(BATS_FILES:.bats=.bats.d)
