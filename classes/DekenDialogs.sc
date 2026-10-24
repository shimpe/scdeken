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

	// The answers to hand to Deken.installConfirmed. duplicate is "replace" or
	// "keep". plan (optional) is the plan shown: its commits tie the answers to
	// it, so the helper asks again if the package list changed meanwhile.
	*choicesFor { |kinds, duplicate, plan|
		var choices = ();
		if(kinds.indexOfEqual("unreviewed").notNil) { choices[\confirm_unreviewed] = true };
		if(kinds.indexOfEqual("retracted").notNil) { choices[\confirm_retracted] = true };
		if(kinds.indexOfEqual("duplicates").notNil) { choices[\on_duplicate] = duplicate };
		if(plan.notNil) { choices[\commits] = Deken.planCommits(plan) };
		^choices
	}

	// The labels of the buttons that say yes. With only duplicates to decide,
	// the two answers are the buttons; with nothing (a plan that changed
	// since it was confirmed) there is a plain Install.
	*yesLabels { |kinds|
		case
		{ kinds.indexOfEqual("unreviewed").notNil } { ^["I understand, build it"] }
		{ kinds.indexOfEqual("retracted").notNil } { ^["Install anyway"] }
		{ kinds.indexOfEqual("duplicates").notNil } { ^["Replace (old copy goes to a backup)", "Keep existing"] };
		^["Install"]
	}

	// ---- confirmation

	// Shows everything the plan needs the user to agree to. action gets the
	// choices Event, or nil when the user cancels. changed: the helper said
	// the plan is not the one the user confirmed before, so say that first.
	*confirm { |plan, action, changed = false|
		var plans = this.confirmPlans(plan);
		var kinds = this.confirmKinds(plans);
		var w = this.prWindow("Please confirm", 560, (230 + (plans.size.max(1) * 110)).min(640));
		var duplicate = PopUpMenu().items_(["Replace it (the old copy is moved to a backup folder)", "Keep the existing copy"]);
		var answered = false;
		var finish = { |choices| answered = true; w.close; action.value(choices) };
		var cancel = Button().states_([["Cancel"]]).action_({ finish.value(nil) });
		var sections = if(plans.isEmpty) { [plan] } { plans };
		var canvas = View().layout_(VLayout(*(sections.collect { |p| this.prPlanSection(p) } ++ [nil])));
		var yes = this.prConfirmButtons(kinds, duplicate, plan, finish);
		var askDuplicate = kinds.indexOfEqual("duplicates").notNil and: { kinds.size > 1 };
		w.layout = VLayout(*[
			if(changed) { this.prLabel("The package list changed since you confirmed; please check again.", true) },
			this.prLabel("Nothing has been installed yet. This needs your OK first:", true),
			ScrollView().canvas_(canvas).hasBorder_(false),
			if(askDuplicate) { duplicate },
			HLayout(nil, *(yes ++ [cancel]))
		].select(_.notNil));
		w.onClose = { if(answered.not) { action.value(nil) } };
		this.prShow(w, cancel);
		^w
	}

	// Buttons that say yes (see yesLabels). The duplicates answer comes from
	// the menu, unless the two answers are the buttons themselves.
	*prConfirmButtons { |kinds, duplicate, plan, finish|
		var labels = this.yesLabels(kinds);
		var fromMenu = { ["replace", "keep"][duplicate.value] };
		var answers = if(labels.size == 2) { ["replace", "keep"] } { [fromMenu] };
		^labels.collect { |label, i|
			Button().states_([[label]]).action_({ finish.value(this.choicesFor(kinds, answers[i].value, plan)) })
		}
	}

	*prPlanSection { |plan|
		var items = [this.prLabel("%  %".format(plan["package"], plan["version"]), true)];
		(plan["confirm"] ? []).do { |kind|
			items = items ++ switch(kind,
				"unreviewed", { [this.prLabel(this.unreviewedText(plan))] },
				"retracted", { this.prRetractedViews(plan) },
				"duplicates", { this.prDuplicateViews(plan) },
				{ [] }
			)
		};
		^View().layout_(VLayout(*items).margins_(6)).background_(Color.grey(0.94))
	}

	// Names the repository and commit the package will be taken from. A
	// dependency known only by its address has no commit: its newest one is
	// taken.
	*unreviewedText { |plan|
		var repo = plan["repo"] ? "its repository";
		var commit = plan["commit"].asString;
		var where = if(commit.isEmpty) { "its newest commit (not pinned to a reviewed version)" } { "commit " ++ commit.keep(10) };
		^"Nobody has reviewed this package. It will be taken from % at % and run on your computer. Only go on if you trust its author.".format(
			repo, where)
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
	*afterInstall { |steps, recompile, title = "Installed"|
		var needsRecompile = steps.indexOfEqual("recompile").notNil;
		var w = this.prWindow(title, 460, 190);
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
	// onCancel (optional) is called when the user says no or closes the window.
	*ask { |title, text, okLabel, action, onCancel|
		var w = this.prWindow(title, 460, 170);
		var agreed = false;
		var cancel = Button().states_([["Cancel"]]).action_({ w.close });
		w.layout = VLayout(this.prLabel(text), nil, HLayout(nil,
			Button().states_([[okLabel]]).action_({ agreed = true; w.close; action.value }), cancel));
		w.onClose = { if(agreed.not) { onCancel.value } };
		this.prShow(w, cancel);
		^w
	}

	// ---- after a removal

	// moves: one line per package ("X moved to <folder>"). steps: what the
	// helper's removal asks for next (recompile, reboot-server).
	*afterRemoval { |moves, steps, recompile|
		var needsRecompile = steps.indexOfEqual("recompile").notNil;
		var w = this.prWindow("Removed", 560, 190 + (moves.size * 22));
		var later = Button().states_([[if(needsRecompile) { "Later" } { "OK" }]]).action_({ w.close });
		var views = [this.prLabel("Done. " ++ DekenStyle.afterInstallText(steps) ++ ".", true)]
			++ moves.collect { |m| this.prLabel(m) };
		if(steps.indexOfEqual("reboot-server").notNil) {
			views = views ++ [this.prLabel("Reboot the sound server (s.reboot) so the removed plugin is no longer loaded.")]
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

	// A message with one OK button.
	*notice { |title, text|
		var w = this.prWindow(title, 460, 170);
		var ok = Button().states_([["OK"]]).action_({ w.close });
		w.layout = VLayout(this.prLabel(text), nil, HLayout(nil, ok));
		this.prShow(w, ok);
		^w
	}

	// A local build that failed: the message, a button that opens a pre-filled
	// issue on the recipes repository (see DekenStyle.buildFailureIssueURL),
	// and OK.
	*buildFailed { |title, text, issueURL|
		var w = this.prWindow(title, 520, 200);
		var ok = Button().states_([["OK"]]).action_({ w.close });
		var report = Button().states_([["Report this problem ↗"]]).action_({ issueURL.openOS });
		w.layout = VLayout(this.prLabel(text), nil, HLayout(nil, report, ok));
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
