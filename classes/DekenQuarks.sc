// The only place sc-deken touches the Quarks system. sclang performs quark
// installs itself (the Go helper never runs git for them). Tests can replace
// the real calls with `DekenQuarks.override_((install: { |url, commit| ... }))`;
// pass nil to restore.
DekenQuarks {
	classvar override;

	*override_ { |events| override = events }

	// Installs the quark at url, pinned to commit (nil or "" = default branch).
	*install { |url, commit|
		var refspec = if(commit.isNil or: { commit.isEmpty }) { nil } { commit };
		^if(override.notNil) { override[\install].value(url, refspec) } { Quarks.install(url, refspec) }
	}

	*uninstall { |name|
		^if(override.notNil) { override[\uninstall].value(name) } { Quarks.uninstall(name) }
	}

	// Moves an installed quark (found by name) to commit. If it is not installed
	// and a url is given, installs it from there instead.
	*checkout { |name, commit, url|
		var quark;
		if(override.notNil) { ^override[\checkout].value(name, commit) };
		quark = this.prFind(name);
		if(quark.isNil) {
			if(url.isNil) { Error("sc-deken: the quark % is not installed".format(name)).throw };
			^this.install(url, commit)
		};
		quark.refspec = commit;
		quark.checkout;
		^quark
	}

	*prFind { |nameOrUrl|
		^Quarks.installed.detect { |q| q.name.toLower == nameOrUrl.asString.toLower }
	}
}
