# FmtShift: Product Requirements Document

**Version:** 0.1 draft  
**License:** MIT  
**v0.1 platforms:** macOS and Linux  
**Planned v0.2 platform:** Windows

## 1. Overview

FmtShift is an open-source, local-first file conversion utility. Its initial focus is converting images between common formats without requiring an internet connection or uploading files to an external service.

The product will launch as a Go CLI, followed by a cross-platform desktop application built with Wails. Both clients will use the same conversion engine.

## 2. Problem

Images received from Apple devices and other applications may use formats such as HEIC that are not universally supported by recipients or applications. Users often have to manually convert each file, rely on online converters, or install complex image processing tools.

FmtShift aims to make conversion simple, repeatable and automatic, including for users who regularly receive or create images in a monitored directory.

## 3. Target users

- Everyday users sharing photos across devices and applications.
- Developers who need image conversion in scripts and workflows.
- Photographers and content creators processing collections of images.
- Teams that need to standardize image formats while keeping files private.

## 4. Product goals

- Convert supported images with a single command.
- Support batch processing and automatic directory monitoring.
- Keep all image processing local.
- Preserve original files unless the user explicitly requests replacement.
- Provide consistent behavior across supported operating systems.
- Make the conversion engine reusable by the CLI and desktop application.

## 5. MVP scope

### 5.1 Supported formats

| Format | Read | Write |
|---|---|---|
| HEIC / HEIF (still images) | Yes | Not required |
| JPEG / JPG | Yes | Yes |
| PNG | Yes | Yes |
| WebP (still images) | Yes | Yes |

Animated images, true image sequences, videos and documents are outside the
initial scope. For HEIF containers, FmtShift converts the primary image and
ignores thumbnails, depth maps, gain maps and other auxiliary images. A file
with multiple top-level user images or a timed image sequence must produce a
clear error rather than silently discarding user-visible frames.

### 5.2 Single-file conversion (P0)

Users can convert one supported image to an explicitly selected or configured default format.

```sh
fmtshift convert photo.heic --to jpeg
fmtshift convert photo.heic --to png --output ./converted
```

Requirements:

- Default output format: JPEG.
- Preserve the source and image dimensions by default.
- Apply HEIF container transforms exactly once and produce physically upright
  output pixels. If EXIF is preserved, normalize its orientation to 1 so a
  viewer cannot rotate the output a second time.
- Preserve an embedded ICC profile when the output format supports it, or use
  colour-managed conversion to sRGB. Document and test the policy for Display
  P3, 10-bit and HDR/gain-map inputs, including any tone mapping or precision
  loss.
- Never silently overwrite an existing output.
- Treat JPG and JPEG as equivalent target format names.
- Reject unsupported formats, unreadable images and invalid arguments clearly.

### 5.3 Batch conversion (P0)

Users can supply multiple files or a directory.

```sh
fmtshift convert photo1.heic photo2.heic --to jpeg
fmtshift convert ./photos --to webp --output ./converted
```

- Directory conversion is non-recursive by default; recursion is opt-in.
- Skip unsupported files in directory mode and report them.
- Continue processing other files when one conversion fails.
- Report successful, skipped and failed conversions.
- Use bounded concurrency to avoid excessive memory and CPU usage.

### 5.4 Directory monitoring (deferred to v0.2)

Users can watch a directory and automatically convert new supported images.

```sh
fmtshift watch ~/Downloads --from heic --to jpeg
```

- Support an optional source format filter and output directory.
- Process new files after they are fully written.
- Do not reprocess FmtShift-generated outputs or create conversion loops.
- Deduplicate filesystem events and handle filename collisions safely.
- By default, process newly created files only; existing files are opt-in.
- Remain active until stopped by the user and report conversion failures.
- Persistent background services and login startup are deferred.

### 5.5 Conversion options (P0)

- Destination format and output directory.
- JPEG/WebP lossy quality between 1 and 100; default 85.
- Preserve originals by default.
- Explicit overwrite of existing destination files only when requested.
- When converting transparency to JPEG, composite against a white background by default and disclose the loss of transparency.
- Produce predictable output names based on the original filename.
- Skip same-format conversions by default.

Metadata should be preserved where supported, with limitations documented. Users should be able to opt out of metadata preservation for privacy. Orientation must still be handled correctly when metadata is removed.

### 5.6 Configuration (P0)

Users can save defaults for output format, quality and output location.

```sh
fmtshift config set format jpeg
fmtshift config set quality 90
fmtshift config show
```

v0.1 provides `set`, `get`, `show` and `path`. The `unset` and `reset`
convenience commands are deferred to v0.2.

Explicit CLI flags take precedence over configuration, which takes precedence over built-in defaults. Configurations are scoped to the current user.

### 5.7 CLI experience (P0)

- Help and version commands.
- Human-readable results and actionable errors.
- Stable exit codes for success, invalid usage and conversion failures.
- No interactive prompts required for normal scripted conversions.
- Installation instructions and release artifacts for macOS and Linux.
- Machine-readable `--json` output is deferred to v0.2.

The final command grammar, flags and exit codes are defined in `CLI.md`.

## 6. Desktop application (Phase 2)

Build a Wails application using the same Go conversion engine.

- File and folder selection, including drag and drop.
- Format, quality and destination controls.
- Batch progress and individual conversion results.
- Watch directory management and background conversion.
- Conversion history stored locally.
- Optional startup at login and tray/menu bar access, subject to platform support.
- Native packaging and installation for macOS, Windows and Linux.

The desktop application should not require the CLI to be installed separately.

## 7. Non-functional requirements

| Area | Requirement |
|---|---|
| Privacy | No file uploads, external conversion API or mandatory telemetry. |
| Reliability | Write outputs to temporary files and publish only completed conversions. Clean up incomplete outputs. |
| Safety | Do not modify originals by default. Validate input size and dimensions and guard against excessive resource consumption. |
| Performance | Bound parallel jobs and memory consumption; benchmark representative image sizes on each target platform. |
| Compatibility | Support documented macOS and Linux v0.1 release targets. Add Windows only after its packaged artifact passes clean-machine tests. |
| Accessibility | Provide clear CLI messages and accessible desktop controls. |
| Portability | Handle Unicode filenames, spaces and platform-specific paths. |
| Maintainability | Keep user interfaces separate from format codecs and conversion logic. |

## 8. Technology direction

- Go for the reusable core and CLI.
- Cobra for CLI commands.
- A codec abstraction with two M0 candidates: libvips/libheif through cgo, and
  a CGo-free build using Go's JPEG/PNG support plus embedded HEIC/WebP codecs.
- fsnotify for directory monitoring.
- Wails for the future desktop application.

M0 must compare correctness, performance, memory use, metadata, colour
management, binary size, clean-machine installation and licensing before an
image-processing stack is selected. Any libheif build used for decoding must
exclude GPL encoders such as x265. FmtShift source will use the MIT license;
third-party components retain their own licenses and require notices and any
other applicable distribution materials.

## 9. Out of scope for v0.1

- Cloud storage, user accounts or online conversion.
- Video, audio and document conversion.
- Image editing, cropping and resizing.
- Paid plans or billing.
- Automatic replacement of source files.
- Desktop GUI and persistent background services.
- Directory monitoring and the `watch` command.
- Windows release artifacts.
- Machine-readable JSON output and the `config unset`/`config reset` commands.

## 10. Release acceptance criteria

- A fresh installation can convert a HEIC image into JPEG, PNG and WebP.
- Batch conversion handles a mixture of valid, unsupported and corrupt files without losing original files.
- Output collisions do not cause unintended overwrites.
- Images with orientation metadata display correctly after conversion.
- A failed conversion leaves no partial destination image.
- The CLI runs offline on documented macOS and Linux targets.
- Automated tests cover conversion, configuration and safe output handling.
- Installation, supported formats and known limitations are documented.

## 11. Future roadmap

- v0.1: Core image conversion and batch CLI for macOS and Linux.
- v0.2: Directory monitoring, Windows, JSON output, complete configuration
  commands and packaging improvements.
- v1.0: Cross-platform Wails desktop application.
- Later: Image compression, resizing and additional file categories based on user feedback.

## 12. Related documentation

- `ARCHITECTURE.md`: core engine, dependencies, platform support and package structure.
- `CLI.md`: command syntax, flags, configuration and error handling.
- `TESTING.md`: test strategy, fixtures and cross-platform verification.
- `IMPLEMENTATION_PLAN.md`: milestones, dependencies and engineering tasks.
