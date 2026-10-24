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

	test_insideLinesForAQuark {
		var pkg = Dictionary.newFrom(["classes", ["Panola", "PanolaPlayer"]]);
		this.assertEquals(DekenDetailsView.insideLines(pkg), ["sclang library: 2 classes"]);
	}

	test_versionsAndNeeds {
		var pkg = Dictionary.newFrom(["versions", [
			Dictionary.newFrom(["version", "0.3.0"]),
			Dictionary.newFrom(["version", "0.3.0-rc1", "unstable", true]),
			Dictionary.newFrom(["version", "0.2.0"])],
			"depends", Dictionary.newFrom(["zlib", "*", "alpha", "*"])]);
		this.assertEquals(DekenDetailsView.versionLabels(pkg), ["0.3.0 (latest)", "0.3.0-rc1 (unstable)", "0.2.0"]);
		this.assertEquals(DekenDetailsView.needsText(pkg), "Needs: alpha, zlib");
		this.assertEquals(DekenDetailsView.needsText(Dictionary.new), "");
	}
}
