// The rows of the package list, as plain Events, and what each row shows.
// No GUI here, so it can be tested without a window.
//
// A row has: name, kind, summary, trust (nil when unknown), latest, retracted,
// match (what a code search hit), installed (version or nil), update (Boolean),
// route (nil until the package has been planned).
DekenRows {
	// A search hit from the helper. have: the installed item with that name, or nil.
	*fromHit { |hit, have|
		^(name: hit["name"], kind: hit["kind"], summary: hit["summary"], trust: hit["trust"], latest: hit["latest"],
			retracted: hit["retracted"], match: hit["match"],
			installed: have !? { |i| i["version"] }, update: have.notNil and: { have["update_available"] == true })
	}

	// An installed item. known: the package list's entry for it (gives the
	// summary, and the kind and trust when the item has none), or nil.
	*fromInstalled { |item, known|
		var lookup = { |field| known !? { |k| k[field] } };
		^(name: item["name"], kind: item["kind"] ?? { lookup.value("kind") }, summary: lookup.value("summary") ? "",
			trust: item["trust"] ?? { lookup.value("trust") }, latest: item["latest"], retracted: item["advisory"].notNil,
			installed: item["version"], update: item["update_available"] == true)
	}

	// The installed packages as rows, sorted by name. Only updates, only this
	// kind (nil = all), and only those with the words in name or summary.
	*installedRows { |installed, catalog, onlyUpdates, kind, words|
		var list = installed.values.select { |i| onlyUpdates.not or: { i["update_available"] == true } };
		var text = words.asString.toLower;
		list = list.sort { |a, b| a["name"].toLower < b["name"].toLower };
		list = list.collect { |i| this.fromInstalled(i, catalog[i["name"].toLower]) };
		^list.select { |r|
			(kind.isNil or: { r[\kind] == kind }) and: { text.isEmpty or: { (r[\name] ++ " " ++ r[\summary]).toLower.find(text).notNil } }
		}
	}

	// The package in catalog (search hits by lowercase name) whose repository
	// is address, however it is spelled, or nil.
	*findRepo { |catalog, address|
		var key;
		if(address.isNil) { ^nil };
		key = DekenStyle.repoKey(address);
		^catalog.values.detect { |hit| hit["repo"].notNil and: { DekenStyle.repoKey(hit["repo"]) == key } }
	}

	// The badge text of a row; busy rows say "working…". Before the row is
	// planned its route is unknown, but a quark is never built, so its kind
	// stands in for the route.
	*status { |row, isBusy|
		var route = row[\route] ?? { if(row[\kind] == "quark") { "quark" } };
		if(isBusy) { ^"working…" };
		^DekenStyle.badgeText(row[\trust], route, row[\retracted], row[\update])
	}

	// The summary, with the code match (if any) in front: a long summary is cut
	// off at the column's right edge, and the match must stay visible.
	*summaryCell { |row|
		var note = DekenStyle.matchedText(row[\match]);
		var summary = row[\summary] ? "";
		if(note.isEmpty) { ^summary };
		^if(summary.isEmpty) { note } { note ++ " — " ++ summary }
	}
}
