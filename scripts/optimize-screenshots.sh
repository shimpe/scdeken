#!/usr/bin/env bash
# Shrinks the screenshots in docs/screenshots (they ship in the repository):
# 256-colour palette, no dithering, no metadata. Then makes the smaller copies
# the SuperCollider help guide shows (SuperCollider copies everything under
# HelpSource, so the guide's pictures live in HelpSource/Guides/images).
# Needs ImageMagick.
#
#   scripts/optimize-screenshots.sh
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
command -v magick >/dev/null || { echo "ImageMagick (magick) is needed" >&2; exit 1; }

# The pictures the guide shows.
help_images="01-browse 02-search 03-local-build 07-installed dialog-doctor advisory-malware"

cd "$repo/docs/screenshots"
for f in *.png; do
	magick "$f" -dither None -colors 256 -strip "PNG8:$f.tmp" && mv "$f.tmp" "$f"
done

mkdir -p "$repo/HelpSource/Guides/images"
for name in $help_images; do
	magick "$name.png" -resize 900x -dither None -colors 256 -strip "PNG8:../../HelpSource/Guides/images/$name.png"
done
du -ch "$repo"/docs/screenshots/*.png | tail -1
du -ch "$repo"/HelpSource/Guides/images/*.png | tail -1
