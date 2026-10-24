package doctor

import (
	"context"
	"errors"
	"testing"

	"github.com/shimpe/scdeken/internal/recipe"
	"github.com/shimpe/scdeken/internal/run"
)

const buildToolsHint = `winget install --id Microsoft.VisualStudio.2022.BuildTools -e --override "--add Microsoft.VisualStudio.Workload.VCTools --includeRecommended --passive"`

const vswhereArgs = `-latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath`

var errMissing = errors.New("exit status 1")

func ok(out string) run.FakeResult { return run.FakeResult{Out: out} }

func missing() run.FakeResult { return run.FakeResult{Err: errMissing} }

// find returns the report item with the given name.
func find(t *testing.T, r Report, name string) Item {
	t.Helper()
	for _, it := range r.Items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("no item %q in %+v", name, r.Items)
	return Item{}
}

// ubuntuFake scripts a Linux machine that has every tool.
func ubuntuFake() *run.Fake {
	return &run.Fake{
		Script: map[string]run.FakeResult{
			"git --version":   ok("git version 2.47.1"),
			"cmake --version": ok("cmake version 3.31.0\n\nCMake suite maintained by Kitware"),
		},
		Paths: map[string]string{"g++": "/usr/bin/g++", "make": "/usr/bin/make"},
	}
}

func TestDoctorMacMissingCLT(t *testing.T) {
	f := &run.Fake{
		Script: map[string]run.FakeResult{
			"git --version":   ok("git version 2.39.0"),
			"cmake --version": ok("cmake version 3.31.0"),
			"xcode-select -p": missing(),
		},
		Paths: map[string]string{"make": "/usr/bin/make"},
	}
	r := Check(context.Background(), f, Env{OS: "macos"}, nil)
	c := find(t, r, "compiler")
	if c.Found || c.Hint != "xcode-select --install" || r.OK {
		t.Fatalf("compiler = %+v, OK = %v", c, r.OK)
	}
}

func TestDoctorWindowsNoBuildTools(t *testing.T) {
	f := &run.Fake{Script: map[string]run.FakeResult{
		"git --version":                    ok("git version 2.47.1.windows.1"),
		"cmake --version":                  ok("cmake version 3.31.0"),
		`C:\vs\vswhere.exe ` + vswhereArgs: ok(""),
	}}
	r := Check(context.Background(), f, Env{OS: "windows", VSWhere: `C:\vs\vswhere.exe`}, nil)
	c := find(t, r, "compiler")
	if c.Found || c.Hint != buildToolsHint {
		t.Fatalf("compiler = %+v", c)
	}
	if git := find(t, r, "git"); git.Version != "2.47.1" || !git.Found {
		t.Fatalf("git = %+v", git)
	}
	for _, it := range r.Items {
		if it.Name == "build-driver" {
			t.Fatalf("Windows should skip the build driver, got %+v", it)
		}
	}
}

func TestDoctorCMakeTooOld(t *testing.T) {
	f := ubuntuFake()
	f.Script["cmake --version"] = ok("cmake version 3.10.2")
	r := Check(context.Background(), f, Env{OS: "linux", Distro: "debian"}, nil)
	c := find(t, r, "cmake")
	if c.Found || c.Version != "3.10.2" || c.Hint != "sudo apt install cmake" || r.OK {
		t.Fatalf("cmake = %+v, OK = %v", c, r.OK)
	}
}

func TestDoctorUbuntuAllGood(t *testing.T) {
	r := Check(context.Background(), ubuntuFake(), Env{OS: "linux", Distro: "debian"}, nil)
	if !r.OK {
		t.Fatalf("not OK: %+v", r.Items)
	}
	for _, it := range r.Items {
		if !it.Found || it.Hint != "" {
			t.Errorf("item %+v should be found without a hint", it)
		}
	}
	if len(r.Items) != 4 {
		t.Errorf("got %d items, want git, cmake, compiler, build-driver", len(r.Items))
	}
}

func TestDoctorArchHints(t *testing.T) {
	f := ubuntuFake()
	delete(f.Paths, "g++")
	r := Check(context.Background(), f, Env{OS: "linux", Distro: "arch"}, nil)
	if c := find(t, r, "compiler"); c.Found || c.Hint != "sudo pacman -S base-devel" {
		t.Fatalf("compiler = %+v", c)
	}
}

func TestDoctorRequirementPkgConfig(t *testing.T) {
	f := ubuntuFake()
	f.Paths["pkg-config"] = "/usr/bin/pkg-config"
	f.Script["pkg-config --exists fftw3"] = missing()
	reqs := map[string]recipe.Requirement{
		"fftw": {Apt: "libfftw3-dev", PkgConfig: "fftw3"},
	}
	r := Check(context.Background(), f, Env{OS: "linux", Distro: "debian"}, reqs)
	it := find(t, r, "fftw")
	if it.Found || !it.Verified || it.Hint != "sudo apt install libfftw3-dev" || it.Label != "fftw library" || r.OK {
		t.Fatalf("fftw = %+v, OK = %v", it, r.OK)
	}
}

func TestDoctorRequirementUnverifiable(t *testing.T) {
	reqs := map[string]recipe.Requirement{
		"fftw": {Apt: "libfftw3-dev", PkgConfig: "fftw3"},
	}
	r := Check(context.Background(), ubuntuFake(), Env{OS: "linux", Distro: "debian"}, reqs)
	it := find(t, r, "fftw")
	if it.Verified || it.Hint != "sudo apt install libfftw3-dev" || !r.OK {
		t.Fatalf("fftw = %+v, OK = %v", it, r.OK)
	}
}

func TestDoctorUnknownDistroGenericHint(t *testing.T) {
	f := ubuntuFake()
	delete(f.Paths, "g++")
	r := Check(context.Background(), f, Env{OS: "linux"}, nil)
	want := "Install C++ compiler with your system's package manager."
	if c := find(t, r, "compiler"); c.Hint != want {
		t.Fatalf("hint = %q, want %q", c.Hint, want)
	}
}

func TestDoctorUnknownOSChecksGitAndCMake(t *testing.T) {
	f := &run.Fake{Script: map[string]run.FakeResult{
		"git --version":   ok("git version 2.47.1"),
		"cmake --version": missing(),
	}}
	r := Check(context.Background(), f, Env{OS: "plan9"}, nil)
	if len(r.Items) != 2 || r.OK {
		t.Fatalf("report = %+v", r)
	}
	want := "Install CMake (build tool) with your system's package manager."
	if c := find(t, r, "cmake"); c.Hint != want {
		t.Fatalf("hint = %q, want %q", c.Hint, want)
	}
}
