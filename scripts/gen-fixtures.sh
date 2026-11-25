#!/usr/bin/env bash
# Regenerates the synthetic image fixtures in testdata/images.
#
# All fixtures are generated from scratch, so they are redistributable and
# contain no personal metadata. Requires: libvips CLI (vips), libheif CLI
# (heif-enc, with an HEVC encoder) and python3 with Pillow.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/testdata/images"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mkdir -p "$out"/{heic,jpeg,png,webp,invalid}

# Base image: 64x32 RGB, grey gradient that brightens left to right.
# Orientation tests rely on this: after a 90° clockwise rotation the gradient
# runs top (dark) to bottom (bright).
vips xyz "$work/xy.v" 64 32
vips extract_band "$work/xy.v" "$work/x.v" 0
vips linear "$work/x.v" "$work/g.v" 4 0 --uchar
vips bandjoin "$work/g.v $work/g.v $work/g.v" "$work/rgb.png"

# Same gradient with an alpha channel: left half fully transparent.
python3 - "$work/rgb.png" "$work/rgba.png" <<'EOF'
import sys
from PIL import Image
im = Image.open(sys.argv[1]).convert("RGBA")
w, h = im.size
px = im.load()
for y in range(h):
    for x in range(w // 2):
        r, g, b, _ = px[x, y]
        px[x, y] = (r, g, b, 0)
im.save(sys.argv[2])
EOF

# --- HEIC ---------------------------------------------------------------
heif-enc -q 90 -o "$out/heic/gradient.heic" "$work/rgb.png"
# Pixels stored landscape; HEIF irot transform makes the displayed image portrait.
heif-enc -q 90 --rotate-cw 90 -o "$out/heic/irot90.heic" "$work/rgb.png"
heif-enc -q 90 -o "$out/heic/alpha.heic" "$work/rgba.png"
# Primary image plus a thumbnail: must convert as a single image.
heif-enc -q 90 -t 16 -o "$out/heic/with-thumbnail.heic" "$work/rgb.png"
# Two top-level images: must be rejected.
heif-enc -q 90 -o "$out/heic/multi-image.heic" "$work/rgb.png" "$work/rgb.png"
# Display P3 ICC profile (libvips built-in profile).
vips icc_transform "$work/rgb.png" "$work/p3.v" p3 --input-profile srgb
vips heifsave "$work/p3.v" "$out/heic/display-p3.heic" --Q 90 --compression hevc

# --- JPEG ---------------------------------------------------------------
# EXIF orientation 6 (rotate 90° clockwise to display).
python3 - "$work/rgb.png" "$out/jpeg/exif-orientation-6.jpg" <<'EOF'
import sys
from PIL import Image
im = Image.open(sys.argv[1])
exif = Image.Exif()
exif[0x0112] = 6
im.save(sys.argv[2], quality=90, exif=exif.tobytes())
EOF
vips copy "$work/rgb.png" "$out/jpeg/gradient.jpg[Q=90]"

# --- PNG / WebP ---------------------------------------------------------
cp "$work/rgba.png" "$out/png/alpha.png"
vips copy "$work/rgb.png" "$out/webp/gradient.webp[Q=90]"

# --- Invalid ------------------------------------------------------------
: > "$out/invalid/empty.heic"
head -c 200 "$out/heic/gradient.heic" > "$out/invalid/truncated.heic"
printf 'this is not an image\n' > "$out/invalid/text.jpg"

echo "fixtures written to $out"
