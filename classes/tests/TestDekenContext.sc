TestDekenContext : UnitTest {
	test_buildReturnsContext {
		var ctx = DekenContext.build("scan");
		this.assertEquals(ctx[\sc_version], Main.version);
		this.assertEquals(ctx[\job], "scan");
		this.assertEquals(ctx[\osc_port], NetAddr.langPort);
		this.assertEquals(ctx[\include_paths], LanguageConfig.includePaths);
		this.assert(ctx[\quarks].isKindOf(Array), "quarks is a list")
	}

	test_writeProducesContextJson {
		var path = Platform.defaultTempDir +/+ "deken-context-test.json";
		var ctx;
		this.assertEquals(DekenContext.write(path, "scan"), path);
		ctx = DekenJSON.parse(File.readAllString(path));
		File.delete(path);
		this.assertEquals(ctx["sc_version"], Main.version);
		this.assertEquals(ctx["job"], "scan");
		this.assertEquals(ctx["osc_port"], NetAddr.langPort);
		this.assertEquals(ctx["include_paths"], LanguageConfig.includePaths);
		this.assert(ctx["quarks"].isKindOf(Array), "quarks is a list")
	}
}
