// The package manager window (Deken.gui): a sidebar (Browse, Installed,
// Updates, Build log, Settings), a list with type chip, name, summary, status
// and match note on every row, and the details pane on the right.
//
// Everything that takes time is asked of Deken, which answers later, so the
// window never waits. While a package has a job running its row says
// "working…" and its buttons are off. Rows are TreeView items (one add call
// and a few color calls each), filled a few dozen at a time so a long list
// does not freeze the window.
DekenGui {
	classvar kinds;           // filter chips: label and the helper's kind
	classvar <columnWidths;   // type, name, status; the summary gets the rest
	var <window, <sidebar, stack, <details, <search, <kindButtons, <unreviewedBox, <tree, logView, statusLabel, activity;
	var <rows, items, generation = 0, searchToken = 0, kind, page = 0, selectedName, selectedRow;
	var <installed, catalog, busy, log, jobPlans;

	*initClass {
		kinds = [["All", nil], ["Plugins", "plugin"], ["Quarks", "quark"], ["Plugin + Quark", "plugin+quark"]];
		columnWidths = [112, 130, 165];
	}

	*new { ^super.new.init }

	front {
		window.front;
		^this
	}

	init {
		rows = []; items = []; installed = Dictionary.new; catalog = Dictionary.new;
		busy = Set.new; jobPlans = Dictionary.new;
		if(DekenSettings.get(\helperPath).notEmpty) { Deken.helperPath = DekenSettings.get(\helperPath) };
		this.prBuildWindow;
		log.hook({ |fraction, label| activity.string = "% %%".format(label, (fraction * 100).round.asInteger) });
		this.prStart;
	}

	// ---- starting up: one helper call after the other

	prStart {
		this.prThen([
			{ |next| Deken.platform({ |r| { this.prShowPlatform(r); next.value }.defer }) },
			{ |next| this.prLoadCatalog(next) },
			{ |next| this.prReloadInstalled(next) },
			{ |next| this.prSearch(next) },
			{ |next| this.prCheckAdvisories(next) }
		])
	}

	prThen { |steps|
		var go = nil;
		go = { |i| if(i < steps.size) { steps[i].value({ go.value(i + 1) }) } };
		go.value(0)
	}

	prShowPlatform { |r|
		statusLabel.string = if(r.ok) { DekenStyle.statusBarText(r.data, this.prEngines) } { r.error };
	}

	prEngines {
		var supernova = Platform.resourceDir +/+ "supernova" ++ if(DekenShell.isWindows) { ".exe" } { "" };
		^if(File.exists(supernova)) { "scsynth+supernova" } { "scsynth" }
	}

	// Learns every package once, so installed packages can show a summary too.
	prLoadCatalog { |next|
		Deken.search("", nil, true, { |r|
			{
				if(r.ok) { r.data.do { |hit| catalog[hit["name"].toLower] = hit } };
				next.value
			}.defer
		})
	}

	prCheckAdvisories { |next|
		if(DekenSettings.get(\checkAdvisories).not) { ^next.value };
		Deken.advisories({ |r|
			{
				if(r.ok and: { r.data.notEmpty }) {
					DekenDialogs.notice("Withdrawn packages",
						"% of the packages installed on this computer were withdrawn. Open Installed to see them (marked retracted).".format(r.data.size))
				};
				next.value
			}.defer
		})
	}

	// ---- building the window

	prBuildWindow {
		var screen = Window.screenBounds;
		window = Window("Packages", Rect((screen.width - 1320) / 2, (screen.height - 660) / 2, 1320, 660));
		details = DekenDetailsView((
			install: { |name, version| this.prInstall(name, version) },
			update: { |name| this.prRun(name, nil, "Updating") },
			remove: { |name| this.prAskRemove(name) },
			versionChanged: { this.prPlanSelected }
		));
		sidebar = ListView().items_(this.prSidebarItems).fixedWidth_(130).value_(0).action_({ |v| this.prShowPage(v.value) });
		stack = StackLayout(this.prListPage, this.prLogPage, this.prSettingsPage);
		statusLabel = StaticText().string_(DekenStyle.statusBarText(nil, "")).stringColor_(Color.grey(0.35)).font_(Font.monospace(11));
		activity = StaticText().align_(\right).stringColor_(Color.grey(0.35));
		window.layout = VLayout(
			HLayout(sidebar, stack, details.view).spacing_(8),
			HLayout(statusLabel, activity)
		).margins_(8).spacing_(4);
		tree.onResize = { this.prFitColumns };
		window.onClose = { log.unhook };
		this.prFitColumns;
	}

	prListPage {
		search = TextField().string_("").keyUpAction_({ this.prSearchSoon });
		unreviewedBox = CheckBox().string_("Show unreviewed").value_(DekenSettings.get(\showUnreviewed))
			.action_({ this.prRefreshList });
		kindButtons = kinds.collect { |pair, i|
			Button().states_([[pair[0], Color.black, Color.grey(0.93)], [pair[0], Color.black, Color.new255(207, 220, 247)]])
				.value_(if(i == 0) { 1 } { 0 }).maxHeight_(24).action_({ this.prPickKind(i) })
		};
		tree = TreeView().columns_(["Type", "Name", "Summary", "Status"]).minWidth_(480);
		tree.onItemChanged = { this.prSelect };
		tree.setProperty(\indentation, 0);
		^View().layout_(VLayout(
			HLayout(search, unreviewedBox),
			HLayout(*(kindButtons ++ [nil])),
			tree
		).margins_(0).spacing_(6))
	}

	prLogPage {
		log = DekenLogView({ window.isClosed.not });
		^log.view
	}

	prSettingsPage {
		^DekenSettingsView((
			refresh: { this.prRefreshPackages },
			clearQuarantine: { this.prAskClearQuarantine }
		)).view
	}

	prSidebarItems {
		var updates = installed.select { |i| i["update_available"] == true }.size;
		^["Browse", "Installed (%)".format(installed.size), "Updates (%)".format(updates), "Build log", "Settings"]
	}

	prUpdateCounts { sidebar.items = this.prSidebarItems }

	// Summary column takes what is left.
	prFitColumns {
		var rest = (tree.bounds.width - columnWidths.sum - 24).max(120).asInteger;
		[columnWidths[0], columnWidths[1], rest, columnWidths[2]].do { |w, i| tree.setColumnWidth(i, w) }
	}

	// ---- pages, search, filters

	prShowPage { |index|
		page = index;
		stack.index = [0, 0, 0, 1, 2][index];
		details.view.visible = index < 3;
		if(index < 3) { this.prRefreshList }
	}

	prPickKind { |i|
		kindButtons.do { |b, j| b.value = if(i == j) { 1 } { 0 } };
		kind = kinds[i][1];
		this.prRefreshList
	}

	prSearchSoon {
		var token = searchToken = searchToken + 1;
		AppClock.sched(0.25, { if(token == searchToken) { this.prRefreshList }; nil })
	}

	prRefreshList {
		if(page == 0) { this.prSearch } { this.prShowRows(DekenRows.installedRows(installed, catalog, page == 2, kind, search.string)) }
	}

	prSearch { |next|
		var token = searchToken = searchToken + 1;
		Deken.search(search.string, kind, unreviewedBox.value, { |r|
			{
				if(token == searchToken) {
					if(r.ok) { this.prShowRows(r.data.collect { |hit| DekenRows.fromHit(hit, installed[hit["name"].toLower]) }) } { activity.string = r.error }
				};
				next.value
			}.defer
		})
	}

	// ---- the list

	prShowRows { |newRows|
		var generationNow = generation = generation + 1;
		rows = newRows;
		items = Array.new(newRows.size);
		tree.clear;
		Routine {
			var i = 0;
			while { i < newRows.size and: { generationNow == generation } } {
				items.add(this.prAddItem(newRows[i]));
				i = i + 1;
				if(i % 40 == 0) { 0.01.wait }
			}
		}.play(AppClock)
	}

	prAddItem { |row|
		var text = this.prStatusOf(row);
		var item = tree.addItem([DekenStyle.typeText(row[\kind]), row[\name], DekenRows.summaryCell(row), text]);
		item.setColor(0, DekenStyle.typeColor(row[\kind]));
		item.setTextColor(0, DekenStyle.chipText);
		this.prColorStatus(item, text);
		^item
	}

	prColorStatus { |item, text|
		item.setColor(3, DekenStyle.badgeColor(text));
		item.setTextColor(3, DekenStyle.badgeTextColor(text));
	}

	prStatusOf { |row| ^DekenRows.status(row, busy.includes(row[\name].toLower)) }

	// Rewrites the status cell of every row of this package.
	prRefreshStatus { |name|
		rows.do { |row, i|
			var text;
			if(row[\name] == name and: { items[i].notNil }) {
				text = this.prStatusOf(row);
				items[i].setString(3, text);
				this.prColorStatus(items[i], text)
			}
		}
	}

	// ---- the selected package

	prSelect {
		var item = tree.currentItem;
		var i = item !? { |it| it.index };
		if(i.notNil and: { i >= 0 } and: { rows[i].notNil }) { this.prShowRow(rows[i]) }
	}

	prShowRow { |row|
		var name = row[\name];
		selectedName = name;
		selectedRow = row;
		details.show(row);
		details.setBusy(busy.includes(name.toLower));
		Deken.info(name, { |r|
			{
				if(selectedName == name) {
					if(r.ok) { details.fill(r.data, installed, catalog); this.prPlanSelected } { details.setPlanError(r.error) }
				}
			}.defer
		})
	}

	prPlanSelected {
		var name = selectedName, version = details.selectedVersion;
		Deken.plan(name, version, { |r|
			{
				if(selectedName == name and: { details.selectedVersion == version }) {
					if(r.ok) { details.setPlan(r.data); this.prRememberRoute(r.data) } { details.setPlanError(r.error) }
				}
			}.defer
		})
	}

	// The list shows the route once it is known (only for the newest version).
	prRememberRoute { |plan|
		if(selectedRow.notNil and: { plan["version"] == selectedRow[\latest] }) {
			selectedRow[\route] = plan["route"];
			this.prRefreshStatus(selectedRow[\name])
		}
	}

	// ---- jobs

	prInstall { |name, version|
		var plan = details.plan;
		var report = plan !? { |p| p["doctor"] };
		if(plan.notNil and: { plan["route"] == "local-build" } and: { report.notNil } and: { report["ok"] != true }) {
			DekenDialogs.doctor(report,
				{ |dialog| Deken.doctor(name, { |r| { if(r.ok) { dialog[\show].value(r.data) } }.defer }) },
				{ this.prRun(name, version, "Installing") })
		} {
			this.prRun(name, version, "Installing")
		}
	}

	prRun { |name, version, verb|
		// The plan on show, if it is this package's, says whether a failure was a local build.
		jobPlans[name.toLower] = details.plan !? { |p| if(p["package"] == name) { p } };
		this.prSetBusy(name, true, verb ++ " " ++ name);
		Deken.install(name, version, { |r| { this.prInstallResult(name, version, r) }.defer })
	}

	prInstallResult { |name, version, r|
		if(r[\needsConfirmation] == true) { ^this.prConfirm(name, version, r) };
		this.prSetBusy(name, false, nil);
		if(r.ok) {
			this.prReloadInstalled;
			if(r[\after_install].notNil and: { r[\after_install].notEmpty }) { DekenDialogs.afterInstall(r[\after_install]) }
		} {
			this.prInstallFailed(name, r)
		}
	}

	// A failed local build offers to report the problem to the reviewers.
	prInstallFailed { |name, r|
		var plan = jobPlans[name.toLower];
		var url;
		if(plan.isNil or: { plan["route"] != "local-build" }) { ^this.prFailed("Could not install " ++ name, r) };
		log.add("Could not install " ++ name ++ ": " ++ r.error);
		url = DekenStyle.buildFailureIssueURL(name, plan["version"], plan["platform"] ? DekenHelper.platformKey, Main.version, log.lastLines(30));
		DekenDialogs.buildFailed("Could not build " ++ name, r.error ? "Something went wrong.", url)
	}

	prConfirm { |name, version, r|
		var changed = r.confirm.indexOfEqual("changed").notNil;
		DekenDialogs.confirm(r.data, { |choices|
			if(choices.isNil) {
				this.prSetBusy(name, false, nil);
				log.add("Cancelled: nothing was installed.")
			} {
				Deken.installConfirmed(name, version, choices, { |r2| { this.prInstallResult(name, version, r2) }.defer })
			}
		}, changed)
	}

	prAskRemove { |name|
		DekenDialogs.ask("Remove " ++ name, "Remove % from this computer? Its files are deleted.".format(name), "Remove", {
			this.prSetBusy(name, true, "Removing " ++ name);
			Deken.remove(name, { |r|
				{
					this.prSetBusy(name, false, nil);
					if(r.ok) { this.prReloadInstalled } { this.prFailed("Could not remove " ++ name, r) }
				}.defer
			})
		})
	}

	prAskClearQuarantine {
		DekenDialogs.ask("Delete quarantined packages",
			"Permanently delete the packages that were put in quarantine? This cannot be undone.", "Delete them", {
				log.header("Deleting quarantined packages");
				Deken.quarantineClear({ |r| { if(r.ok.not) { this.prFailed("Could not delete them", r) } }.defer })
			})
	}

	prRefreshPackages {
		log.header("Refreshing the package list");
		activity.string = "Refreshing the package list…";
		Deken.refresh({ |r|
			{
				activity.string = "";
				if(r.ok) { this.prThen([{ |next| this.prLoadCatalog(next) }, { |next| this.prReloadInstalled(next) }, { |next| this.prRefreshList; next.value }]) } { this.prFailed("Could not refresh", r) }
			}.defer
		})
	}

	// Marks the package as working (or not) in the list and the details pane.
	prSetBusy { |name, flag, title|
		if(flag) { busy.add(name.toLower) } { busy.remove(name.toLower) };
		this.prRefreshStatus(name);
		if(selectedName == name) { details.setBusy(flag) };
		if(flag) { log.header(title); activity.string = title ++ "…" } { activity.string = "" };
	}

	prReloadInstalled { |next|
		Deken.list({ |r|
			{
				if(r.ok) {
					installed = Dictionary.new;
					r.data.do { |i| installed[i["name"].toLower] = i };
					this.prUpdateCounts;
					if(page > 0 and: { page < 3 }) { this.prRefreshList } { this.prMarkInstalledRows };
				};
				next.value
			}.defer
		})
	}

	// Browse rows follow what is installed without being rebuilt.
	prMarkInstalledRows {
		rows.do { |row|
			var have = installed[row[\name].toLower];
			row[\installed] = have !? { |i| i["version"] };
			row[\update] = have.notNil and: { have["update_available"] == true };
			this.prRefreshStatus(row[\name])
		};
		selectedRow !? { |row| details.setInstalled(row[\installed], row[\update]); details.showNeeds(installed, catalog) }
	}

	prFailed { |title, r|
		log.add(title ++ ": " ++ r.error);
		DekenDialogs.notice(title, r.error ? "Something went wrong.")
	}
}
