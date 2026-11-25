#!/usr/bin/env bash
# Generates large benchmark images in testdata/bench (git-ignored).
#
# The images are synthetic 12-megapixel (4032x3024, iPhone main camera size)
# photos: blurred noise over a colour gradient, which compresses roughly like
# a real photo. Requires the libvips CLI with HEVC encoding (x265).
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$root/testdata/bench"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mkdir -p "$out"
w=4032 h=3024

vips xyz "$work/xy.v" $w $h
vips extract_band "$work/xy.v" "$work/x.v" 0
vips extract_band "$work/xy.v" "$work/y.v" 1
vips linear "$work/x.v" "$work/r.v" 0.05 20
vips linear "$work/y.v" "$work/g.v" 0.06 30
vips linear -- "$work/x.v" "$work/b.v" -0.04 220
vips bandjoin "$work/r.v $work/g.v $work/b.v" "$work/base.v"
vips gaussnoise "$work/n.v" $w $h --sigma 30 --mean 0
vips gaussblur "$work/n.v" "$work/nb.v" 1.5
vips bandjoin "$work/nb.v $work/nb.v $work/nb.v" "$work/n3.v"
vips add "$work/base.v" "$work/n3.v" "$work/sum.v"
vips cast "$work/sum.v" "$work/photo8.v" uchar
vips copy "$work/photo8.v" "$work/photo8s.v" --interpretation srgb

vips heifsave "$work/photo8s.v" "$out/12mp.heic" --Q 80 --compression hevc

# 10-bit SDR variant: same sRGB content at higher precision. This is NOT an
# HDR image (no PQ/HLG transfer function, no gain map) and measures only the
# cost of 10-bit decoding and bit-depth reduction.
vips linear "$work/photo8s.v" "$work/photo16.v" 257 0
vips cast "$work/photo16.v" "$work/photo16u.v" ushort
vips copy "$work/photo16u.v" "$work/photo16s.v" --interpretation rgb16
vips heifsave "$work/photo16s.v" "$out/12mp-10bit-sdr.heic" --Q 80 --compression hevc --bitdepth 10

ls -l "$out"
