// The Settings page of the package window. Choices are saved at once in
// DekenSettings. The two buttons call the actions passed to `new`:
// refresh and clearQuarantine.
DekenSettingsView {
	var actions;
	var <view;

	*new { |actions| ^super.newCopyArgs(actions).prBuild }

	prBuild {
		var advisories = CheckBox().string_("Check for withdrawn packages (at startup and when the window opens)")
			.value_(DekenSettings.get(\checkAdvisories))
			.action_({ |box| DekenSettings.put(\checkAdvisories, box.value) });
		var unreviewed = CheckBox().string_("Show unreviewed packages in Browse by default")
			.value_(DekenSettings.get(\showUnreviewed))
			.action_({ |box| DekenSettings.put(\showUnreviewed, box.value) });
		var helper = TextField().string_(DekenSettings.get(\helperPath))
			.action_({ |field| this.prUseHelper(field.string.stripWhiteSpace) });
		view = View().layout_(VLayout(
			StaticText().string_("Settings").font_(Font(size: 15, bold: true)),
			advisories,
			unreviewed,
			StaticText().string_("Use this scdeken program instead of the downloaded one (press Enter; empty = the downloaded one):"),
			helper,
			StaticText().string_("Packages in quarantine were withdrawn by their author or by the reviewers. They are kept until you delete them."),
			HLayout(
				Button().states_([["Delete quarantined packages…"]]).action_({ actions[\clearQuarantine].value }),
				Button().states_([["Refresh package list"]]).action_({ actions[\refresh].value }),
				nil
			),
			nil
		).margins_(16).spacing_(10))
	}

	prUseHelper { |path|
		DekenSettings.put(\helperPath, path);
		Deken.helperPath = if(path.isEmpty) { nil } { path }
	}
}
