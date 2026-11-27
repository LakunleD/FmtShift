#!/usr/bin/env bash
# Renders the Homebrew formula for a source archive.
#
#   scripts/render-formula.sh <archive-url> <archive-file-or-sha256> [version]
#
# Prints Formula/fmtshift.rb to stdout. The second argument is either the
# downloaded archive (its SHA-256 is computed) or the SHA-256 itself.
#
# Homebrew infers the version from tag archive URLs (.../v0.1.0.tar.gz), and
# `brew audit --strict` rejects a redundant version line, so omit [version]
# for releases. Pass it when the URL carries no version, e.g. a commit
# archive used for a dry run; the formula then gets an explicit version.
set -euo pipefail

if [ $# -lt 2 ] || [ $# -gt 3 ]; then
	echo "usage: $0 <archive-url> <archive-file-or-sha256> [version]" >&2
	exit 2
fi
version=${3:-}
if [ -n "$version" ] && ! [[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "error: version must look like 1.2.3, got: $version" >&2
	exit 1
fi
url=$1
if [ -f "$2" ]; then
	sha=$(shasum -a 256 "$2" | cut -d' ' -f1)
else
	sha=$2
fi
if ! [[ $sha =~ ^[0-9a-f]{64}$ ]]; then
	echo "error: invalid SHA-256: $sha" >&2
	exit 1
fi

template="$(dirname "$0")/../packaging/homebrew/fmtshift.rb.tmpl"
if [ -n "$version" ]; then
	version_line="  version \"$version\""
else
	version_line=""
fi
sed -e "s|@URL@|$url|" -e "s|@SHA256@|$sha|" "$template" |
	awk -v line="$version_line" '{ print } /^  url / && line != "" { print line }'
