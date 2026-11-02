# Targets: build, cross, dist, install, test, test-golden, lint, clean.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# a tag is v1.2.3, and the archives and install.sh use 1.2.3
VER := $(patsubst v%,%,$(VERSION))
LDFLAGS := -s -w -X main.version=$(VER)
WTREE_BIN_DIR ?= $(HOME)/.local/bin
TARGETS := linux_amd64 linux_arm64 darwin_amd64 darwin_arm64
GO_BUILD = CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)'

.PHONY: build cross dist install test test-golden lint clean

build:
	cd tui && $(GO_BUILD) -o ../dist/wtree-tui ./cmd/wtree-tui

cross:
	@set -e; for target in $(TARGETS); do \
		echo "build $$target"; \
		(cd tui && GOOS=$${target%_*} GOARCH=$${target#*_} $(GO_BUILD) -o ../dist/$$target/wtree-tui ./cmd/wtree-tui); \
	done

dist: cross
	@set -e; cd dist; rm -f *.tar.gz checksums.txt; \
	for target in $(TARGETS); do \
		tar -czf wtree-tui_$(VER)_$$target.tar.gz -C $$target wtree-tui -C $(CURDIR) LICENSE; \
	done; \
	cp ../bin/wtree ../init.lua ../install.sh .; \
	if command -v sha256sum >/dev/null 2>&1; then sum='sha256sum'; else sum='shasum -a 256'; fi; \
	$$sum *.tar.gz wtree init.lua install.sh >checksums.txt; \
	ls -1 *.tar.gz checksums.txt wtree init.lua install.sh

# With WTREE_BIN_DIR set to the bin directory of this checkout, the script is already in place.
install: build
	mkdir -p '$(WTREE_BIN_DIR)'
	@if [ ! bin/wtree -ef '$(WTREE_BIN_DIR)/wtree' ]; then install -m 755 bin/wtree '$(WTREE_BIN_DIR)/wtree'; fi
	install -m 755 dist/wtree-tui '$(WTREE_BIN_DIR)/wtree-tui'
	@echo 'Installed $(WTREE_BIN_DIR)/wtree and wtree-tui'

test:
	test/run.sh
	tui/internal/core/testdata/guarded.sh test
	cd tui && go test ./...

test-golden:
	test/run.sh --update-golden
	cd tui && go test ./internal/treetable -update

lint:
	shellcheck bin/wtree install.sh test/run.sh test/lib/*.sh test/cases/*.sh test/lua/*.sh
	cd tui && go vet ./...

clean:
	rm -rf dist
