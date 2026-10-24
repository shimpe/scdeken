// The window that tells the user a package they have was withdrawn, and asks
// what to do. Nothing happens unless the user clicks a button: Remove now
// moves the package to quarantine (never deletes it), Update installs the fixed
// version, Keep remembers the choice (malware asks a second time), Remind me
// later does nothing.
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

	// ---- showing

	// One dialog after the other for each hit; when all are answered and
	// something was removed, one reminder to recompile (and reboot the server).
	*showAll { |hits|
		var steps = Set.new;
		var next = nil;
		next = { |i|
			if(i < hits.size) {
				this.show(hits[i], { |more| steps.addAll(more); next.value(i + 1) })
			} {
				if(steps.notEmpty) {
					DekenDialogs.afterInstall(["recompile", "reboot-server"].select { |s| steps.includes(s) }, nil, "Removed")
				}
			}
		};
		next.value(0)
	}

	// onDone gets the steps the user still has to take (maybe none), once the
	// hit has been dealt with.
	*show { |hit, onDone|
		var w = DekenDialogs.prWindow("A package was withdrawn", 600, 360);
		var answered = false;
		var finish = { |steps| if(answered.not) { answered = true; w.close; onDone.value(steps ? []) } };
		var buttons = Deken.prAdvisoryActions(hit).collect { |a|
			Button().states_([[this.buttonLabel(a, hit)]]).action_({ this.prAct(a, hit, finish) })
		};
		w.layout = VLayout(*(this.prViews(hit) ++ [nil, HLayout(nil, *buttons)]));
		w.onClose = { finish.value([]) };
		DekenDialogs.prShow(w, buttons[0]);
		^w
	}

	*prViews { |hit|
		var advisory = hit["advisory"];
		var title = DekenDialogs.prLabel("⛔  % was withdrawn".format(this.title(hit)));
		title.font = Font(size: 15, bold: true);
		^[
			title,
			DekenDialogs.prLabel(this.severityText(hit), true),
			DekenDialogs.prLabel(advisory["summary"]),
			Button().states_([["Read the details ↗"]]).maxWidth_(160).action_({ DekenStyle.advisoryURL(advisory["id"]).openOS }),
			DekenDialogs.prLabel("It is installed at:"),
			TextField().string_(hit["item"]["path"]).maxHeight_(24),
			DekenDialogs.prLabel(this.limitsText),
			DekenDialogs.prLabel("Nothing is deleted: removing moves it to a quarantine folder.")
		]
	}

	*prAct { |action, hit, finish|
		switch(action,
			\remove, { Deken.removeWithdrawn(hit, { |r| { this.prDone(r, "remove", finish) }.defer }) },
			\update, { Deken.update(hit["item"]["name"], { |r| { this.prDone(r, "update", finish) }.defer }) },
			\keep, { this.prKeep(hit, finish) },
			\later, { finish.value([]) }
		)
	}

	*prDone { |r, verb, finish|
		if(r.ok) { finish.value(r[\after_install]) } {
			DekenDialogs.notice("Could not " ++ verb ++ " it", r[\error] ? "Something went wrong.");
			finish.value([])
		}
	}

	*prKeep { |hit, finish|
		var keep = { Deken.keepWithdrawn(hit["advisory"]["id"], { |r| { finish.value([]) }.defer }) };
		if(Deken.prKeepNeedsConfirm(hit)) {
			DekenDialogs.ask("Keep it anyway?", this.keepAnywayText, "Keep it", keep)
		} {
			keep.value
		}
	}
}
