TestDekenSettings : UnitTest {
	setUp {
		DekenSettings.pathOverride = Platform.defaultTempDir +/+ "deken-settings-test" ++ UniqueID.next ++ ".scd";
		DekenSettings.reload
	}
	tearDown {
		if(File.exists(DekenSettings.path)) { File.delete(DekenSettings.path) };
		DekenSettings.pathOverride = nil;
		DekenSettings.reload
	}

	test_defaults {
		this.assertEquals(DekenSettings.get(\checkAdvisories), true, "advisory check is on by default");
		this.assertEquals(DekenSettings.get(\showUnreviewed), false);
		this.assertEquals(DekenSettings.get(\helperPath), "");
	}

	test_putIsKept {
		DekenSettings.put(\showUnreviewed, true);
		DekenSettings.put(\helperPath, "C:/tools/scdeken.exe");
		DekenSettings.reload;
		this.assertEquals(DekenSettings.get(\showUnreviewed), true);
		this.assertEquals(DekenSettings.get(\helperPath), "C:/tools/scdeken.exe");
		this.assertEquals(DekenSettings.get(\checkAdvisories), true, "others keep their default");
	}

	test_damagedFileFallsBackToDefaults {
		var f;
		DekenHelper.prMkdirs(DekenSettings.path.dirname);
		f = File(DekenSettings.path, "w"); f.write("this is not an archive"); f.close;
		DekenSettings.reload;
		this.assertEquals(DekenSettings.get(\checkAdvisories), true);
	}
}
