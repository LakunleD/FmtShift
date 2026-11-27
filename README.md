# FmtShift

FmtShift converts images on your own computer, for example iPhone HEIC photos
to JPEG. Nothing is uploaded, and your original files are never modified.

```console
$ fmtshift convert IMG_0001.HEIC
Converted IMG_0001.HEIC -> IMG_0001.jpg
```

> **Status:** early development (pre-v0.1). File and folder conversion and
> saved settings work; directory watching is planned. There are no release
> binaries yet; see Installing below.

## Supported formats

| Format | Read | Write |
|---|---|---|
| HEIC / HEIF | Yes | No |
| JPEG | Yes | Yes |
| PNG | Yes | Yes |
| WebP | Yes | Yes |

Still images only. Animations, image sequences and HDR (PQ/HLG) images are
reported as unsupported rather than converted incorrectly. iPhone HDR photos
convert using their standard (SDR) image.

## Installing

FmtShift uses [libvips](https://www.libvips.org/) (with libheif) for image
processing. Your package manager installs it; FmtShift does not bundle it.

> The first release (v0.1.0) is in preparation. Until it is published, use
> [Building from source](#building-from-source).

**macOS** (Homebrew; also works with Homebrew on Linux):

```sh
brew install lakunled/tap/fmtshift
```

**Linux** (Ubuntu 24.04+, Debian 13+; amd64 or arm64): install the runtime
libraries, then download `fmtshift_<version>_linux_<arch>.tar.gz` from
[Releases](https://github.com/lakunled/fmtshift/releases), check it against
`SHA256SUMS`, and put `fmtshift` on your `PATH`:

```sh
sudo apt install libvips42t64 libheif-plugin-libde265
sha256sum -c --ignore-missing SHA256SUMS
tar -xzf fmtshift_*_linux_*.tar.gz
sudo install fmtshift_*/fmtshift /usr/local/bin/
```

On Fedora 40+, install `vips` and `libheif` with `dnf` instead.

**Upgrading:** `brew upgrade fmtshift`, or replace the binary with one from a
newer release. **Uninstalling:** `brew uninstall fmtshift`, or delete the
binary; saved settings live in the file printed by `fmtshift config path`.

### Building from source

Needs Go and the libvips development files.

```sh
brew install go vips                                   # macOS
sudo apt install golang libvips-dev libheif-dev \
  libheif-plugin-libde265 pkg-config                   # Ubuntu / Debian
git clone https://github.com/lakunled/fmtshift.git
cd fmtshift
make build                                             # produces bin/fmtshift
```

Use `make` rather than plain `go build` on macOS: the Makefile adds
Homebrew's keg-only libarchive to `PKG_CONFIG_PATH`, which libvips needs.

## Usage

```sh
fmtshift convert photo.heic                          # -> photo.jpg next to it
fmtshift convert photo.heic --to png                 # PNG instead
fmtshift convert *.HEIC --output ./converted         # into another folder
fmtshift convert ~/Pictures/Trip --from heic         # every HEIC in a folder
fmtshift convert ./photos -r -o ./jpegs -w 4         # subfolders too, 4 at a time
fmtshift convert photo.heic --quality 90             # JPEG/WebP quality 1-100 (default 85)
fmtshift convert photo.heic --strip-metadata         # remove EXIF, location, etc.
fmtshift convert photo.heic --overwrite              # replace an existing photo.jpg
fmtshift config set quality 90                       # save defaults for convert
fmtshift config show                                 # settings in effect
fmtshift formats                                     # what this build can read/write
fmtshift version
```

What FmtShift guarantees:

- **Originals are never modified**. An output is never written over its own
  source or over any other file you are converting, even with `--overwrite`
  (for example, `photo.png` → `photo.jpg` next to an original `photo.jpg`).
- **Existing files are kept** unless you pass `--overwrite`; they are reported
  as skipped, so re-running a command is safe.
- **No partial files**: outputs appear only when complete.
- **Correct orientation and colour**: rotation is applied once, and colour
  profiles (such as iPhone Display P3) are kept, or converted to sRGB when
  metadata is stripped.

Exit codes: `0` success, `1` a conversion failed, `2` invalid arguments, `4`
nothing to convert, `130` interrupted. See [docs/CLI.md](docs/CLI.md) for the
full specification.

## Development

```sh
make check    # gofmt, go vet and all tests
make race     # tests with the race detector
make bench    # benchmarks (generates large test images on first run)
make fixtures # regenerate the small test images
```

Design documents live in [docs/](docs/): product requirements, architecture,
CLI specification, testing strategy, the implementation plan and the release
process ([docs/RELEASING.md](docs/RELEASING.md)).

## License

MIT, see [LICENSE](LICENSE). Release archives include
`THIRD_PARTY_LICENSES` for the Go modules compiled into the binary.
