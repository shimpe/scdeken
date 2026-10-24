// What the package window shows for a package: type chip text and colors,
// status badge text and colors, and the small notes. No GUI objects here, so
// all of it can be tested without a window.
DekenStyle {
	classvar <chipText;      // white text on the type colors

	*initClass {
		chipText = Color.white;
	}

	// ---- package type chip

	// "PLUGIN", "QUARK" or "PLUGIN + QUARK".
	*typeText { |kind|
		^switch((kind !? { |k| k.asString }) ? "",
			"plugin", { "PLUGIN" },
			"quark", { "QUARK" },
			"plugin+quark", { "PLUGIN + QUARK" },
			{ "" }
		)
	}

	*typeColor { |kind|
		^Color.fromHexString(switch((kind !? { |k| k.asString }) ? "",
			"plugin", { "#6a3fb5" },
			"quark", { "#0f7f86" },
			"plugin+quark", { "#4d5f9e" },
			{ "#777777" }
		))
	}

	// ---- status badge

	// trust: reviewed | community | unreviewed; nil or anything else means we do
	// not know the package, which is just "installed". route: prebuilt | local-build |
	// quark, or nil while it is not known yet (a list row has no route until
	// the package is looked at). A withdrawn version beats an update, which
	// beats the trust level.
	*badgeText { |trust, route, retracted = false, updateAvailable = false|
		if(retracted == true) { ^"⛔ retracted" };
		if(updateAvailable == true) { ^"update available" };
		^switch((trust !? { |t| t.asString }) ? "",
			"reviewed", { this.prReviewedText(route) },
			"community", { "community-listed" },
			"unreviewed", { if(route.asString == "quark") { "⚠ unreviewed" } { "⚠ unreviewed · build locally" } },
			{ "installed" }
		)
	}

	*prReviewedText { |route|
		^switch(route.asString,
			"prebuilt", { "reviewed · prebuilt" },
			"local-build", { "reviewed · build locally" },
			{ "reviewed" }
		)
	}

	// Background color for a badge text; anything unknown (such as "working…") is grey.
	*badgeColor { |text|
		^Color.fromHexString(switch(text,
			"reviewed · prebuilt", { "#d8f2dc" },
			"reviewed", { "#d8f2dc" },
			"reviewed · build locally", { "#fff1c9" },
			"⚠ unreviewed · build locally", { "#ffd9d6" },
			"⚠ unreviewed", { "#ffd9d6" },
			"update available", { "#dbe7ff" },
			"⛔ retracted", { "#f7b3ad" },
			{ "#eeeeee" }
		))
	}

	// Dark text that reads well on the badge color.
	*badgeTextColor { |text|
		^Color.fromHexString(switch(text,
			"reviewed · prebuilt", { "#1d6b2b" },
			"reviewed", { "#1d6b2b" },
			"reviewed · build locally", { "#7a5a00" },
			"⚠ unreviewed · build locally", { "#9b1c12" },
			"⚠ unreviewed", { "#9b1c12" },
			"update available", { "#1f47a8" },
			"⛔ retracted", { "#7a0e06" },
			{ "#444444" }
		))
	}

	// ---- links

	// Where an advisory is written down.
	*advisoryURL { |id|
		^"https://github.com/sc-deken/sc-deken-recipes/blob/main/advisories/%.toml".format(id)
	}

	// The form for asking for a package to be reviewed.
	*suggestURL { |repo|
		^"https://github.com/sc-deken/sc-deken-recipes/issues/new?template=add-package.yml&repo_url=%".format(this.percentEncode(repo))
	}

	// Escapes everything but letters, digits and - . _ ~ for use in a web address.
	*percentEncode { |text|
		var out = List.new;
		text.asString.do { |c|
			out.add(if(c.isAlphaNum and: { c.ascii < 128 } or: { "-._~".includes(c) }) { c.asString } { "%" ++ c.ascii.asHexString(2).toUpper })
		};
		^out.join
	}

	// ---- small notes

	// Only hits that come from code get a note; name and summary hits are
	// visible in the row already. match is a Dictionary or Event with field and value.
	*matchedText { |match|
		var field = match !? { |m| m[\field] ? m["field"] };
		var value = match !? { |m| m[\value] ? m["value"] };
		^switch(field.asString,
			"ugen", { "matched UGen: " ++ value },
			"class", { "matched class: " ++ value },
			{ "" }
		)
	}

	// platform is the helper's platform answer; engines names what the server can run.
	*statusBarText { |platform, engines|
		if(platform.isNil) { ^"checking this computer…" };
		if(platform["api_known"] != true) {
			^"plugin API unknown for % — packages will be built on this computer".format(platform["sc_version"])
		};
		^"% % · plugin API % · %".format(this.prOSName(platform["os"]), platform["arch"], platform["api"], engines)
	}

	*prOSName { |os|
		^switch(os.asString, "macos", { "macOS" }, "windows", { "Windows" }, "linux", { "Linux" }, { os.asString })
	}

	// steps: the helper's after_install list.
	*afterInstallText { |steps|
		var parts = List.new;
		if(steps.indexOfEqual("recompile").notNil) { parts.add("Recompile class library") };
		if(steps.indexOfEqual("reboot-server").notNil) { parts.add("reboot server") };
		^if(parts.isEmpty) { "nothing to do" } { parts.join(" · ") }
	}

	// Cuts text to at most `max` characters, ending in "…" when it was cut.
	// Never leaves half of a multi-byte character behind.
	*shorten { |text, max|
		var cut;
		if(text.size <= max) { ^text };
		cut = text.keep(max - 1);
		^this.prDropPartialChar(cut) ++ "…"
	}

	*prDropPartialChar { |text|
		var i = text.size - 1;
		var lead, need;
		while { i >= 0 and: { text[i].ascii.inclusivelyBetween(128, 191) } } { i = i - 1 };
		if(i < 0) { ^"" };
		lead = text[i].ascii;
		need = case { lead >= 240 } { 4 } { lead >= 224 } { 3 } { lead >= 192 } { 2 } { 1 };
		^if(text.size - i < need) { text.keep(i) } { text }
	}
}
