// The startup advisory flow. The first tests need no helper. The rest run
// the real helper against a signed test index (fixtures/advisory) in which
// EvilQuark 1.0.0 is withdrawn as malware and 1.1.0 is the fix, with a fake
// quark folder (a fake .git, as in TestDekenGit) put on the throwaway
// profile's include paths. Quarks.install is not exercised here, but the real
// Quarks.uninstall is: it only edits the include paths of the throwaway
// profile's own config file.
TestDekenAdvisory : UnitTest {
	var root, quarkPath, sha;

	hit { |severity, fixed|
		var advisory = Dictionary.newFrom(["id", "SCD-2026-0001", "severity", severity, "summary", "It does bad things."]);
		var item = Dictionary.newFrom(["name", "Evil", "version", "1.0.0", "commit", "", "kind", "quark", "path", "C:/q/Evil", "source", "quark"]);
		var h = Dictionary.newFrom(["advisory", advisory, "item", item]);
		if(fixed.notNil) { h["fixed_version"] = fixed };
		^h
	}

	// ---- pure

	test_hitsParsed {
		this.assertEquals(Deken.prAdvisoryActions(this.hit("malware", "1.1.0")), [\remove, \update, \keep, \later]);
		this.assertEquals(Deken.prAdvisoryActions(this.hit("malware", nil)), [\remove, \keep, \later]);
		this.assertEquals(Deken.prAdvisoryActions(this.hit("vulnerable", "")), [\remove, \keep, \later], "an empty fixed_version is none");
	}

	test_keepNeedsConfirmOnlyForMalware {
		this.assertEquals(Deken.prKeepNeedsConfirm(this.hit("malware", nil)), true);
		this.assertEquals(Deken.prKeepNeedsConfirm(this.hit("vulnerable", nil)), false);
		this.assertEquals(Deken.prKeepNeedsConfirm(this.hit("broken", nil)), false);
	}

	test_dialogTexts {
		var h = this.hit("malware", "1.1.0");
		this.assertEquals(DekenAdvisoryDialog.buttonLabel(\update, h), "Update to 1.1.0");
		this.assertEquals(DekenAdvisoryDialog.title(h), "Evil  1.0.0");
		this.assertEquals(DekenAdvisoryDialog.keepAnywayText, "This package was withdrawn because it contains malware. Keep it anyway?");
		this.assert(DekenAdvisoryDialog.limitsText.beginsWith("Removing it stops it from running again. Anything it already did while SuperCollider was running can't be undone by removal"), "honest about limits");
	}

	test_withdrawnRowsAreBadged {
		var item = Dictionary.newFrom(["name", "Evil", "advisory", Dictionary.newFrom(["id", "SCD-2026-0001"])]);
		var row = DekenRows.fromInstalled(item, nil);
		this.assertEquals(DekenRows.status(row, false), "⛔ retracted");
	}

	test_removalStepsMentionTheServerForPlugins {
		this.assertEquals(Deken.prRemovalSteps(Dictionary.newFrom(["kind", "quark"])), ["recompile"]);
		this.assertEquals(Deken.prRemovalSteps(Dictionary.newFrom(["kind", "plugin+quark"])), ["recompile", "reboot-server"]);
	}

	// ---- with the helper

	setUp {
		var fixtures = "SCDEKEN_FIXTURES".getenv;
		this.assert(fixtures.notNil, "SCDEKEN_FIXTURES is set (run through scripts/run-sclang-tests.sh)");
		sha = String.fill(40, $e);
		root = Platform.userAppSupportDir +/+ "advisory-test";
		quarkPath = root +/+ "EvilQuark";
		Deken.helperPath = fixtures +/+ File.exists(fixtures +/+ "scdeken.exe").if { "scdeken.exe" } { "scdeken" };
		Deken.indexURL = this.fileURL(fixtures +/+ "advisory" +/+ "index.json");
		DekenQuarks.override_(nil);
	}

	tearDown {
		Deken.helperPath = nil;
		Deken.indexURL = nil;
		LanguageConfig.removeIncludePath(quarkPath);
		Quarks.clearCache;
		File.deleteAll(root);
		File.deleteAll(Platform.userAppSupportDir +/+ "sc-deken" +/+ "quarantine");
		DekenHelper.prDiscard(Platform.userAppSupportDir +/+ "sc-deken" +/+ "state.json");
	}

	fileURL { |path|
		var p = path.replace(92.asAscii.asString, "/");
		^if(p.beginsWith("/")) { "file://" ++ p } { "file:///" ++ p }
	}

	write { |path, text|
		var f;
		DekenHelper.prMkdirs(path.dirname);
		f = File(path, "w");
		f.write(text);
		f.close;
	}

	await { |what, call|
		var result;
		call.value({ |r| result = r });
		this.wait({ result.notNil }, "the helper answered: " ++ what, 40);
		^result
	}

	// A quark checkout at version 1.0.0 that Quarks.installed lists.
	installFakeQuark {
		this.write(quarkPath +/+ ".git/HEAD", sha ++ "\n");
		this.write(quarkPath +/+ ".git/config", "[remote \"origin\"]\n\turl = https://github.com/example/evilquark\n");
		this.write(quarkPath +/+ "EvilQuark.quark", "(\n\tname: \"EvilQuark\",\n\tsummary: \"fake\"\n)\n");
		LanguageConfig.addIncludePath(quarkPath);
		Quarks.clearCache;
		this.assert(Quarks.installed.any { |q| q.name == "EvilQuark" }, "the fake quark is installed");
	}

	check {
		^this.await("advisories", { |a| Deken.checkAdvisories(false, a) })
	}

	test_checkFindsTheQuarkWithItsFix {
		var r, hit;
		this.installFakeQuark;
		r = this.check;
		this.assert(r.ok, "check ok: " ++ r[\error]);
		this.assertEquals(r.data.size, 1);
		hit = r.data[0];
		this.assertEquals(hit["advisory"]["id"], "SCD-2026-0001");
		this.assertEquals(hit["advisory"]["severity"], "malware");
		this.assertEquals(hit["item"]["source"], "quark");
		this.assertEquals(hit["fixed_version"], "1.1.0");
		this.assertEquals(Deken.prAdvisoryActions(hit), [\remove, \update, \keep, \later]);
	}

	test_nothingInstalledNothingFound {
		var r = this.check;
		this.assert(r.ok, "check ok: " ++ r[\error]);
		this.assertEquals(r.data.size, 0);
	}

	test_removeQuarantinesTheQuark {
		var hit, r, moved;
		this.installFakeQuark;
		hit = this.check.data[0];
		r = this.await("remove", { |a| Deken.removeWithdrawn(hit, a) });
		this.assert(r.ok, "removal ok: " ++ r[\error]);
		moved = Platform.userAppSupportDir +/+ "sc-deken" +/+ "quarantine" +/+ "SCD-2026-0001" +/+ "EvilQuark";
		this.assert(File.exists(moved +/+ "EvilQuark.quark"), "the folder is in quarantine");
		this.assert(File.exists(quarkPath).not, "and gone from where SuperCollider loads it");
		Quarks.clearCache;
		this.assert(Quarks.installed.any { |q| q.name == "EvilQuark" }.not, "Quarks.installed no longer lists it");
		this.assert(LanguageConfig.includePaths.indexOfEqual(quarkPath).isNil, "not in the include paths");
		this.assertEquals(r[\after_install], ["recompile"]);
		this.assertEquals(this.check.data.size, 0, "nothing is withdrawn any more");
	}

	test_failedQuarkUninstallLeavesTheFolderAlone {
		var hit, r;
		this.installFakeQuark;
		hit = this.check.data[0];
		DekenQuarks.override_((uninstall: { |name| false }));
		r = this.await("remove", { |a| Deken.removeWithdrawn(hit, a) });
		this.assert(r.ok.not, "reports the failure");
		this.assert(File.exists(quarkPath), "the folder was not moved");
	}

	test_keepHidesTheHit {
		var hit, r;
		this.installFakeQuark;
		hit = this.check.data[0];
		r = this.await("keep", { |a| Deken.keepWithdrawn("SCD-2026-0001", a) });
		this.assert(r.ok, "keep ok: " ++ r[\error]);
		this.assertEquals(this.check.data.size, 0, "a kept advisory is not reported again");
		this.assert(File.exists(quarkPath), "keeping moves nothing");
	}

	test_offlineIsSilent {
		var r, posted = false;
		Deken.indexURL = "file:///does/not/exist/index.json";
		Deken.logAction = { |line| posted = true };
		r = this.check;
		Deken.logAction = nil;
		this.assert(r.ok.not, "the check fails");
		this.assert(posted.not, "and says nothing");
	}

	test_recommendedAndDetailsLink {
		var h = this.hit("broken", nil);
		this.assertEquals(DekenAdvisoryDialog.removeIsRecommended(this.hit("malware", nil)), true);
		this.assertEquals(DekenAdvisoryDialog.removeIsRecommended(this.hit("vulnerable", nil)), true);
		this.assertEquals(DekenAdvisoryDialog.removeIsRecommended(h), false, "not for broken");
		this.assertEquals(DekenAdvisoryDialog.detailsURL(h), DekenStyle.advisoryURL("SCD-2026-0001"), "falls back when there is no details");
		h["advisory"]["details"] = "https://example.org/the-details";
		this.assertEquals(DekenAdvisoryDialog.detailsURL(h), "https://example.org/the-details");
	}

	// ---- failure and double click (a quark whose folder is not there to move)

	missingFolderHit {
		var h = this.hit("malware", nil);
		h["item"]["path"] = root +/+ "NotThere";
		^h
	}

	test_failedQuarantineAfterUninstallSaysSoAndAsksForRecompile {
		var h = this.missingFolderHit;
		var r;
		DekenQuarks.override_((uninstall: { |name| nil }));
		r = this.await("remove", { |a| Deken.removeWithdrawn(h, a) });
		this.assert(r.ok.not, "reports the failure");
		this.assert(r[\error].beginsWith("Evil was taken out of SuperCollider's class library (recompile to stop it loading), but its folder could not be moved and is still at " ++ h["item"]["path"] ++ ": "), "says what happened: " ++ r[\error]);
		this.assertEquals(r[\after_install], ["recompile"]);
	}

	test_secondClickDuringAJobDoesNothing {
		var h = this.missingFolderHit;
		var uninstalls = 0, outcome, dialog, busyAtOnce;
		DekenQuarks.override_((uninstall: { |name| uninstalls = uninstalls + 1 }));
		// Windows only work on the GUI clock.
		{
			dialog = DekenAdvisoryDialog.show(h, { |o| outcome = o });
			dialog[\act].value(\remove);
			busyAtOnce = dialog[\isBusy].value;
			dialog[\act].value(\remove);
			dialog[\act].value(\later);
		}.defer;
		this.wait({ outcome.notNil }, "the job finished", 40);
		this.assert(busyAtOnce, "busy at once");
		this.assertEquals(uninstalls, 1, "the second click started nothing");
		this.assertEquals(outcome[\kind], \removed, "and Remind me later did not end the dialog early");
		this.assertEquals(outcome[\steps], ["recompile"]);
		{ Window.allWindows.do { |win| win.close } }.defer;
	}

	test_updateAsksBeforeInstalling {
		var hit, plan, answer, finished = false, cancelled = false;
		this.installFakeQuark;
		hit = this.check.data[0];
		DekenQuarks.override_((
			install: { |url, commit| finished = true },
			checkout: { |name, commit| finished = true }));
		DekenAdvisoryDialog.update(hit, { |p, a| plan = p; answer = a }, { |r| if(r.isNil) { cancelled = true } { finished = true } });
		this.wait({ plan.notNil }, "the confirmation was asked for", 40);
		this.assertEquals(plan["version"], "1.1.0", "the fixed version, not just the newest");
		this.assert(plan["confirm"].indexOfEqual("unreviewed").notNil, "unreviewed needs an OK");
		this.assert(finished.not and: { cancelled.not }, "nothing is reported or installed before the answer");
		answer.value(nil);
		this.assert(cancelled, "cancelling reports a cancel");
		this.assert(finished.not, "and installs nothing");
		this.assert(File.exists(quarkPath), "the quark is untouched");
	}
}
