TestDekenShell : UnitTest {
	test_posixQuoting {
		this.assertEquals(DekenShell.prPosixQuote("it's"), "'it'\\''s'")
	}

	test_posixCommand {
		this.assertEquals(
			DekenShell.prPosixCommand("/a b/scdeken", ["run", "it's"]),
			"'/a b/scdeken' 'run' 'it'\\''s'"
		)
	}

	// No outer pair of quotes: sclang's unixCmd wraps the string for cmd /c itself
	// (verified on 3.14.1, see the DekenShell class comment).
	test_windowsQuoting {
		this.assertEquals(
			DekenShell.prWindowsCommand("C:\\Users\\Stefaan Himpe\\scdeken.exe", ["run", "C:\\a b\\req.json"]),
			"\"C:\\Users\\Stefaan Himpe\\scdeken.exe\" \"run\" \"C:\\a b\\req.json\""
		)
	}

	test_windowsTrailingBackslashIsDoubled {
		this.assertEquals(DekenShell.prWindowsQuote("C:\\dir x\\"), "\"C:\\dir x\\\\\"")
	}

	test_windowsRefusesDoubleQuote {
		this.assertException({ DekenShell.prWindowsQuote("a\"b") }, Error, "a quote mark cannot be quoted")
	}

	test_commandUsesPlatformStyle {
		var cmd = DekenShell.command("x y", ["z"]);
		this.assertEquals(cmd, if(DekenShell.isWindows) { "\"x y\" \"z\"" } { "'x y' 'z'" })
	}
	// End to end: the quoted command line really runs through unixCmdGetStdOut,
	// with a space in the program's folder and a space and an apostrophe in the argument.
	test_commandRunsWithSpacesInPath {
		var dir = Platform.defaultTempDir +/+ "deken shell test";
		var exe = if(DekenShell.isWindows) { dir +/+ "echo arg.bat" } { dir +/+ "echo-arg.sh" };
		var out;
		File.mkdir(dir);
		File.use(exe, "w") { |f|
			f.write(if(DekenShell.isWindows) { "@echo off\r\necho %~1\r\n" } { "#!/bin/sh\necho \"$1\"\n" })
		};
		if(DekenShell.isWindows.not) { ("chmod +x " ++ DekenShell.quote(exe)).unixCmd };
		out = DekenShell.command(exe, ["it's a b"]).unixCmdGetStdOut.stripWhiteSpace;
		File.delete(exe);
		this.assertEquals(out, "it's a b")
	}
}
