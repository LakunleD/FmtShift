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

clean:
	rm -rf bin coverage.out
