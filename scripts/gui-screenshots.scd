// Opens Deken.gui against the signed documentation index (about two dozen real
// packages, see scripts/make-doc-index.sh), drives it, and saves the
// screenshots used in README.md and docs/ to docs/screenshots/.
//
//   SCDEKEN_SCRIPT=<absolute path of this file> scripts/run-sclang-tests.sh
//
// The harness provides the throwaway profile (so no personal folders show up),
// the helper and the signed index. Everything the screenshots show is real
// except the advisory dialogs and the "Removed" dialog, which are shown with
// made-up findings, and the downloads, which are one placeholder file.
// Needs the network once: Panola and Scparco are installed as real quarks.
//
// Afterwards run scripts/optimize-screenshots.sh to shrink the PNGs.
(
var fixtures = "SCDEKEN_FIXTURES".getenv;
var outDir = thisProcess.nowExecutingPath.dirname.dirname +/+ "docs" +/+ "screenshots";
var shot = { |view, name|
	var path = outDir +/+ name ++ ".png";
	Image.fromWindow(view).write(path);
	("saved " ++ path).postln;
};
// A dialog is made smaller than its default, so the picture has no empty space.
var shotDialog = { |w, name, height|
	if(height.notNil) { w.bounds = w.bounds.height_(height) };
	0.8.wait;
	shot.value(w, name);
};
// Types text into the search box and waits for the answer.
var search = { |g, text|
	g.search.string = text;
	g.search.keyUpAction.value;
	2.wait;
};
// Selects the row with this name and waits for the details pane to fill.
var select = { |g, name|
	var i = g.rows.detectIndex { |r| r[\name] == name };
	g.tree.currentItem = g.tree.itemAt(i);
	3.wait;
};
var chip = { |g, i| g.kindButtons[i].valueAction = 1; 2.wait };
var tryInstall = { |name, version|
	var done = false, result;
	4.do { |attempt|
		if(done.not) {
			var box = [nil];
			Deken.install(name, version, { |r| box[0] = r });
			while { box[0].isNil } { 0.25.wait };
			result = box[0];
			done = result.ok;
			if(done.not) { ("install " ++ name ++ " attempt " ++ (attempt + 1) ++ " failed: " ++ result[\error]).postln };
		}
	};
	done
};
var main = {
	var g, path, moves, hit, logObj, plan, waited;
	Deken.helperPath = fixtures +/+ if(File.exists(fixtures +/+ "scdeken.exe")) { "scdeken.exe" } { "scdeken" };
	Deken.indexURL = "file:///" ++ (fixtures +/+ "docs/index.json").replace(92.asAscii.asString, "/");
	File.mkdir(outDir);

	// The harness lists its own folders as include paths: they would show up as installed quarks.
	LanguageConfig.includePaths.copy.do { |p| LanguageConfig.removeIncludePath(p) };

	// What is already installed on this "computer": three plugins, one of them an
	// older version (so there is an update), and two quarks (not Panola, so that
	// Mitola can show that it brings Panola along).
	tryInstall.value("portedplugins", "0.4.1");
	tryInstall.value("f0plugins", nil);
	tryInstall.value("XPlayBuf", nil);
	tryInstall.value("Scparco", nil);
	tryInstall.value("Phijack", nil);

	g = DekenGui.new.front;
	g.window.bounds = Rect(40, 40, 1320, 660);
	6.wait;

	// 1. Browse: the whole list, a prebuilt plugin selected
	select.value(g, "vb_UGens");
	shot.value(g.window, "01-browse");

	// 2. Search: "fm" finds packages by name, summary and UGen name
	search.value(g, "fm");
	select.value(g, "sc3-plugins");
	shot.value(g.window, "02-search");

	// 3. A plugin that has no prebuilt download: built on this computer
	search.value(g, "nome6");
	select.value(g, "nome6");
	shot.value(g.window, "03-local-build");

	// 4. Quarks only, with one that needs another quark
	search.value(g, "");
	chip.value(g, 2);
	select.value(g, "Mitola");
	shot.value(g.window, "04-quarks");
	chip.value(g, 0);

	// 5. Unreviewed packages are hidden until asked for
	g.unreviewedBox.valueAction = true;
	search.value(g, "dronepad");
	select.value(g, "dronepad-ugens");
	shot.value(g.window, "05-unreviewed");

	// 6. A withdrawn package
	search.value(g, "SampleHarvest");
	select.value(g, "SampleHarvest");
	shot.value(g.window, "06-withdrawn");
	g.unreviewedBox.valueAction = false;
	search.value(g, "");

	// 7. Installed, with an update waiting
	g.sidebar.valueAction = 1;
	3.wait;
	select.value(g, "portedplugins");
	shot.value(g.window, "07-installed");

	// 8. Updates
	g.sidebar.valueAction = 2;
	3.wait;
	select.value(g, "portedplugins");
	shot.value(g.window, "08-updates");
	g.sidebar.valueAction = 0;
	2.wait;

	// 9. An install in progress, then the dialog that follows it
	search.value(g, "vb_UGens");
	select.value(g, "vb_UGens");
	g.perform(\prInstall, "vb_UGens", nil);
	0.2.wait;
	shot.value(g.window, "09-working");
	// The install may take a while (virus scanners); wait for the "Installed" window.
	waited = 0;
	while { (Window.allWindows.last.name != "Installed") and: { waited < 90 } } { 0.5.wait; waited = waited + 0.5 };
	2.wait;
	shotDialog.value(Window.allWindows.last, "dialog-installed", 150);
	Window.allWindows.last.close;
	2.wait;
	shot.value(g.window, "10-installed-now");

	// 10. Removing
	select.value(g, "vb_UGens");
	g.perform(\prAskRemove, "vb_UGens");
	1.5.wait;
	shotDialog.value(Window.allWindows.last, "dialog-remove", 130);
	Window.allWindows.last.close;
	2.wait;

	// 11. Unreviewed install asks first (real answer from the helper)
	g.unreviewedBox.valueAction = true;
	search.value(g, "dronepad");
	select.value(g, "dronepad-ugens");
	g.perform(\prInstall, "dronepad-ugens", nil);
	4.wait;
	shotDialog.value(Window.allWindows.last, "dialog-confirm", 215);
	Window.allWindows.last.close;
	2.wait;
	g.unreviewedBox.valueAction = false;

	// 12. A missing tool: pretend cmake is not installed (this process only)
	path = "PATH".getenv;
	"PATH".setenv(path.split($;).reject { |p| p.toLower.find("cmake").notNil }.join(";"));
	search.value(g, "nome6");
	select.value(g, "nome6");
	g.perform(\prInstall, "nome6", nil);
	3.wait;
	shotDialog.value(Window.allWindows.last, "dialog-doctor", 230);
	Window.allWindows.last.close;
	"PATH".setenv(path);
	2.wait;

	// 13. Build log (shown with the output of a typical local build: the real log of this
	// run would show the temporary folder of the throwaway profile) and Settings
	logObj = g.instVarAt(g.class.instVarNames.indexOf(\log));
	logObj.instVarAt(logObj.class.instVarNames.indexOf(\lines)).clear;
	[
		"== Installing nome6 (14:02:11) ==",
		"Fetching SuperCollider 3.14.1 headers (once)",
		"Fetching https://github.com/shimpe/nome6 at fc3a5051d470",
		"-- Building for: Visual Studio 17 2022",
		"-- The CXX compiler identification is MSVC 19.44.35217.0",
		"-- Configuring done",
		"-- Generating done",
		"  Nome6.vcxproj -> build/Release/Nome6.scx",
		"-- Installing: Extensions/sc-deken/nome6/Nome6/plugins/Nome6.scx",
		"-- Installing: Extensions/sc-deken/nome6/Nome6/Classes/Nome6.sc",
		"Installed nome6 0.1.0"
	].do { |line| logObj.add(line) };
	g.sidebar.valueAction = 3;
	2.wait;
	shot.value(g.window, "11-build-log");
	g.sidebar.valueAction = 4;
	2.wait;
	shot.value(g.window, "12-settings");
	g.window.close;

	// 13b. Two more questions the helper can ask (made-up plans)
	plan = { |name, version, confirm, extra|
		var p = Dictionary.newFrom(["package", name, "version", version, "commit", "0123456789abcdef0123456789abcdef01234567",
			"confirm", confirm, "depends", [], "after_install", ["recompile", "reboot-server"]]);
		extra.keysValuesDo { |k, v| p[k.asString] = v };
		p
	};
	DekenDialogs.confirm(plan.value("XPlayBuf", "0.1.3", ["duplicates"],
		(duplicates: [Dictionary.newFrom(["path", "C:/Users/musician/AppData/Local/SuperCollider/Extensions/XPlayBuf", "matched", "plugin file"])])), {});
	1.5.wait;
	shotDialog.value(Window.allWindows.last, "dialog-duplicates", 215);
	Window.allWindows.last.close;
	DekenDialogs.confirm(plan.value("XPlayBuf", "0.1.3", ["retracted"],
		(retraction: Dictionary.newFrom(["id", "SCD-2026-0004", "severity", "vulnerable", "summary", "Reads past the end of a buffer when the loop length is zero, which can crash the server."]))), {});
	1.5.wait;
	shotDialog.value(Window.allWindows.last, "dialog-withdrawn-install", 240);
	Window.allWindows.last.close;

	// 14. Withdrawn packages found on this computer (made-up findings)
	hit = { |severity, summary, fixed, name, version, kind, path|
		var h = Dictionary.newFrom(["advisory", Dictionary.newFrom(["id", "SCD-2026-0003", "severity", severity, "summary", summary,
			"details", "https://github.com/shimpe/scdeken-recipes/issues/12"]),
			"item", Dictionary.newFrom(["name", name, "version", version, "commit", "", "kind", kind, "path", path, "source", "quark"])]);
		if(fixed.notNil) { h["fixed_version"] = fixed };
		h
	};
	DekenAdvisoryDialog.show(hit.value("malware", "Version 1.0.0 uploads the contents of your Documents folder when the class library compiles.",
		nil, "SampleHarvest", "1.0.0", "quark", "C:/Users/musician/AppData/Local/SuperCollider/downloaded-quarks/SampleHarvest"), {});
	1.5.wait;
	shotDialog.value(Window.allWindows.last, "advisory-malware", 345);
	Window.allWindows.last.close;
	DekenAdvisoryDialog.show(hit.value("vulnerable", "Reads past the end of a buffer when the loop length is zero, which can crash the server.",
		"0.1.4", "XPlayBuf", "0.1.3", "plugin", "C:/Users/musician/AppData/Local/SuperCollider/Extensions/sc-deken/XPlayBuf"), {});
	1.5.wait;
	shotDialog.value(Window.allWindows.last, "advisory-vulnerable", 345);
	Window.allWindows.last.close;
	DekenDialogs.ask("Keep it anyway?", DekenAdvisoryDialog.keepAnywayText, "Keep it", {});
	1.5.wait;
	shotDialog.value(Window.allWindows.last, "advisory-keep", 130);
	Window.allWindows.last.close;
	moves = ["SampleHarvest moved to C:/Users/musician/AppData/Local/SuperCollider/sc-deken/quarantine/SCD-2026-0003"];
	DekenDialogs.afterRemoval(moves, ["recompile"]);
	1.5.wait;
	shotDialog.value(Window.allWindows.last, "dialog-removed", 230);
	Window.allWindows.last.close;
};
SystemClock.sched("SCDEKEN_WATCHDOG".getenv.asFloat, { "RESULT: watchdog timeout".postln; 1.exit });
fork({
	try { main.value; "RESULT: screenshots done".postln; 0.exit } { |err| err.reportError; "RESULT: screenshots failed".postln; 1.exit }
}, AppClock);
)
