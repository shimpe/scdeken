package recipe

import "testing"

func TestSafeRepoURL(t *testing.T) {
	good := []string{"https://github.com/o/Repo", "https://github.com/o/Repo.git", "https://git.example.org/a_b/c-d~e/"}
	bad := []string{
		"", "http://github.com/o/r", "git://github.com/o/r", "ssh://github.com/o/r", "file:///etc/passwd",
		"https://", "https:///o/r", "https://user@github.com/o/r", "https://user:pw@github.com/o/r",
		"https://github.com:8080/o/r", "https://github.com/o/r?x=1", "https://github.com/o/r#frag", "https://github.com/o/r?",
		"https://github.com/o/r;rm", "https://github.com/o/r&calc", "https://github.com/o/r|x", "https://github.com/o/$HOME",
		"https://github.com/o/r%20x", "https://github.com/o/`x`", "https://github.com/o/r x", "https://github.com/o/\"x",
		"https://github.com/o/../r", "https://github.com/./r", "https://gi thub.com/o/r", "https://github.com/o/r'x",
	}
	for _, s := range good {
		if !SafeRepoURL(s) {
			t.Errorf("SafeRepoURL(%q) = false", s)
		}
	}
	for _, s := range bad {
		if SafeRepoURL(s) {
			t.Errorf("SafeRepoURL(%q) = true", s)
		}
	}
}

func TestDependsRejectsUnsafeURL(t *testing.T) {
	src := quarkRecipe("\n[depends]\n\"https://github.com/o/r;rm -rf\" = \"*\"\n")
	if _, err := Parse([]byte(src)); err == nil {
		t.Error("an unsafe dependency address was accepted")
	}
}
