package version

import "testing"

func TestConstraintAllows(t *testing.T) {
	cases := []struct {
		constraint, version string
		want                bool
	}{
		{"*", "1.2", true},
		{"", "9.9.9", true},
		{"*", "0.0.0+abc1234", true},
		{"1.2", "1.2.0", true},
		{"=1.2", "1.2", true},
		{"=1.2", "1.3", false},
		{">=1.0", "1.0", true},
		{">=1.0", "0.9", false},
		{">1.0", "1.0", false},
		{">1.0", "1.0.1", true},
		{"<=2.0", "2.0", true},
		{"<=2.0", "2.0.1", false},
		{"<2.0", "2.0", false},
		{"<2.0", "1.9.9", true},
		{">=1.0,<2.0", "1.5", true},
		{">=1.0,<2.0", "2.0", false},
		{" >= 1.0 , < 2.0 ", "1.0", true},
		{">=1.0", "1.0-rc1", false}, // a prerelease sorts below its release
		{"<1.0", "1.0-rc1", true},
		{">=1.0-rc1", "1.0-rc2", true},
		{">=1.0", "0.0.0+abc1234", false}, // unstable builds satisfy only "*"
		{"=0.0.0", "0.0.0+abc1234", false},
		{"<9", "0.0.0+abc1234", false},
		{"v1.2", "1.2", true},
	}
	for _, c := range cases {
		con, err := ParseConstraint(c.constraint)
		if err != nil {
			t.Errorf("ParseConstraint(%q): %v", c.constraint, err)
			continue
		}
		v, err := Parse(c.version)
		if err != nil {
			t.Fatal(err)
		}
		if got := con.Allows(v); got != c.want {
			t.Errorf("%q allows %q = %v, want %v", c.constraint, c.version, got, c.want)
		}
	}
}

func TestParseConstraintErrors(t *testing.T) {
	for _, s := range []string{">=", "abc", ">=x", "1.0,", ",1.0", ">=1.0,,<2", "~1.0", "=>1.0", "1.2.3.4.5"} {
		if _, err := ParseConstraint(s); err == nil {
			t.Errorf("ParseConstraint(%q): expected an error", s)
		}
	}
}

func TestConstraintString(t *testing.T) {
	con, _ := ParseConstraint(" >=1.0 , <2.0")
	if got := con.String(); got != ">=1.0,<2.0" {
		t.Errorf("String() = %q", got)
	}
	any, _ := ParseConstraint("")
	if any.String() != "*" {
		t.Errorf("any.String() = %q", any.String())
	}
}
