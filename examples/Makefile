# Example DATS Makefile
DATS := ../dats
RUNTIME_DIR := ../runtime

DATS_FILES := $(wildcard *.dats)
BATS_FILES := $(DATS_FILES:.dats=.gen.bats)

.PHONY: all test clean

all: $(BATS_FILES)

%.gen.bats: %.dats $(DATS)
	$(DATS) $< . --runtime-dir=$(RUNTIME_DIR)

test: $(BATS_FILES)
	bats *.gen.bats

clean:
	rm -rf *.gen.bats *.gen.bats.d fixtures/

# Include generated dependency files
-include $(BATS_FILES:.bats=.bats.d)
