# FmtShift CLI Specification

## 1. Overview

The FmtShift CLI provides commands for converting files, processing directories, monitoring file changes and managing user preferences.

The executable name is `fmtshift`.

## 2. Command Structure

```bash
fmtshift <command> [arguments] [flags]
```

| Command | Description |
|---|---|
| convert | Convert files or directories |
| watch | Monitor directories and automatically convert files (v0.2) |
| config | Manage user configuration |
| formats | List supported formats |
| version | Display the installed version |
| help | Display command documentation |

## 3. Convert Command

### Syntax

```bash
fmtshift convert <input...> [flags]
```

### Options

| Flag | Description | Default |
|---|---|---|
| --to, -t | Output format | jpeg |
| --from | Filter input formats | All supported |
| --output, -o | Output directory | Source directory |
| --quality, -q | Compression quality (1–100) | 85 |
| --recursive, -r | Include subdirectories | false |
| --overwrite | Overwrite existing output files | false |
| --strip-metadata | Remove image metadata | false |
| --workers, -w | Maximum concurrent conversions | 2 |

### Single File Conversion

```bash
fmtshift convert photo.heic
```

Output:

```text
Converted photo.heic -> photo.jpg
```

### Multiple Files

```bash
fmtshift convert photo1.heic photo2.heic --to png
```

### Directory Conversion

```bash
fmtshift convert ./photos --to jpeg
```

### Recursive Conversion

```bash
fmtshift convert ./photos --to webp --recursive
```

Relative directory structure should be preserved when recursively converting files into a separate output directory.

### Format Filtering

```bash
fmtshift convert ./photos --from heic --to jpeg
```

`--from` is a filter for both explicit files and directory discovery. An
explicit file that does not match is reported as skipped; a non-matching file
found during directory traversal is filtered out and included in the final
skipped count. A command for which no input is eligible exits with code 4.

### Custom Output Directory

```bash
fmtshift convert photo.heic --output ./converted
```

### Compression Quality

```bash
fmtshift convert photo.heic --to jpeg --quality 90
```

### Batch Output

```text
Conversion complete

Converted: 24
Skipped:    3
Failed:     1
Duration:  4.2s
```

## 4. Watch Command (v0.2)

### Syntax

```bash
fmtshift watch <directory...> [flags]
```

### Options

| Flag | Description | Default |
|---|---|---|
| --to, -t | Output format | jpeg |
| --from | Filter input formats | All supported |
| --output, -o | Output directory | Source directory |
| --quality, -q | Compression quality | 85 |
| --recursive, -r | Include subdirectories | false |
| --existing | Process existing files on startup | false |
| --overwrite | Overwrite existing output files | false |
| --strip-metadata | Remove image metadata | false |
| --workers, -w | Maximum concurrent conversions | 2 |

### Watch a Directory

```bash
fmtshift watch ~/Downloads --from heic --to jpeg
```

### Watch Multiple Directories

```bash
fmtshift watch ~/Downloads ~/Desktop --to jpeg
```

### Example Output

```text
Watching /Users/ola/Downloads
Source format: HEIC
Target format: JPEG

[14:32:01] Detected photo.heic
[14:32:02] Converted photo.heic -> photo.jpg
```

The watch command runs until stopped by the user. Ctrl+C initiates a graceful shutdown.

Generated output files must not trigger additional conversions. Files must be fully written before conversion begins.

## 5. Configuration Command

### Syntax

```bash
fmtshift config <subcommand>
```

### Subcommands

| Subcommand | Description |
|---|---|
| set | Set a configuration value |
| get | Retrieve a configuration value |
| show | Display current configuration |
| unset | Remove a configuration value |
| reset | Restore default configuration |
| path | Display configuration file location |

v0.1 includes `set`, `get`, `show` and `path`. `unset` and `reset` are v0.2
commands.

### Set Configuration

```bash
fmtshift config set format jpeg
fmtshift config set quality 90
fmtshift config set workers 4
```

### Retrieve Configuration

```bash
fmtshift config get format
```

### Display Configuration

```bash
fmtshift config show
```

Example output:

```json
{
  "version": 1,
  "format": "jpeg",
  "quality": 90,
  "workers": 4,
  "preserveMetadata": true,
  "overwrite": false
}
```

### Reset Configuration (v0.2)

```bash
fmtshift config reset
```

### Configuration Precedence

1. Explicit command-line flags.
2. Saved user configuration.
3. Built-in defaults.

## 6. Formats Command

Lists supported input and output formats.

```bash
fmtshift formats
```

Example output:

```text
FORMAT    READ    WRITE
HEIC      Yes     No
JPEG      Yes     Yes
PNG       Yes     Yes
WEBP      Yes     Yes
```

The output must reflect the codecs available in the installed build.

## 7. Version Command

```bash
fmtshift version
```

Example output:

```text
FmtShift v0.1.0
Platform: darwin/arm64
```

## 8. Global Flags

| Flag | Description |
|---|---|
| --help, -h | Display help |
| --verbose, -v | Enable verbose logging |
| --quiet | Suppress informational output |
| --json | Return machine-readable command results (v0.2) |

The JSON flag is intended for scripting and automation.

Example:

```bash
fmtshift convert photo.heic --to png --json
```

```json
{
  "status": "success",
  "source": "photo.heic",
  "output": "photo.png",
  "duration_ms": 320
}
```

Errors and diagnostic logs must be written to stderr.

## 9. Exit Codes

| Code | Description |
|---|---|
| 0 | Command completed successfully |
| 1 | One or more conversions failed |
| 2 | Invalid command or arguments |
| 3 | Configuration error |
| 4 | No eligible input files |
| 130 | Operation interrupted by the user |

## 10. File Handling Rules

- Preserve original files.
- Never silently overwrite existing output files.
- Treat JPEG and JPG as equivalent formats.
- Skip same-format conversions by default.
- Use the original filename with the new extension.
- Publish output files only after successful conversion.
- Remove incomplete temporary files when conversion fails.
- Report unsupported or corrupt images clearly.
- Never allow an output path to overwrite its source file.

## 11. Installation

v0.1 is distributed as standalone macOS and Linux artifacts through GitHub
Releases. The following package-manager commands are later distribution goals,
not v0.1 acceptance requirements:

### macOS

```bash
brew install fmtshift
```

### Windows

```powershell
winget install FmtShift.FmtShift
```

### Linux

```bash
sudo apt install fmtshift
```

Package manager commands become available after the corresponding packages have been published.

Standalone release binaries will also be available through GitHub Releases.

## 12. Future Commands

The following commands are outside the initial MVP:

```bash
fmtshift service start
fmtshift service stop
fmtshift service status

fmtshift compress image.jpg
fmtshift resize image.jpg --width 1080
fmtshift inspect image.heic
```

These commands may be introduced as FmtShift expands beyond basic image conversion.
