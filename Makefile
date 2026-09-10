.PHONY: build install uninstall test vet fmt clean

BIN := fibery
PREFIX ?= $(HOME)/.local
PKG := github.com/iamnikolie/fibery-cli/cmd

# Version metadata stamped into the binary. VERSION falls back to the short
# commit when the tree has no tag yet, so a local build is still identifiable.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(PKG).version=$(VERSION) \
	-X $(PKG).commit=$(COMMIT) \
	-X $(PKG).date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

install: build
	mkdir -p $(PREFIX)/bin
	ln -sf $(CURDIR)/$(BIN) $(PREFIX)/bin/$(BIN)

uninstall:
	rm -f $(PREFIX)/bin/$(BIN)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

clean:
	rm -f $(BIN)
	rm -rf dist
