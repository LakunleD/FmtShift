# FmtShift

## Project Overview

FmtShift is an open-source, local-first file conversion tool that allows users to convert files between different formats.

The initial focus is image conversion, particularly HEIC to JPEG, PNG and other widely supported image formats.

FmtShift will provide a CLI and, eventually, a desktop application. All file conversions must happen locally on the user's device.

## Technology Stack

- Language: Go
- CLI framework: Cobra
- Image processing: selected by the M0 codec evaluation; candidates include
  libvips/libheif and CGo-free Go/Wasm codecs
- File monitoring: fsnotify
- Desktop application: Wails (future)

## Initial Scope

- Single file conversion
- Batch file conversion
- Automatic directory monitoring and conversion
- Configurable output format and quality
- Preservation of original files by default

## Engineering Guidelines

- Keep the conversion engine independent of the CLI.
- Design for cross-platform compatibility.
- Prioritize simplicity, performance and privacy.
- Avoid unnecessary dependencies.
- Never overwrite or delete original files without explicit permission.
- Write tests for all core functionality.
- Use idiomatic Go and standard Go project conventions.

## Documentation

Detailed product requirements, architecture, CLI specifications and implementation plans will be maintained in the `docs/` directory.

Consult the relevant documentation before implementing features.

## License

MIT
