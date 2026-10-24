// The window's settings, kept in <userAppSupportDir>/sc-deken/settings.scd
// (an sclang archive). Reading falls back to the defaults for anything
// missing or unreadable, so a damaged file never stops the window opening.
DekenSettings {
	classvar values;
	classvar <>pathOverride;  // tests point this at a temporary file

	*defaults {
		^(checkAdvisories: true, showUnreviewed: false, helperPath: "")
	}

	*path { ^pathOverride ?? { Platform.userAppSupportDir +/+ "sc-deken" +/+ "settings.scd" } }

	// Forgets what was read, so the next get reads the file again.
	*reload { values = nil }

	*get { |key|
		this.prLoad;
		^values[key]
	}

	*put { |key, value|
		this.prLoad;
		values[key] = value;
		this.prSave
	}

	*prLoad {
		var stored;
		if(values.notNil) { ^this };
		values = this.defaults;
		if(File.exists(this.path)) {
			stored = try { Object.readArchive(this.path) };
			if(stored.isKindOf(Dictionary)) {
				stored.keysValuesDo { |k, v| if(values.includesKey(k)) { values[k] = v } }
			}
		}
	}

	*prSave {
		DekenHelper.prMkdirs(this.path.dirname);
		values.writeArchive(this.path)
	}
}
