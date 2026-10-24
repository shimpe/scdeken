// The real DekenQuarks.install against a local git repository: the quark is
// installed at its commit and its .quark dependencies are left alone (they
// come from the plan). Only runs inside the throwaway profile of
// scripts/run-sclang-tests.sh, because it adds a quark to the include paths.
TestDekenQuarksInstall : UnitTest {
	var work, repo;

	setUp {
		DekenQuarks.override_(nil);
		work = Platform.defaultTempDir +/+ "scdeken-quark-test";
		repo = work +/+ "DepDemo";
		this.run("rm -rf", work);
	}

	tearDown {
		var installed = DekenQuarks.prFind("DepDemo");
		if(installed.notNil) { Quarks.uninstall("DepDemo") };
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
}
