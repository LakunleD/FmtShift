# libvips is found through pkg-config. Homebrew installs some of its
# dependencies (libarchive) keg-only, so add their pkg-config directories.
ifeq ($(shell uname -s),Darwin)
  ifneq ($(shell command -v brew 2>/dev/null),)
    export PKG_CONFIG_PATH := $(shell brew --prefix libarchive)/lib/pkgconfig:$(PKG_CONFIG_PATH)
  endif
endif

.PHONY: all build test race fuzz bench vet fmt check fixtures dist clean

all: check build

build:
	go build -o bin/ ./cmd/...

test:
	go test ./...

race:
	go test -race ./...

fuzz:
	go test -run='^$$' -fuzz=FuzzDetect -fuzztime=30s ./internal/format

# Large benchmark inputs are git-ignored and regenerated whenever the
# generator script changes. The stamp is written only after the script
# succeeds, so a partial or outdated set of inputs is never reused.
BENCH_STAMP := testdata/bench/.stamp

$(BENCH_STAMP): scripts/gen-bench-images.sh
	rm -rf testdata/bench
	./scripts/gen-bench-images.sh
	touch $@

# FMTSHIFT_REQUIRE_BENCH_INPUTS turns a missing input into a failure
# instead of a skip.
bench: $(BENCH_STAMP)
	FMTSHIFT_REQUIRE_BENCH_INPUTS=1 go test -run='^$$' -bench=. -benchtime=5x ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# Fails if any file needs formatting.
check: vet
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "run 'make fmt'" && exit 1)
	go test ./...

fixtures:
	./scripts/gen-fixtures.sh

# Release archive for the current platform: a stripped binary with the
# version embedded, LICENSE, README and generated third-party notices, in
# dist/fmtshift_<version>_<os>_<arch>.tar.gz with a .sha256 file. libvips is
# not bundled: users install it with their package manager (see README).
VERSION ?= $(shell git describe --tags --always --dirty)
DIST_NAME := fmtshift_$(VERSION)_$(shell go env GOOS)_$(shell go env GOARCH)

dist:
	rm -rf dist/$(DIST_NAME) dist/$(DIST_NAME).tar.gz dist/$(DIST_NAME).tar.gz.sha256
	mkdir -p dist/$(DIST_NAME)
	CGO_ENABLED=1 go build -trimpath \
		-ldflags "-s -w -X github.com/lakunled/fmtshift/internal/version.Version=$(VERSION)" \
		-o dist/$(DIST_NAME)/fmtshift ./cmd/fmtshift
	cp LICENSE README.md dist/$(DIST_NAME)/
	./scripts/third-party-licenses.sh > dist/$(DIST_NAME)/THIRD_PARTY_LICENSES
	tar -C dist -czf dist/$(DIST_NAME).tar.gz $(DIST_NAME)
	cd dist && shasum -a 256 $(DIST_NAME).tar.gz > $(DIST_NAME).tar.gz.sha256
	@echo "built dist/$(DIST_NAME).tar.gz"

clean:
	rm -rf bin dist coverage.out
