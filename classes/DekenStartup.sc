// Checks for withdrawn packages once sclang has started.
//
// It only runs when the helper is already on this computer: a check must never
// download anything behind the user's back. The check itself is asynchronous,
// so starting SuperCollider is never held up. Offline or any error: silent
// (unless Deken.verbose). Switched off by the "check for withdrawn packages"
// setting, which is on by default.
DekenStartup {
	*initClass {
		Class.initClassTree(StartUp);
		StartUp.add { this.run }
	}

	*run {
		if(DekenSettings.get(\checkAdvisories) == false or: { DekenHelper.isInstalled.not }) { ^this };
		try { Deken.checkAdvisories(true) } { |err| Deken.prQuiet(err.errorString) }
	}
}
