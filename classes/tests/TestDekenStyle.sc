TestDekenStyle : UnitTest {
	test_typeChip {
		this.assertEquals(DekenStyle.typeText("plugin"), "PLUGIN");
		this.assertEquals(DekenStyle.typeText("quark"), "QUARK");
		this.assertEquals(DekenStyle.typeText("plugin+quark"), "PLUGIN + QUARK");
		this.assertEquals(DekenStyle.typeText(\quark), "QUARK", "symbols work too");
		this.assertEquals(DekenStyle.typeText(nil), "", "unknown kind is blank, never NIL");
	}

	test_typeColors {
		this.assertEquals(DekenStyle.typeColor("plugin"), Color.fromHexString("#6a3fb5"));
		this.assertEquals(DekenStyle.typeColor("quark"), Color.fromHexString("#0f7f86"));
		this.assertEquals(DekenStyle.typeColor("plugin+quark"), Color.fromHexString("#4d5f9e"));
	}

	test_badge {
		this.assertEquals(DekenStyle.badgeText("reviewed", "prebuilt"), "reviewed · prebuilt");
		this.assertEquals(DekenStyle.badgeText("reviewed", "local-build"), "reviewed · build locally");
		this.assertEquals(DekenStyle.badgeText("unreviewed", "local-build"), "⚠ unreviewed · build locally");
		this.assertEquals(DekenStyle.badgeText("unreviewed", "quark"), "⚠ unreviewed");
		this.assertEquals(DekenStyle.badgeText("community", "local-build"), "community-listed");
		this.assertEquals(DekenStyle.badgeText("community", "prebuilt"), "community-listed");
		this.assertEquals(DekenStyle.badgeText("reviewed", "quark"), "reviewed");
		this.assertEquals(DekenStyle.badgeText("reviewed", nil), "reviewed", "route not known yet");
	}

	test_unknownTrustIsInstalledNotReviewed {
		this.assertEquals(DekenStyle.badgeText(nil, nil), "installed");
		this.assertEquals(DekenStyle.badgeText("", "prebuilt"), "installed");
		this.assertEquals(DekenStyle.badgeText("surprise", nil), "installed");
		this.assertEquals(DekenStyle.badgeColor("installed"), Color.fromHexString("#eeeeee"));
	}

	test_badgeStates {
		this.assertEquals(DekenStyle.badgeText("reviewed", "prebuilt", true), "⛔ retracted", "retracted wins");
		this.assertEquals(DekenStyle.badgeText("reviewed", "prebuilt", false, true), "update available");
		this.assertEquals(DekenStyle.badgeText("unreviewed", nil, true, true), "⛔ retracted");
	}

	test_badgeColors {
		this.assertEquals(DekenStyle.badgeColor("reviewed · prebuilt"), Color.fromHexString("#d8f2dc"));
		this.assertEquals(DekenStyle.badgeColor("reviewed · build locally"), Color.fromHexString("#fff1c9"));
		this.assertEquals(DekenStyle.badgeColor("⚠ unreviewed · build locally"), Color.fromHexString("#ffd9d6"));
		this.assertEquals(DekenStyle.badgeColor("community-listed"), Color.fromHexString("#eeeeee"));
		this.assertEquals(DekenStyle.badgeColor("update available"), Color.fromHexString("#dbe7ff"));
		this.assert(DekenStyle.badgeColor("⛔ retracted") != DekenStyle.badgeColor("community-listed"), "retracted stands out");
		this.assertEquals(DekenStyle.badgeColor("working…"), DekenStyle.badgeColor("community-listed"));
	}

	test_matchedText {
		this.assertEquals(DekenStyle.matchedText((field: "ugen", value: "FM7")), "matched UGen: FM7");
		this.assertEquals(DekenStyle.matchedText(Dictionary.newFrom(["field", "class", "value", "PfmNote"])), "matched class: PfmNote");
		this.assertEquals(DekenStyle.matchedText(Dictionary.newFrom(["field", "name", "value", "fmx6"])), "");
		this.assertEquals(DekenStyle.matchedText(Dictionary.newFrom(["field", "summary", "value", "fm"])), "");
		this.assertEquals(DekenStyle.matchedText(nil), "");
	}

	test_statusBar {
		var known = Dictionary.newFrom(["os", "macos", "arch", "arm64", "api", 3, "api_known", true, "sc_version", "3.14.1"]);
		var unknown = Dictionary.newFrom(["os", "linux", "arch", "x86_64", "api", 0, "api_known", false, "sc_version", "3.15.0-dev"]);
		this.assertEquals(DekenStyle.statusBarText(known, "scsynth+supernova"), "macOS arm64 · plugin API 3 · scsynth+supernova");
		this.assertEquals(DekenStyle.statusBarText(unknown, "scsynth"),
			"plugin API unknown for 3.15.0-dev — packages will be built on this computer");
		this.assertEquals(DekenStyle.statusBarText(nil, "scsynth"), "checking this computer…");
	}

	test_afterInstall {
		this.assertEquals(DekenStyle.afterInstallText(["recompile", "reboot-server"]), "Recompile class library · reboot server");
		this.assertEquals(DekenStyle.afterInstallText(["recompile"]), "Recompile class library");
		this.assertEquals(DekenStyle.afterInstallText([]), "nothing to do");
	}

	test_percentEncodeNonASCII {
		// é is the two bytes C3 A9 in UTF-8; sclang's Char:ascii is signed.
		this.assertEquals(DekenStyle.percentEncode("café ünd"), "caf%C3%A9%20%C3%BCnd");
		this.assertEquals(DekenStyle.percentEncode("a-b_c.d~e"), "a-b_c.d~e");
	}

	test_buildFailureIssue {
		var url = DekenStyle.buildFailureIssueURL("fmx6", "0.1.0", "windows-x86_64-api3", "3.14.1", ["cmake failed", "error: no FFTW"]);
		var title = "title=" ++ DekenStyle.percentEncode("Build failed: fmx6 0.1.0 on windows-x86_64-api3");
		this.assert(url.beginsWith("https://github.com/shimpe/scdeken-recipes/issues/new?"), "goes to the recipes repo: " ++ url);
		this.assert(url.find(title).notNil, "has the title: " ++ url);
		this.assert(url.find(DekenStyle.percentEncode("windows-x86_64-api3")).notNil, "names the platform");
		this.assert(url.find(DekenStyle.percentEncode("3.14.1")).notNil, "names the SuperCollider version");
		this.assert(url.find(DekenStyle.percentEncode("error: no FFTW")).notNil, "has the log lines");
	}

	test_buildFailureIssueStaysShort {
		var lines = (1..30).collect { |i| "line % ".format(i) ++ String.fill(400, $x) };
		var url = DekenStyle.buildFailureIssueURL("fmx6", "0.1.0", "linux-x86_64-api3", "3.14.1", lines);
		this.assert(url.size <= 8000, "under 8 KB: " ++ url.size);
		this.assert(url.find(DekenStyle.percentEncode("line 30 ")).notNil, "keeps the last lines");
		this.assert(url.find(DekenStyle.percentEncode("line 1 ")).isNil, "drops the first ones");
	}

	test_links {
		this.assertEquals(DekenStyle.percentEncode("https://github.com/a/b"), "https%3A%2F%2Fgithub.com%2Fa%2Fb");
		this.assertEquals(DekenStyle.suggestURL("https://github.com/a/b"),
			"https://github.com/shimpe/scdeken-recipes/issues/new?template=add-package.yml&repo_url=https%3A%2F%2Fgithub.com%2Fa%2Fb");
		this.assertEquals(DekenStyle.advisoryURL("SCD-2026-001"),
			"https://github.com/shimpe/scdeken-recipes/blob/main/advisories/SCD-2026-001.toml");
	}

	test_suggestNewPackage {
		var form = "https://github.com/shimpe/scdeken-recipes/issues/new?template=add-package.yml";
		this.assertEquals(DekenStyle.suggestURL(nil), form, "no address: the empty form");
		this.assertEquals(DekenStyle.suggestURL(""), form, "empty address: the empty form");
		this.assertEquals(DekenStyle.githubAddressIn("  https://github.com/a/b "), "https://github.com/a/b", "a pasted address");
		this.assertEquals(DekenStyle.githubAddressIn("github.com/a/b"), "https://github.com/a/b", "without https://");
		this.assertEquals(DekenStyle.githubAddressIn("granular reverb"), nil, "search words are not an address");
		this.assertEquals(DekenStyle.githubAddressIn("https://github.com/a/b c"), nil, "not one address");
	}

	test_alreadyListed {
		var reviewed = Dictionary.newFrom(["name", "Panola", "repo", "https://github.com/shimpe/panola", "trust", "reviewed"]);
		var unreviewed = Dictionary.newFrom(["name", "thing", "repo", "https://github.com/o/thing", "trust", "unreviewed"]);
		this.assertEquals(DekenStyle.repoKey(" HTTPS://github.com/O/R.git/ "), "github.com/o/r");
		this.assertEquals(DekenStyle.repoKey("github.com/o/r"), "github.com/o/r");
		this.assert(DekenStyle.alreadyListedText(reviewed).find("Panola (https://github.com/shimpe/panola) is already").notNil, "names the package");
		this.assert(DekenStyle.alreadyListedText(reviewed).find("Suggest for review").isNil, "reviewed: nothing to suggest");
		this.assert(DekenStyle.alreadyListedText(unreviewed).find("Suggest for review").notNil, "unreviewed: points to Suggest for review");
	}
}
