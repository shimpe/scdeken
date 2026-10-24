// The Build log page: shows what the helper reports while it works, with a
// header line for each job. Lines are collected as they arrive and written
// to the text view a few times a second, so a chatty build cannot slow the
// window down. Only the last 3000 lines are kept.
DekenLogView {
	var isOpen;
	var <view;
	var lines, dirty = false, oldLogAction, oldProgressAction;

	// isOpen: a function that says whether the window is still open.
	*new { |isOpen| ^super.newCopyArgs(isOpen).prBuild }

	prBuild {
		lines = List.new;
		view = TextView().editable_(false).font_(Font.monospace(11));
	}

	// Takes over Deken's log lines and progress reports until `unhook`.
	// onProgress gets (fraction, label).
	hook { |onProgress|
		oldLogAction = Deken.logAction;
		oldProgressAction = Deken.progressAction;
		Deken.logAction = { |line| { this.add(line) }.defer };
		Deken.progressAction = { |fraction, label| { onProgress.value(fraction, label) }.defer };
		AppClock.sched(0.3, { if(isOpen.value) { this.prFlush; 0.3 } { nil } })
	}

	unhook {
		Deken.logAction = oldLogAction;
		Deken.progressAction = oldProgressAction;
	}

	add { |line|
		lines.add(line);
		if(lines.size > 3000) { lines.removeAt(0) };
		dirty = true;
	}

	// The newest n lines, oldest first.
	lastLines { |n|
		^lines.asArray.copyToEnd((lines.size - n).max(0))
	}

	header { |title|
		this.add("");
		this.add("== % (%) ==".format(title, Date.getDate.format("%H:%M:%S")))
	}

	prFlush {
		if(dirty) {
			view.string = lines.join("\n");
			view.select(view.string.size, 0);
			dirty = false
		}
	}
}
