#!/usr/bin/env bash
# Writes THIRD_PARTY_LICENSES for the fmtshift binary to stdout.
#
# It lists the licence of every Go module linked into ./cmd/fmtshift for the
# current GOOS/GOARCH, plus Go's own licence (the runtime and standard
# library are compiled in). libvips and its dependencies are NOT included in
# release archives; they are installed separately by Homebrew or the Linux
# distribution under their own licences.
set -euo pipefail

cd "$(dirname "$0")/.."

cat <<'EOF'
FmtShift third-party licences
=============================

The fmtshift binary contains the following third-party software, compiled
in. Image processing libraries (libvips, libheif, libde265, libjpeg-turbo,
libpng, libwebp, little-cms and their dependencies) are not part of this
archive: they are installed separately by your package manager and are
distributed under their own licences.

EOF

section() {
	printf '\n%s\n' "--------------------------------------------------------------------------------"
	printf '%s\n' "$1"
	printf '%s\n\n' "--------------------------------------------------------------------------------"
}

section "Go runtime and standard library ($(go env GOVERSION))"
cat "$(go env GOROOT)/LICENSE"

go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' ./cmd/fmtshift |
	sort -u |
	while read -r path version dir; do
		licence=$(find "$dir" -maxdepth 1 -type f \( -iname 'LICENSE*' -o -iname 'LICENCE*' -o -iname 'COPYING*' \) | sort | head -1)
		if [ -z "$licence" ]; then
			echo "error: no licence file found for $path $version" >&2
			exit 1
		fi
		section "$path $version"
		cat "$licence"
	done
