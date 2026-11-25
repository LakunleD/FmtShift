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
| --from | Only convert these input formats, by content (`heic`, `heic,png`) | All supported |
| --output, -o | Output directory, created if missing | Source directory |
| --quality, -q | Compression quality (1–100) | 85 |
| --recursive, -r | Include subfolders; with `--output`, recreate them under it | false |
| --overwrite | Overwrite existing output files | false |
| --strip-metadata | Remove image metadata | false |
| --workers, -w | Concurrent conversions (1–64) | 2 |

Each worker can use roughly 250 MB for a 12-megapixel photo, so peak memory
grows with `--workers` (about 920 MB at 4 on the reference machine, for 4×
the throughput of 1).

### Folder inputs

A folder argument converts the supported images directly inside it, or in
all its subfolders with `--recursive`. While scanning folders:

- Hidden files and folders (names starting with `.`) are skipped, including
  FmtShift's own temporary files.
- Symlinked files are followed; symlinked folders are not, so a link cannot
  cause a loop or escape the tree.
- If `--output` is inside a scanned folder, it is not scanned, so re-runs do
  not reconvert earlier outputs.
- Files that are not supported images, files already in the target format and
  files excluded by `--from` are counted as skipped but only listed with
  `--verbose`. The same situations for a file named explicitly are always
  reported.

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
Skipped:   3
Failed:    1
Duration:  4.2s
```

The summary is printed when there is more than one input or any folder
input. Results are printed as conversions finish, so with several workers
their order can vary. Per-file
`Converted` and `Skipped` lines go to stdout (suppressed by `--quiet`);
`Error:` lines go to stderr.

### Outcomes

Each input ends in exactly one outcome:

| Outcome | When | Counted as | Affects exit code |
|---|---|---|---|
| Converted | Output written | Converted | — |
| Skipped (exists) | Output exists and `--overwrite` not given; prints a hint | Skipped | No: re-running a command is safe and exits 0 |
| Skipped (same format) | Input already in the target format | Skipped | Not eligible: if no input is eligible, exit 4 |
| Skipped (filtered) | Excluded by `--from` | Skipped | Not eligible |
| Skipped (unsupported) | Found in a folder but not a supported image | Skipped | Not eligible |
| Failed | Missing, unreadable, unsupported (HDR, multi-image, or a named file that is not an image), corrupt, or the output would replace the source | Failed | Exit 1 |
| Failed (duplicate) | Would write the same output as an earlier input in the same command, e.g. `IMG_1.HEIC` and `IMG_1.PNG` both becoming `IMG_1.jpg`; detected before anything is converted; the first input still converts | Failed | Exit 1, with or without `--overwrite` |
| Failed (replaces an input) | With `--overwrite`, the output would replace another input of the same command, even one that is skipped or filtered, e.g. `photo.png` → `photo.jpg` next to an original `photo.jpg`. FmtShift cannot tell an original from an earlier output, so it never replaces either; use `--output` | Failed | Exit 1. Without `--overwrite` the same case is "Skipped (exists)" |
| Failed (creates an input) | The output would be written at the path of another input that does not exist, e.g. `photo.png photo.jpg` with `photo.jpg` missing. The missing input is reported as not found, never created and then "found" | Failed | Exit 1, with or without `--overwrite`; decided before converting, so independent of `--workers` |
| Cancelled | Ctrl+C before the input's output was published | Cancelled | Exit 130 |

Exit code precedence: 130 if interrupted, else 1 if any input failed, else 4
if nothing was converted or skipped-because-exists, else 0.

### Interrupting

The first Ctrl+C (or SIGTERM) starts nothing further, lets the conversions
already in progress finish decoding, discards their outputs and exits 130.
Up to `--workers` files (2 by default) can be in progress, and libvips cannot
abort an image mid-way, so stopping can take as long as the slowest of them,
using their memory until then. A second Ctrl+C quits immediately; temporary
files may then be left behind as `.<name>.*.fmtshift-tmp`.

Ctrl+C while folders are still being scanned stops the scan at once;
nothing is converted. Exit code 130 takes precedence over every other
outcome, so an interrupted run is never reported as "nothing to convert"
(4) or as a success.

Inputs are opened without blocking, so a named pipe (FIFO) given as an
input or found in a folder is rejected or skipped immediately rather than
hanging the command.

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
| --verbose, -v | Show error details and per-file timings |
| --quiet | Hide per-file and summary messages; errors still print |
| --json | Return machine-readable command results (v0.2) |

`--quiet` suppresses progress output only: the `Converted`/`Skipped` lines
and the batch summary. Errors are always printed to stderr, and output a
command exists to produce (the `formats` table, `version`) is printed
regardless, as with `git --quiet`. `--verbose` and `--quiet` cannot be
combined.

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
