// The small windows of the package manager: doctor, the confirmation for
// unreviewed / withdrawn / duplicate packages, after-install, and a plain
// "are you sure". Each is a window of its own; the answer arrives through the
// action you pass. Cancel is always the default button.
DekenDialogs {
	// ---- what the confirmation has to cover (pure, tested)

	// The plan and its dependencies (all levels), in install order, that
	// still need an answer.
	*confirmPlans { |plan|
		var all = ((plan["depends"] ? []).collect { |d| this.confirmPlans(d) }.flatten) ++ [plan];
		^all.select { |p| (p["confirm"] ? []).notEmpty }
	}

	// The kinds of confirmation over all those plans, each once.
	*confirmKinds { |plans|
		var kinds = List.new;
		plans.do { |p| p["confirm"].do { |k| if(kinds.indexOfEqual(k).isNil) { kinds.add(k) } } };
		^kinds.asArray
	}

	// The answers to hand to Deken.installConfirmed. duplicate is "replace" or "keep".
	*choicesFor { |kinds, duplicate|
		var choices = ();
		if(kinds.indexOfEqual("unreviewed").notNil) { choices[\confirm_unreviewed] = true };
		if(kinds.indexOfEqual("retracted").notNil) { choices[\confirm_retracted] = true };
		if(kinds.indexOfEqual("duplicates").notNil) { choices[\on_duplicate] = duplicate };
		^choices
	}

	// ---- confirmation

	// Shows everything the plan needs the user to agree to. repos maps a
	// package name to its repository address (the plan itself carries only the
	// commit). action gets the choices Event, or nil when the user cancels.
	*confirm { |plan, repos, action|
		var plans = this.confirmPlans(plan);
		var kinds = this.confirmKinds(plans);
		var w = this.prWindow("Please confirm", 560, (230 + (plans.size * 110)).min(640));
		var duplicate = PopUpMenu().items_(["Replace it (the old copy is moved to a backup folder)", "Keep the existing copy"]);
		var answered = false;
		var finish = { |choices| answered = true; w.close; action.value(choices) };
		var cancel = Button().states_([["Cancel"]]).action_({ finish.value(nil) });
		var canvas = View().layout_(VLayout(*(plans.collect { |p| this.prPlanSection(p, repos) } ++ [nil])));
		var yes = this.prConfirmButtons(kinds, duplicate, finish);
		var askDuplicate = kinds.indexOfEqual("duplicates").notNil and: { kinds.size > 1 };
		w.layout = VLayout(
			this.prLabel("Nothing has been installed yet. This needs your OK first:", true),
			ScrollView().canvas_(canvas).hasBorder_(false),
			if(askDuplicate) { duplicate } { nil },
			HLayout(nil, *(yes ++ [cancel]))
		);
		w.onClose = { if(answered.not) { action.value(nil) } };
		this.prShow(w, cancel);
		^w
	}

	// Buttons that say yes. With only duplicates to decide, the two answers are the buttons.
	*prConfirmButtons { |kinds, duplicate, finish|
		var yes = { |label, choice|
			Button().states_([[label]]).action_({ finish.value(this.choicesFor(kinds, choice.value)) })
		};
		var fromMenu = { ["replace", "keep"][duplicate.value] };
		if(kinds.indexOfEqual("unreviewed").notNil) { ^[yes.value("I understand, build it", fromMenu)] };
		if(kinds.indexOfEqual("retracted").notNil) { ^[yes.value("Install anyway", fromMenu)] };
		^[yes.value("Replace (old copy goes to a backup)", "replace"), yes.value("Keep existing", "keep")]
	}

	*prPlanSection { |plan, repos|
		var items = [this.prLabel("%  %".format(plan["package"], plan["version"]), true)];
		(plan["confirm"] ? []).do { |kind|
			items = items ++ switch(kind,
				"unreviewed", { [this.prUnreviewedText(plan, repos)] },
				"retracted", { this.prRetractedViews(plan) },
				"duplicates", { this.prDuplicateViews(plan) },
				{ [] }
			)
		};
		^View().layout_(VLayout(*items).margins_(6)).background_(Color.grey(0.94))
	}

	*prUnreviewedText { |plan, repos|
		var quarkURL = plan["quark"] !? { |q| q["url"] };
		var repo = (repos !? { |r| r[plan["package"]] }) ? quarkURL ? "its repository";
		^this.prLabel("Nobody has reviewed this package. It will be taken from % at commit % and run on your computer. Only go on if you trust its author.".format(
			repo, plan["commit"].asString.keep(10)))
	}

	*prRetractedViews { |plan|
		var r = plan["retraction"];
		if(r.isNil) { ^[this.prLabel("This version was withdrawn.")] };
		^[
			this.prLabel("This version was withdrawn (%): %".format(r["severity"], r["summary"])),
			Button().states_([["Read the details ↗"]]).maxWidth_(160).action_({ DekenStyle.advisoryURL(r["id"]).openOS })
		]
	}

	*prDuplicateViews { |plan|
		var found = (plan["duplicates"] ? []).collect { |d| this.prLabel("    " ++ d["path"]) };
		^[this.prLabel("A copy is already on your computer:")] ++ found
	}

	// ---- doctor

	// report is the helper's doctor report. onCheckAgain gets the dialog
	// (call dialog[\show].value(newReport) when you have it); onContinue is
	// offered once nothing is missing. Returns an Event with window and show.
	*doctor { |report, onCheckAgain, onContinue|
		var w = this.prWindow("What this computer needs", 560, 380);
		var list = View();
		var summary = this.prLabel("", true);
		var again = Button().states_([["Check again"]]);
		var close = Button().states_([["Close"]]).action_({ w.close });
		var go = Button().states_([["Continue"]]);
		var dialog = ();
		dialog[\window] = w;
		dialog[\show] = { |r|
			list.removeAll;
			list.layout = VLayout(*(this.prDoctorRows(r) ++ [nil])).margins_(0);
			summary.string = if(r["ok"] == true) { "Everything needed is here." } { "Some tools are missing. Install them, then check again." };
			go.visible = (r["ok"] == true) and: { onContinue.notNil };
			again.enabled = true;
		};
		again.action = { again.enabled = false; onCheckAgain.value(dialog) };
		go.action = { w.close; onContinue.value };
		w.layout = VLayout(summary, ScrollView().canvas_(list).hasBorder_(false), HLayout(nil, again, go, close));
		dialog[\show].value(report);
		this.prShow(w, close);
		^dialog
	}

	*prDoctorRows { |report|
		var items = (report !? { |r| r["items"] }) ? [];
		^items.reject { |i| i["found"] == true }.collect { |i| this.prDoctorRow(i) }
	}

	*prDoctorRow { |item|
		var what = if(item["verified"] == true) { "is missing" } { "could not be checked" };
		var rows = [this.prLabel("% %, needed for %".format(item["label"], what, item["needed_for"]), true)];
		if(item["hint"].notNil and: { item["hint"].notEmpty }) {
			rows = rows ++ [HLayout(
				TextField().string_(item["hint"]).maxHeight_(24),
				Button().states_([["Copy"]]).maxWidth_(60).action_({ this.copyToClipboard(item["hint"]) })
			)]
		};
		^View().layout_(VLayout(*rows).margins_(6)).background_(Color.grey(0.94))
	}

	// ---- after install

	// steps: the helper's after_install list. recompile is called for "Recompile now".
	*afterInstall { |steps, recompile|
		var needsRecompile = steps.indexOfEqual("recompile").notNil;
		var w = this.prWindow("Installed", 460, 190);
		var later = Button().states_([[if(needsRecompile) { "Later" } { "OK" }]]).action_({ w.close });
		var views = [this.prLabel("Done. " ++ DekenStyle.afterInstallText(steps) ++ ".", true)];
		if(steps.indexOfEqual("reboot-server").notNil) {
			views = views ++ [this.prLabel("The sound server has to be rebooted too (s.reboot) so it loads the new plugins.")]
		};
		if(needsRecompile) {
			views = views ++ [this.prLabel("Recompiling restarts the class library and closes this window.")]
		};
		w.layout = VLayout(*(views ++ [nil, HLayout(nil,
			if(needsRecompile) {
				Button().states_([["Recompile class library now"]]).action_({ w.close; (recompile ? { thisProcess.recompile }).value })
			} { nil },
			later)]));
		this.prShow(w, later);
		^w
	}

	// ---- plain question

	// action is called only when the user agrees. Cancel is the default.
	*ask { |title, text, okLabel, action|
		var w = this.prWindow(title, 460, 170);
		var cancel = Button().states_([["Cancel"]]).action_({ w.close });
		w.layout = VLayout(this.prLabel(text), nil, HLayout(nil,
			Button().states_([[okLabel]]).action_({ w.close; action.value }), cancel));
		this.prShow(w, cancel);
		^w
	}

	// A message with one OK button.
	*notice { |title, text|
		var w = this.prWindow(title, 460, 170);
		var ok = Button().states_([["OK"]]).action_({ w.close });
		w.layout = VLayout(this.prLabel(text), nil, HLayout(nil, ok));
		this.prShow(w, ok);
		^w
	}

	// ---- clipboard

	// There is no clipboard in sclang, so the platform's own tool is used.
	*copyToClipboard { |text|
		var file = Platform.defaultTempDir +/+ "deken-clipboard.txt";
		var tool = case { DekenShell.isWindows } { "clip" } { thisProcess.platform.name == \osx } { "pbcopy" } { "xclip -selection clipboard" };
		var f = File(file, "w");
		protect { f.write(text) } { f.close };
		(tool ++ " < " ++ DekenShell.quote(file)).unixCmd({ |exit| File.delete(file) }, false)
	}

	// ---- shared bits

	*prWindow { |title, width, height|
		var screen = Window.screenBounds;
		var w = Window(title, Rect((screen.width - width) / 2, (screen.height - height) / 2, width, height), resizable: false);
		w.alwaysOnTop = true;
		^w
	}

	*prShow { |w, defaultButton|
		w.front;
		defaultButton.focus(true)
	}

	*prLabel { |text, bold = false|
		var label = StaticText().string_(text);
		if(bold) { label.font = Font(size: 12, bold: true) };
		^label
	}
}
