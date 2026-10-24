// Builds command lines for String:unixCmd / unixCmdGetStdOut / Pipe, which
// hand the string to a shell. Every argument is quoted so paths with spaces
// (C:\Users\Stefaan Himpe\...) or apostrophes survive.
//
// POSIX (macOS, Linux): each word in single quotes; ' becomes '\''.
//
// Windows, verified on SuperCollider 3.14.1 with a .bat in a folder with a
// space in its name: unixCmd, unixCmdGetStdOut and Pipe all run the string as
// `cmd /c "<string>"`, i.e. sclang adds the outer pair of double quotes
// itself. So the command is only `"exe" "arg" "arg"`. Adding a second outer
// pair (what a plain `cmd /c` would need) fails with `'""C:' is not
// recognized as an internal or external command`. & stays safe inside the
// quotes; %VAR% is still expanded by cmd and cannot be quoted, and a double
// quote cannot appear in a Windows path, so quote marks in words are refused.
// Not verified on 3.13.
DekenShell {
	*isWindows { ^thisProcess.platform.name == \windows }

	*quote { |string|
		^if(this.isWindows) { this.prWindowsQuote(string) } { this.prPosixQuote(string) }
	}

	// exe followed by args, each quoted for the current platform.
	*command { |exe, args|
		^if(this.isWindows) { this.prWindowsCommand(exe, args) } { this.prPosixCommand(exe, args) }
	}

	*prPosixQuote { |string|
		^"'" ++ string.asString.replace("'", "'\\''") ++ "'"
	}

	*prPosixCommand { |exe, args|
		^([exe] ++ args).collect { |word| this.prPosixQuote(word) }.join(" ")
	}

	*prWindowsQuote { |string|
		var s = string.asString, trailing = 0;
		if(s.includes($")) { Error("DekenShell: cannot quote a word containing a double quote: %".format(s)).throw };
		// A backslash run before the closing quote would escape it for most
		// programs' argument parsers, so double it.
		while { trailing < s.size and: { s[s.size - 1 - trailing] == 92.asAscii } } { trailing = trailing + 1 };
		^"\"" ++ s ++ String.fill(trailing, 92.asAscii) ++ "\""
	}

	*prWindowsCommand { |exe, args|
		^([exe] ++ args).collect { |word| this.prWindowsQuote(word) }.join(" ")
	}
}
