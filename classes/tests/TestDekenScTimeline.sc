// The real quark round trip, with the network: install, update and remove
// ScTimeline (https://github.com/shimpe/sc-timeline) at the commit the
// recipes repository approved, through Deken, the real helper and the real
// DekenQuarks. Its folder is called sc-timeline, yet Installed must show it
// as ScTimeline and Remove must find it. Only runs inside the throwaway
// profile of scripts/run-sclang-tests.sh.
TestDekenScTimeline : UnitTest {
	classvar commit = "ca27d83083932fdb286fa8c3f67a76f91a74bfe5";
	var fixtures;

	setUp {
		fixtures = "SCDEKEN_FIXTURES".getenv;
		Deken.helperPath = fixtures +/+ File.exists(fixtures +/+ "scdeken.exe").if { "scdeken.exe" } { "scdeken" };
		Deken.indexURL = this.fileURL(fixtures +/+ "sctimeline" +/+ "index.json");
		Deken.logAction = { };
		DekenQuarks.override_(nil);
	}

	tearDown {
		var folder = Quarks.folder +/+ "sc-timeline";
		Quarks.unlink(folder);
		Quarks.clearCache;
		if(DekenShell.isWindows) {
			("if exist % rmdir /s /q %".format(DekenShell.quote(folder), DekenShell.quote(folder))).unixCmdGetStdOut
		} {
			("rm -rf " ++ DekenShell.quote(folder)).unixCmdGetStdOut
		};
		Deken.helperPath = nil;
		Deken.indexURL = nil;
		Deken.logAction = nil;
	}

	fileURL { |path|
		var p = path.replace(92.asAscii.asString, "/");
		^if(p.beginsWith("/")) { "file://" ++ p } { "file:///" ++ p }
	}

	await { |what, call|
		var result;
		call.value({ |r| result = r });
		this.wait({ result.notNil }, "answered: " ++ what, 120);
		^result
	}

	installedItem {
		var r = this.await("list", { |a| Deken.list(a) });
		^r.data.detect { |i| i["name"] == "ScTimeline" }
	}

	inProfile {
		^Quarks.folder.beginsWith(("SCDEKEN_PROFILE".getenv ? "no-profile").replace("\\", "/"))
	}

	test_installUpdateRemove {
		var r, item;
		if(this.inProfile.not) { ^this.assert(false, "this test must run through scripts/run-sclang-tests.sh") };

		r = this.await("install", { |a| Deken.install("ScTimeline", nil, a) });
		this.assert(r.ok, "install ok: " ++ r[\error]);
		this.assertEquals(DekenGit.commit(Quarks.folder +/+ "sc-timeline"), commit, "checked out at the approved commit");
		item = this.installedItem;
		this.assert(item.notNil, "Installed shows it as ScTimeline");
		this.assertEquals(item !? { |i| i["quark_name"] }, "sc-timeline", "and knows its folder name");
		this.assertEquals(item !? { |i| i["version"] }, "0.1.0", "at the approved version");

		r = this.await("update", { |a| Deken.update("ScTimeline", a) });
		this.assert(r.ok, "update ok: " ++ r[\error]);
		this.assertEquals(DekenGit.commit(Quarks.folder +/+ "sc-timeline"), commit, "still at the approved commit");
		this.assertEquals(Quarks.installed.count { |q| q.name == "sc-timeline" }, 1, "not installed twice");

		r = this.await("remove", { |a| Deken.remove("ScTimeline", a) });
		this.assert(r.ok, "remove ok: " ++ r[\error]);
		this.assertEquals(this.installedItem, nil, "Installed no longer shows it");
		this.assertEquals(Quarks.pathIsInstalled(Quarks.folder +/+ "sc-timeline"), false, "out of the class library");
	}
}
