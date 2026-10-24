TestDekenRows : UnitTest {
	test_unknownTrustIsNeverReviewed {
		var row = DekenRows.fromInstalled(Dictionary.newFrom(["name", "classes", "kind", "quark", "version", ""]), nil);
		this.assertEquals(DekenRows.status(row, false), "installed");
		this.assertEquals(DekenRows.status((trust: nil), false), "installed");
		this.assertEquals(DekenRows.status((trust: ""), false), "installed");
		this.assertEquals(DekenRows.status((trust: "reviewed"), false), "reviewed");
	}

	test_statusOrder {
		this.assertEquals(DekenRows.status((trust: nil, update: true), false), "update available");
		this.assertEquals(DekenRows.status((trust: nil, retracted: true, update: true), false), "⛔ retracted");
		this.assertEquals(DekenRows.status((trust: "reviewed", route: "prebuilt"), true), "working…");
	}

	test_installedFillsFromTheList {
		var known = Dictionary.newFrom(["summary", "Patterns", "kind", "quark", "trust", "reviewed"]);
		var row = DekenRows.fromInstalled(Dictionary.newFrom(["name", "Panola", "version", "1.0", "update_available", true]), known);
		this.assertEquals([row[\summary], row[\kind], row[\trust], row[\installed], row[\update]], ["Patterns", "quark", "reviewed", "1.0", true]);
		this.assertEquals(DekenRows.fromInstalled(Dictionary.newFrom(["name", "X", "trust", "community"]), known)[\trust], "community", "the item's own trust wins");
	}

	test_hitKnowsWhatIsInstalled {
		var hit = Dictionary.newFrom(["name", "A", "kind", "plugin", "summary", "s", "trust", "reviewed", "latest", "2", "retracted", false]);
		var have = Dictionary.newFrom(["version", "1", "update_available", true]);
		this.assertEquals(DekenRows.fromHit(hit, have)[\installed], "1");
		this.assertEquals(DekenRows.fromHit(hit, have)[\update], true);
		this.assertEquals(DekenRows.fromHit(hit, nil)[\installed], nil);
		this.assertEquals(DekenRows.fromHit(hit, nil)[\update], false);
	}

	test_summaryCellEndsWithTheMatch {
		var hit = (summary: "Community plugins", match: Dictionary.newFrom(["field", "ugen", "value", "FM7"]));
		this.assertEquals(DekenRows.summaryCell(hit), "Community plugins — matched UGen: FM7");
		this.assertEquals(DekenRows.summaryCell((summary: "Plain", match: Dictionary.newFrom(["field", "name", "value", "x"]))), "Plain");
		this.assertEquals(DekenRows.summaryCell((summary: nil)), "");
	}

	test_installedRowsFilter {
		var installed = Dictionary.new, catalog = Dictionary.new;
		installed["b"] = Dictionary.newFrom(["name", "B", "kind", "quark", "update_available", true]);
		installed["a"] = Dictionary.newFrom(["name", "A", "kind", "plugin", "update_available", false]);
		catalog["a"] = Dictionary.newFrom(["summary", "FM thing"]);
		this.assertEquals(DekenRows.installedRows(installed, catalog, false, nil, "").collect { |r| r[\name] }, ["A", "B"]);
		this.assertEquals(DekenRows.installedRows(installed, catalog, true, nil, "").collect { |r| r[\name] }, ["B"]);
		this.assertEquals(DekenRows.installedRows(installed, catalog, false, "plugin", "").collect { |r| r[\name] }, ["A"]);
		this.assertEquals(DekenRows.installedRows(installed, catalog, false, nil, "fm").collect { |r| r[\name] }, ["A"]);
	}
}
