package install

import (
	"testing"

	"github.com/sc-deken/sc-deken/internal/advisory"
	"github.com/sc-deken/sc-deken/internal/doctor"
	"github.com/sc-deken/sc-deken/internal/index"
	"github.com/sc-deken/sc-deken/internal/platform"
	"github.com/sc-deken/sc-deken/internal/recipe"
	"github.com/sc-deken/sc-deken/internal/run"
	"github.com/sc-deken/sc-deken/internal/scenv"
	"github.com/sc-deken/sc-deken/internal/state"
)

const (
	panolaCommit = "1111111111111111111111111111111111111111"
	fmx6Commit   = "2222222222222222222222222222222222222222"
)

// hostKey is the platform key of the machine running the tests, for
// SuperCollider 3.14.1 on x86_64.
func hostKey() string {
	return platform.Key{OS: platform.HostOS(), Arch: "x86_64", API: 3}.String()
}

// allBuilds gives a release a prebuilt archive for every OS the tests may run
// on, so the host OS decides which one is used.
func allBuilds(name string) []index.Build {
	var builds []index.Build
	for _, p := range []string{"windows-x86_64-api3", "macos-universal-api3", "linux-x86_64-api3"} {
		builds = append(builds, index.Build{
			Platform: p, URL: "https://example.org/" + name + "-" + p + ".zip",
			SHA256: "abcd", Size: 1000,
		})
	}
	return builds
}

func cmakeBuild() *recipe.Build { return &recipe.Build{System: "cmake", SCPathVar: "SC_PATH"} }

func pkg(name string, kind recipe.Kind, trust index.Trust, commit string) index.Package {
	return index.Package{
		Name: name, Kind: kind, Trust: trust,
		Repo:     "https://github.com/example/" + name,
		Versions: []index.Release{{Version: "1.0.0", Commit: commit}},
	}
}

// fixtureIndex is the catalog the planning tests (and later install tests)
// run against.
func fixtureIndex() *index.Index {
	fmx6 := pkg("fmx6", recipe.KindPlugin, index.Reviewed, fmx6Commit)
	fmx6.Build = cmakeBuild()
	fmx6.Versions[0].Builds = allBuilds("fmx6")

	hybrid := pkg("Hybrid", recipe.KindPluginQuark, index.Reviewed, fmx6Commit)
	hybrid.Build = cmakeBuild()
	hybrid.Versions[0].Builds = allBuilds("hybrid")

	wild := pkg("Wild", recipe.KindPlugin, index.Unreviewed, fmx6Commit)
	wild.Build = cmakeBuild()
	raw := pkg("Raw", recipe.KindPlugin, index.Unreviewed, fmx6Commit)

	a := pkg("A", recipe.KindQuark, index.Reviewed, panolaCommit)
	a.Depends = map[string]string{"B": "*"}
	b := pkg("B", recipe.KindQuark, index.Reviewed, panolaCommit)
	b.Depends = map[string]string{"c": "*"}
	c := pkg("C", recipe.KindQuark, index.Reviewed, panolaCommit)

	ping := pkg("Ping", recipe.KindQuark, index.Reviewed, panolaCommit)
	ping.Depends = map[string]string{"Pong": "*"}
	pong := pkg("Pong", recipe.KindQuark, index.Reviewed, panolaCommit)
	pong.Depends = map[string]string{"Ping": "*"}

	lost := pkg("Lost", recipe.KindQuark, index.Reviewed, panolaCommit)
	lost.Depends = map[string]string{"Nowhere": "*"}

	return &index.Index{
		Schema:     index.SchemaVersion,
		SCVersions: []platform.SCVersion{{Version: "3.14.1", API: 3, Min: "3.13.0"}},
		Packages: []index.Package{
			fmx6, hybrid, wild, raw, a, b, c, ping, pong, lost,
			pkg("Panola", recipe.KindQuark, index.Reviewed, panolaCommit),
			pkg("Evil", recipe.KindQuark, index.Reviewed, panolaCommit),
			pkg("Shaky", recipe.KindQuark, index.Reviewed, panolaCommit),
		},
		Advisories: []advisory.Advisory{
			{ID: "SCD-1", Package: "Evil", Versions: []string{"1.0.0"}, Severity: advisory.Malware, Summary: "steals files"},
			{ID: "SCD-2", Package: "Shaky", Versions: []string{"1.0.0"}, Severity: advisory.Broken, Summary: "crashes the server"},
		},
	}
}

// newService returns a Service over fixtureIndex for SuperCollider 3.14.1 on
// x86_64 with nothing installed and a fake process runner.
func newService(t *testing.T) *Service {
	t.Helper()
	return &Service{
		Index:  fixtureIndex(),
		Ctx:    scenv.Context{SCVersion: "3.14.1", SCArch: "x86_64"},
		State:  &state.State{Packages: map[string]state.Entry{}},
		Runner: &run.Fake{},
		Env:    doctor.Env{OS: platform.HostOS()},
	}
}
