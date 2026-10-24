// Reads a git checkout's commit and origin url from its files, without
// starting any process (Git:sha and Quark:url run git, which would block the
// interpreter). Understands a .git folder, a .git file ("gitdir: ..." as in
// worktrees and submodules), loose and packed refs.
DekenGit {
	// The folder holding HEAD for the checkout at path, or nil.
	*gitDir { |path|
		var dot = path +/+ ".git";
		var target;
		if(File.type(dot) == \directory) { ^dot };
		if(File.exists(dot).not) { ^nil };
		target = this.prRead(dot).stripWhiteSpace;
		if(target.beginsWith("gitdir:").not) { ^nil };
		target = target.copyToEnd(7).stripWhiteSpace;
		if(this.prIsAbsolute(target).not) { target = path +/+ target };
		^if(File.exists(target +/+ "HEAD")) { target }
	}

	// A worktree's refs and config live in the folder its commondir names.
	*commonDir { |gitDir|
		var file = gitDir +/+ "commondir";
		var rel;
		if(File.exists(file).not) { ^gitDir };
		rel = this.prRead(file).stripWhiteSpace;
		^if(this.prIsAbsolute(rel)) { rel } { gitDir +/+ rel }
	}

	// The commit id HEAD points to, or nil.
	*commit { |path|
		var gitDir = this.gitDir(path);
		var head;
		if(gitDir.isNil or: { File.exists(gitDir +/+ "HEAD").not }) { ^nil };
		head = this.prRead(gitDir +/+ "HEAD").stripWhiteSpace;
		if(head.beginsWith("ref:")) { ^this.prResolve(gitDir, head.copyToEnd(4).stripWhiteSpace) };
		^if(this.prIsSha(head)) { head }
	}

	*prResolve { |gitDir, ref|
		var common = this.commonDir(gitDir);
		var sha = nil;
		[gitDir, common].do { |dir|
			var file = dir +/+ ref;
			if(sha.isNil and: { File.exists(file) }) {
				sha = this.prRead(file).stripWhiteSpace;
			}
		};
		^if(this.prIsSha(sha)) { sha } { this.prPacked(common, ref) }
	}

	*prPacked { |common, ref|
		var file = common +/+ "packed-refs";
		var found = nil;
		if(File.exists(file)) {
			this.prRead(file).split(Char.nl).do { |line|
				var parts = line.stripWhiteSpace.split($ );
				if(found.isNil and: { parts.size == 2 and: { parts[1] == ref and: { this.prIsSha(parts[0]) } } }) {
					found = parts[0]
				}
			}
		};
		^found
	}

	// The url of remote "origin", or nil.
	*url { |path|
		var gitDir = this.gitDir(path);
		var inOrigin = false, found = nil, file;
		if(gitDir.isNil) { ^nil };
		file = this.commonDir(gitDir) +/+ "config";
		if(File.exists(file).not) { ^nil };
		this.prRead(file).split(Char.nl).do { |raw|
			var line = raw.stripWhiteSpace;
			if(line.beginsWith("[")) {
				inOrigin = line.replace(" ", "") == "[remote\"origin\"]"
			} {
				if(inOrigin and: { found.isNil } and: { line.beginsWith("url") }) {
					found = line.copyToEnd(3).stripWhiteSpace;
					found = if(found.beginsWith("=")) { found.copyToEnd(1).stripWhiteSpace } { nil }
				}
			}
		};
		^found
	}

	// Unix "/x", Windows "C:/x", "C:\x" or a UNC path.
	*prIsAbsolute { |p|
		^p.beginsWith("/") or: { p.beginsWith(92.asAscii.asString) or: { p.size > 2 and: { p[0].isAlpha and: { p[1] == $: and: { p[2] == $/ or: { p[2] == 92.asAscii } } } } } }
	}

	*prIsSha { |s| ^s.notNil and: { "^[0-9a-f]{40}$".matchRegexp(s) } }

	*prRead { |file| ^File.readAllString(file) }
}
