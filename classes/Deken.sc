// sc-deken for SuperCollider: find, install, update and remove plugins and
// quarks. Every method is asynchronous: it returns at once and calls
// `action` with a response Event when the helper has finished. Without an
// action the result is posted for people to read. See Deken.schelp.
//
// A response has ok (Boolean), data, error (String, plain words) and confirm
// (Array of what still needs the user's OK). `data` is parsed JSON:
// Dictionaries with String keys. An install that must wait for the user's
// answer has needsConfirmation: true and the list in confirm.
//
// Quarks are installed here, in sclang, with DekenQuarks, after the helper
// has planned the install. The Go helper does everything else.
Deken {
	classvar <>helperPath;  // dev override: use this scdeken instead of the downloaded one
	classvar <>indexURL;    // package list address to use instead of the default
	classvar <>logAction;   // called with each log line of a job; default posts it
	classvar <>progressAction; // called with (fraction, label); default does nothing
	classvar <>verbose = false; // post what the quiet startup check would otherwise keep to itself

	// ---- finding

	*platform { |action|
		this.prSimple((command: "platform"), action, "platform")
	}

	// kind: nil, \plugin, \quark or \pluginQuark's JSON spelling "plugin+quark".
	*search { |query, kind, includeUnreviewed = false, action|
		var done = action ? { |r| this.prPostSearch(r) };
		this.prSimple((command: "search", query: query.asString, kind: kind !? { |k| k.asString },
			include_unreviewed: includeUnreviewed), done)
	}

	*info { |name, action|
		this.prSimple((command: "info", package: name.asString), action, "info")
	}

	*list { |action|
		this.prSimple((command: "list"), action ? { |r| this.prPostList(r) })
	}

	*updates { |action|
		this.prSimple((command: "updates"), action ? { |r| this.prPostList(r) })
	}

	*doctor { |name, action|
		this.prSimple((command: "doctor", package: name !? { |n| n.asString }), action, "doctor")
	}

	// Downloads the package list again.
	*refresh { |action|
		this.prSimple((command: "refresh"), action, "refresh")
	}

	// Plans an install without doing it: data is the plan (route, doctor, confirm, depends).
	*plan { |name, version, action|
		this.prSimple((command: "plan", package: name.asString, version: version), action, "plan")
	}

	// Installed packages that an advisory has withdrawn.
	*advisories { |action|
		this.prSimple((command: "advisories-check"), action, "advisories")
	}

	// Looks for installed packages that an advisory has withdrawn. action (optional)
	// gets the helper's response, whose data is the list of hits. When interactive,
	// each hit gets a DekenAdvisoryDialog. Never posts anything unless verbose:
	// this runs at startup, where being offline is normal.
	*checkAdvisories { |interactive = true, action|
		DekenHelper.run((command: "advisories-check"), { |r|
			if(r.ok.not) { this.prQuiet(r[\error]) };
			if(interactive and: { r.ok } and: { r.data.notEmpty }) { { DekenAdvisoryDialog.showAll(r.data) }.defer };
			action.value(r)
		}, { |line| this.prQuiet(line) })
	}

	// What the dialog may offer for a hit, in the order of its buttons.
	*prAdvisoryActions { |hit|
		var fixed = hit["fixed_version"];
		^[\remove] ++ if(fixed.notNil and: { fixed.notEmpty }) { [\update] } { [] } ++ [\keep, \later]
	}

	// Keeping a package that holds malware needs a second yes.
	*prKeepNeedsConfirm { |hit|
		^hit["advisory"]["severity"] == "malware"
	}

	// Moves the withdrawn package to quarantine (nothing is deleted). A quark is
	// first taken out of the class library's include paths. action gets a
	// response whose after_install lists what the user has to do next.
	*removeWithdrawn { |hit, action|
		var item = hit["item"];
		var problem = if(item["source"] == "quark") {
			try { DekenQuarks.uninstall(item["name"]); nil } { |err|
				"Could not take the quark " ++ item["name"] ++ " out of SuperCollider: " ++ err.errorString
			}
		};
		if(problem.notNil) { ^action.value(DekenHelper.prResponse(false, nil, problem, [], 1)) };
		this.prRequest((command: "quarantine", path: item["path"], advisory_id: hit["advisory"]["id"]), { |r|
			if(r.ok) { r[\after_install] = this.prRemovalSteps(item) };
			if(r.ok.not and: { item["source"] == "quark" }) {
				// The quark is already out of the class library: say so, and ask for a recompile.
				r[\error] = "% was taken out of SuperCollider's class library (recompile to stop it loading), but its folder could not be moved and is still at %: %".format(
					item["name"], item["path"], r[\error]);
				r[\after_install] = ["recompile"]
			};
			action.value(r)
		})
	}

	// Remembers that the user wants to keep what advisoryID withdrew.
	*keepWithdrawn { |advisoryID, action|
		this.prRequest((command: "advisories-keep", advisory_id: advisoryID), action ? this.prDefaultAction("keep"))
	}

	// Removing files always needs a recompile; plugins need the server restarted too.
	*prRemovalSteps { |item|
		var kind = item["kind"].asString;
		^["recompile"] ++ if(kind.find("plugin").notNil) { ["reboot-server"] } { [] }
	}

	*prQuiet { |line|
		if(verbose) { this.prLog(line.asString) }
	}

	// Permanently deletes the packages in quarantine.
	*quarantineClear { |action|
		this.prSimple((command: "quarantine-clear"), action, "quarantine-clear")
	}

	// Opens the package manager window.
	*gui {
		^DekenGui.new.front
	}

	// ---- installing

	// Plans the install. If something needs the user's OK, action gets the
	// plan with needsConfirmation: true (and confirm lists what); otherwise
	// the package is installed straight away.
	*install { |name, version, action|
		var done = action ? { |r| this.prPostInstall(r, name, version) };
		this.prRequest((command: "plan", package: name.asString, version: version), { |planned|
			var needed;
			if(planned.ok) {
				needed = this.prNeeded(planned.data);
				if(needed.isEmpty) { this.installConfirmed(name, version, (), done) } {
					planned[\needsConfirmation] = true;
					planned[\confirm] = needed;
					done.value(planned)
				}
			} {
				done.value(planned)
			}
		})
	}

	// Installs with the user's answers in choices, an Event such as
	// (confirm_unreviewed: true, confirm_retracted: true, on_duplicate: "replace").
	// Quarks (also the quark part of plugin+quark) are installed here once the
	// helper has finished, dependencies first.
	*installConfirmed { |name, version, choices, action|
		var done = action ? { |r| this.prPostInstall(r, name, version) };
		this.prRequest((command: "install", package: name.asString, version: version, choices: choices ? ()), { |r|
			if(r.ok) { this.prInstallQuarks(r, done) } {
				if(r.confirm.notEmpty) { r[\needsConfirmation] = true };
				done.value(r)
			}
		})
	}

	// Installs the newest version. Quarks are moved to it with DekenQuarks.checkout.
	*update { |name, action|
		this.install(name, nil, action)
	}

	// Removes a package installed through sc-deken, or a quark.
	*remove { |name, action|
		var done = action ? this.prDefaultAction("remove");
		this.prRequest((command: "list"), { |listed|
			var item = if(listed.ok) { this.prFindItem(listed.data, name) };
			if(item.notNil and: { item["source"] == "quark" }) { this.prRemoveQuark(item["name"], done) } {
				this.prRequest((command: "remove", package: name.asString), done)
			}
		})
	}

	// ---- helpers for the methods above

	*prSimple { |request, action, label|
		this.prRequest(request, action ? this.prDefaultAction(label))
	}

	*prRequest { |request, onDone|
		DekenHelper.run(request, onDone, { |line| this.prLog(line) }, { |fraction, label| this.prProgress(fraction, label) })
	}

	*prLog { |line|
		if(logAction.notNil) { logAction.value(line) } { ("sc-deken: " ++ line).postln }
	}

	*prProgress { |fraction, label|
		progressAction !? { |f| f.value(fraction, label) }
	}

	// What the plan and its dependencies still ask the user.
	*prNeeded { |plan|
		var all = (plan["confirm"] ? []) ++ (plan["depends"] ? []).collect { |d| d["confirm"] ? [] }.flat;
		^all.asSet.asArray.sort
	}

	*prFindItem { |items, name|
		^items.detect { |i| i["name"].toLower == name.asString.toLower }
	}

	*prRemoveQuark { |name, done|
		var response = try {
			DekenQuarks.uninstall(name.asString);
			DekenHelper.prResponse(true, Dictionary.newFrom(["removed", name.asString]), nil, [], 0)
		} { |err|
			DekenHelper.prResponse(false, nil, err.errorString, [], 1)
		};
		done.value(response)
	}

	// Runs the quark part of every result, in the order the helper installed
	// them, then reports.
	*prInstallQuarks { |r, done|
		var problem = nil, steps = Set.new;
		r.data["results"].do { |res|
			if(problem.isNil) {
				try { this.prInstallQuark(res) } { |err| problem = err.errorString }
			};
			steps.addAll(res["plan"]["after_install"] ? [])
		};
		if(problem.notNil) {
			r[\ok] = false;
			r[\error] = "The package files are in place, but the quark could not be installed: " ++ problem;
		} {
			r[\after_install] = ["recompile", "reboot-server"].select { |s| steps.includes(s) };
			this.prAfterInstall(r[\after_install]);
		};
		done.value(r)
	}

	*prInstallQuark { |res|
		var plan = res["plan"];
		var ref = plan["quark"];
		if(ref.notNil and: { res["kept"] != true }) {
			if(plan["is_update"] == true) {
				DekenQuarks.checkout(plan["package"], ref["commit"], ref["url"])
			} {
				DekenQuarks.install(ref["url"], ref["commit"])
			}
		}
	}

	// Tells the user what to do now that files have changed.
	*prAfterInstall { |steps|
		if(steps.indexOfEqual("recompile").notNil) {
			"sc-deken: Done. Recompile the class library to use it (Language > Recompile Class Library, or Ctrl/Cmd+Shift+L).".postln
		};
		if(steps.indexOfEqual("reboot-server").notNil) {
			"sc-deken: Reboot the sound server too so it loads the new plugins (s.reboot).".postln
		}
	}

	// ---- default actions: readable posts

	*prDefaultAction { |label|
		^{ |r|
			if(r.ok) {
				"sc-deken: % finished. %".format(label, DekenJSON.stringify(r.data)).postln
			} {
				this.prPostFailure(r)
			}
		}
	}

	*prPostFailure { |r|
		("sc-deken: " ++ (r[\error] ? "something went wrong.")).postln
	}

	*prPostSearch { |r|
		if(r.ok.not) { ^this.prPostFailure(r) };
		if(r.data.isEmpty) { "sc-deken: nothing found.".postln };
		r.data.do { |h|
			"%  (%, %, latest %)  %".format(h["name"], h["kind"], h["trust"], h["latest"], h["summary"]).postln
		}
	}

	*prPostList { |r|
		if(r.ok.not) { ^this.prPostFailure(r) };
		if(r.data.isEmpty) { "sc-deken: nothing to show.".postln };
		r.data.do { |i|
			"%  %  (%)%".format(i["name"], i["version"], i["source"],
				if(i["update_available"] == true) { "  update available: " ++ i["latest"] } { "" }).postln
		}
	}

	*prPostInstall { |r, name, version|
		if(r[\needsConfirmation] == true) { ^this.prPostConfirmation(r, name, version) };
		if(r.ok) {
			"sc-deken: % installed.".format(name).postln
		} {
			this.prPostFailure(r)
		}
	}

	*prPostConfirmation { |r, name, version|
		var choices = ();
		r.confirm.do { |c|
			switch(c,
				"unreviewed", { choices[\confirm_unreviewed] = true },
				"retracted", { choices[\confirm_retracted] = true },
				"duplicates", { choices[\on_duplicate] = "replace" }
			)
		};
		"sc-deken: % needs your OK first: %. If you agree, run: Deken.installConfirmed(%, %, %)".format(
			name, r.confirm.join(", "), name.asString.asCompileString, version.asCompileString, choices.asCompileString).postln
	}
}
