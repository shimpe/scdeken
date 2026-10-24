// Writes the session description that the scdeken helper reads (Go type
// scenv.Context): where SuperCollider keeps things, which quarks are installed
// and which OSC port the language listens on.
//
// Quark API verified on SuperCollider 3.14.1: Quarks.installed lists a Quark
// for every LanguageConfig.includePaths entry (so the class library folder
// shows up too). Quark has name, url (nil if unknown), localPath, and git
// (a Git, or nil when the folder is not a git checkout); Git:sha runs
// `git rev-parse HEAD`. A quark without git, or a failing git, gives no commit.
// Dictionary entries whose value is nil are dropped, so unknown fields are
// simply absent from the JSON (the helper reads them as empty).
DekenContext {
	*write { |path, job|
		var file = File(path, "w");
		protect { file.write(DekenJSON.stringify(this.prCollect(job))) } { file.close };
		^path
	}

	*prCollect { |job|
		^(
			sc_version: Main.version,
			sc_arch: Platform.architecture.asString,
			extension_dir: Platform.userExtensionDir,
			system_extension_dir: Platform.systemExtensionDir,
			app_support_dir: Platform.userAppSupportDir,
			include_paths: LanguageConfig.includePaths,
			quarks: Quarks.installed.collect { |q| this.prQuark(q) },
			osc_port: NetAddr.langPort,
			job: job
		)
	}

	*prQuark { |q|
		^(name: q.name, url: q.url, path: q.localPath, commit: this.prCommit(q))
	}

	*prCommit { |q|
		^try { q.git !? { |g| g.sha } }
	}
}
