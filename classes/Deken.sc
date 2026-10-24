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
			try { DekenQuarks.uninstall(item["path"]); nil } { |err|
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
				if(needed.isEmpty) { this.installConfirmed(name, version, (commits: this.planCommits(planned.data)), done) } {
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
	// (confirm_unreviewed: true, confirm_retracted: true, on_duplicate: "replace",
	// commits: Deken.planCommits(plan)). With commits, the helper installs only
	// if the plan is still the one shown; otherwise it asks again ("changed").
	// Quarks (also the quark part of plugin+quark) are installed here once the
	// helper has finished, dependencies first.
	*installConfirmed { |name, version, choices, action|
		var done = action ? { |r| this.prPostInstall(r, name, version) };
		this.prInstallRequest((command: "install", package: name.asString, version: version, choices: choices ? ()), done)
	}

	// Installs the newest version of name; without a name, of every package
	// that has an update. Quarks are moved to it with DekenQuarks.update.
	// Updating everything asks first (needsConfirmation, data["plans"]) when
	// any of the packages needs the user's OK; answer with updateConfirmed.
	*update { |name, action|
		if(name.notNil) { ^this.install(name, nil, action) };
		this.updateConfirmed((), action)
	}

	// Updates every package that has an update, with the user's answers (see
	// installConfirmed).
	*updateConfirmed { |choices, action|
		var done = action ? { |r| this.prPostUpdates(r) };
		this.prInstallRequest((command: "update", choices: choices ? ()), done)
	}

	// The commit of every package in a plan and its dependencies (or in each
	// of the plans of an update answer), keyed by package name: what the user
	// is shown, for the choices' commits.
	*planCommits { |plan|
		var commits = Dictionary.new;
		var add = nil;
		add = { |p|
			commits[p["package"]] = p["commit"] ? "";
			(p["depends"] ? []).do(add)
		};
		if(plan["plans"].notNil) { plan["plans"].do(add) } { add.value(plan) };
		^commits
	}

	// Removes a package installed through sc-deken, or a quark.
	*remove { |name, action|
		var done = action ? this.prDefaultAction("remove");
		this.prRequest((command: "list"), { |listed|
			var item = if(listed.ok) { this.prFindItem(listed.data, name) };
			if(item.notNil and: { item["source"] == "quark" }) { this.prRemoveQuark(item, done) } {
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

	// Runs an install or update request and then the quark part of its results.
	*prInstallRequest { |request, done|
		this.prRequest(request, { |r|
			if(r.ok) { this.prInstallQuarks(r, done) } {
				if(r.confirm.notEmpty) { r[\needsConfirmation] = true };
				done.value(r)
			}
		})
	}

	// The installed item called name: its package name, or a quark's own name.
	*prFindItem { |items, name|
		var wanted = name.asString.toLower;
		^items.detect { |i| i["name"].toLower == wanted or: { (i["quark_name"] ? "").toLower == wanted } }
	}

	// Uninstalls the quark of a list item, found by its folder.
	*prRemoveQuark { |item, done|
		var response = try {
			DekenQuarks.uninstall(item["path"]);
			DekenHelper.prResponse(true, Dictionary.newFrom(["removed", item["name"]]), nil, [], 0)
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
				DekenQuarks.update(ref["url"], ref["commit"])
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
		this.prPostAsk(r, name, "Deken.installConfirmed(%, %, %)".format(
			name.asString.asCompileString, version.asCompileString, this.prSuggestedChoices(r).asCompileString))
	}

	*prPostUpdates { |r|
		var count;
		if(r[\needsConfirmation] == true) {
			^this.prPostAsk(r, "Updating", "Deken.updateConfirmed(%)".format(this.prSuggestedChoices(r).asCompileString))
		};
		if(r.ok.not) { ^this.prPostFailure(r) };
		count = r.data["results"].size;
		if(count == 0) { "sc-deken: everything is up to date.".postln } { "sc-deken: % packages updated.".format(count).postln }
	}

	// Posts what still needs the user's OK and the call that gives it.
	*prPostAsk { |r, what, call|
		var kinds = r.confirm.reject { |c| c == "changed" };
		if(kinds.size < r.confirm.size) { ("sc-deken: " ++ r[\error]).postln };
		"sc-deken: % needs your OK%. If you agree, run: %".format(
			what, if(kinds.isEmpty) { "" } { " first: " ++ kinds.join(", ") }, call).postln
	}

	// Choices that say yes to everything r asks, for the plan r carries.
	*prSuggestedChoices { |r|
		var choices = (commits: this.planCommits(r.data));
		r.confirm.do { |c|
			switch(c,
				"unreviewed", { choices[\confirm_unreviewed] = true },
				"retracted", { choices[\confirm_retracted] = true },
				"duplicates", { choices[\on_duplicate] = "replace" }
			)
		};
		^choices
	}
}
