// Opens Deken.gui against the signed test index, drives it, and saves
// screenshots of the window and every dialog to docs/screenshots.
//
//   SCDEKEN_SCRIPT=scripts/gui-screenshots.scd scripts/run-sclang-tests.sh
//
// The harness provides the throwaway profile and the fixtures. The list is
// first filled by a real search ("fm"), then with made-up rows so every type
// chip and status badge is on screen at once.
(
var fixtures = "SCDEKEN_FIXTURES".getenv;
var outDir = (thisProcess.nowExecutingPath.dirname.dirname +/+ "docs" +/+ "screenshots");
var shot = { |view, name|
	var path = outDir +/+ name ++ ".png";
	Image.fromWindow(view).write(path);
	("saved " ++ path).postln;
};
var row = { |name, kind, summary, trust, extra|
	var r = (name: name, kind: kind, summary: summary, trust: trust, latest: "1.0.0", retracted: false);
	extra.keysValuesDo { |k, v| r[k] = v };
	r
};
var plan = { |name, version, confirm, extra|
	var p = Dictionary.newFrom(["package", name, "version", version, "commit", "0123456789abcdef0123456789abcdef01234567",
		"confirm", confirm, "depends", [], "after_install", ["recompile"]]);
	extra.keysValuesDo { |k, v| p[k.asString] = v };
	p
};
var main = {
	var g, dep, tree, canned, doctorDialog;
	Deken.helperPath = fixtures +/+ if(File.exists(fixtures +/+ "scdeken.exe")) { "scdeken.exe" } { "scdeken" };
	Deken.indexURL = "file:///" ++ (fixtures +/+ "index.json").replace(92.asAscii.asString, "/");
	File.mkdir(outDir);

	g = DekenGui.new.front;
	g.window.bounds = Rect(40, 40, 1060, 660);
	4.wait;
	shot.value(g.window, "deken-start");

	// 1. a real search for "fm", first row selected
	g.search.string = "fm";
	g.search.keyUpAction.value;
	2.wait;
	g.tree.currentItem = g.tree.itemAt(0);
	3.wait;
	shot.value(g.window, "deken-gui");
	("rows after fm: " ++ g.rows.collect { |r| r[\name] }).postln;

	// 2. quarks filter (empty search)
	g.search.string = "";
	g.kindButtons[2].valueAction = 1;
	2.wait;
	g.tree.currentItem = g.tree.itemAt(0);
	3.wait;
	shot.value(g.window, "deken-quarks");
	("rows after quarks: " ++ g.rows.collect { |r| r[\name] }).postln;

	// 3. made-up rows: every chip and badge, with a long summary
	canned = [
		row.value("fmx6", "plugin", "Six-operator phase-modulation matrix UGen with selectable waveforms and oversampling", "reviewed", (route: "prebuilt")),
		row.value("nome6", "plugin", "Six-operator FM7-style UGen with Jacobi elliptic oscillators", "reviewed", (route: "local-build")),
		row.value("sc3-plugins", "plugin+quark", "Community plugins for SuperCollider", "reviewed", (route: "prebuilt", match: (field: "ugen", value: "FM7"))),
		row.value("Panola", "quark", "Concise notation language for writing patterns", "reviewed", (match: (field: "class", value: "PfmNote"), update: true, installed: "1.1.0")),
		row.value("SomeQuark", "quark", "Utilities for FM synthesis presets", "community", ()),
		row.value("fm-thing", "plugin", "Experimental FM feedback oscillators", "unreviewed", ()),
		row.value("oldfm", "plugin", "Withdrawn FM oscillator", "reviewed", (retracted: true)),
		row.value("busyfm", "plugin", "A package whose job is running", "reviewed", ())
	];
	g.search.string = "fm";
	g.perform(\prShowRows, canned);
	2.wait;
	g.perform(\prSetBusy, "busyfm", true, "Installing busyfm");
	3.wait;
	g.tree.currentItem = g.tree.itemAt(5);
	3.wait;
	shot.value(g.window, "deken-gui-rows");

	// 4. Build log and Settings
	Deken.logAction.value("Building FMX6 (cmake --build . --config Release)");
	Deken.logAction.value("[ 50%] Building CXX object FMX6.cpp.obj");
	Deken.logAction.value("[100%] Linking FMX6.scx");
	g.sidebar.valueAction = 3;
	2.wait;
	shot.value(g.window, "deken-log");
	g.sidebar.valueAction = 4;
	2.wait;
	shot.value(g.window, "deken-settings");
	g.search.string = "";
	g.kindButtons[0].valueAction = 1;
	g.sidebar.valueAction = 1;
	2.wait;
	shot.value(g.window, "deken-installed");
	// 5. real installs through the window (quarks are faked: no network)
	DekenQuarks.override_((install: { |url, commit| ("fake quark install " ++ url).postln }, uninstall: { }, checkout: { }));
	g.search.string = "";
	g.kindButtons[0].valueAction = 1;
	g.sidebar.valueAction = 0;
	g.search.string = "fmx";
	g.search.keyUpAction.value;
	2.wait;
	g.tree.currentItem = g.tree.itemAt(0);
	3.wait;
	g.perform(\prInstall, "FMX6", "1.0.0");
	0.2.wait;
	shot.value(g.window, "deken-working");
	6.wait;
	shot.value(g.window, "deken-installed-ok");
	shot.value(Window.allWindows.last, "deken-dialog-after-real");
	Window.allWindows.last.close;
	g.unreviewedBox.valueAction = true;
	g.search.string = "sketchy";
	g.search.keyUpAction.value;
	2.wait;
	g.tree.currentItem = g.tree.itemAt(0);
	3.wait;
	shot.value(g.window, "deken-unreviewed");
	g.perform(\prInstall, "Sketchy", nil);
	4.wait;
	shot.value(Window.allWindows.last, "deken-dialog-unreviewed");
	Window.allWindows.last.close;
	2.wait;
	shot.value(g.window, "deken-after-cancel");
	g.window.close;

	// 6. dialogs
	dep = plan.value("dep-lib", "0.2.0", ["duplicates", "unreviewed"],
		(duplicates: [Dictionary.newFrom(["path", "C:/Users/me/AppData/Local/SuperCollider/Extensions/DepLib", "matched", "name"])],
			quark: Dictionary.newFrom(["url", "https://github.com/example/dep-lib", "commit", "fedcba"])));
	tree = plan.value("fm-thing", "0.1.0", ["unreviewed", "retracted"],
		(depends: [dep], retraction: Dictionary.newFrom(["id", "SCD-2026-001", "severity", "vulnerable", "summary", "Reads files outside its folder"])));
	DekenDialogs.confirm(tree, Dictionary.newFrom(["fm-thing", "https://github.com/example/fm-thing"]), { |c| ("confirm answer: " ++ c).postln }).front;
	1.5.wait;
	shot.value(Window.allWindows.last, "deken-dialog-confirm");
	Window.allWindows.last.close;
	DekenDialogs.confirm(plan.value("x", "1", ["duplicates"], (duplicates: [Dictionary.newFrom(["path", "C:/old/X", "matched", "name"])])), Dictionary.new, { |c| ("answer: " ++ c).postln });
	1.5.wait;
	shot.value(Window.allWindows.last, "deken-dialog-duplicates");
	Window.allWindows.last.close;

	doctorDialog = DekenDialogs.doctor(
		Dictionary.newFrom(["ok", false, "items", [
			Dictionary.newFrom(["name", "cmake", "label", "CMake (build tool)", "found", false, "verified", true, "needed_for", "building plugins", "hint", "winget install Kitware.CMake"]),
			Dictionary.newFrom(["name", "compiler", "label", "C++ compiler", "found", false, "verified", false, "needed_for", "building plugins", "hint", "winget install Microsoft.VisualStudio.2022.BuildTools"]),
			Dictionary.newFrom(["name", "git", "label", "Git", "found", true, "verified", true, "needed_for", "building plugins"])]]),
		{ |d| "check again".postln }, { "continue".postln });
	1.5.wait;
	shot.value(doctorDialog[\window], "deken-dialog-doctor");
	doctorDialog[\window].close;

	DekenDialogs.afterInstall(["recompile", "reboot-server"]);
	1.5.wait;
	shot.value(Window.allWindows.last, "deken-dialog-after");
	Window.allWindows.last.close;
	DekenDialogs.ask("Delete quarantined packages", "Permanently delete the packages that were put in quarantine? This cannot be undone.", "Delete them", {});
	1.5.wait;
	shot.value(Window.allWindows.last, "deken-dialog-ask");
	Window.allWindows.last.close;
};
SystemClock.sched("SCDEKEN_WATCHDOG".getenv.asFloat, { "RESULT: watchdog timeout".postln; 1.exit });
fork({
	try { main.value; "RESULT: screenshots done".postln; 0.exit } { |err| err.reportError; "RESULT: screenshots failed".postln; 1.exit }
}, AppClock);
)
