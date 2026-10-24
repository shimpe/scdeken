# sc-deken — design

*Status: approved · 2026-10-24 · working name "sc-deken" (renaming is cheap; see §11)*

## 1. Intent

SuperCollider has no easy way to install server plugins (C++ UGens), especially the many
that only exist as source repos on GitHub. Users get stuck on compilers, CMake, matching SC
headers and third-party dependencies. Quarks (sclang extensions) have their own separate
installer, so there are two systems for one job.

Pure Data solved this with **deken**: a searchable index of prebuilt, per-platform packages,
installable from inside Pd. sc-deken brings that model to SuperCollider, with a stronger trust
story than deken's ("anyone with an account can upload a binary"):

- Packages enter a curated repository as **recipes** (data, not code), reviewed by maintainers.
- **CI builds and signs** binaries from those recipes. Users never receive binaries uploaded by
  other users.
- When no binary exists for a user's platform, the tool **builds locally from the same recipe**,
  fetching matching SC headers and telling the user exactly which build tools are missing.
- **One system** for plugins and quarks, used from a GUI **inside SuperCollider**.
- **Search needs no manual upkeep**: the index is regenerated automatically from several sources.

Prior art studied: deken (Pd), plugins.quark (Mads Kjeldgaard — local-build-only, recipe idea,
in-SC GUI; reference only, no code reuse required), conda-forge (recipe PRs + CI build farm, the
closest model), PyPI/npm provenance, F-Droid reproducible builds.

## 2. Goals, non-goals, anti-goals

### First release must (owner's success criteria)

1. **Search** packages (plugins and quarks) from inside SC.
2. **Install** packages on **Linux, macOS and Windows**.
3. **Download CI-built, signed binaries** for at least a few reviewed packages.
4. **Build locally** when no binary exists, including **fetching SC headers matching the
   installed SC version**.
5. **Diagnose the toolchain**: tell the user which compilers / build tools / libraries are
   missing for a local build, with per-OS install hints.

### First-release packages (owner's decision)

Reviewed recipes, CI-built wherever the package builds, for **every package plugins.quark
currently lists** (its `packages/` folder, not `draft-packages/`) **plus fmx6 and nome6**:

| Package | Repo | Tags today |
|---|---|---|
| MyUGens | github.com/sonoro1234/MyUGens | none |
| f0plugins | github.com/redFrik/f0plugins | yes |
| guttersynth-sc | github.com/madskjeldgaard/guttersynth-sc | yes |
| IBufWr | github.com/tremblap/IBufWr | yes |
| portedplugins | github.com/madskjeldgaard/portedplugins | yes |
| sc3-plugins | github.com/supercollider/sc3-plugins | yes (`-DSUPERNOVA=ON`) |
| simplexsynth | github.com/esluyter/simplexsynth | none |
| squinewave | github.com/required-field/squinewave | yes (CMake in `supercollider/` subfolder) |
| steroids-ugens | github.com/tai-studio/steroids-ugens | none |
| supercollider-cd-skip | github.com/nhthn/supercollider-cd-skip | yes |
| supercollider-pll | github.com/nhthn/supercollider-pll | none |
| supercollider-safety-limiter | github.com/nhthn/supercollider-safety-limiter | yes |
| vb_UGens | github.com/v7b1/vb_UGens | yes |
| XPlayBuf | github.com/elgiano/XPlayBuf | yes |
| fmx6 | github.com/shimpe/fmx6 | none |
| nome6 | github.com/shimpe/nome6 | none |

**Quarks in the first release (owner's decision):** the entire **official quarks directory**
(`supercollider-quarks/quarks`, about 320 entries, `community-listed`), plus these quarks, which
are **not** in that directory and so get reviewed `kind = "quark"` recipes:

| Quark | Repo | Tags today |
|---|---|---|
| MSScore | github.com/shimpe/msscore | yes |
| Panola | github.com/shimpe/panola | yes |
| Scparco | github.com/shimpe/scparco | yes |
| Mitola | github.com/shimpe/mitola | yes |
| Phijack | github.com/shimpe/phijack | yes |
| ScTimeline | github.com/shimpe/sc-timeline | none |
| ScMidiControls | github.com/shimpe/sc-midi-controls | none |
| TemporalDict | github.com/shimpe/temporaldict | none |
| LSystem | github.com/shimpe/sc-lsystem | none |

Each has a `.quark` file at its root; the recipe takes name, summary and dependencies from it.

The owner will add more later, so **adding a repository must be easy** (§6a). If a package
fails to build on some platform, that platform gets no binary, shows "not available for your
platform", and the failure is reported on the recipe repo. It does not block the release
(my call).

### Also in the first release (my call — cheap once 1–5 exist)

- Remove and update installed packages; pick a specific version.
- Show unreviewed (auto-discovered) packages with a warning and allow local builds of them.

### Non-goals (for now)

- Users uploading binaries. Contributions are recipes (pull requests), never binaries.
- Sandboxing plugins. Installed code runs with full user rights, as in every SC install today.
- Installing compilers or system libraries automatically. The tool diagnoses and hints; the
  user installs.
- Build systems other than CMake (covers the SC plugin cookiecutter template and most repos).
- Own server infrastructure or user accounts. Everything lives on GitHub.
- Per-project lockfiles / environments.
- SuperCollider versions older than 3.13 (my call; see §11).

### Anti-goals (would be a failure even if it "works")

- Running unreviewed code without an explicit, informed confirmation.
- Breaking or hiding quarks the user installed with the existing `Quarks` system.
- Freezing the SC interpreter/GUI while downloading or building.
- A moved git tag silently changing what users receive.
- Code that is hard to review. The owner explicitly asked for **beautiful, easy-to-review
  code**: small focused packages, plain names, few dependencies, no frameworks, no cleverness.

### License (owner's decision)

sc-deken itself is **MIT-licensed**: the Go helper, the quark, the recipes repo, CI workflows
and indexer. Packages keep their own licenses. Many are GPL, so every published binary's index
entry links to the exact public source commit it was built from, and the archive includes the
package's license file (my call: this satisfies GPL source availability for redistributed builds).

## 3. Architecture overview

```
┌──────────────── user machine ────────────────┐       ┌──────────── GitHub ────────────────┐
│                                              │       │                                    │
│  SuperCollider (sclang)                      │       │  sc-deken-recipes (repo)           │
│   └─ Deken quark  (GUI + sclang API)         │       │   ├─ recipes/*.toml  (reviewed)    │
│        │  launches, reads JSON / OSC events  │       │   ├─ CI: build + sign on new tags  │
│        ▼                                     │ HTTPS │   ├─ CI: nightly index rebuild     │
│  scdeken  (Go helper, one file per OS)  ─────┼──────►│   ├─ Releases: binaries            │
│   ├─ index cache   (signed index.json)       │       │   └─ Pages: index.json + .minisig  │
│   ├─ download + verify + unpack              │       │                                    │
│   ├─ local build (git, cmake, SC headers)    │       │  sc-deken (repo)                   │
│   └─ toolchain doctor                        │       │   ├─ Go helper source + releases   │
│                                              │       │   └─ the Deken quark               │
└──────────────────────────────────────────────┘       └────────────────────────────────────┘
```

Two repositories (my call):

- **`sc-deken`** — the tool: Go helper (`scdeken`) source, its release workflow, and the quark
  (repo root is installable with `Quarks.install("https://github.com/<org>/sc-deken")`).
- **`sc-deken-recipes`** — the repository of packages: recipes, build/sign workflows, the
  indexer job, published index (GitHub Pages) and binaries (GitHub Releases). Reviewed by a
  small group of maintainers, starting with the owner.

The same `scdeken` binary is used on user machines, in the build CI and in the indexer CI, so
there is one implementation of recipe parsing, platform detection and building.

## 4. Packages, recipes and versions

### 4.1 Package

A package is a git repository containing a **server plugin part** (C++ UGens built with CMake),
a **sclang part** (classes, help files — today's "quark"), or both. Many plugin repos already
ship `.sc` classes and `.schelp` files beside their C++ code; their CMake install step copies
those too, so a built plugin folder is self-contained.

### 4.2 Recipe (reviewed packages)

One TOML file per package in `sc-deken-recipes/recipes/`. TOML because humans write these in
pull requests; it is pure data, so a malicious recipe cannot execute sclang (plugins.quark loads
`.scd` recipes as code — explicitly avoided). Illustrative shape; the builder finalizes field
names:

```toml
name     = "portedplugins"
summary  = "Ported / remixed UGens from elsewhere"
repo     = "https://github.com/madskjeldgaard/portedplugins"
license  = "GPL-3.0"
authors  = ["Mads Kjeldgaard"]
kind     = "plugin"              # "plugin" | "quark" | "plugin+quark"

[versions]
tag_pattern = "v?{version}"      # how git tags map to versions
minimum     = "0.4.0"            # older tags are not built

[build]                          # for kinds containing a plugin
system      = "cmake"
subdir      = ""                 # if CMakeLists.txt is not at repo root
sc_path_var = "SC_PATH"          # CMake variable receiving the SC source path
cmake_flags = ["-DSUPERNOVA=ON"]
submodules  = true

[requires]                       # system libraries; used by doctor + CI images
fftw = { apt = "libfftw3-dev", brew = "fftw", pacman = "fftw", vcpkg = "fftw3", pkgconfig = "fftw3" }

[depends]                        # other sc-deken packages / quarks
# "SomeQuark" = ">=1.0"
```

### 4.3 Versions = git tags, approved one pull request at a time (owner's decision)

- A package version is a **git tag** in its repo, mapped through `tag_pattern`.
- **Every version of a reviewed package is approved by a maintainer.** The recipe lists its
  versions explicitly as `version = commit` pairs. That list is the **single authoritative
  record** of what may be built and signed.
- A scheduled bot (in the nightly indexer job) notices new upstream tags on reviewed packages
  and opens **one pull request per new version**. The PR adds the `version = commit` line, CI
  builds it on all platforms, and links the upstream diff since the previous approved version.
  Binaries are signed and published **only after a maintainer merges**. Fork PRs and unmerged
  PRs never publish.
- **Moved tags:** if an upstream tag now points to a different commit than the recipe records,
  the bot opens an issue instead of a PR. The recorded commit stays what users get (anti-goal:
  moved tags).
- Signed binaries therefore always trace back to a commit a maintainer approved. A local build
  of a not-yet-approved tag of a reviewed package is treated as **unreviewed** (warning applies).
- **Repos without tags** (6 of the 16 first-release packages): a reviewed recipe lists
  versions explicitly as `version = commit` pairs (e.g. `"0.1.0" = "3f2c9e1…"`) and CI builds
  those. Unreviewed untagged repos appear as `0.0.0+<shortcommit>` from the default branch,
  labeled **unstable**, local build only (my call). For the owner's own repos (fmx6, nome6),
  adding tags is simpler still.
- Users can install the latest, any listed version, and see "update available".

### 4.4 Platform key for binaries

A binary is identified by: **OS** (linux / macos / windows) × **arch** (x86_64 / arm64) ×
**plugin API version** (`sc_api_version` from `SC_InterfaceTable.h`, read from the SC
headers used to build). Not by SC release number: several releases share one plugin API (3.13
and 3.14 both use 3; `develop` is at 7 at time of writing). There is no separate supernova API
version. scsynth and supernova builds are separate files (`_supernova` suffix) shipped in the
same archive.

## 5. Trust and signing

- **Recipe review**: recipes are added/changed by pull request; a maintainer approves.
  Fork PRs build in CI (to show the recipe works) but **never publish or sign**.
- **Signing**: one **minisign** key. The secret key lives only in a GitHub Actions environment
  restricted to the protected `main` branch. CI signs `index.json`; the index contains the
  SHA-256 of every published archive. So one signature check covers everything.
- **Client verification**: `scdeken` embeds the minisign public key. It refuses an index whose
  signature fails, and refuses an archive whose hash does not match the index.
- **Unreviewed packages** (auto-discovered, §6) carry no binaries and no hashes; local build
  only, after a warning that names the repo and the exact commit.
- **Key rotation** (my call): the index may list a next public key signed by the current one;
  the client accepts it. Kept minimal in v1.
- **Bootstrap**: the quark pins the `scdeken` release version and the SHA-256 of each platform's
  helper binary. On first use the quark downloads the helper from the `sc-deken` GitHub release
  and verifies its hash using OS tools (`shasum -a 256` on macOS, `sha256sum` on Linux,
  PowerShell `Get-FileHash` on Windows), because sclang has no hashing of its own.
- **Retraction / blacklisting**: see §5a.
- Reproducible-build verification (F-Droid style) is **out of scope** for v1. Bit-identical
  C++ builds on macOS/Windows are hard; CI provenance + signing is the trust anchor.

## 5a. Advisories: retraction, blacklisting and removal (owner's requirement)

The goal: if a package (or one version of it) is found to contain malware, a serious
vulnerability or a dangerous bug, maintainers can **stop new installs** and **get it off users'
machines, with the user's permission**.

### Publishing an advisory

- Advisories are TOML files in `sc-deken-recipes/advisories/`, added by pull request like
  recipes. Illustrative shape:

  ```toml
  id        = "SCD-2026-0001"
  package   = "somepackage"              # or repo = "https://github.com/…" for unreviewed repos/quarks
  versions  = ["1.2.0", "1.2.1"]         # or ["*"]; commits = ["abc123…"] also allowed
  severity  = "malware"                  # "malware" | "vulnerable" | "broken"
  summary   = "Version 1.2.x downloads and runs a remote script at class library compile time."
  details   = "https://github.com/<org>/sc-deken-recipes/issues/123"
  published = 2026-11-02
  ```

- Applies to **anything the tool can install**: reviewed packages, unreviewed/auto-discovered
  repos (by repo URL and/or commit), and quarks, including official-directory quarks.
- Advisories go into the **signed index**, so they can't be forged or stripped in transit.
- Retracted versions are **removed from the installable list** immediately. Their release
  assets are kept (renamed, delisted) for forensics, not deleted (my call).
- Once there are two or more maintainers, `advisories/` needs **two approvals** (branch
  protection / CODEOWNERS). A false "malware" alarm on a legitimate package is itself harmful
  (my call).

### What users see

- **Background check:** at sclang startup, the quark launches `scdeken advisories check`
  asynchronously. It refreshes the signed index (cheap if unchanged, via HTTP ETag) and matches
  advisories against **everything installed**: packages in `state.json`, unmanaged copies found
  in the extension dirs (by plugin file names, class names and, where known, file hashes), and
  quarks in `Quarks.installed` (by repo URL and commit). Offline → silently skipped. Users can
  turn the check off in Settings; it is **on by default** (my call).
- **On a match**, an SC dialog shows: package and version, severity, summary, link to details,
  and where it is installed. Choices:
  - **Remove now** (default for `malware` and `vulnerable`)
  - **Keep** — for `malware`, needs a second explicit confirmation; recorded so the dialog
    doesn't nag at every startup, but listed with a red badge in "Installed"
  - **Remind me later**
- **Nothing is removed without the user's permission** (owner's requirement).
- **Removal** moves the package folder to `<app-support>/sc-deken/quarantine/<id>/`, outside
  any folder SC loads from. For quarks it also removes the entry from `LanguageConfig`. Then it
  prompts to recompile the class library and reboot the server. Quarantined items can be deleted
  for good from Settings (my call: quarantine rather than delete, so nothing is lost on a false
  alarm).
- **Install-time block:** installing a retracted version is refused for `malware`, and allowed
  after a warning for `vulnerable` / `broken`. Search shows retracted entries with a red badge.
- **Updates as remedy:** if a fixed version exists, the dialog offers **Update to x.y.z**
  instead of plain removal.
- The dialog is honest about limits: code that already ran (for example during class library
  compilation) can't be undone by removal, and the advisory's details link says what it did.

## 6. Index and search (no manual upkeep)

A nightly CI job in `sc-deken-recipes` (plus on every merge to `main`) runs
`scdeken index build` and publishes `index.json` + `index.json.minisig` to GitHub Pages.
Sources:

1. **Recipes** (reviewed) — with versions, commits and binary URLs/hashes from CI builds.
2. **Official quarks directory** (`supercollider-quarks/quarks`, `directory.txt`).
3. **GitHub topic search**: `supercollider-quark`, `supercollider-plugin`, `supercollider-ugens`
   (and the builder may add obvious synonyms).

For every repo: description, license, stars, last push, tags, and **searchable contents** —
class names and UGen names taken from `.sc` and `.schelp` files via the GitHub trees/contents
API. So a user can search "pitch tracker" or a UGen name seen in someone's code. The indexer
caches per repo by last-push time to stay within API rate limits.

For auto-discovered plugin repos, the indexer **generates a draft recipe** when the repo looks
like the standard cookiecutter CMake layout. Drafts appear as **unreviewed**; a maintainer can
promote one into `recipes/` by PR.

**Quarks and trust** (my call): quarks from the **official quarks directory** get a
`community-listed` badge and install without a warning, as `Quarks.gui` does today. Quarks found
only by topic search are **unreviewed** and get the warning. For all quarks the indexer records
tag → commit, and the install passes the **commit hash** to Quarks, so the moved-tag rule
holds for quarks too.

Search runs **locally** in `scdeken` against the cached index: fast, works offline once
fetched. **Searched fields (owner's requirement):** package name, **summary**, and the
code-derived class and UGen names. Summary source, in order: recipe `summary`, `.quark` file
`summary`, the quarks directory entry, then the GitHub repo description. Every index entry
should have one. Ranking: name match > summary match > class/UGen name match; reviewed before
unreviewed; ties by stars (my call). Each result says which field matched, so the GUI can explain
code-derived hits.

## 6a. Adding a package (must be easy — owner's requirement)

Three entry points, all ending in the same reviewed pull request:

1. **Issue form, no git knowledge needed.** The recipes repo has a GitHub issue form "Add a
   package" with one required field: the repo URL. A workflow then runs `scdeken recipe new
   <url>`, which inspects the repo (license, description, tags or latest commit, CMake layout,
   `SC_PATH`-style variable, subfolder, submodules, `.quark` file) and writes a draft recipe.
   It then opens a pull request with the draft, and the PR's CI builds it on all platforms. The
   issue gets a link to the PR plus a build summary per platform. A maintainer reviews, tweaks
   if needed, and merges.
2. **From inside SC.** On an unreviewed package, the details pane has a "Suggest for review"
   button that opens the issue form in the browser, pre-filled with the URL.
3. **Command line, for maintainers.** `scdeken recipe new <url>` locally prints the draft
   recipe; `scdeken recipe check <file>` validates it and runs a local build.

Adding the owner's next batch of repos should take one issue (or one `recipe new`) per repo,
plus a merge.

## 7. CI builds

Workflow in `sc-deken-recipes`. On pull requests it builds new or changed recipe versions
(no signing, no publishing). On merge to `main` it builds, publishes and signs. Matrix per
(package version × platform):

| Platform | Runner (my call) | Notes |
|---|---|---|
| linux x86_64 | ubuntu-22.04 | older glibc for wider compatibility |
| linux arm64 | ubuntu-22.04-arm (or 24.04-arm) | |
| macos universal | macos-latest (arm64) | builds arm64 and x86_64 separately, then `lipo` into one archive |
| windows x86_64 | windows-latest | MSVC |

**Prebuilt archives must be self-contained** (my call): third-party libraries from
`[requires]` are linked statically, using vcpkg static triplets per OS/arch in CI
(e.g. `x64-linux`, `arm64-osx`, `x64-osx`, `x64-windows-static`). No Homebrew/apt runtime
dependencies and no DLLs to install. So prebuilt installs never need a doctor check. Building
each macOS arch separately avoids the "Homebrew bottles are single-arch" problem. A recipe whose
dependency can't be linked statically gets no binary on that platform until the recipe
handles it (local build still possible).

Each plugin API version still in use gets built against the newest SC release that uses it.
Each job runs `scdeken build --recipe … --version … --out …` (the same code path as local
builds), archives the installed plugin folder, and uploads it to a GitHub Release named
`<package>-<version>`. A final job merges results into the index and signs it.

## 8. The `scdeken` helper (Go)

Language: **Go** (my call): a single static binary per OS, easy cross-compilation, and the
standard library covers HTTP, SHA-256, tar/zip, JSON and process execution. Dependencies kept
to roughly two: a TOML parser and a minisign verifier.

### 8.1 Commands (shape; builder finalizes)

```
scdeken search <query> [--include-unreviewed]        → JSON results
scdeken info <package>                               → JSON details, versions, install route
scdeken install <package>[@version] [--build] [--yes-unreviewed]
scdeken update [<package>]   scdeken remove <package>
scdeken list                                         → installed packages (JSON)
scdeken doctor [<package>]                           → toolchain report (JSON)
scdeken advisories check                             → matches against installed (JSON)
scdeken recipe new <url> | recipe check <file>       (maintainers, issue-form bot)
scdeken build --recipe <file> --version <v> --out <dir>   (CI + local)
scdeken index build | index refresh
```

sclang passes context the helper can't detect itself: SC version (`Main.version`), extension
dir (`Platform.userExtensionDir`), app-support dir, and the OSC reply port.

### 8.2 Talking to sclang

- **Quick queries** (search, info, list, doctor): synchronous, JSON on stdout.
- **Long operations** (install, build, update): launched asynchronously; the helper sends
  **OSC messages** to sclang (`NetAddr.langPort`) — progress, log lines, and a final done/error
  message. sclang already speaks OSC natively, so the GUI stays responsive without polling
  (anti-goal: frozen interpreter). OSC encoding is small enough to hand-write in Go. Large
  payloads go to a file whose path is sent over OSC.

### 8.3 Install routes

1. **Prebuilt** (reviewed package, binary for this platform): download → verify hash against
   signed index → unpack into `Platform.userExtensionDir/sc-deken/<package>/` → on macOS,
   remove any `com.apple.quarantine` attribute defensively.
2. **Local build** (reviewed recipe without a binary, or unreviewed + confirmed):
   `doctor` check → stop with the report if anything is missing → fetch SC headers (§8.4) →
   `git clone` the package at the tag's recorded commit (with submodules if the recipe says so)
   → `cmake` configure with `SC_PATH`, recipe flags and `CMAKE_INSTALL_PREFIX` → build Release →
   install into the same folder as route 1. All output streams to the Build log.
   On failure: offer a pre-filled GitHub issue link on the recipe repo (platform, SC version,
   last log lines).
3. **sclang-only** packages: delegate to the existing Quarks system
   (`Quarks.install(url, "<commit>")`, by recorded commit per §6) so they appear in `Quarks.installed` and
   `LanguageConfig` exactly as today (anti-goal: breaking Quarks).

**Already installed elsewhere** (my call): before installing, the helper scans
`Platform.userExtensionDir` and `Platform.systemExtensionDir` for the package's plugin files
(`*.scx` / `*.so` / `*.dll` names) and class names. A second copy would cause "duplicate class"
errors and double-loaded UGens. If it finds a copy it didn't install (a hand copy, a
plugins.quark build, the sc3-plugins installer), it shows where, and offers **replace**
(the old copy moves to a backup folder, never deleted), **keep existing** (record it as
"external" and stop), or cancel. "Installed" lists such external copies too, marked
unmanaged.

After any install that adds classes, the quark prompts to recompile the class library; after
plugin installs, it reminds the user to reboot the server.

Dependencies (`[depends]` / `.quark` dependencies) are resolved first and listed for
confirmation. Update = install the new version into a temporary folder, then swap; the old
folder is restored on failure. Remove deletes the folder (or calls `Quarks.uninstall`).

### 8.4 SC headers for local builds

Match the **installed** SC version (`Main.version`, e.g. `3.13.0`): shallow-clone tag
`Version-3.13.0` of `github.com/supercollider/supercollider` **with submodules**
(`--depth 1 --recurse-submodules --shallow-submodules`) into
`<app-support>/sc-deken/sc-headers/3.13.0/`, and reuse it on later builds. Submodules **are**
required. `external_libraries/nova-tt` and `nova-simd` are submodules, GitHub tag tarballs
leave them out, and the cookiecutter CMake module stops with an error without nova-tt. git is
needed for local builds anyway. If the release asset `SuperCollider-<ver>-Source.tar.bz2`
turns out to include submodules, the builder may download it instead. If the exact tag is missing
(dev builds), fall back to the nearest lower release tag and say so in the log.

### 8.5 Doctor

Checks, per OS, and reports `{name, found, version, needed_for, hint}` per item:

- **git**, **cmake** (with minimum version), a build driver (make/ninja, or MSBuild on Windows)
- **C++ compiler**: macOS — Xcode Command Line Tools (`xcode-select -p`); Windows — Visual
  Studio / Build Tools with the C++ workload (via `vswhere`); Linux — `c++`/`g++`/`clang++`
- **Recipe libraries** from `[requires]`: via `pkg-config` where available; otherwise
  reported as "cannot verify" with the install hint

Hints name the concrete command: `xcode-select --install`, `winget install Kitware.CMake`,
`winget install Microsoft.VisualStudio.2022.BuildTools` (plus workload), `sudo apt install …`,
`sudo pacman -S …`, `brew install …`. The owner listed this as a must-have; its messages should
be written for musicians, not build engineers.

### 8.6 Local state

`<app-support>/sc-deken/state.json`: per installed package — version, commit, install route
(prebuilt / local-build / quark), platform key, install path, timestamp. "Installed" and
"Updates" read this plus `Quarks.installed` (so quarks installed the old way show up too,
marked as such).

## 9. The Deken quark (sclang)

Entry points (names are my call): `Deken.gui`, plus a scriptable API mirroring the helper:
`Deken.search("pitch")`, `Deken.install(\portedplugins)`, `Deken.install(\portedplugins, "0.4.5")`,
`Deken.update`, `Deken.remove(\x)`, `Deken.doctor`.

GUI — **layout B (chosen by the owner)**, one Qt window:

- **Sidebar**: Browse · Installed (n) · Updates (n) · Build log · Settings
- **List**: search field; one row per package with **type chip · name · one-line summary ·
  status badge** (owner's requirement: get an idea of each package without clicking it). Long
  summaries are cut off with "…", with the full text in a tooltip. Matching text is highlighted
  where Qt allows it. Hits that come only from code show a small "matched UGen: FM7" /
  "matched class: …" note. The list gets most of the window width; the details pane stays
  narrow.
- **Details pane**: name, authors, license, summary, version picker (tags), install-route badge,
  contents (UGen/class counts), dependencies, Install / Update / Remove, link to repo
- **Status bar**: detected platform — OS, arch, plugin API, scsynth/supernova
- **Badges**: `reviewed · prebuilt` · `reviewed · build locally` · `⚠ unreviewed · build locally`
  · `community-listed` · `update available` · `⛔ retracted` (red)
- **Package type, always visible (owner's requirement).** Each list row starts with a colored
  type chip, the details pane repeats it at the top, and Browse has filter chips
  All · Plugins · Quarks · Plugin + Quark:
  - `PLUGIN`: compiled server plugin, plus the UGen class files and help every plugin ships.
  - `QUARK`: sclang only (classes, help, no compiled code).
  - `PLUGIN + QUARK`: a compiled plugin **plus** an sclang library beyond the UGen wrappers.
    Detected when the repo has a `.quark` file or the recipe says so (`kind = "plugin+quark"`).

  The details pane has a **"What's inside"** box: server plugins by name (scsynth/supernova),
  UGen class files, sclang library classes (or "none"). It also has an **"After installing"**
  line: "reboot server", "recompile class library", or both. Colors differ per type and the
  chip also carries the text, so it doesn't rely on color alone (my call).
- **Doctor dialog** before local builds, listing missing items with hints and a "check again"
  button. **Unreviewed warning dialog** naming repo + commit, requiring explicit confirmation.

Mockups: `.superpowers/brainstorm/*/content/layout-b-summary.html` (latest; layout B with type chips and summaries).

## 10. Testing and quality

- Go: unit tests per package (recipe parsing, tag mapping, platform key, index verification
  with a test key, search ranking, OSC encoding, doctor parsing of tool output with fixtures).
- CI for `sc-deken`: tests + `go vet` + `gofmt` check on Linux/macOS/Windows; an end-to-end job
  per OS that runs `scdeken doctor` and a real local build of one small plugin.
- `sc-deken-recipes` CI itself exercises the build path on every recipe PR.
- sclang: a few UnitTest cases for JSON/OSC handling; GUI checked manually.
- Code style: small Go packages with one job each (e.g. `recipe`, `index`, `platform`, `fetch`,
  `build`, `doctor`, `install`, `osc`, `discover`), short functions, clear names, doc comments on
  exported identifiers, errors wrapped with context. No framework, no code generation.

## 11. Calls I made (owner did not decide these)

- Working name **sc-deken**, helper **`scdeken`**, quark class **`Deken`**.
- Go for the helper; TOML recipes; minisign signing; two GitHub repos; GitHub Pages + Releases.
- Minimum supported SC version **3.13**.
- macOS universal binaries; Linux built on ubuntu-22.04.
- OSC for progress events from helper to sclang.
- sclang-only packages installed through the existing Quarks API.
- Untagged repos: reviewed recipes pin `version = commit`; unreviewed ones show as unstable.
- Remove/update/version picking included in v1.
- Platform failures of a first-release package don't block the release.
- Prebuilt archives self-contained (static deps via vcpkg); macOS arches built separately, then lipo.
- Official-directory quarks install without warning; topic-only quarks are unreviewed; quarks
  are installed by commit hash.
- Copies installed outside sc-deken: detect, then offer replace (with backup) / keep / cancel.
- Adding packages: issue form → bot-generated recipe PR → CI build → maintainer merge.
- Advisories: quarantine instead of delete; startup check on by default; two approvals once
  there are two or more maintainers; retracted release assets kept, but delisted.

## 12. Left to the builder

Exact recipe field names and index schema; Go package layout; CLI flag names; OSC address names;
GUI widget details within layout B; ranking tweaks; caching details; CI YAML structure; which
GitHub topic synonyms to include; error message wording (keep it friendly to musicians).
