#!/usr/bin/env bash
# Builds everything the sclang tests need, from scratch, in a throwaway folder:
#   - scdeken (the helper) built from this checkout
#   - a throwaway signing key, a tiny signed package index (index.json and
#     index.json.minisig) and a prebuilt package zip served through file:// URLs
#   - helper locks for the bootstrap tests (one right checksum, one wrong)
# The public key is added to <profile>/sc-deken/trusted-keys.txt.
#
#   scripts/make-sclang-fixtures.sh <fixtures-dir> <profile-dir>
#
# Never use these keys for anything real.
set -euo pipefail

fx="$1"; profile="$2"
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
native() { if command -v cygpath >/dev/null; then cygpath -m "$1"; else echo "$1"; fi; }
fileurl() { local p; p="$(native "$1")"; case "$p" in /*) echo "file://$p" ;; *) echo "file:///$p" ;; esac; }

command -v go >/dev/null || export PATH="/c/Scripts/Temp/claude/go-dl/x/go/bin:$PATH"
case "$(uname -s)" in
	MINGW*|MSYS*|CYGWIN*) os=windows; ext=.exe ;;
	Darwin) os=macos; ext= ;;
	*) os=linux; ext= ;;
esac
case "$(uname -m)" in arm64|aarch64) arch=arm64 ;; *) arch=x86_64 ;; esac

mkdir -p "$fx/keys" "$fx/dist" "$fx/pkg/fmx6" "$profile/sc-deken"
helper="$fx/scdeken$ext"
(cd "$repo" && go build -o "$helper" ./cmd/scdeken)

# Signing key and trust.
export SCDEKEN_FIXTURE_PASSWORD=fixture-only
"$helper" index keygen --out-dir "$fx/keys" --password-env SCDEKEN_FIXTURE_PASSWORD >/dev/null
cp "$fx/keys/scdeken.pub" "$profile/sc-deken/trusted-keys.txt"

# A prebuilt package.
echo "not a real plugin" > "$fx/pkg/fmx6/FMX6.scx"
"$helper" ci pack "$fx/pkg/fmx6" "$fx/fmx6.zip" >/dev/null
zip_sha="$(sha256sum "$fx/fmx6.zip" | cut -d' ' -f1)"
zip_size="$(wc -c < "$fx/fmx6.zip" | tr -d ' ')"
zip_url="$(fileurl "$fx/fmx6.zip")"

builds=""
for p in windows-x86_64 windows-arm64 linux-x86_64 linux-arm64 macos-x86_64 macos-arm64 macos-universal; do
	builds="$builds{\"platform\":\"$p-api3\",\"url\":\"$zip_url\",\"sha256\":\"$zip_sha\",\"size\":$zip_size},"
done
builds="[${builds%,}]"

cat > "$fx/index.json" <<JSON
{
  "schema": 1,
  "generated": "2026-10-24T12:00:00Z",
  "sc_versions": [{"version": "3.14.1", "api": 3, "min": "3.13.0"}],
  "advisories": [],
  "packages": [
    {"name": "FMX6", "summary": "FM synthesis plugin for tests", "repo": "https://github.com/example/fmx6",
     "license": "MIT", "kind": "plugin", "trust": "reviewed", "stars": 3, "pushed": "2026-01-01T00:00:00Z",
     "scanned_commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "ugens": ["FMX6"], "plugin_files": ["FMX6.scx"],
     "versions": [{"version": "1.0.0", "commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "builds": $builds}]},
    {"name": "Panola", "summary": "Pattern notation quark for tests", "repo": "https://github.com/example/panola",
     "license": "GPL-3.0", "kind": "quark", "trust": "reviewed", "stars": 5, "pushed": "2026-01-01T00:00:00Z",
     "scanned_commit": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "classes": ["Panola"],
     "versions": [{"version": "1.2.0", "commit": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}]},
    {"name": "Sketchy", "summary": "An unreviewed quark for tests", "repo": "https://github.com/example/sketchy",
     "license": "MIT", "kind": "quark", "trust": "unreviewed", "stars": 0, "pushed": "2026-01-01T00:00:00Z",
     "scanned_commit": "cccccccccccccccccccccccccccccccccccccccc", "classes": ["Sketchy"],
     "versions": [{"version": "0.1.0", "commit": "cccccccccccccccccccccccccccccccccccccccc"}]}
  ]
}
JSON
SCDEKEN_SIGNING_KEY="$(cat "$fx/keys/scdeken.key")" SCDEKEN_SIGNING_PASSWORD="$SCDEKEN_FIXTURE_PASSWORD" \
	"$helper" index sign --in "$fx/index.json" >/dev/null

# Bootstrap locks: the helper itself is "released" at a file:// URL.
host="$os-$arch"
cp "$helper" "$fx/dist/scdeken-$host$ext"
exe_sha="$(sha256sum "$helper" | cut -d' ' -f1)"
url_template="$(fileurl "$fx/dist")/scdeken-{platform}{ext}"
write_lock() { # file sha
	local shas=""
	for p in windows-x86_64 windows-arm64 linux-x86_64 linux-arm64 macos-x86_64 macos-arm64; do shas="$shas\"$p\":\"$2\","; done
	printf '{"version":"9.9.9","sha256":{%s},"url":"%s"}\n' "${shas%,}" "$url_template" > "$1"
}
write_lock "$fx/lock-ok.json" "$(echo "$exe_sha" | tr a-f A-F)" # also proves the compare ignores case
write_lock "$fx/lock-bad.json" "$(printf '0%.0s' $(seq 64))"

echo "$(native "$helper")" > "$fx/helper-path.txt"
