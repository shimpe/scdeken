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
DekenQuarks {
	classvar override;

	*override_ { |events| override = events }

	// Installs the quark at url, pinned to commit (nil or "" = default branch).
	*install { |url, commit|
		var refspec = if(commit.isNil or: { commit.isEmpty }) { nil } { commit };
		var quark;
		if(override.notNil) { ^this.prExpect(override[\install].value(url, refspec), "install " ++ url) };
		quark = Quarks.install(url, refspec);
		Quarks.clearCache;
		if(quark.isNil or: { Quarks.pathIsInstalled(quark.localPath).not }) {
			Error("the quark at % could not be installed (see the messages above)".format(url)).throw
		};
		this.prVerifyCommit(quark, refspec);
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
