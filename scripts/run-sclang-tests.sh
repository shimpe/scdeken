#!/usr/bin/env bash
# Run the Deken sclang UnitTests in a sandboxed sclang, without touching the
# owner's real SuperCollider profile.
#
#   scripts/run-sclang-tests.sh                      # all Test* classes in classes/tests
#   scripts/run-sclang-tests.sh TestDekenJSON ...    # only these classes
#
# Environment:
#   SCDEKEN_SC_HOME   SuperCollider install dir (default: Windows 3.14.1 path)
#   SCDEKEN_PROFILE   throwaway profile dir (default: a fresh temp dir, removed
#                     afterwards). Platform.userAppSupportDir, userExtensionDir
#                     and userConfigDir are redirected into it.
#   SCDEKEN_TIMEOUT   seconds before sclang is killed (default 300; TestDekenScTimeline clones from GitHub)
#   SCDEKEN_SCRIPT    run this .scd instead of the test runner (scripts/gui-screenshots.scd);
#                     it gets the same sandbox, fixtures and SCDEKEN_* variables
#   (SCDEKEN_FIXTURES is set for the tests: built helper, signed test index, locks)
#
# Exit code: 0 all tests passed, 1 failures, 124 timeout, other = sclang error.
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sc_home="${SCDEKEN_SC_HOME:-/c/Program Files/SuperCollider-3.14.1}"
sclang="$sc_home/sclang.exe"
[ -x "$sclang" ] || sclang="$(command -v sclang)"

if [ -n "${SCDEKEN_PROFILE:-}" ]; then
	profile="$SCDEKEN_PROFILE"; mkdir -p "$profile"
else
	profile="$(mktemp -d)"; trap 'rm -rf "$profile"' EXIT
fi
# Forward-slash native paths: sclang and YAML both accept them on Windows.
native() { if command -v cygpath >/dev/null; then cygpath -m "$1"; else echo "$1"; fi; }

mkdir -p "$profile/harness"
# Redirect the platform dirs. LOCALAPPDATA is NOT honoured on Windows (sclang
# asks the OS for the known folder), so override the primitives instead. The
# file is generated here because .sc files may live only under classes/.
cat > "$profile/harness/DekenTestProfile.sc" <<SC
+ Platform {
	userAppSupportDir { ^"SCDEKEN_PROFILE".getenv }
	userExtensionDir { ^"SCDEKEN_PROFILE".getenv +/+ "Extensions" }
	userConfigDir { ^"SCDEKEN_PROFILE".getenv }
}
SC
cat > "$profile/run.scd" <<'SC'
// Runs the named UnitTest classes (script arguments) or every Test* class
// defined under classes/tests, then exits 0 (pass) or 1 (failures).
fork {
	var names = thisProcess.argv;
	try {
		var classes = names.collect { |n| n.asSymbol.asClass ?? { "unknown test class %".format(n).error; 1.exit } };
		UnitTest.reset;
		classes.do { |c| c.run(false, false); 0.1.wait };
		UnitTest.report;
		"RESULT: % passed, % failed".format(UnitTest.passes.size, UnitTest.failures.size).postln;
		(UnitTest.failures.size > 0 or: { UnitTest.passes.size == 0 }).if { 1.exit } { 0.exit };
	} { |err| err.reportError; "RESULT: test run aborted by an error".postln; 1.exit };
};
// An error inside a test can kill the routine silently: never hang.
SystemClock.sched("SCDEKEN_WATCHDOG".getenv.asFloat, { "RESULT: watchdog timeout".postln; 1.exit });
SC
cat > "$profile/sclang_conf.yaml" <<YAML
includePaths:
    - $(native "$sc_home/SCClassLibrary")
    - $(native "$repo/classes")
    - $(native "$profile/harness")
excludeDefaultPaths: true
postInlineWarnings: false
YAML

if [ "$#" -gt 0 ]; then tests=("$@")
else mapfile -t tests < <(grep -rhoE '^Test[A-Za-z0-9_]* *:' "$repo/classes/tests" | tr -d ': ')
fi

# Helper, signed test index and bootstrap locks for the helper tests (task 28+).
bash "$repo/scripts/make-sclang-fixtures.sh" "$profile/fixtures" "$profile" >&2
export SCDEKEN_FIXTURES="$(native "$profile/fixtures")"
export SCDEKEN_PROFILE="$(native "$profile")"
export SCDEKEN_WATCHDOG=$(( ${SCDEKEN_TIMEOUT:-300} - 10 ))
export LOCALAPPDATA="$(command -v cygpath >/dev/null && cygpath -w "$profile" || echo "$profile")" # belt and braces
set +e
timeout "${SCDEKEN_TIMEOUT:-300}" "$sclang" -l "$(native "$profile/sclang_conf.yaml")" \
	"$(native "${SCDEKEN_SCRIPT:-$profile/run.scd}")" "${tests[@]}" </dev/null 2>&1 | tee "$profile/last-run.log"
rc=${PIPESTATUS[0]}
exit "$rc"
