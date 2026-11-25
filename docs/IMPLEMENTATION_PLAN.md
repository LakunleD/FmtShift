# FmtShift Implementation Plan

## 1. Objective

Deliver FmtShift v0.1 as an installable, open-source Go CLI for macOS and
Linux. The first release converts images locally and supports batch
processing. Directory monitoring and Windows packaging follow in v0.2.

The implementation must follow PRD.md, ARCHITECTURE.md, CLI.md and
TESTING.md. The Wails desktop application is a subsequent release.

## 2. Milestones

| Milestone | Deliverable | Depends on |
|---|---|---|
| M0 | Repository and codec proof of concept | None |
| M1 | Shared conversion engine | M0 |
| M2 | Installable single-file CLI | M1 |
| M3 | Batch conversion and configuration | M2 |
| M4 | v0.1 packaging and validation | M3 |
| M5 | v0.2 monitoring and platform expansion | M4 |
| M6 | Wails desktop application | M5 |

## 3. M0: Repository and codec feasibility

- [ ] Initialize the Go module using the actual GitHub repository path.
- [ ] Add AGENTS.md, README.md, MIT LICENSE and project documentation.
- [ ] Add Makefile, .gitignore and initial GitHub Actions workflow.
- [ ] Build the same minimal converter behind a codec interface using two
  approaches:
  - libvips/libheif through cgo;
  - a CGo-free build using Go JPEG/PNG support and embedded HEIC/WebP codecs,
    evaluating packages such as `gen2brain/heic` and `gen2brain/webp` without
    assuming they preserve metadata or colour information.
- [ ] Validate HEIC decoding and JPEG, PNG and WebP encoding for both paths.
- [ ] Benchmark representative single and batch conversions, peak memory and
  release artifact size.
- [ ] Test HEIF container orientation, EXIF orientation normalization, alpha,
  EXIF/XMP, ICC/NCLX profiles, Display P3, 10-bit and HDR/gain-map behavior.
- [ ] Verify that the primary image is converted while auxiliary thumbnails,
  depth maps and gain maps do not cause a normal photo to be rejected.
- [ ] Build and run minimal artifacts on macOS and Linux, and document Windows
  feasibility for v0.2.
- [ ] For the native path, document target C toolchains, bundled or external
  libraries, clean-machine installation and redistribution licenses.
- [ ] Ensure any evaluated libheif distribution excludes GPL encoders such as
  x265.
- [ ] For the CGo-free path, force and verify the embedded fallback rather than
  accidentally loading an installed shared library.
- [ ] Audit the source and transitive licenses of embedded Wasm or transpiled
  codecs; a single binary does not remove redistribution obligations.
- [ ] Prove that each candidate artifact can be installed and run on clean
  macOS and Linux environments.
- [ ] Record the selected codec approach and fallback options in ARCHITECTURE.md.

Acceptance criteria: Both candidates convert the agreed fixture matrix on
macOS and Linux. Measurements and known metadata/colour limitations are
recorded. Packaging, security maintenance and license obligations are
understood, and one approach is selected before the engine is implemented.

## 4. M1: Core conversion engine

- [ ] Define Format, ConversionRequest and ConversionResult models.
- [ ] Implement format detection and supported-format registry.
- [ ] Implement codec abstraction and the adapter selected by M0.
- [ ] Validate quality, image dimensions and supported still-image inputs.
- [ ] Apply HEIF transforms exactly once and normalize preserved EXIF
  orientation to 1.
- [ ] Implement the selected ICC/sRGB and HDR-to-SDR policy.
- [ ] Implement alpha compositing for JPEG outputs.
- [ ] Implement deterministic output names and collision handling.
- [ ] Reject source/destination aliases, including symlinks and hard links.
- [ ] Write to temporary files and publish completed outputs safely.
- [ ] Clean up failed or cancelled operations.
- [ ] Add typed errors, unit tests and real-codec integration tests.

Acceptance criteria: The engine converts every supported input to every
supported output without altering the source. Failure and collision tests
pass. The engine has no dependency on Cobra or Wails.

## 5. M2: CLI and first development release

- [ ] Create cmd/fmtshift/main.go and the Cobra root command.
- [ ] Implement convert, formats, version and help.
- [ ] Add --to, --output, --quality, --overwrite and --strip-metadata.
- [ ] Add human-readable output, logging and exit code mapping.
- [ ] Add cancellation through context and operating-system signals.
- [ ] Document local installation and common usage.
- [ ] Build the initial executable and run end-to-end tests.

Acceptance criteria:

    fmtshift convert photo.heic --to jpeg
    fmtshift convert photo.heic --to png --output ./converted
    fmtshift formats
    fmtshift version

All commands work on development environments. A valid conversion produces
a readable output image without modifying the source.

## 6. M3: Batch processing and configuration

- [ ] Discover eligible files from multiple paths and directories.
- [ ] Support recursive traversal and input-format filters.
- [ ] Preserve relative directory structure for recursive output.
- [ ] Implement bounded worker pool and job cancellation.
- [ ] Prevent concurrent jobs from targeting the same output.
- [ ] Report converted, skipped and failed files.
- [ ] Implement platform-appropriate JSON configuration storage.
- [ ] Add config set, get, show and path.
- [ ] Implement flag > configuration > default precedence.
- [ ] Validate and safely persist configuration changes.
- [ ] Add integration tests and representative batch benchmarks.

Acceptance criteria: A batch of mixed supported and unsupported files is
processed correctly. Individual failures do not stop unrelated jobs.
Global settings and explicit flags behave predictably.

## 7. M4: v0.1 packaging and validation

- [ ] Configure CI builds and dependency installation for the selected codec.
- [ ] Run unit, integration, race and end-to-end tests as supported.
- [ ] Build macOS ARM64 and AMD64 artifacts.
- [ ] Build Linux AMD64 and ARM64 artifacts.
- [ ] Produce checksums and third-party license notices.
- [ ] Test artifact installation and conversion on clean target environments.
- [ ] Publish installation instructions and supported platforms.
- [ ] Prepare GitHub Releases with versioned binaries and any required
  dependencies.
- [ ] Document upgrade and uninstall procedures.
- [ ] Tag and publish v0.1.0 after review.

Acceptance criteria:

    fmtshift convert photo.heic --to jpeg

A user can install a published artifact on clean macOS and Linux systems and
convert the fixture set without installing Go. Originals are preserved and
all required third-party notices and runtime dependencies are present.

Initial distribution is through GitHub Releases. Homebrew publication is
deferred to v0.2 so it does not block the first validated artifacts.

## 8. M5: v0.2 monitoring and platform expansion

- [ ] Implement the fsnotify adapter and directory monitoring specified in
  CLI.md.
- [ ] Support multiple directories, recursive monitoring, stability checks,
  event deduplication, queue limits and graceful shutdown.
- [ ] Prevent generated outputs from causing conversion loops.
- [ ] Test partial writes, `.crdownload`/`.part` files, atomic renames, burst
  arrivals and conversion failures.
- [ ] Build Windows AMD64 artifacts.
- [ ] Add config unset and reset.
- [ ] Add machine-readable `--json` output.
- [ ] Publish a Homebrew tap and verify installation from it.
- [ ] Repeat clean-machine installation and conversion tests for all v0.2
  targets.

Acceptance criteria: Watch mode converts a final, stable input exactly once;
Windows users can install and run the CLI without Go; and the deferred CLI
features behave consistently across documented targets. Persistent services
and startup at login remain out of scope.

## 9. M6: Cross-platform desktop application

This milestone begins after the CLI release.

- [ ] Initialize the Wails desktop project.
- [ ] Connect shared Go services through desktop bindings.
- [ ] Implement drag-and-drop and file/folder selection.
- [ ] Implement conversion settings and batch progress.
- [ ] Implement watch-directory management.
- [ ] Add local conversion history.
- [ ] Evaluate tray and startup-at-login support for each platform.
- [ ] Build platform-specific installers and validate native dependencies.
- [ ] Add accessibility and desktop interaction tests.

Acceptance criteria: Users can install and use the desktop application
without installing the CLI or Go separately.

## 10. Development workflow

1. Select one milestone task and review its acceptance criteria.
2. Implement the smallest functional change.
3. Add or update tests alongside implementation.
4. Run formatting, static analysis and relevant tests.
5. Update documentation when behavior or architecture changes.
6. Commit the change and mark the task complete only after verification.

Recommended local checks:

    go fmt ./...
    go vet ./...
    go test ./...
    go test -race ./...
    go build ./cmd/fmtshift

Native codec integration tests require the documented dependencies.

## 11. Estimated delivery

| Phase | Estimated engineering time |
|---|---|
| M0 | 3–5 days |
| M1 | 3–5 days |
| M2 | 2–3 days |
| M3 | 3–5 days |
| M4 | 3–6 days |
| M5 | 5–9 days |

Estimated v0.1 CLI: 14–24 focused engineering days, subject to M0 findings.
The estimate must be revised after the codec decision rather than treating
native packaging uncertainty as ordinary implementation variance. A macOS
development build should be available earlier, after M2. v0.2 and desktop
work are estimated separately.

## 12. Definition of done

- Acceptance criteria in the PRD and relevant milestone are met.
- Unit, integration and release smoke tests pass.
- Original files are protected and partial outputs are cleaned up.
- Supported release binaries run on their documented platforms.
- Installation and usage documentation is accurate.
- External dependencies and their licensing are documented.
- No cloud conversion or mandatory network access is introduced.

## 13. Immediate next task

Start M0: initialize the repository and build matching HEIC-to-JPEG proof-of-
concepts for the cgo/native and CGo-free approaches. Compare their fixture
matrix, measurements, packaging and licenses before implementing the complete
conversion engine.
