package discover

import (
	"reflect"
	"testing"
)

func TestParseDependency(t *testing.T) {
	cases := []struct {
		raw  string
		want Dependency
	}{
		{"Name", Dependency{Name: "Name"}},
		{"Name@tags/1.2", Dependency{Name: "Name", Refspec: "tags/1.2"}},
		{"Name@abc1234", Dependency{Name: "Name", Refspec: "abc1234"}},
		{"https://github.com/owner/Repo", Dependency{Name: "Repo", URL: "https://github.com/owner/Repo"}},
		{"https://github.com/owner/Repo.git", Dependency{Name: "Repo", URL: "https://github.com/owner/Repo.git"}},
		{"git://github.com/owner/Repo.git", Dependency{Name: "Repo", URL: "git://github.com/owner/Repo.git"}},
		{"https://github.com/owner/Repo@tags/1.0", Dependency{Name: "Repo", URL: "https://github.com/owner/Repo", Refspec: "tags/1.0"}},
		{"https://github.com/owner/Repo/", Dependency{Name: "Repo", URL: "https://github.com/owner/Repo/"}},
	}
	for _, c := range cases {
		got, ok := ParseDependency(c.raw)
		if !ok || got != c.want {
			t.Errorf("ParseDependency(%q) = %+v, %v; want %+v", c.raw, got, ok, c.want)
		}
	}
}

func TestParseDependencyRejects(t *testing.T) {
	for _, raw := range []string{"", "   ", "@1.0", "Quark(\"x\")", "has space", "https://"} {
		if d, ok := ParseDependency(raw); ok {
			t.Errorf("ParseDependency(%q) = %+v, want rejection", raw, d)
		}
	}
}

func TestDependencyRoundTrip(t *testing.T) {
	for _, raw := range []string{"Name", "Name@1.2", "https://github.com/o/R.git@tags/v1"} {
		d, _ := ParseDependency(raw)
		if d.String() != raw {
			t.Errorf("String() = %q, want %q", d.String(), raw)
		}
	}
}

func TestDependencyConstraint(t *testing.T) {
	cases := []struct {
		refspec, want string
		note          bool
	}{
		{"", "*", false},
		{"tags/1.2", "=1.2", false},
		{"v1.2", "=1.2", false},
		{"1.2", "=1.2", false},
		{"tags/v2.0.1", "=2.0.1", false},
		{"main", "*", true},
		{"0123abc", "*", true},
		{"heads/dev", "*", true},
	}
	for _, c := range cases {
		got, note := Dependency{Name: "X", Refspec: c.refspec}.Constraint()
		if got != c.want || (note != "") != c.note {
			t.Errorf("Constraint(%q) = %q, %q; want %q, note=%v", c.refspec, got, note, c.want, c.note)
		}
	}
}

func TestParseQuarkFileDependencyForms(t *testing.T) {
	src := `( name: "Q", dependencies: [
		"Plain", "Pinned@tags/1.2", "https://github.com/o/Repo.git@1.0", \Sym, 'Quoted',
		Quark("Fancy"), 42, "bad entry@" ] )`
	q, err := ParseQuarkFile(src)
	if err != nil {
		t.Fatal(err)
	}
	want := []Dependency{
		{Name: "Plain"},
		{Name: "Pinned", Refspec: "tags/1.2"},
		{Name: "Repo", URL: "https://github.com/o/Repo.git", Refspec: "1.0"},
		{Name: "Sym"},
		{Name: "Quoted"},
	}
	if !reflect.DeepEqual(q.Dependencies, want) {
		t.Errorf("Dependencies = %+v", q.Dependencies)
	}
	if len(q.Notes) != 3 {
		t.Errorf("Notes = %q, want 3 skipped entries", q.Notes)
	}
}
