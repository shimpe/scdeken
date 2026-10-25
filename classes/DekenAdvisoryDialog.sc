// The window that tells the user a package they have was withdrawn, and asks
// what to do. Nothing happens unless the user clicks a button: Remove now
// moves the package to quarantine (never deletes it), Update installs the fixed
// version, Keep remembers the choice (malware asks a second time), Remind me
// later does nothing.
//
// While a job runs every button is disabled, so a second click cannot start
// a second job. A job that fails leaves the window open with the reason.
DekenAdvisoryDialog {
	// ---- what the window says (pure, tested)

	*title { |hit|
		var item = hit["item"];
		var version = if(item["version"].asString.notEmpty) { item["version"] } { item["commit"].asString.keep(10) };
		^"%  %".format(item["name"], version).stripWhiteSpace
	}

	*severityText { |hit|
		var severity = hit["advisory"]["severity"];
		^"Severity: " ++ severity ++ switch(severity,
			"malware", { " — it contains malicious code." },
			"vulnerable", { " — it has a serious security problem." },
			{ " — it is known to be broken." })
	}

	*limitsText {
		^"Removing it stops it from running again. Anything it already did while SuperCollider was running can't be undone by removal — see the details link."
	}

	*keepAnywayText {
		^"This package was withdrawn because it contains malware. Keep it anyway?"
	}

	*buttonLabel { |action, hit|
		^switch(action,
			\remove, { "Remove now" },
			\update, { "Update to " ++ hit["fixed_version"] },
			\keep, { "Keep" },
			\later, { "Remind me later" })
	}

	// Remove now is the recommended choice for malware and vulnerable, not for broken.
	*removeIsRecommended { |hit|
		^hit["advisory"]["severity"] != "broken"
	}

	// The advisory's own page, or ours when it gave none.
	*detailsURL { |hit|
		var details = (hit["advisory"]["details"] ? "").asString;
		^if(details.isEmpty) { DekenStyle.advisoryURL(hit["advisory"]["id"]) } { details }
	}

	// ---- showing

	// One dialog after the other for each hit; when all are answered, one
	// closing dialog for what was removed and one for what was updated.
	// Nothing takes keyboard focus unless focus is true: at startup the user
	// may be typing somewhere else.
	*showAll { |hits, focus = false|
		var outcomes = List.new;
		var next = nil;
		next = { |i|
			if(i < hits.size) {
				this.show(hits[i], { |o| outcomes.add(o); next.value(i + 1) }, focus)
			} {
				this.prFinish(outcomes)
			}
		};
		next.value(0)
	}

	*prFinish { |outcomes|
		var removed = outcomes.select { |o| o[\kind] == \removed };
		var updated = outcomes.select { |o| o[\kind] == \updated };
		var stepsOf = { |list| ["recompile", "reboot-server"].select { |s| list.any { |o| o[\steps].indexOfEqual(s).notNil } } };
		if(removed.notEmpty) { DekenDialogs.afterRemoval(removed.collect { |o| o[\line] }, stepsOf.value(removed)) };
		if(updated.notEmpty) { DekenDialogs.afterInstall(stepsOf.value(updated)) }
	}

	// onDone gets an Event (kind: \removed | \updated | \kept | \none, steps, line)
	// once the hit has been dealt with. Returns an Event with the window and
	// act, which a click on a button calls with the action's name.
	*show { |hit, onDone, focus = false|
		var w = DekenDialogs.prWindow("A package was withdrawn", 600, 400);
		var busy = false, answered = false;
		var status = DekenDialogs.prLabel("");
		var buttons, act;
		var finish = { |kind, steps, line|
			if(answered.not) { answered = true; w.close; onDone.value((kind: kind ? \none, steps: steps ? [], line: line)) }
		};
		var setBusy = { |on, text| busy = on; buttons.do { |b| b.enabled = on.not }; status.string = text ? "" };
		// The job ended without finishing the dialog: say why, offer the buttons again.
		var back = { |problem|
			if(problem.notNil) { DekenDialogs.notice("That did not work", problem) };
			if(w.isClosed) { finish.value } { setBusy.value(false) }
		};
		var actions = Deken.prAdvisoryActions(hit);
		act = { |action|
			if(busy.not) {
				switch(action,
					\remove, { setBusy.value(true, "Working…"); Deken.removeWithdrawn(hit, { |r| { this.prRemoved(hit, r, finish, back) }.defer }) },
					\update, { setBusy.value(true, "Updating…"); this.prUpdate(hit, finish, back) },
					\keep, { setBusy.value(true, "Working…"); this.prKeep(hit, finish, back, { setBusy.value(true, "Waiting for your answer…") }) },
					\later, { finish.value(\none) }
				)
			}
		};
		buttons = actions.collect { |a| Button().states_([[this.buttonLabel(a, hit)]]).action_({ act.value(a) }) };
		if(this.removeIsRecommended(hit)) { buttons[0].font = Font(size: 12, bold: true) };
		w.layout = VLayout(*(this.prViews(hit) ++ [status, nil, HLayout(nil, *buttons)]));
		// Closing the window while a job runs must not lose the job's answer.
		w.onClose = { if(busy.not) { finish.value(\none) } };
		w.front;
		if(focus and: { this.removeIsRecommended(hit) }) { buttons[0].focus(true) };
		^(window: w, act: act, isBusy: { busy })
	}

	*prViews { |hit|
		var advisory = hit["advisory"];
		var title = DekenDialogs.prLabel("⛔  % was withdrawn".format(this.title(hit)));
		title.font = Font(size: 15, bold: true);
		^[
			title,
			DekenDialogs.prLabel(this.severityText(hit), true),
			DekenDialogs.prLabel(advisory["summary"]),
			Button().states_([["Read the details ↗"]]).maxWidth_(160).action_({ this.detailsURL(hit).openOS }),
			DekenDialogs.prLabel("It is installed at:"),
			TextField().string_(hit["item"]["path"]).maxHeight_(24),
			DekenDialogs.prLabel(this.limitsText),
			DekenDialogs.prLabel("Nothing is deleted: removing moves it to a quarantine folder, where it cannot load.")
		]
	}

	*prRemoved { |hit, r, finish, back|
		var item = hit["item"];
		var steps = r[\after_install] ? [];
		if(r.ok) {
			^finish.value(\removed, steps, "% moved to %".format(item["name"], r.data["moved_to"]))
		};
		if(steps.isEmpty) { ^back.value(r[\error] ? "Something went wrong.") };
		// The quark was already taken out of the class library: go on to the recompile.
		DekenDialogs.notice("Only half removed", r[\error]);
		finish.value(\removed, steps, "% is still at %".format(item["name"], item["path"]))
	}

	// Installs the fixed version. If that needs the user's OK, shows the
	// confirmation; cancelling it brings the buttons back.
	*prUpdate { |hit, finish, back|
		this.update(hit, { |plan, answer| { DekenDialogs.confirm(plan, answer) }.defer }, { |r|
			{
				if(r.isNil) { back.value(nil) } {
					if(r.ok) { finish.value(\updated, r[\after_install] ? []) } { back.value(r[\error] ? "Something went wrong.") }
				}
			}.defer
		})
	}

	// Installs hit's fixed_version, not "the newest". askConfirm gets the plan
	// and a function to call with the user's choices (nil = cancelled) when the
	// install needs an OK. done gets the response, or nil after a cancel. Nothing
	// is installed before the answer.
	*update { |hit, askConfirm, done|
		var name = hit["item"]["name"];
		var fixed = hit["fixed_version"];
		Deken.install(name, fixed, { |r|
			if(r[\needsConfirmation] == true) {
				askConfirm.value(r.data, { |choices|
					if(choices.isNil) { done.value(nil) } { Deken.installConfirmed(name, fixed, choices, done) }
				})
			} {
				done.value(r)
			}
		})
	}

	*prKeep { |hit, finish, back, waiting|
		var keep = {
			Deken.keepWithdrawn(hit["advisory"]["id"], { |r|
				{ if(r.ok) { finish.value(\kept) } { back.value(r[\error] ? "Something went wrong.") } }.defer
			})
		};
		if(Deken.prKeepNeedsConfirm(hit)) {
			waiting.value;
			DekenDialogs.ask("Keep it anyway?", this.keepAnywayText, "Keep it", keep, { back.value(nil) })
		} {
			keep.value
		}
	}
}
