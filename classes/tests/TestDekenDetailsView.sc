TestDekenDetailsView : UnitTest {
	test_pluginLinesGroupEngines {
		this.assertEquals(DekenDetailsView.pluginLines(["FMX6.scx", "FMX6_supernova.scx"]), ["Server plugin: FMX6 (scsynth + supernova)"]);
		this.assertEquals(DekenDetailsView.pluginLines(["A.scx", "B_supernova.scx"]),
			["Server plugin: A (scsynth)", "Server plugin: B (supernova)"]);
		this.assertEquals(DekenDetailsView.pluginLines([]), []);
	}

	test_insideLinesForAPlugin {
		var pkg = Dictionary.newFrom(["plugin_files", ["FMX6.scx"], "ugens", ["FMX6"], "classes", ["FMX6"]]);
		this.assertEquals(DekenDetailsView.insideLines(pkg),
			["Server plugin: FMX6 (scsynth)", "UGen classes: 1", "sclang library: none"], "UGen wrappers are not library classes");
	}

	// sc3-plugins has 171 server plugins: listing them all made the window taller than the screen.
	test_insideLinesShortenALongPluginList {
		var files = (1..171).collect { |i| "P%.scx".format(i) };
		var lines = DekenDetailsView.insideLines(Dictionary.newFrom(["plugin_files", files]));
		this.assertEquals(lines.size, 7, "5 plugins, the rest as a count, the library line");
		this.assertEquals(lines[4], "Server plugin: P5 (scsynth)");
		this.assertEquals(lines[5], "… and 166 more server plugins");
		this.assertEquals(DekenDetailsView.insideLines(Dictionary.newFrom(["plugin_files", files.keep(6)])).size, 7,
			"six are all listed: \"and 1 more\" saves nothing");
	}

	// Whatever the package lists, the pane never asks for more height than a screen has.
	test_paneHeightStaysBounded {
		var files = (1..171).collect { |i| "P%.scx".format(i) };
		var depends = Dictionary.new, height, done = Condition.new;
		60.do { |i| depends["Dep" ++ i] = "*" };
		{
			var pane = DekenDetailsView((install: {}, update: {}, remove: {}, versionChanged: {}));
			pane.show((name: "sc3-plugins", kind: "plugin", summary: "Community plugins", trust: "reviewed"));
			pane.fill(Dictionary.newFrom(["package", Dictionary.newFrom(["plugin_files", files, "depends", depends,
				"summary", "Community plugins", "versions", [Dictionary.newFrom(["version", "3.13.0"])]])]));
			height = pane.view.minSizeHint.height;
			pane.view.remove;
			done.test = true;
			done.signal;
		}.defer;
		done.wait;
		this.assert(height < 500, "pane asks for % pixels".format(height));
	}

	test_insideLinesForAQuark {
		var pkg = Dictionary.newFrom(["classes", ["Panola", "PanolaPlayer"]]);
		this.assertEquals(DekenDetailsView.insideLines(pkg), ["sclang library: 2 classes"]);
		this.assertEquals(DekenDetailsView.insideLines(Dictionary.newFrom(["classes", ["Panola"]])), ["sclang library: 1 class"]);
	}

	test_bylineLeavesOutWhatIsMissing {
		this.assertEquals(DekenDetailsView.bylineText(Dictionary.newFrom(["authors", ["a", "b"], "license", "MIT"])), "a, b · MIT");
		this.assertEquals(DekenDetailsView.bylineText(Dictionary.newFrom(["license", "MIT"])), "MIT");
		this.assertEquals(DekenDetailsView.bylineText(Dictionary.newFrom(["authors", ["a"]])), "a");
		this.assertEquals(DekenDetailsView.bylineText(Dictionary.new), "");
	}

	test_versionsAndNeeds {
		var pkg = Dictionary.newFrom(["versions", [
			Dictionary.newFrom(["version", "0.3.0"]),
			Dictionary.newFrom(["version", "0.3.0-rc1", "unstable", true]),
			Dictionary.newFrom(["version", "0.2.0"])],
			"depends", Dictionary.newFrom(["zlib", "*", "alpha", "*"])]);
		this.assertEquals(DekenDetailsView.versionLabels(pkg), ["0.3.0 (latest)", "0.3.0-rc1 (unstable)", "0.2.0"]);
		this.assertEquals(DekenDetailsView.needsText(pkg, nil, nil), "Needs:
  alpha - not in the package list
  zlib - not in the package list");
		this.assertEquals(DekenDetailsView.needsText(Dictionary.new, nil, nil), "");
	}

	test_needsStates {
		var pkg = Dictionary.newFrom(["depends", Dictionary.newFrom(["Beta", ">=1.0,<2.0", "alpha", "*", "Ghost", "*"]),
			"depends_unresolved", Dictionary.newFrom(["https://example.org/x/Extra.git", "=2.0"])]);
		var installed = Dictionary.newFrom(["alpha", Dictionary.new]);
		var catalog = Dictionary.newFrom(["beta", Dictionary.new, "alpha", Dictionary.new]);
		this.assertEquals(DekenDetailsView.needsLines(pkg, installed, catalog), [
			"  alpha - installed ✓",
			"  Beta (>=1.0,<2.0) - will be installed",
			"  Ghost - not in the package list",
			"  Extra (=2.0) - unreviewed, from https://example.org/x/Extra.git"]);
	}
}
