// Quark dependencies at install time: the helper's plan returns them first,
// and DekenQuarks installs them in that order, each pinned to its own
// commit. DekenQuarks is replaced by a recorder, so nothing touches git.
TestDekenDependencies : UnitTest {
	var calls;

	setUp {
		calls = List.new;
		DekenQuarks.override_((
			install: { |url, commit| calls.add([\install, url, commit]) },
			checkout: { |name, commit, url| calls.add([\checkout, name, commit]) }
		));
	}

	tearDown { DekenQuarks.override_(nil) }

	result { |name, url, commit, isUpdate|
		^Dictionary.newFrom(["plan", Dictionary.newFrom(["package", name, "is_update", isUpdate,
			"quark", Dictionary.newFrom(["url", url, "commit", commit]), "after_install", ["recompile"]])])
	}

	test_dependenciesAreInstalledBeforeTheParentAtTheirOwnCommits {
		var answer = (ok: true, data: Dictionary.newFrom(["results", [
			this.result("Deep", "https://example.org/Deep", "aaa", false),
			this.result("Extra", "https://example.org/Extra", "", false),
			this.result("Lib", "https://example.org/Lib", "bbb", true),
			this.result("App", "https://example.org/App", "ccc", false)]]));
		var finished = false;
		Deken.prInstallQuarks(answer, { finished = true });
		this.assert(finished, "reported back");
		this.assertEquals(calls.asArray, [
			[\install, "https://example.org/Deep", "aaa"],
			[\install, "https://example.org/Extra", nil],
			[\checkout, "Lib", "bbb"],
			[\install, "https://example.org/App", "ccc"]]);
	}

	test_aFailedDependencyStopsTheParent {
		var answer = (ok: true, data: Dictionary.newFrom(["results", [
			this.result("Deep", "https://example.org/Deep", "aaa", false),
			this.result("App", "https://example.org/App", "ccc", false)]]));
		DekenQuarks.override_((install: { |url, commit| calls.add(url); Error("clone failed").throw }));
		Deken.prInstallQuarks(answer, { });
		this.assertEquals(calls.asArray, ["https://example.org/Deep"], "the parent is not tried");
		this.assertEquals(answer[\ok], false);
	}
}
