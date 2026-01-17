# DATS - Declarative Automated Testing System

.PHONY: all build test clean install examples

all: build

build:
	go build -o dats ./src/dats

test: build
	cd examples && $(MAKE) test

clean:
	rm -f dats
	cd examples && $(MAKE) clean

install: build
	cp dats /usr/local/bin/

examples: build
	cd examples && $(MAKE)

# Development
.PHONY: fmt vet

fmt:
	go fmt ./...

vet:
	go vet ./...
