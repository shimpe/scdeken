TestDekenDialogs : UnitTest {
	var tree;

	setUp {
		var dep = Dictionary.newFrom(["package", "dep", "confirm", ["duplicates"], "depends", []]);
		var other = Dictionary.newFrom(["package", "clean", "confirm", [], "depends", []]);
		tree = Dictionary.newFrom(["package", "main", "confirm", ["unreviewed", "retracted"], "depends", [dep, other]]);
	}

	test_confirmPlansWalksDependenciesFirst {
		this.assertEquals(DekenDialogs.confirmPlans(tree).collect { |p| p["package"] }, ["dep", "main"],
			"only plans with something to confirm, dependencies before the package");
	}

	test_confirmKindsListsEachOnce {
		var plans = DekenDialogs.confirmPlans(tree);
		plans[0]["confirm"] = ["unreviewed", "duplicates"];
		this.assertEquals(DekenDialogs.confirmKinds(plans), ["unreviewed", "duplicates", "retracted"]);
	}

	test_unreviewedTextNamesRepoAndCommit {
		var plugin = Dictionary.newFrom(["package", "shady", "repo", "https://github.com/example/shady", "commit", "0123456789abcdef", "confirm", ["unreviewed"], "depends", []]);
		var text = DekenDialogs.unreviewedText(plugin);
		this.assert(text.find("https://github.com/example/shady").notNil, "repo is named");
		this.assert(text.find("0123456789").notNil, "commit is named");
		this.assertEquals(plugin["quark"], nil, "a plugin has no quark part to take the repo from");
	}

	test_choicesCoverWhatIsShown {
		this.assertEquals(DekenDialogs.choicesFor(["unreviewed", "retracted"], "replace"),
			(confirm_unreviewed: true, confirm_retracted: true));
		this.assertEquals(DekenDialogs.choicesFor(["duplicates"], "keep"), (on_duplicate: "keep"));
		this.assertEquals(DekenDialogs.choicesFor([], "keep"), ());
	}

	test_choicesNameTheCommitsShown {
		var choices = DekenDialogs.choicesFor(["unreviewed"], nil, this.withCommits);
		this.assertEquals(choices[\confirm_unreviewed], true);
		this.assertEquals(choices[\commits], Dictionary.newFrom(["main", "aaa", "dep", "bbb", "clean", ""]));
	}

	withCommits {
		tree["commit"] = "aaa";
		tree["depends"][0]["commit"] = "bbb";
		tree["depends"][1]["commit"] = "";
		^tree
	}

	test_planCommitsWalkEveryPlan {
		var plans = Dictionary.newFrom(["plans", [this.withCommits, Dictionary.newFrom(["package", "solo", "commit", "ccc"])]]);
		this.assertEquals(Deken.planCommits(this.withCommits), Dictionary.newFrom(["main", "aaa", "dep", "bbb", "clean", ""]));
		this.assertEquals(Deken.planCommits(plans)["solo"], "ccc", "update answers list several plans");
		this.assertEquals(Deken.planCommits(plans)["dep"], "bbb");
	}

	test_yesButtons {
		this.assertEquals(DekenDialogs.yesLabels(["unreviewed", "duplicates"]), ["I understand, build it"]);
		this.assertEquals(DekenDialogs.yesLabels(["retracted"]), ["Install anyway"]);
		this.assertEquals(DekenDialogs.yesLabels(["duplicates"]), ["Replace (old copy goes to a backup)", "Keep existing"]);
		this.assertEquals(DekenDialogs.yesLabels([]), ["Install"], "a changed plan with nothing else to confirm");
	}

	test_unpinnedURLDependencyText {
		var dep = Dictionary.newFrom(["package", "Extra", "repo", "https://github.com/other/Extra", "commit", "", "confirm", ["unreviewed"]]);
		var text = DekenDialogs.unreviewedText(dep);
		this.assert(text.find("its newest commit (not pinned to a reviewed version)").notNil, text);
	}
}
