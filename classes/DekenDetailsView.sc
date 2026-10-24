// The narrow pane on the right of the package window: everything about the
// selected package, with the buttons that act on it.
//
// The window feeds it in steps, because the facts arrive one after another
// from the helper: `show` at once from the list row, `fill` when the package
// details arrive, `setPlan` when the install route is known. Nothing here
// talks to the helper; the buttons call the actions passed to `new`:
// install, update, remove (each gets name and version) and versionChanged.
DekenDetailsView {
	classvar <width = 280;
	var actions;
	var <view;
	var chip, nameLabel, byline, summary, inside, after, needs, versionMenu, badge, route;
	var installButton, updateButton, removeButton, repoButton, suggestButton, versionNames;
	var row, <package, <plan, busy = false;

	*new { |actions| ^super.newCopyArgs(actions).prBuild }

	// ---- feeding it

	// row is the list row: name, kind, summary, trust, retracted, installed
	// (version or nil), update (Boolean).
	show { |newRow|
		row = newRow;
		package = nil;
		plan = nil;
		chip.string = " " ++ DekenStyle.typeText(row[\kind]) ++ " ";
		chip.background = DekenStyle.typeColor(row[\kind]);
		chip.visible = true;
		nameLabel.string = row[\name];
		byline.string = "";
		summary.string = row[\summary] ? "";
		inside.string = "Loading details…";
		after.string = DekenStyle.afterInstallText(this.prStepsFor(row[\kind]));
		needs.string = "";
		versionNames = [];
		versionMenu.items = [row[\installed] ? "…"];
		route.string = "";
		this.prShowBadge;
		this.prUpdateButtons;
	}

	// info is the helper's info answer: package and retractions.
	fill { |info|
		package = info["package"];
		byline.string = DekenDetailsView.bylineText(package);
		summary.string = package["summary"];
		inside.string = DekenDetailsView.insideLines(package).join("\n");
		needs.string = DekenDetailsView.needsText(package);
		versionNames = DekenDetailsView.versionLabels(package);
		versionMenu.items = versionNames;
		row[\retractedVersions] = (info["retractions"] ? []).collect { |r| r["version"] };
		this.prShowBadge;
		this.prUpdateButtons;
	}

	// plan: the helper's plan for the selected version.
	setPlan { |newPlan|
		plan = newPlan;
		after.string = DekenStyle.afterInstallText(plan["after_install"]);
		this.prShowBadge;
		this.prUpdateButtons;
	}

	// The helper could not plan this version (for example it was withdrawn as malware).
	setPlanError { |message|
		plan = nil;
		route.string = message;
		installButton.enabled = false;
		updateButton.enabled = false;
	}

	// While a job runs on this package its buttons are off.
	setBusy { |flag|
		busy = flag;
		this.prUpdateButtons;
	}

	// The row's installed state changed (after install or remove).
	setInstalled { |version, update|
		row[\installed] = version;
		row[\update] = update;
		this.prUpdateButtons;
	}

	// "1.0.0" of "1.0.0 (latest)": nil while the package details are loading.
	selectedVersion {
		^if(versionNames.isEmpty) { nil } { package["versions"][versionMenu.value]["version"] }
	}

	// ---- what to show (pure)

	// Server plugins by name with the engines they run on, UGen class count,
	// sclang library size. package is the index entry.
	*insideLines { |pkg|
		var lines = this.pluginLines(pkg["plugin_files"] ? []);
		var ugens = pkg["ugens"] ? [];
		var library = (pkg["classes"] ? []).reject { |c| ugens.indexOfEqual(c).notNil };
		var count = library.size;
		if(ugens.notEmpty) { lines = lines ++ ["UGen classes: " ++ ugens.size] };
		lines = lines ++ [if(library.isEmpty) { "sclang library: none" } { "sclang library: % %".format(count, if(count == 1) { "class" } { "classes" }) }];
		^lines
	}

	// "shimpe · GPL-3.0"; whatever is missing is left out.
	*bylineText { |pkg|
		var authors = pkg["authors"] ? [];
		var parts = [authors.join(", "), pkg["license"] ? ""].select { |p| p.notEmpty };
		^parts.join(" · ")
	}

	// FMX6.scx + FMX6_supernova.scx -> "Server plugin: FMX6 (scsynth + supernova)".
	*pluginLines { |files|
		var engines = Dictionary.new, order = List.new;
		files.do { |f|
			var base = f.splitext[0];
			var supernova = base.endsWith("_supernova");
			if(supernova) { base = base.drop(-10) };
			if(engines[base].isNil) { engines[base] = Set.new; order.add(base) };
			engines[base].add(if(supernova) { "supernova" } { "scsynth" });
		};
		^order.collect { |b|
			var e = ["scsynth", "supernova"].select { |n| engines[b].includes(n) };
			"Server plugin: % (%)".format(b, e.join(" + "))
		}.asArray
	}

	*needsText { |pkg|
		var names = (pkg["depends"] ? Dictionary.new).keys.asArray.sort;
		^if(names.isEmpty) { "" } { "Needs: " ++ names.join(", ") }
	}

	*versionLabels { |pkg|
		^pkg["versions"].collect { |v, i|
			v["version"] ++ if(i == 0) { " (latest)" } { "" } ++ if(v["unstable"] == true) { " (unstable)" } { "" }
		}
	}

	// ---- building

	prBuild {
		busy = false;
		versionNames = [];
		chip = StaticText().align_(\center).font_(Font(size: 10, bold: true)).stringColor_(DekenStyle.chipText).maxWidth_(130).fixedHeight_(18);
		nameLabel = StaticText().font_(Font(size: 15, bold: true));
		byline = StaticText().stringColor_(Color.grey(0.45));
		summary = StaticText();
		inside = StaticText().background_(Color.white);
		after = StaticText();
		needs = StaticText();
		versionMenu = PopUpMenu().items_(["…"]).action_({ actions[\versionChanged].value(row[\name], this.selectedVersion) });
		badge = StaticText().align_(\center).fixedHeight_(20);
		route = StaticText().stringColor_(Color.grey(0.4)).font_(Font(size: 10));
		installButton = Button().states_([["Install"]]).action_({ actions[\install].value(row[\name], this.selectedVersion) });
		updateButton = Button().states_([["Update"]]).action_({ actions[\update].value(row[\name], nil) });
		removeButton = Button().states_([["Remove"]]).action_({ actions[\remove].value(row[\name]) });
		repoButton = Button().states_([["Repo ↗"]]).action_({ package !? { |p| p["repo"].openOS } });
		suggestButton = Button().states_([["Suggest for review"]]).action_({ package !? { |p| DekenStyle.suggestURL(p["repo"]).openOS } });
		view = View().fixedWidth_(width).layout_(VLayout(
			chip, nameLabel, byline, summary,
			this.prHeading("What's inside"), inside, needs,
			this.prHeading("After installing"), after,
			this.prHeading("Version"), versionMenu,
			badge, route,
			HLayout(installButton, updateButton, removeButton, repoButton),
			suggestButton,
			nil
		).margins_(8).spacing_(3)).background_(Color.grey(0.98));
		chip.visible = false;
		[installButton, updateButton, removeButton, repoButton, suggestButton, versionMenu].do { |b| b.enabled = false };
	}

	prHeading { |text|
		^StaticText().string_(text).stringColor_(Color.grey(0.45)).font_(Font(size: 10))
	}

	prStepsFor { |kind|
		^switch(kind.asString, "quark", { ["recompile"] }, { ["recompile", "reboot-server"] })
	}

	prShowBadge {
		var text = DekenStyle.badgeText(row[\trust], plan !? { |p| p["route"] }, this.prRetracted, false);
		badge.string = text;
		badge.background = DekenStyle.badgeColor(text);
		badge.stringColor = DekenStyle.badgeTextColor(text);
		route.string = plan !? { |p| p["reason"] } ? "";
	}

	// The selected version is withdrawn (before the details arrive: whatever the list says).
	prRetracted {
		var chosen = this.selectedVersion;
		var versions = row[\retractedVersions];
		^if(versions.isNil or: { chosen.isNil }) { row[\retracted] == true } { versions.indexOfEqual(chosen).notNil }
	}

	prUpdateButtons {
		var installed = row[\installed].notNil;
		var ready = busy.not and: { package.notNil };
		installButton.visible = installed.not;
		updateButton.visible = installed and: { row[\update] == true };
		removeButton.visible = installed;
		suggestButton.visible = row[\trust] == "unreviewed";
		installButton.enabled = ready;
		updateButton.enabled = ready;
		removeButton.enabled = busy.not;
		repoButton.enabled = package.notNil;
		suggestButton.enabled = package.notNil;
		versionMenu.enabled = ready and: { versionNames.size > 1 };
	}
}
