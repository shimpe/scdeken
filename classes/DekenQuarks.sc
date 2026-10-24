// The only place sc-deken touches the Quarks system. sclang performs quark
// installs itself (the Go helper never runs git for them).
//
// Quarks reports many failures only by posting a message: Quarks.install
// returns the Quark even when the clone failed, and Quarks.uninstall does
// nothing for a name it does not know. So every call here checks the outcome
// afterwards and throws an Error if it did not work.
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
	classvar override;

	*override_ { |events| override = events }

	// Installs the quark at url, pinned to commit (nil or "" = default branch).
	*install { |url, commit|
		var refspec = if(commit.isNil or: { commit.isEmpty }) { nil } { commit };
		var quark;
		if(override.notNil) { ^this.prExpect(override[\install].value(url, refspec), "install " ++ url) };
		quark = this.prInstallOnly(url, refspec);
		if(quark.isNil or: { Quarks.pathIsInstalled(quark.localPath).not }) {
			Error("the quark at % could not be installed (see the messages above)".format(url)).throw
		};
		this.prVerifyCommit(quark, refspec);
		^quark
	}

	// Quarks.installQuark without its dependency loop: checks the quark out at
	// refspec, runs the install hooks and adds it to the include paths.
	*prInstallOnly { |url, refspec|
		var quark = Quark(url, refspec);
		var prev = this.prFind(quark.name);
		if(prev.notNil and: { prev.localPath != quark.localPath }) {
			Error("a version of % is already installed at %".format(quark.name, prev.localPath)).throw
		};
		quark.checkout;
		if(Git.isGit(quark.localPath).not) {
			Error("the quark at % could not be downloaded".format(url)).throw
		};
		if(quark.isCompatible.not) {
			Error("% reports an incompatibility with this SuperCollider version or with other installed quarks".format(quark.name)).throw
		};
		quark.runHook('preInstall');
		Quarks.link(quark.localPath);
		quark.runHook('postInstall');
		Quarks.clearCache;
		^quark
	}

	// Uninstalls the quark called name (any capitalisation).
	*uninstall { |name|
		var quark;
		if(override.notNil) { ^this.prExpect(override[\uninstall].value(name), "uninstall " ++ name) };
		quark = this.prFind(name);
		if(quark.isNil) { Error("the quark % is not installed".format(name)).throw };
		Quarks.uninstall(quark.name);
		Quarks.clearCache;
		if(this.prFind(name).notNil) { Error("the quark % could not be uninstalled".format(name)).throw };
	}

	// Moves an installed quark (found by name) to commit. If it is not installed
	// and a url is given, installs it from there instead.
	*checkout { |name, commit, url|
		var quark;
		if(override.notNil) { ^this.prExpect(override[\checkout].value(name, commit), "checkout " ++ name) };
		quark = this.prFind(name);
		if(quark.isNil) {
			if(url.isNil) { Error("the quark % is not installed".format(name)).throw };
			^this.install(url, commit)
		};
		quark.git.fetch;
		quark.refspec = commit;
		quark.checkout;
		this.prVerifyCommit(quark, commit);
		^quark
	}

	*prExpect { |result, what|
		if(result == false) { Error("the quark call failed: " ++ what).throw };
		^result
	}

	// When refspec is a commit id, the checked-out commit must be it.
	*prVerifyCommit { |quark, refspec|
		var sha;
		if(refspec.notNil and: { "^[0-9a-fA-F]{7,40}$".matchRegexp(refspec) }) {
			sha = quark.git.sha;
			if(sha.isNil or: { sha.beginsWith(refspec.toLower).not }) {
				Error("the quark % is not at commit %".format(quark.name, refspec)).throw
			}
		}
	}

	*prFind { |name|
		^Quarks.installed.detect { |q| q.name.toLower == name.asString.toLower }
	}
}
