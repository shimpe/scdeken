#!/usr/bin/env bash
# Writes the package list the documentation screenshots are taken against:
# about two dozen packages with the real names, summaries, versions and commits
# of the first release, plus a few community quarks, one unreviewed package and
# one withdrawn package. Signed with the throwaway fixture key (never a real one).
#
#   scripts/make-doc-index.sh <fixtures-dir>     # writes <fixtures-dir>/docs/index.json (+ .minisig)
#
# Prebuilt downloads all point at the dummy zip made by make-sclang-fixtures.sh,
# so "installing" anything in a screenshot run copies that one placeholder file.
set -euo pipefail

fx="$1"; helper="$(cat "$fx/helper-path.txt")"
native() { if command -v cygpath >/dev/null; then cygpath -m "$1"; else echo "$1"; fi; }
fileurl() { local p; p="$(native "$1")"; case "$p" in /*) echo "file://$p" ;; *) echo "file:///$p" ;; esac; }
zip_url="$(fileurl "$fx/fmx6.zip")"
zip_sha="$(sha256sum "$fx/fmx6.zip" | cut -d' ' -f1)"
zip_size="$(wc -c < "$fx/fmx6.zip" | tr -d ' ')"
out="$fx/docs"; mkdir -p "$out"

builds=""
for p in windows-x86_64 windows-arm64 linux-x86_64 linux-arm64 macos-x86_64 macos-arm64 macos-universal; do
	builds="$builds{\"platform\":\"$p-api3\",\"url\":\"$zip_url\",\"sha256\":\"$zip_sha\",\"size\":$zip_size},"
done
builds="[${builds%,}]"

# pkg NAME KIND TRUST STARS LICENSE AUTHOR SUMMARY COMMIT VERSION PREBUILT(yes|no) [EXTRA]
#   EXTRA is more JSON members, with a leading comma (ugens, classes, depends, ...).
#   OLDER=x.y.z in the environment adds an older version.
pkg() {
	local name="$1" kind="$2" trust="$3" stars="$4" license="$5" author="$6" summary="$7" commit="$8" version="$9" prebuilt="${10}" extra="${11:-}"
	local release="{\"version\":\"$version\",\"commit\":\"$commit\"}" build=""
	if [ "$prebuilt" = yes ]; then release="{\"version\":\"$version\",\"commit\":\"$commit\",\"builds\":$builds}"; fi
	case "$kind" in plugin|plugin+quark) build=',"build":{"system":"cmake","subdir":"","sc_path_var":"SC_PATH"}' ;; esac
	if [ -n "${OLDER:-}" ]; then release="$release,{\"version\":\"$OLDER\",\"commit\":\"0123456789abcdef0123456789abcdef01234567\",\"builds\":$builds}"; fi
	printf '    {"name":"%s","summary":"%s","repo":"https://github.com/%s","license":"%s","authors":["%s"],"kind":"%s","trust":"%s","stars":%s,"pushed":"2026-10-20T12:00:00Z","scanned_commit":"%s","scan_format":2%s%s,"versions":[%s]}\n' \
		"$name" "$summary" "${REPO:-$author/$name}" "$license" "$author" "$kind" "$trust" "$stars" "$commit" "$extra" "$build" "$release"
}

list() {
	pkg fmx6 plugin reviewed 1 GPL-3.0 shimpe "extended version of supercollider fm7 with six operators" 208cac8b5a71b7bc8d26ad4a419a426f38b85c36 0.1.0 yes ',"ugens":["FMX6"]'
	pkg nome6 plugin reviewed 0 GPL-3.0 shimpe "6 operator fm-like synth based on jacobian elliptic functions" fc3a5051d4702f9000a629ad5fb0d5d2e52105e7 0.1.0 no ',"ugens":["Nome6"]'
	pkg sc3-plugins plugin+quark reviewed 552 GPL-2.0-or-later supercollider "Community plugins for SuperCollider" fa926d6b554acef35b2fd9deb2f996085fa86bd7 3.14.1 yes ',"ugens":["FM7","BFManipulate","DiscreteShape","TJ","Gendy4"],"classes":["Gendy4"]'
	OLDER=0.4.1 pkg portedplugins plugin reviewed 226 GPL-3.0 madskjeldgaard "A collection of plugins for the SuperCollider sound environment, all of which are ported / remixed from elsewhere" 26fa985d6987d642baefe461b25606396362cdd5 0.4.2 yes ',"ugens":["AnalogFoldOsc","DiodeRingMod","BMoog","SVF","Wavefolder","Tube"]'
	pkg f0plugins plugin reviewed 52 GPL-2.0-or-later redFrik "Sound-chip emulations, euclidean rhythm and other UGens by redFrik" 7030aaf9c6450010d4536c2c1c1ecbdc0416c520 4.2.3 yes ',"ugens":["Ay8910","Sid6581","Euclid"]'
	pkg vb_UGens plugin reviewed 16 GPL-3.0 v7b1 "A modest collection of UGens by Volker Boehm: filters, reverb, phase vocoder, utilities and more" 01366c44aba814800efba6e3b29a24b9e477bb3d 0.0.6 yes ',"ugens":["VBJonVerb","VBPVoc","VBFourses"]'
	pkg XPlayBuf plugin reviewed 28 GPL-3.0 elgiano "A buffer player for SuperCollider that loops, fades and does not glitch on the float-precision issue" 44b90771c4508913ad76cb7347b6b10183395351 0.1.3 yes ',"ugens":["XPlayBuf"]'
	pkg squinewave plugin reviewed 25 GPL-3.0 required-field "Bandlimited square-sine-saw-pulse morphing oscillator with hardsync" 582e011c4f5bbeb148f6dc22d1086e666527c5eb 1.0.0-RC4 yes ',"ugens":["SquiNewave"]'
	pkg supercollider-safety-limiter plugin reviewed 27 MIT nhthn "A zero-latency safety limiter for SuperCollider" 7d99fe3301712ebd666eab659197317f2a73a8bb 0.1.0 yes ',"ugens":["SafetyLimiter"]'
	pkg guttersynth-sc plugin reviewed 50 GPL-3.0 madskjeldgaard "Chaotic physical modelling in SuperCollider" fb481d47336cdf45c1c19ca8392d982657a8c136 0.1.0 yes ',"ugens":["GutterSynth"]'
	pkg supercollider-cd-skip plugin reviewed 30 MIT nhthn "A CD skipping UGen for SuperCollider" c87b4a707b38d42b1b85e1e022e481fa2cf679d1 0.1.0 yes ',"ugens":["CDSkip"]'
	pkg simplexsynth plugin reviewed 3 GPL-3.0 esluyter "Simplex noise UGen for SuperCollider" 6fb3235b3bfd7b26edbbf18459f438a3a102a19e 0.1.0 yes ',"ugens":["SimplexNoise"]'
	pkg MyUGens plugin reviewed 18 GPL-3.0 sonoro1234 "Physical-modelling UGens for trombone, clarinet, flute, plucked strings and more" f62135a1314076a30e1bafd2229ba9b85264b373 0.1.0 no ',"ugens":["Trombone","Clarinet","Flute"]'
	pkg steroids-ugens plugin reviewed 13 GPL-3.0 tai-studio "SuperCollider standard UGens on steroids" 1d4a9b2a0aa439bf9aac9d4257f3d7867cad36c1 0.1.0 no ',"ugens":["SineSteroids","SawSteroids"]'
	pkg Panola quark reviewed 17 GPL-3.0 shimpe "Quark to simplify music composition with patterns" d5e55dc581373b1b4bdd671bba56281fa73d6f95 1.1.0 no ',"classes":["Panola","PanolaTester"]'
	pkg Mitola quark reviewed 10 GPL-3.0 shimpe "Microtonal counterpart to Panola - write microtonal music using a text-based score" dbffd07401e5b94467d0cc0dc65aa1a21170fef5 0.0.8 no ',"classes":["Mitola"],"depends":{"Panola":">=1.1.0"}'
	pkg MSScore quark reviewed 0 GPL-3.0 shimpe "Write, play and visualize scores in MusicScene from Panola strings" 547444a19420fbc61467b737a02206d36d91c8b5 0.7.0 no ',"classes":["MSScore"],"depends":{"Panola":">=1.1.0"}'
	pkg Scparco quark reviewed 7 GPL-3.0 shimpe "Quark to simplify parsing text" a88db64e7f870a2ebd22218c089f76490010d8f9 0.0.6 no ',"classes":["Scparco"]'
	pkg Phijack quark reviewed 0 GPL-3.0 shimpe "A value pattern that hijacks another value pattern output when a predicate is true" 2f4f06d96d849afeebbc2fa52c41c0a836134ac2 0.0.1 no ',"classes":["Phijack"]'
	REPO=shimpe/sc-lsystem pkg LSystem quark reviewed 0 GPL-3.0 shimpe "Quark to generate and interpret Lindenmayer Systems" 2c7e78bfe44a9283c74579a478fca1a23a6e17e9 0.1.0 no ',"classes":["LSystem"]'
	pkg AlgaLib quark community 63 GPL-3.0 vitreo12 "Interpolating live coding environment" 6e6e5225c8f6eb4915f46f9d95dd3c8975a1013d 1.3.3 no ',"classes":["Alga","AlgaNode"]'
	pkg atk-sc3 quark community 92 GPL-3.0 ambisonictoolkit "An extension library for the SuperCollider programming language" b15c604a3dec5e95ba2485e86ad5af455900f408 5.0.7 no ',"classes":["FoaEncode","FoaDecode"]'
	pkg Modality-toolkit quark community 100 GPL-3.0 ModalityTeam "A SuperCollider toolkit to simplify the creation of personal (electronic) instruments utilising hardware and software controllers of any kind." 2a8434539b099ccf7a10a7da99e99c246b4672aa 0.2.1 no ',"classes":["MKtl","MKtlElement"]'
	pkg dronepad-ugens plugin unreviewed 0 MIT example "Experimental drone pad generator with slowly drifting partials" 3f9a1c7d2b8e4a6051c9d7e2b4f8a0c3d5e7b912 0.1.0 no ',"ugens":["DronePad"]'
	pkg SampleHarvest quark unreviewed 2 MIT example "Fetch free sample packs from the web into a buffer folder" eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee 1.0.0 no ',"classes":["SampleHarvest"]'
}

{
	cat <<'HEAD'
{
  "schema": 1,
  "generated": "2026-10-24T12:00:00Z",
  "sc_versions": [{"version": "3.14.1", "api": 3, "min": "3.13.0"}],
  "advisories": [
    {"id": "SCD-2026-0003", "package": "SampleHarvest", "versions": ["1.0.0"], "severity": "malware",
     "summary": "Version 1.0.0 uploads the contents of your Documents folder when the class library compiles.",
     "details": "https://github.com/shimpe/scdeken-recipes/issues/12", "published": "2026-10-22"}
  ],
  "packages": [
HEAD
	list | paste -sd, - | sed 's/},    {"name"/},\n    {"name"/g'
	printf '\n  ]\n}\n'
} > "$out/index.json"

SCDEKEN_SIGNING_KEY="$(cat "$fx/keys/scdeken.key")" SCDEKEN_SIGNING_PASSWORD="${SCDEKEN_FIXTURE_PASSWORD:-fixture-only}" \
	"$helper" index sign --in "$out/index.json" >/dev/null
