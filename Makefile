# libvips is found through pkg-config. Homebrew installs some of its
# dependencies (libarchive) keg-only, so add their pkg-config directories.
ifeq ($(shell uname -s),Darwin)
  ifneq ($(shell command -v brew 2>/dev/null),)
    export PKG_CONFIG_PATH := $(shell brew --prefix libarchive)/lib/pkgconfig:$(PKG_CONFIG_PATH)
  endif
endif

.PHONY: all build test race fuzz bench vet fmt check fixtures clean

all: check build

build:
	go build -o bin/ ./cmd/...

test:
	go test ./...

race:
	go test -race ./...

fuzz:
	go test -run='^$$' -fuzz=FuzzDetect -fuzztime=30s ./internal/format

bench:
	go test -run='^$$' -bench=. -benchmem ./...

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

clean:
	rm -rf bin coverage.out
