// The only place sc-deken touches the Quarks system. sclang performs quark
// installs itself (the Go helper never runs git for them).
//
// Quarks reports many failures only by posting a message: Quarks.install
// returns the Quark even when the clone failed, and Quark:checkout only
// warns about a folder with changes of its own. So every call here checks
// the outcome afterwards and throws an Error if it did not work.
//
// An installed quark is found by its folder (uninstall) or by its
// repository address (update), never by its name: the folder of
// https://github.com/shimpe/sc-timeline is "sc-timeline" while the package
// is called ScTimeline.
//
// Nothing of a quark runs before its commit is verified: the folder is
// fetched and checked out, HEAD is compared with the commit (DekenGit reads
// it from the files, no process), and only then do the compatibility check,
// the install hooks and the link to the class library happen.
//
// Tests can replace the real calls with
// `DekenQuarks.override_((install: { |url, commit| ... }, ...))`; a function
// that returns false (or throws) models a failure. Pass nil to restore.
//
// Dependencies: the plan lists every dependency of a quark and the helper
// returns them first, in order, each pinned to its own commit (see
// Deken.prInstallQuarks). SuperCollider 3.14.1's Quarks.installQuark does NOT
// skip a dependency that is already installed: for every entry of the
// .quark file's dependencies it calls Quark:install again, which runs
// Quark:checkout (moving an installed dependency to the refspec written in
// the .quark file, such as "Name@tags/1.2", and undoing our pin), runs the
// preInstall and postInstall hooks a second time and fetches anything the
// plan decided to leave alone. Only an entry without a refspec is harmless.
// So *install does not call Quarks.install: it checks out the quark and links
// it (what installQuark does apart from the dependency loop), and leaves the
// dependencies to the plan.
DekenQuarks {
	classvar override, fileURLsAllowed = false;

	*override_ { |events| override = events }

	// Tests only: also accept file:/// addresses (a local git repository).
	*allowFileURLs_ { |flag| fileURLsAllowed = flag }

	// Installs the quark at url, pinned to commit (nil or "" = default branch).
	*install { |url, commit|
		var refspec = this.prRefspec(commit);
		var quark;
		if(override.notNil) { ^this.prExpect(override[\install].value(url, refspec), "install " ++ url) };
		this.prCheckSafe(url, refspec);
		quark = Quark(url, refspec);
		this.prRefuseSecondCopy(quark, url);
		this.prCheckOut(quark, refspec, url);
		this.prLink(quark);
		^quark
	}

	// Moves the installed quark that came from url to commit (fetching first),
	// or installs it when none is installed.
	*update { |url, commit|
		var refspec = this.prRefspec(commit);
		var quark;
		if(override.notNil) { ^this.prExpect(override[\update].value(url, refspec), "update " ++ url) };
		this.prCheckSafe(url, refspec);
		quark = this.prFindByURL(url);
		if(quark.isNil) { ^this.install(url, commit) };
		quark.refspec = refspec;
		this.prCheckOut(quark, refspec, url);
		Quarks.clearCache;
		^quark
	}

	// Uninstalls the quark in the folder path (the path the helper's list
	// reports).
	*uninstall { |path|
		var quark;
		if(override.notNil) { ^this.prExpect(override[\uninstall].value(path), "uninstall " ++ path) };
		quark = this.prFindByPath(path);
		if(quark.isNil) { Error("no quark is installed at %".format(path)).throw };
		Quarks.uninstallQuark(quark);
		if(this.prFindByPath(path).notNil) { Error("the quark % could not be uninstalled".format(quark.name)).throw };
	}

	// ---- the steps

	*prRefspec { |commit| ^if(commit.isNil or: { commit.isEmpty }) { nil } { commit } }

	// Another folder already linked for the same repository would define
	// every class twice.
	*prRefuseSecondCopy { |quark, url|
		var prev = this.prFindByURL(url);
		if(prev.notNil and: { this.prSamePath(prev.localPath, quark.localPath).not }) {
			Error("% is already installed at %".format(url, prev.localPath)).throw
		}
	}

	// Brings the folder to refspec: clones it, or (when it is there already)
	// refuses changes of its own and fetches. Then checks out and verifies the
	// commit, before any of the quark's code can run.
	*prCheckOut { |quark, refspec, url|
		if(quark.isDownloaded) {
			if(Git.isGit(quark.localPath).not) {
				Error("% is not a git checkout, so % is not installed there".format(quark.localPath, url)).throw
			};
			if(quark.git.isDirty) {
				Error("% has changes of its own, so it is left as it is".format(quark.localPath)).throw
			};
			quark.git.fetch;
		};
		quark.checkout;
		if(Git.isGit(quark.localPath).not) {
			Error("the quark at % could not be downloaded".format(url)).throw
		};
		this.prVerifyCommit(quark, refspec);
	}

	// What Quarks.installQuark does apart from the dependency loop.
	*prLink { |quark|
		if(quark.isCompatible.not) {
			Error("% reports an incompatibility with this SuperCollider version or with other installed quarks".format(quark.name)).throw
		};
		quark.runHook('preInstall');
		Quarks.link(quark.localPath);
		quark.runHook('postInstall');
		Quarks.clearCache;
		if(Quarks.pathIsInstalled(quark.localPath).not) {
			Error("the quark % could not be added to the class library".format(quark.name)).throw
		}
	}

	// When refspec is a commit id, the checked-out commit must be it.
	*prVerifyCommit { |quark, refspec|
		var head;
		if(refspec.notNil) {
			head = DekenGit.commit(quark.localPath);
			if(head.isNil or: { head != refspec.toLower }) {
				Error("% is not at commit % (it is at %), so none of its code was run".format(quark.localPath, refspec, head)).throw
			}
		}
	}

	// ---- safety

	// SuperCollider's Git builds a shell command from the address and the
	// refspec without quoting them, so nothing else than a plain https address
	// (letters, digits and . _ ~ - / only) and a full 40-digit commit id is
	// ever handed to it. url or commit may be nil to skip that check; a
	// missing or empty commit means the default branch.
	*prCheckSafe { |url, commit|
		if(url.notNil and: { this.prSafeURL(url).not }) {
			Error("the quark address % is not a plain https://host/path address, so it is not installed".format(url)).throw
		};
		if(commit.notNil and: { commit.notEmpty and: { "^[0-9a-fA-F]{40}$".matchRegexp(commit).not } }) {
			Error("the commit % is not a 40-digit commit id, so it is not used".format(commit)).throw
		}
	}

	*prSafeURL { |url|
		var s = url.asString;
		var plain = "^https://[A-Za-z0-9._~-]+(/[A-Za-z0-9._~-]+)*/?$".matchRegexp(s)
			and: { s.split($/).any { |part| part == "." or: { part == ".." } }.not };
		^plain or: { fileURLsAllowed and: { s.beginsWith("file:///") and: { s.findRegexp("[\"'&|<>^%;$` \t]").isEmpty } } }
	}

	*prExpect { |result, what|
		if(result == false) { Error("the quark call failed: " ++ what).throw };
		^result
	}

	// ---- finding installed quarks

	*prFindByURL { |url|
		var key = this.repoKey(url);
		^Quarks.installed.detect { |q| (DekenGit.url(q.localPath) !? { |u| this.repoKey(u) }) == key }
	}

	*prFindByPath { |path|
		^Quarks.installed.detect { |q| this.prSamePath(q.localPath, path) }
	}

	// The same repository address however it is written (case, scheme,
	// trailing / or .git), like the helper's NormalizeRepo.
	*repoKey { |url|
		var s = url.asString.stripWhiteSpace.toLower;
		var scheme;
		if(s.beginsWith("git@")) { s = s.copyToEnd(4).replace(":", "/") };
		scheme = ["https://", "http://", "git://", "ssh://git@", "ssh://"].detect { |p| s.beginsWith(p) };
		if(scheme.notNil) { s = s.copyToEnd(scheme.size) };
		s = this.prTrimSlashes(s);
		if(s.endsWith(".git")) { s = this.prTrimSlashes(s.drop(-4)) };
		^s
	}

	*prTrimSlashes { |s|
		var end = s.size;
		while { end > 0 and: { s[end - 1] == $/ } } { end = end - 1 };
		^s.keep(end)
	}

	// Windows paths compare without case and with either slash.
	*prSamePath { |a, b|
		var norm = { |p|
			var s = p.asString.replace(92.asAscii.asString, "/").withoutTrailingSlash;
			if(DekenShell.isWindows) { s.toLower } { s }
		};
		^norm.value(a) == norm.value(b)
	}
}
