# FmtShift Testing Strategy

## 1. Overview

FmtShift v0.1 must be tested on macOS and Linux. Windows joins the required
matrix in v0.2. Tests must verify conversion behavior, reliable filesystem
operations and compatibility with the codec implementation selected by M0.

v0.1 testing covers the conversion engine, CLI, configuration and batch
processing. Directory-monitoring tests become release requirements in v0.2.

## 2. Testing Tools

| Tool | Purpose |
|---|---|
| Go testing | Unit and integration testing |
| Go race detector | Concurrency testing |
| Go fuzzing | Input validation and robustness |
| Testify | Assertions where necessary |
| GitHub Actions | Cross-platform automated testing |

Prefer the Go standard testing library where possible.

## 3. Testing Levels

### 3.1 Unit Tests

Test individual components independently.
- Format detection.
- Conversion option validation.
- Output filename generation.
- Configuration parsing.
- Configuration precedence.
- File filtering.
- Error handling.
- Worker pool management.
- Filesystem event deduplication (v0.2).

### 3.2 Integration Tests

Test components using real files and native image codecs.
- Image decoding and encoding.
- Single-file conversion.
- Batch conversion.
- Directory monitoring (v0.2).
- Configuration persistence.
- Metadata handling.
- Destination file publication.

### 3.3 End-to-End Tests

Build and execute the FmtShift CLI as a standalone binary.
Validate command arguments, output files, console output and exit codes. JSON
response validation is added with `--json` in v0.2.

## 4. Test File Structure

```text
fmtshift/
├── internal/
│   ├── converter/
│   │   └── converter_test.go
│   ├── codecs/
│   ├── batch/
│   │   └── batch_test.go
│   ├── watcher/
│   │   └── watcher_test.go
│   ├── config/
│   │   └── config_test.go
│   └── cli/
│       └── cli_test.go
├── tests/
│   ├── integration/
│   └── e2e/
└── testdata/
    └── images/
        ├── heic/
        ├── jpeg/
        ├── png/
        ├── webp/
        └── invalid/
```

Test fixtures must be small, legally redistributable and free of private or personally identifiable metadata.

## 5. Conversion Test Matrix

| Source | JPEG | PNG | WebP |
|---|---|---|---|
| HEIC | Convert | Convert | Convert |
| JPEG | Skip | Convert | Convert |
| PNG | Convert | Skip | Convert |
| WebP | Convert | Convert | Skip |

Every successful conversion must verify:
- Output file exists.
- Output is a valid image.
- Output format matches the requested format.
- Image dimensions are preserved.
- Image orientation is correct.
- Original file remains unchanged.

Do not rely solely on file extensions to validate output formats.

## 6. Image Fixtures

Test images should include:
- Standard HEIC photographs.
- HEIC images with container rotation plus EXIF orientation, verifying that
  transforms are applied once and the output orientation is 1.
- Portrait and landscape images.
- Transparent PNG images.
- Small images.
- Large, high-resolution images.
- Images containing EXIF, XMP and ICC metadata.
- Display P3, 10-bit and HDR/gain-map iPhone images, with expected colour and
  tone-mapping behavior documented for each output format.
- Corrupted images.
- Empty files.
- Images with incorrect file extensions.
- HEIF primary images with thumbnails, depth maps and gain maps, which must
  convert the primary image without treating auxiliaries as extra photos.
- Unsupported animated inputs, true image sequences and HEIF files with
  multiple top-level user images.

## 7. Filesystem Tests

| Scenario | Expected Result |
|---|---|
| Source file does not exist | Return descriptive error |
| Destination already exists | Reject unless overwrite is enabled |
| Source equals destination | Reject conversion |
| Output directory is missing | Create directory when permitted |
| Output directory is read-only | Return permission error |
| Conversion fails | Remove temporary output |
| Filename contains spaces | Convert successfully |
| Filename contains Unicode | Convert successfully |
| Two jobs target the same output | Prevent unintended overwrite |

Use temporary directories for filesystem tests.

Tests must never modify files outside their isolated test directories.

## 8. Batch Processing Tests

Verify that batch conversion:
- Processes multiple supported images.
- Skips unsupported files during directory traversal.
- Continues when individual files fail.
- Respects recursive and non-recursive modes.
- Preserves relative directory structure.
- Applies input format filters.
- Enforces the configured worker limit.
- Handles cancellation gracefully.
- Reports accurate success, skip and failure counts.

## 9. Directory Monitoring Tests (v0.2)

| Scenario | Expected Result |
|---|---|
| New supported image | Convert once |
| Unsupported file | Ignore |
| Duplicate filesystem events | Convert once |
| Partially written file | Wait until stable |
| `.crdownload` or `.part` temporary file | Ignore until renamed to a supported final name |
| Temporary file atomically renamed into place | Convert the final file once |
| Generated output file | Do not reprocess |
| Existing file on startup | Ignore by default |
| Existing file with --existing | Convert |
| New subdirectory | Monitor in recursive mode |
| Destination collision | Apply collision policy |
| User interruption | Shut down gracefully |

Watcher tests should use synchronization and bounded timeouts instead of relying on arbitrary sleep durations.

## 10. Configuration Tests

Verify:
- Built-in defaults.
- Configuration file creation.
- Reading and updating values.
- Resetting configuration.
- Invalid configuration handling.
- Unsupported configuration keys.
- CLI flag precedence.
- Platform-specific configuration paths.
- Concurrent configuration writes.

Invalid configuration files must not be silently overwritten.

## 11. CLI Tests

Test all public commands:

```bash
fmtshift convert
fmtshift config
fmtshift formats
fmtshift version
fmtshift help
```

Add `fmtshift watch` to this command matrix in v0.2.

Validate:
- Required arguments.
- Optional flags.
- Invalid formats and quality values.
- Help messages.
- JSON output (v0.2).
- Standard output and standard error separation.
- Exit codes.

### Exit Code Matrix

| Code | Scenario |
|---|---|
| 0 | Successful operation |
| 1 | Conversion failure |
| 2 | Invalid command or arguments |
| 3 | Configuration error |
| 4 | No eligible input files |
| 130 | Interrupted operation |

## 12. Performance Tests

Benchmark:
- HEIC to JPEG.
- HEIC to PNG.
- HEIC to WebP.
- Batch conversion of 100 images.
- Batch conversion of 1,000 images.
- Directory monitoring under burst file arrivals.

Measure:
- Conversion duration.
- Images processed per second.
- Peak memory consumption.
- CPU utilization.
- Worker pool efficiency.

Performance targets should be established after obtaining baseline measurements on each supported platform.

## 13. Robustness Tests

- Malformed image files.
- Excessively large image dimensions.
- Unexpected metadata.
- Repeated conversion failures.
- Native codec resource cleanup.
- Invalid or inaccessible filesystem paths.
- Cancellation during batch processing.

Fuzz tests should cover format detection, configuration parsing and conversion option validation.

## 14. Cross-Platform Testing

| Platform | Architecture |
|---|---|
| macOS | ARM64 |
| macOS | AMD64 |
| Windows | AMD64 (v0.2) |
| Linux | AMD64 |
| Linux | ARM64 |

Each release target must pass integration tests on a compatible environment with its required native libraries.

Published release artifacts must also undergo installation and conversion smoke tests.

## 15. CI/CD Pipeline

Every pull request must run:
1. Go formatting checks.
2. Static analysis.
3. Unit tests.
4. Integration tests.
5. Race detection on supported targets.
6. Cross-platform build verification.

Release workflows must additionally validate packaged binaries and installation instructions.

## 16. Running Tests

### Unit Tests

```bash
go test ./internal/...
```

### All Tests

```bash
go test ./...
```

### Race Detection

```bash
go test -race ./...
```

### Coverage

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Benchmarks

```bash
make bench
```

`make bench` first generates synthetic 12-megapixel HEIC inputs in
`testdata/bench/` (git-ignored) with `scripts/gen-bench-images.sh`, then runs
the codec benchmarks. Benchmarks report MP/s; Go's `-benchmem` cannot see
libvips' C allocations, so peak memory is measured with `/usr/bin/time -l`
on the built binary. Codec benchmarks skip when the inputs are missing.

## 17. Coverage Requirements

Target at least 80% coverage for core Go application logic.

Prioritize conversion correctness, filesystem safety, configuration and concurrency over achieving coverage numbers alone.

Native image processing must be validated through integration tests.

## 18. Definition of Done

A feature is considered complete when:
- Its acceptance criteria are satisfied.
- Unit and integration tests pass.
- Failure scenarios are tested.
- Original files are protected.
- Relevant documentation is updated.
- CI checks pass.
- Cross-platform implications are addressed.
