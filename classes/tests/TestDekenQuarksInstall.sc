// The real DekenQuarks.install against a local git repository: the quark is
// installed at its commit and its .quark dependencies are left alone (they
// come from the plan). Only runs inside the throwaway profile of
// scripts/run-sclang-tests.sh, because it adds a quark to the include paths.
TestDekenQuarksInstall : UnitTest {
	var work, repo;

	setUp {
		DekenQuarks.override_(nil);
		DekenQuarks.allowFileURLs_(true);
		work = Platform.defaultTempDir +/+ "scdeken-quark-test";
		repo = work +/+ "DepDemo";
		this.run("rm -rf", work);
	}

	tearDown {
		DekenQuarks.allowFileURLs_(false);
		Quarks.uninstall("DepDemo");
		this.run("rm -rf", Quarks.folder +/+ "DepDemo");
		this.run("rm -rf", work);
	}

	// Runs a command, native to the platform (rm -rf as rmdir on Windows).
	run { |what, path|
		var cmd = if(what == "rm -rf") {
			if(DekenShell.isWindows) { "if exist % rmdir /s /q %".format(DekenShell.quote(path), DekenShell.quote(path)) } { "rm -rf " ++ DekenShell.quote(path) }
		} { what };
		cmd.unixCmdGetStdOut
	}

	git { |...args|
		^DekenShell.command("git", ["-C", repo, "-c", "user.name=t", "-c", "user.email=t@example.org"] ++ args).unixCmdGetStdOut
	}

	makeRepo { |quarkFile|
		File.mkdir(work);
		File.mkdir(repo);
		this.git("init", "--quiet");
		File.use(repo +/+ "DepDemo.quark", "w", { |f| f.write(quarkFile) });
		File.mkdir(repo +/+ "classes");
		File.use(repo +/+ "classes/DepDemoClass.sc", "w", { |f| f.write("DepDemoClass { }\n") });
		this.git("add", ".");
		this.git("commit", "--quiet", "-m", "first");
	}

	inProfile {
		^Quarks.folder.beginsWith(("SCDEKEN_PROFILE".getenv ? "no-profile").replace("\\", "/"))
	}

	test_unsafeAddressesAndCommitsAreRefused {
		DekenQuarks.allowFileURLs_(false);
		[
			"http://github.com/o/r", "git@github.com:o/r.git", "file:///etc", "https://user@github.com/o/r", "https://github.com/o/r;calc",
			"https://github.com/o/r&calc", "https://github.com/o/$HOME", "https://github.com/o/r x", "https://github.com/o/../r",
			"https://github.com/o/r?a=b", "https://github.com/o/`x`", "https://github.com/o/\"r", "https://github.com:99/o/r"
		].do { |bad|
			this.assertException({ DekenQuarks.install(bad, nil) }, Error, "refuses " ++ bad)
		};
		["abc", "main", "1234567890123456789012345678901234567890; calc", "tags/1.0"].do { |bad|
			this.assertException({ DekenQuarks.install("https://github.com/o/r", bad) }, Error, "refuses commit " ++ bad);
			this.assertException({ DekenQuarks.update("https://github.com/o/r", bad) }, Error, "refuses commit on update " ++ bad)
		}
	}

	test_safeAddressesPass {
		DekenQuarks.allowFileURLs_(false);
		["https://github.com/o/r", "https://github.com/o/r.git", "https://git.example.org/a_b/c-d~e/"].do { |good|
			this.assert(DekenQuarks.prSafeURL(good), good);
		};
		this.assertEquals(DekenQuarks.prSafeURL("file:///C:/x"), false, "file addresses only in tests");
	}

	test_aFailedDownloadIsAnError {
		if(this.inProfile.not) { ^this.assert(false, "this test must run through scripts/run-sclang-tests.sh") };
		this.assertException({ DekenQuarks.install("file:///" ++ (work +/+ "Missing").replace("\\", "/"), nil) }, Error, "clone failure throws");
		this.run("rm -rf", Quarks.folder +/+ "Missing");
	}

	test_installLeavesDependenciesToThePlan {
		var url, quark;
		if(this.inProfile.not) { ^this.assert(false, "this test must run through scripts/run-sclang-tests.sh") };
		this.makeRepo("( name: \"DepDemo\", summary: \"s\", dependencies: [\"NoSuchQuarkAnywhere@tags/9.9\", \"https://example.invalid/x/Other.git\"] )");
		url = "file:///" ++ repo.replace("\\", "/");
		quark = DekenQuarks.install(url, nil);
		this.assertEquals(quark.name, "DepDemo");
		this.assert(Quarks.pathIsInstalled(quark.localPath), "the quark is linked");
		this.assertEquals(File.exists(Quarks.folder +/+ "NoSuchQuarkAnywhere"), false, "no dependency was fetched");
		this.assertEquals(File.exists(Quarks.folder +/+ "Other"), false, "no URL dependency was fetched");
	}

	// The .quark file's preInstall hook leaves a marker when it runs.
	hookedQuarkFile {
		var marker = (work +/+ "hook-ran").replace("\\", "/");
		^"( name: \"DepDemo\", summary: \"s\", preInstall: { File.use(%, \"w\", { |f| f.write(\"x\") }) } )".format(marker.asCompileString)
	}

	test_aCommitThatCannotBeReachedRunsNothing {
		var url;
		if(this.inProfile.not) { ^this.assert(false, "this test must run through scripts/run-sclang-tests.sh") };
		this.makeRepo(this.hookedQuarkFile);
		url = "file:///" ++ repo.replace("\\", "/");
		this.assertException({ DekenQuarks.install(url, String.fill(40, $9)) }, Error, "a commit the checkout cannot reach is an error");
		this.assertEquals(File.exists(work +/+ "hook-ran"), false, "the install hook did not run");
		this.assertEquals(Quarks.pathIsInstalled(Quarks.folder +/+ "DepDemo"), false, "the quark is not linked");
	}

	test_installRunsTheHookAfterTheCommitIsVerified {
		var url, commit;
		if(this.inProfile.not) { ^this.assert(false, "this test must run through scripts/run-sclang-tests.sh") };
		this.makeRepo(this.hookedQuarkFile);
		commit = DekenGit.commit(repo);
		url = "file:///" ++ repo.replace("\\", "/");
		DekenQuarks.install(url, commit);
		this.assertEquals(DekenGit.commit(Quarks.folder +/+ "DepDemo"), commit);
		this.assert(File.exists(work +/+ "hook-ran"), "the install hook ran");
	}

	test_updateFetchesAndRefusesLocalChanges {
		var url, first, second;
		if(this.inProfile.not) { ^this.assert(false, "this test must run through scripts/run-sclang-tests.sh") };
		this.makeRepo("( name: \"DepDemo\", summary: \"s\" )");
		first = DekenGit.commit(repo);
		url = "file:///" ++ repo.replace("\\", "/");
		DekenQuarks.install(url, first);
		File.use(repo +/+ "classes/DepDemoClass.sc", "w", { |f| f.write("DepDemoClass { *two { ^2 } }\n") });
		this.git("commit", "--quiet", "-am", "second");
		second = DekenGit.commit(repo);
		DekenQuarks.update(url, second);
		this.assertEquals(DekenGit.commit(Quarks.folder +/+ "DepDemo"), second, "moved to a commit made after the clone");
		File.use(Quarks.folder +/+ "DepDemo/classes/DepDemoClass.sc", "w", { |f| f.write("// mine\n") });
		this.assertException({ DekenQuarks.update(url, first) }, Error, "a folder with changes of its own is left alone");
		this.assertEquals(DekenGit.commit(Quarks.folder +/+ "DepDemo"), second);
	}

	test_repoKey {
		this.assertEquals(DekenQuarks.repoKey("https://GitHub.com/Shimpe/sc-timeline.git/"), "github.com/shimpe/sc-timeline");
		this.assertEquals(DekenQuarks.repoKey("git@github.com:shimpe/sc-timeline"), "github.com/shimpe/sc-timeline");
		this.assertEquals(DekenQuarks.repoKey("http://github.com/shimpe/sc-timeline"), "github.com/shimpe/sc-timeline");
	}
}
