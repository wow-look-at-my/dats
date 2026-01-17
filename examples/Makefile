# Example DATS Makefile
# File-producing targets only. Use `just` for commands.

DATS := ../dats
RUNTIME_DIR := ../runtime

DATS_FILES := $(wildcard *.dats)
BATS_FILES := $(DATS_FILES:.dats=.gen.bats)

# Generate BATS from DATS
%.gen.bats: %.dats $(DATS)
	$(DATS) $< . --runtime-dir=$(RUNTIME_DIR)

# Include generated dependency files
-include $(BATS_FILES:.bats=.bats.d)
