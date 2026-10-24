package platform

import (
	"testing"

	"github.com/shimpe/scdeken/internal/version"
)

func TestKeyStringRoundTrip(t *testing.T) {
	want := Key{"macos", "universal", 3}
	if got := want.String(); got != "macos-universal-api3" {
		t.Fatalf("String() = %q", got)
	}
	got, err := ParseKey("macos-universal-api3")
	if err != nil || got != want {
		t.Fatalf("ParseKey = %v, %v; want %v", got, err, want)
	}
	for _, bad := range []string{"", "linux-x86_64", "beos-x86_64-api3", "linux-ppc-api3", "linux-x86_64-api", "linux-x86_64-apix", "linux-universal-api3", "linux-x86_64-api3-extra"} {
		if _, err := ParseKey(bad); err == nil {
			t.Errorf("ParseKey(%q) succeeded, want error", bad)
		}
	}
}

func TestAcceptsUniversal(t *testing.T) {
	if !(Key{"macos", "arm64", 3}).Accepts(Key{"macos", "universal", 3}) {
		t.Error("macos arm64 should accept universal")
	}
	if (Key{"linux", "arm64", 3}).Accepts(Key{"linux", "x86_64", 3}) {
		t.Error("linux arm64 must not accept x86_64")
	}
	if (Key{"linux", "x86_64", 3}).Accepts(Key{"linux", "x86_64", 4}) {
		t.Error("different API must not be accepted")
	}
	if (Key{"windows", "x86_64", 3}).Accepts(Key{"macos", "x86_64", 3}) {
		t.Error("different OS must not be accepted")
	}
}

func TestNormalizeArch(t *testing.T) {
	cases := map[string]string{"AArch64": "arm64", "arm64": "arm64", "amd64": "x86_64", "x64": "x86_64", "x86_64": "x86_64"}
	for in, want := range cases {
		if got, err := NormalizeArch(in); err != nil || got != want {
			t.Errorf("NormalizeArch(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := NormalizeArch("ppc"); err == nil {
		t.Error("NormalizeArch(ppc) succeeded, want error")
	}
}

func TestHostOS(t *testing.T) {
	switch HostOS() {
	case "linux", "macos", "windows":
	default:
		t.Errorf("HostOS() = %q", HostOS())
	}
}

var table = []SCVersion{{Version: "3.14.1", API: 3, Min: "3.13.0"}}

func apiFor(t *testing.T, sc string, tbl []SCVersion) (int, bool) {
	t.Helper()
	v, err := version.Parse(sc)
	if err != nil {
		t.Fatal(err)
	}
	return APIFor(v, tbl)
}

func TestAPIFor(t *testing.T) {
	for _, sc := range []string{"3.13.0", "3.14.1"} {
		if api, ok := apiFor(t, sc, table); !ok || api != 3 {
			t.Errorf("APIFor(%s) = %d, %v; want 3, true", sc, api, ok)
		}
	}
	if _, ok := apiFor(t, "3.12.2", table); ok {
		t.Error("APIFor(3.12.2) ok, want false")
	}
}

func TestAPIForPicksGreatestMin(t *testing.T) {
	tbl := []SCVersion{
		{Version: "3.14.1", API: 3, Min: "3.13.0"},
		{Version: "3.16.0", API: 4, Min: "3.15.0"},
	}
	if api, ok := apiFor(t, "3.15.2", tbl); !ok || api != 4 {
		t.Errorf("APIFor(3.15.2) = %d, %v; want 4, true", api, ok)
	}
	if api, ok := apiFor(t, "3.14.0", tbl); !ok || api != 3 {
		t.Errorf("APIFor(3.14.0) = %d, %v; want 3, true", api, ok)
	}
}

func TestAPIForUnknownNewerVersion(t *testing.T) {
	for _, sc := range []string{"3.15.0-dev", "3.15.0"} {
		if _, ok := apiFor(t, sc, table); ok {
			t.Errorf("APIFor(%s) ok, want false", sc)
		}
	}
}
