// Needs the fixtures from scripts/make-sclang-fixtures.sh (the harness makes
// them): the real scdeken helper, a signed index of FMX6 (prebuilt plugin),
// Panola (reviewed quark) and Sketchy (unreviewed quark), and two helper
// locks. Quark installs are recorded by a stand-in for DekenQuarks, because
// the real ones need git and the network.
TestDekenHelper : UnitTest {
	var fixtures, quarkCalls, logLines;

	setUp {
		fixtures = "SCDEKEN_FIXTURES".getenv;
		this.assert(fixtures.notNil, "SCDEKEN_FIXTURES is set (run through scripts/run-sclang-tests.sh)");
		Deken.helperPath = fixtures +/+ File.exists(fixtures +/+ "scdeken.exe").if { "scdeken.exe" } { "scdeken" };
		Deken.indexURL = this.fileURL(fixtures +/+ "index.json");
		Deken.logAction = { |line| logLines = logLines.add(line) };
		quarkCalls = List.new;
		logLines = [];
		DekenQuarks.override_((
			install: { |url, commit| quarkCalls.add([\install, url, commit]) },
			uninstall: { |path| quarkCalls.add([\uninstall, path]) },
			update: { |url, commit| quarkCalls.add([\update, url, commit]) }
		));
	}

	tearDown {
		Deken.helperPath = nil;
		Deken.indexURL = nil;
		Deken.logAction = nil;
		Deken.progressAction = nil;
		DekenHelper.lockPath = nil;
		DekenQuarks.override_(nil);
	}

	fileURL { |path|
		var p = path.replace(92.asAscii.asString, "/");
		^if(p.beginsWith("/")) { "file://" ++ p } { "file:///" ++ p }
	}

	// Runs an asynchronous call and returns the response it produced.
	await { |what, call|
		var result;
		call.value({ |r| result = r });
		this.wait({ result.notNil }, "the helper answered: " ++ what, 40);
		^result
	}

	// ---- job runner

	test_searchRoundTrip {
		var result;
		Deken.search("fm", action: { |r| result = r });
		this.wait({ result.notNil }, "helper answered", 10);
		this.assert(result.ok, "search succeeded: " ++ result[\error]);
		this.assertEquals(result.data.collect { |h| h["name"] }, ["FMX6"]);
	}

	test_platformAnswers {
		var r = this.await("platform", { |a| Deken.platform(a) });
		this.assert(r.ok, "platform ok: " ++ r[\error]);
		this.assertEquals(r.data["sc_version"], Main.version);
		this.assert(r.data["key"].endsWith("-api3"), "platform key has the plugin API");
	}

	test_jobFilesAreRemoved {
		var r = this.await("search", { |a| Deken.search("fm", action: a) });
		this.assert(r.ok, "search ok");
		this.assertEquals(PathName(DekenHelper.jobsDir).files.size, 0, "no request/result files left");
	}

	test_missingHelperIsReportedNotThrown {
		var r;
		Deken.helperPath = fixtures +/+ "no-such-helper";
		r = this.await("missing helper", { |a| Deken.info("FMX6", a) });
		this.assertEquals(r.ok, false);
		this.assert(r[\error].notNil, "says what went wrong");
	}

	test_unknownPackageGivesPlainError {
		var r = this.await("info", { |a| Deken.info("NoSuchThing", a) });
		this.assertEquals(r.ok, false);
		this.assert(r[\error].find("NoSuchThing").notNil, "names the package");
	}

	// ---- confirmation (exit code 3)

	test_unreviewedNeedsConfirmation {
		var r = this.await("install", { |a| Deken.install("Sketchy", nil, a) });
		this.assertEquals(r[\needsConfirmation], true);
		this.assertEquals(r.confirm, ["unreviewed"]);
		this.assertEquals(quarkCalls.size, 0, "nothing installed yet");
	}

	test_unreviewedPluginPlanNamesItsRepo {
		var r = this.await("plan", { |a| Deken.plan("Shady", nil, a) });
		this.assert(r.ok, "plan works: " ++ r[\error]);
		this.assertEquals(r.data["repo"], "https://github.com/example/shady");
		this.assertEquals(r.data["quark"], nil, "a plugin has no quark part");
		this.assert(DekenDialogs.unreviewedText(r.data).find("https://github.com/example/shady").notNil, "the dialog text names the repo");
	}

	test_installConfirmedWithoutAnswerStillAsks {
		var r = this.await("install", { |a| Deken.installConfirmed("Sketchy", nil, (), a) });
		this.assertEquals(r.ok, false);
		this.assertEquals(r.exit_code, 3);
		this.assertEquals(r.confirm, ["unreviewed"]);
		this.assertEquals(r[\needsConfirmation], true);
		this.assertEquals(quarkCalls.size, 0);
	}

	// ---- quarks are installed by sclang

	test_installConfirmedInstallsTheQuark {
		var r = this.await("install", { |a| Deken.installConfirmed("Sketchy", nil, (confirm_unreviewed: true), a) });
		this.assert(r.ok, "install ok: " ++ r[\error]);
		this.assertEquals(quarkCalls.asArray, [[\install, "https://github.com/example/sketchy", "cccccccccccccccccccccccccccccccccccccccc"]]);
		this.assertEquals(r[\after_install], ["recompile"], "tells the user to recompile");
	}

	test_installReviewedQuarkNeedsNoQuestions {
		var r = this.await("install", { |a| Deken.install("Panola", nil, a) });
		this.assert(r.ok, "install ok: " ++ r[\error]);
		this.assertEquals(quarkCalls.collect { |c| c[1] }.asArray, ["https://github.com/example/panola"]);
	}

	test_removeOfAnUnknownPackageAsksTheHelper {
		var r = this.await("remove", { |a| Deken.remove("Panola", a) });
		// Panola is not installed in the throwaway profile, so the helper answers.
		this.assertEquals(r.ok, false);
		this.assertEquals(quarkCalls.size, 0);
	}

	// ---- prebuilt package, OSC log

	test_prebuiltInstallLogsOverOSC {
		var fractions = List.new;
		var r, plugin;
		Deken.progressAction = { |fraction, label| fractions.add(fraction) };
		r = this.await("install FMX6", { |a| Deken.install("FMX6", nil, a) });
		this.assert(r.ok, "install ok: " ++ r[\error]);
		this.assert(logLines.any { |l| l.find("FMX6").notNil }, "log lines arrived over OSC: " ++ logLines);
		this.assert(fractions.size > 0, "progress arrived over OSC");
		plugin = Platform.userExtensionDir +/+ "sc-deken" +/+ "fmx6" +/+ "FMX6.scx";
		this.assert(File.exists(plugin), "plugin file is in the managed folder");
		this.assertEquals(quarkCalls.size, 0, "a plugin needs no quark");
		this.assertEquals(r[\after_install], ["recompile", "reboot-server"]);

		r = this.await("remove FMX6", { |a| Deken.remove("FMX6", a) });
		this.assert(r.ok, "remove ok: " ++ r[\error]);
		this.assert(File.exists(plugin).not, "plugin file is gone");
	}

	// ---- bootstrap

	removeDownloadedHelper {
		PathName(DekenHelper.binDir).files.do { |f| File.delete(f.fullPath) }
	}

	test_platformKeyShape {
		var key = DekenHelper.platformKey;
		this.assert("^(linux|macos|windows)-(x86_64|arm64)$".matchRegexp(key), "key looks right: " ++ key);
	}

	test_downloadURLFillsTheTemplate {
		var url = DekenHelper.downloadURL(Dictionary.newFrom(["version", "1.2.3", "url", "https://x/v{version}/scdeken-{platform}{ext}"]));
		var expected = "https://x/v1.2.3/scdeken-" ++ DekenHelper.platformKey ++ DekenHelper.exeExtension;
		this.assertEquals(url, expected);
	}

	test_missingHelperBeforeTheFirstRelease {
		var msg = DekenHelper.missingHelperMessage(Dictionary.newFrom(["version", "0.0.0", "sha256", Dictionary.new]));
		this.assert(msg.find("before the first sc-deken release").notNil, "says there is no release yet: " ++ msg);
		this.assert(msg.find("Settings").notNil, "points to the Settings page: " ++ msg);
		this.assert(msg.find("Deken.helperPath").notNil, "names the code alternative: " ++ msg);
	}

	test_missingHelperForThisComputer {
		var msg = DekenHelper.missingHelperMessage(Dictionary.newFrom(["version", "1.2.3", "sha256", Dictionary.newFrom(["plan9-mips", "ab"])]));
		this.assert(msg.find("1.2.3").notNil, "names the release: " ++ msg);
		this.assert(msg.find(DekenHelper.platformKey).notNil, "names this computer: " ++ msg);
		this.assert(msg.find("Settings").notNil, "points to the Settings page: " ++ msg);
	}

	test_bootstrapDownloadsAndRunsTheHelper {
		var path, failed, r;
		Deken.helperPath = nil;
		DekenHelper.lockPath = fixtures +/+ "lock-ok.json";
		this.removeDownloadedHelper;
		DekenHelper.ensure({ |p| path = p }, { |msg| failed = msg });
		this.wait({ path.notNil or: { failed.notNil } }, "bootstrap finished", 40);
		this.assertEquals(failed, nil, "no failure");
		this.assertEquals(path, DekenHelper.binDir +/+ ("scdeken-9.9.9" ++ DekenHelper.exeExtension));
		this.assert(File.exists(path), "helper is in place");
		this.assert(File.exists(path ++ ".part").not, "no partial download left");

		r = this.await("version via the downloaded helper", { |a| DekenHelper.run((command: "version"), a) });
		this.assert(r.ok, "the downloaded helper runs: " ++ r[\error]);
		this.removeDownloadedHelper;
	}

	test_bootstrapUsesAnInstalledHelperWithoutDownloading {
		var path;
		Deken.helperPath = nil;
		DekenHelper.lockPath = fixtures +/+ "lock-ok.json";
		this.removeDownloadedHelper;
		DekenHelper.ensure({ |p| path = p });
		this.wait({ path.notNil }, "first bootstrap", 40);
		// A lock with a wrong checksum cannot hurt a helper that is already there.
		DekenHelper.lockPath = fixtures +/+ "lock-bad.json";
		path = nil;
		DekenHelper.ensure({ |p| path = p });
		this.wait({ path.notNil }, "second bootstrap", 5);
		this.removeDownloadedHelper;
	}

	test_bootstrapRefusesAWrongChecksum {
		var path, failed;
		Deken.helperPath = nil;
		DekenHelper.lockPath = fixtures +/+ "lock-bad.json";
		this.removeDownloadedHelper;
		DekenHelper.ensure({ |p| path = p }, { |msg| failed = msg });
		this.wait({ path.notNil or: { failed.notNil } }, "bootstrap finished", 40);
		this.assertEquals(path, nil, "helper not used");
		this.assert(failed.find("did not match its checksum").notNil, "says why: " ++ failed);
		this.assertEquals(PathName(DekenHelper.binDir).files.size, 0, "downloaded file deleted");
	}

	test_runReportsABadBootstrap {
		var r;
		Deken.helperPath = nil;
		DekenHelper.lockPath = fixtures +/+ "lock-bad.json";
		this.removeDownloadedHelper;
		r = this.await("run with bad lock", { |a| DekenHelper.run((command: "version"), a) });
		this.assertEquals(r.ok, false);
		this.assert(r[\error].find("checksum").notNil, "error mentions the checksum");
	}

	// ---- failures

	test_failedQuarkInstallIsNotReportedAsSuccess {
		var r;
		DekenQuarks.override_((install: { |url, commit| false }));
		r = this.await("install", { |a| Deken.install("Panola", nil, a) });
		this.assertEquals(r.ok, false);
		this.assert(r[\error].find("quark").notNil, "says the quark failed: " ++ r[\error]);
		this.assertEquals(r[\after_install], nil, "no 'now recompile' advice");
	}

	test_throwingQuarkInstallIsReported {
		var r;
		DekenQuarks.override_((install: { |url, commit| Error("git clone failed").throw }));
		r = this.await("install", { |a| Deken.install("Panola", nil, a) });
		this.assertEquals(r.ok, false);
		this.assert(r[\error].find("git clone failed").notNil, "keeps the reason");
	}

	test_failedQuarkUninstallThrows {
		DekenQuarks.override_((uninstall: { |path| false }));
		this.assertException({ DekenQuarks.uninstall("/q/Panola") }, Error, "uninstall failure throws");
		DekenQuarks.override_((update: { |url, commit| false }));
		this.assertException({ DekenQuarks.update("https://github.com/o/Panola", "abc") }, Error, "update failure throws");
	}

	test_realQuarkUninstallOfUnknownFolderThrows {
		DekenQuarks.override_(nil);
		this.assertException({ DekenQuarks.uninstall(Quarks.folder +/+ "NoSuchQuarkAnywhere") }, Error, "unknown quark is an error");
	}

	// ---- confirmations belong to the plan that was shown

	test_installSendsTheCommitsOfThePlan {
		var r = this.await("install", { |a| Deken.install("Panola", nil, a) });
		this.assert(r.ok, "the commits of the plan just made match: " ++ r[\error]);
	}

	test_aChangedPlanIsAskedAgain {
		var wrong = Dictionary.newFrom(["Sketchy", String.fill(40, $9)]);
		var r = this.await("install", { |a| Deken.installConfirmed("Sketchy", nil, (confirm_unreviewed: true, commits: wrong), a) });
		this.assertEquals(r[\needsConfirmation], true);
		this.assertEquals(r.confirm, ["changed"]);
		this.assertEquals(r[\error], "The package list changed since you confirmed; please check again.");
		this.assertEquals(r.data["package"], "Sketchy", "with the new plan");
		this.assertEquals(quarkCalls.size, 0, "nothing installed");
	}

	// ---- updates

	test_updateWithoutANameUpdatesEverything {
		var r = this.await("update", { |a| Deken.update(nil, a) });
		this.assert(r.ok, "update ok: " ++ r[\error]);
		this.assertEquals(r.data["results"].size, 0, "nothing is installed in the throwaway profile, so nothing to update");
	}

	test_anUpdatedQuarkIsFoundByItsAddress {
		var result = Dictionary.newFrom(["plan", Dictionary.newFrom(["package", "ScTimeline", "is_update", true,
			"quark", Dictionary.newFrom(["url", "https://github.com/shimpe/sc-timeline", "commit", "abc"])])]);
		Deken.prInstallQuark(result);
		this.assertEquals(quarkCalls.asArray, [[\update, "https://github.com/shimpe/sc-timeline", "abc"]]);
	}

	test_removeOfAQuarkUsesItsFolder {
		var item = Dictionary.newFrom(["name", "ScTimeline", "quark_name", "sc-timeline", "source", "quark", "path", "/q/sc-timeline"]);
		var r;
		Deken.prRemoveQuark(item, { |resp| r = resp });
		this.assert(r.ok, "removed");
		this.assertEquals(quarkCalls.asArray, [[\uninstall, "/q/sc-timeline"]]);
		this.assertEquals(Deken.prFindItem([item], "sc-timeline"), item, "found by the quark's own name too");
		this.assertEquals(Deken.prFindItem([item], "sctimeline"), item, "and by the package name");
	}

	// ---- helper download

	test_curlGivesUp {
		var cmd = DekenHelper.curlCommand("out.part", "https://example.org/x");
		this.assert(cmd.find("--connect-timeout").notNil and: { cmd.find("30").notNil }, cmd);
		this.assert(cmd.find("--max-time").notNil and: { cmd.find("600").notNil }, cmd);
		this.assert(DekenHelper.curlFailure(28).find("took too long").notNil, "a timeout says so");
		this.assert(DekenHelper.curlFailure(6).find("curl exit code 6").notNil, "other failures keep the code");
	}

	test_jobListenersSurviveCmdPeriod {
		var key = DekenHelper.prKey(\log, "jtest");
		DekenHelper.prListen("jtest", { }, { });
		this.assert(OSCdef.all[key].permanent, "Ctrl+. does not drop the job's log");
		DekenHelper.prUnlisten("jtest");
		this.assertEquals(OSCdef.all[key], nil, "freed when the job ends");
	}

	test_installConfirmedWithMissingHelperReportsFailure {
		var r;
		Deken.helperPath = fixtures +/+ "no-such-helper";
		r = this.await("installConfirmed", { |a| Deken.installConfirmed("Panola", nil, (), a) });
		this.assertEquals(r.ok, false);
		this.assertEquals(r.confirm, []);
		this.assert(r[\error].notNil, "has an error");
	}

	test_everyResponseHasAConfirmList {
		var r = DekenHelper.prResponse(false, nil, "x", nil, 1);
		this.assertEquals(r.confirm, []);
	}

	// ---- bootstrap in parallel

	test_overlappingBootstrapsShareOneVerifiedDownload {
		var a, b, failures = List.new, before;
		Deken.helperPath = nil;
		DekenHelper.lockPath = fixtures +/+ "lock-ok.json";
		this.removeDownloadedHelper;
		before = DekenHelper.downloadCount;
		DekenHelper.ensure({ |p| a = p }, { |m| failures.add(m) });
		DekenHelper.ensure({ |p| b = p }, { |m| failures.add(m) });
		this.wait({ (a.notNil and: b.notNil) or: { failures.size > 0 } }, "both answered", 40);
		this.assertEquals(failures.size, 0, "no failure: " ++ failures);
		this.assertEquals(a, b, "same path");
		this.assertEquals(DekenHelper.downloadCount - before, 1, "one download");
		this.assert(File.exists(a), "helper in place");
		this.assertEquals(PathName(DekenHelper.binDir).files.size, 1, "no leftovers");
		this.removeDownloadedHelper;
	}

	test_sha256OfAKnownFile {
		var file = Platform.defaultTempDir +/+ "deken-sha-test.txt";
		var hash;
		var f = File(file, "w");
		f.write("abc");
		f.close;
		DekenHelper.sha256(file, { |h| hash = h });
		this.wait({ hash.notNil }, "hash computed", 20);
		File.delete(file);
		this.assertEquals(hash, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
	}
}
TestDekenGit : UnitTest {
	var root;

	setUp {
		root = Platform.userAppSupportDir +/+ "git-test";
		DekenHelper.prMkdirs(root);
	}

	tearDown { File.deleteAll(root) }

	write { |path, text|
		var f;
		DekenHelper.prMkdirs(path.dirname);
		f = File(path, "w");
		f.write(text);
		f.close;
	}

	sha { |c| ^String.fill(40, c) }

	test_detachedHead {
		this.write(root +/+ "a/.git/HEAD", this.sha($a) ++ "\n");
		this.assertEquals(DekenGit.commit(root +/+ "a"), this.sha($a));
	}

	test_looseBranchRef {
		this.write(root +/+ "a/.git/HEAD", "ref: refs/heads/main\n");
		this.write(root +/+ "a/.git/refs/heads/main", this.sha($b) ++ "\n");
		this.assertEquals(DekenGit.commit(root +/+ "a"), this.sha($b));
	}

	test_packedRef {
		this.write(root +/+ "a/.git/HEAD", "ref: refs/heads/main\n");
		this.write(root +/+ "a/.git/packed-refs", "# pack-refs\n" ++ this.sha($c) ++ " refs/heads/other\n" ++ this.sha($d) ++ " refs/heads/main\n");
		this.assertEquals(DekenGit.commit(root +/+ "a"), this.sha($d));
	}

	test_gitFilePointsElsewhere {
		this.write(root +/+ "store/wt/HEAD", this.sha($e) ++ "\n");
		this.write(root +/+ "a/.git", "gitdir: " ++ root +/+ "store/wt\n");
		this.assertEquals(DekenGit.commit(root +/+ "a"), this.sha($e));
	}

	test_relativeGitFileAndWorktreeCommonDir {
		this.write(root +/+ "main/.git/HEAD", "ref: refs/heads/main\n");
		this.write(root +/+ "main/.git/refs/heads/feature", this.sha($f) ++ "\n");
		this.write(root +/+ "main/.git/config", "[remote \"origin\"]\n\turl = https://example.org/x.git\n");
		this.write(root +/+ "main/.git/worktrees/w/HEAD", "ref: refs/heads/feature\n");
		this.write(root +/+ "main/.git/worktrees/w/commondir", "../..\n");
		this.write(root +/+ "w/.git", "gitdir: ../main/.git/worktrees/w\n");
		this.assertEquals(DekenGit.commit(root +/+ "w"), this.sha($f));
		this.assertEquals(DekenGit.url(root +/+ "w"), "https://example.org/x.git");
	}

	test_originUrlIgnoresOtherRemotes {
		this.write(root +/+ "a/.git/HEAD", this.sha($a));
		this.write(root +/+ "a/.git/config", "[core]\n\tbare = false\n[remote \"fork\"]\n\turl = https://example.org/fork.git\n[remote \"origin\"]\n\turl = https://example.org/real.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n");
		this.assertEquals(DekenGit.url(root +/+ "a"), "https://example.org/real.git");
	}

	test_notAGitFolder {
		this.write(root +/+ "plain/readme.txt", "hi");
		this.assertEquals(DekenGit.commit(root +/+ "plain"), nil);
		this.assertEquals(DekenGit.url(root +/+ "plain"), nil);
	}
}
