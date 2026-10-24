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

	test_choicesCoverWhatIsShown {
		this.assertEquals(DekenDialogs.choicesFor(["unreviewed", "retracted"], "replace"),
			(confirm_unreviewed: true, confirm_retracted: true));
		this.assertEquals(DekenDialogs.choicesFor(["duplicates"], "keep"), (on_duplicate: "keep"));
		this.assertEquals(DekenDialogs.choicesFor([], "keep"), ());
	}
}
