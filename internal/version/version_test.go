package version

import "testing"

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

func mustParse(t *testing.T, s string) Version {
	t.Helper()
	v, err := Parse(s)
	if err != nil {
		t.Fatalf("Parse(%q): %v", s, err)
	}
	return v
}

func TestParseAndCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"0.10", "0.9", 1},
		{"1.0.0-rc1", "1.0.0", -1},
		{"1.0", "1.0.0", 0},
		{"0.0.0+abc1234", "0.0.0", 0},
		{"2.0.0-beta.2", "2.0.0-beta.10", -1},
		{"1.0.0-alpha", "1.0.0-alpha.1", -1},
		{"1.0.0-1", "1.0.0-alpha", -1},
		{"1.0.0-beta", "1.0.0-alpha", 1},
	}
	for _, c := range cases {
		got := sign(mustParse(t, c.a).Compare(mustParse(t, c.b)))
		if got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestParseRejects(t *testing.T) {
	for _, s := range []string{"", "abc", "1..2", "1.2.3.4.5", "1.2-", "1.2+", "1.x"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", s)
		}
	}
}

func TestString(t *testing.T) {
	for _, s := range []string{"1.2.3", "1.2-rc1", "0.0.0+abc1234"} {
		if got := mustParse(t, s).String(); got != s {
			t.Errorf("String() = %q, want %q", got, s)
		}
	}
	if got := mustParse(t, "v1.2").String(); got != "1.2" {
		t.Errorf("String() = %q, want leading v removed", got)
	}
}

func TestFromTag(t *testing.T) {
	cases := []struct {
		tag, prefix, want string
		ok                bool
	}{
		{"v0.4.5", "", "0.4.5", true},
		{"Version-3.13.0", "Version-", "3.13.0", true},
		{"latest", "", "", false},
		{"V1.2", "", "1.2", true},
		{"v1.0", "Version-", "", false},
	}
	for _, c := range cases {
		v, ok := FromTag(c.tag, c.prefix)
		if ok != c.ok || (ok && v.String() != c.want) {
			t.Errorf("FromTag(%q, %q) = %q, %v; want %q, %v", c.tag, c.prefix, v, ok, c.want, c.ok)
		}
	}
}

func TestUnstable(t *testing.T) {
	v := Unstable("3f2c9e1aa0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5")
	if v.String() != "0.0.0+3f2c9e1" || !v.IsUnstable() {
		t.Errorf("Unstable = %q, IsUnstable %v", v, v.IsUnstable())
	}
	if mustParse(t, "1.0").IsUnstable() {
		t.Error("plain release must not be unstable")
	}
	if got := Unstable("abc").String(); got != "0.0.0+abc" {
		t.Errorf("short commit: %q", got)
	}
}

func TestMax(t *testing.T) {
	vs := []Version{mustParse(t, "0.9"), mustParse(t, "0.10"), mustParse(t, "0.10-rc1")}
	got, ok := Max(vs)
	if !ok || got.String() != "0.10" {
		t.Errorf("Max = %q, %v; want 0.10", got, ok)
	}
	if _, ok := Max(nil); ok {
		t.Error("Max(nil) must report false")
	}
}
