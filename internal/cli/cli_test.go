package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shimpe/scdeken/internal/archive"
	"github.com/shimpe/scdeken/internal/index"
	"github.com/shimpe/scdeken/internal/install"
	"github.com/shimpe/scdeken/internal/platform"
	"github.com/shimpe/scdeken/internal/recipe"
	"github.com/shimpe/scdeken/internal/run"
	"github.com/shimpe/scdeken/internal/scenv"
	"github.com/shimpe/scdeken/internal/sign"
	"github.com/shimpe/scdeken/internal/state"
)

const password = "test-password"

var (
	keyOnce sync.Once
	keyPub  string
	keyPriv sign.Key
)

// testKey makes one throwaway signing key per test binary, unlocked once:
// generating and unlocking each run scrypt (about a second and 1 GiB).
func testKey() (string, sign.Key) {
	keyOnce.Do(func() {
		pub, encrypted, err := sign.GenerateKey(password)
		if err != nil {
			panic(err)
		}
		if keyPriv, err = sign.Unlock(encrypted, password); err != nil {
			panic(err)
		}
		keyPub = pub
	})
	return keyPub, keyPriv
}

// world is a temp machine: a context file, a signed index server and a zip.
type world struct {
	dir     string
	ctxFile string
	ctx     scenv.Context
	url     string
	pub     string
}

// serveZip packs one plugin file into a zip and serves it.
func serveZip(t *testing.T) (url, sum string) {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "FMX6.scx"), []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(t.TempDir(), "pkg.zip")
	if err := archive.Zip(src, zip); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(zip)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(data) }))
	t.Cleanup(srv.Close)
	h := sha256.Sum256(data)
	return srv.URL, hex.EncodeToString(h[:])
}

func testIndex(zipURL, sum string) *index.Index {
	var builds []index.Build
	for _, p := range []string{"windows-x86_64-api3", "macos-universal-api3", "linux-x86_64-api3"} {
		builds = append(builds, index.Build{Platform: p, URL: zipURL, SHA256: sum, Size: 100})
	}
	mk := func(name string, trust index.Trust) index.Package {
		return index.Package{
			Name: name, Summary: name + " synth", Repo: "https://github.com/example/" + name,
			Kind: recipe.KindPlugin, Trust: trust, Stars: 3,
			Build:    &recipe.Build{System: "cmake", SCPathVar: "SC_PATH"},
			Versions: []index.Release{{Version: "1.0.0", Commit: strings.Repeat("2", 40), Builds: builds}},
		}
	}
	quark := func(name string, trust index.Trust, dep string) index.Package {
		return index.Package{
			Name: name, Repo: "https://github.com/example/" + name, Kind: recipe.KindQuark, Trust: trust,
			Depends:  map[string]string{dep: "*"},
			Versions: []index.Release{{Version: "1.0.0", Commit: strings.Repeat("3", 40)}},
		}
	}
	return &index.Index{
		Schema: index.SchemaVersion, Generated: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		SCVersions: []platform.SCVersion{{Version: "3.14.1", API: 3, Min: "3.13.0"}},
		Packages: []index.Package{
			mk("fmx6", index.Reviewed), mk("wild", index.Unreviewed),
			quark("risky-app", index.Unreviewed, "fmx6"), // unreviewed main, reviewed dependency
			quark("safe-app", index.Reviewed, "wild"),    // reviewed main, unreviewed dependency
			urlDepQuark(), // reviewed main, dependency only known by repository address
		},
	}
}

// urlDepQuark is a reviewed quark that depends on a repository the index does
// not list.
func urlDepQuark() index.Package {
	return index.Package{
		Name: "url-app", Repo: "https://github.com/example/url-app", Kind: recipe.KindQuark, Trust: index.Reviewed,
		DependsUnresolved: map[string]string{"https://github.com/other/Extra": "=1.0"},
		Versions:          []index.Release{{Version: "1.0.0", Commit: strings.Repeat("4", 40)}},
	}
}

// newWorld starts an index server and writes a context file inside a folder
// whose name contains a space.
func newWorld(t *testing.T) *world {
	t.Helper()
	pub, priv := testKey()
	zipURL, sum := serveZip(t)
	body, err := testIndex(zipURL, sum).Encode()
	if err != nil {
		t.Fatal(err)
	}
	sig := priv.Sign(body)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".minisig") {
			w.Write(sig)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)

	dir := filepath.Join(t.TempDir(), "Stefaan Himpe")
	w := &world{dir: dir, url: srv.URL + "/index.json", pub: pub}
	w.ctx = scenv.Context{
		SCVersion: "3.14.1", SCArch: "x86_64",
		AppSupportDir: filepath.Join(dir, "support"), ExtensionDir: filepath.Join(dir, "ext"),
		Job: "job1",
	}
	w.writeContext(t)
	return w
}

func (w *world) writeContext(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(w.ctx)
	w.ctxFile = filepath.Join(w.dir, "context.json")
	if err := os.WriteFile(w.ctxFile, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// call runs the CLI in-process with the global flags prepended.
func (w *world) call(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	all := append([]string{"--context", w.ctxFile, "--index-url", w.url}, args...)
	code = mainWithDeps(all, &out, &errb, Deps{Runner: &run.Fake{}, EmbeddedKey: w.pub})
	return code, out.String(), errb.String()
}

func decode(t *testing.T, s string) Response {
	t.Helper()
	var r Response
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		t.Fatalf("not a Response: %v\n%s", err, s)
	}
	return r
}

func TestSearchCommand(t *testing.T) {
	w := newWorld(t)
	code, out, errs := w.call(t, "search", "fm")
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errs)
	}
	r := decode(t, out)
	hits, _ := r.Data.([]any)
	if !r.OK || len(hits) != 1 || !strings.HasPrefix(hits[0].(map[string]any)["name"].(string), "fmx6") {
		t.Errorf("response = %+v", r)
	}
}

func TestUnknownCommand(t *testing.T) {
	w := newWorld(t)
	code, _, errs := w.call(t, "frobnicate")
	if code != 2 || !strings.Contains(errs, "usage") {
		t.Errorf("exit %d, stderr %q", code, errs)
	}
	if code, _, errs = w.call(t, "install"); code != 2 || !strings.Contains(errs, "usage") {
		t.Errorf("install without a package: exit %d, stderr %q", code, errs)
	}
}

func TestInstallNeedsConfirmation(t *testing.T) {
	w := newWorld(t)
	code, out, _ := w.call(t, "install", "wild")
	r := decode(t, out)
	if code != 3 || r.OK || !reflect.DeepEqual(r.Confirm, []string{"unreviewed"}) {
		t.Fatalf("exit %d, response %+v", code, r)
	}
	if plan, _ := r.Data.(map[string]any); plan["package"] != "wild" {
		t.Errorf("data = %+v, want the plan needing confirmation", r.Data)
	}
}

func TestInstallPrebuilt(t *testing.T) {
	w := newWorld(t)
	code, out, errs := w.call(t, "install", "fmx6@1.0.0")
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errs)
	}
	data := decode(t, out).Data.(map[string]any)
	results := data["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %v", results)
	}
	if _, err := os.Stat(filepath.Join(w.ctx.ExtensionDir, "sc-deken", "fmx6", "FMX6.scx")); err != nil {
		t.Errorf("plugin not installed: %v", err)
	}
	_, out, _ = w.call(t, "list")
	if items, _ := decode(t, out).Data.([]any); len(items) != 1 {
		t.Errorf("list = %s", out)
	}
	if code, _, errs := w.call(t, "remove", "fmx6"); code != 0 {
		t.Errorf("remove: exit %d, stderr %s", code, errs)
	}
}

func TestInfoCommand(t *testing.T) {
	w := newWorld(t)
	code, out, _ := w.call(t, "info", "fmx6")
	data, _ := decode(t, out).Data.(map[string]any)
	if code != 0 || data["package"] == nil || data["retractions"] == nil {
		t.Errorf("exit %d, data %v", code, data)
	}
	if code, _, _ := w.call(t, "info", "nothing-here"); code != 1 {
		t.Errorf("unknown package: exit %d, want 1", code)
	}
}

func TestRunRequestFile(t *testing.T) {
	w := newWorld(t)
	_, want, _ := w.call(t, "search", "fm")

	req := Request{Command: "search", Query: "fm", IndexURL: w.url, Context: w.ctx}
	reqPath, resPath := filepath.Join(w.dir, "request.json"), filepath.Join(w.dir, "result.json")
	data, _ := json.Marshal(req)
	if err := os.WriteFile(reqPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errs := w.call(t, "run", reqPath, resPath)
	if code != 0 || out != "" {
		t.Fatalf("exit %d, stdout %q, stderr %s", code, out, errs)
	}
	got, err := os.ReadFile(resPath)
	if err != nil || string(got) != want {
		t.Errorf("result file = %q (%v), want %q", got, err, want)
	}
}

func TestRunSpacedPaths(t *testing.T) {
	w := newWorld(t)
	reqPath, resPath := filepath.Join(w.dir, "my request.json"), filepath.Join(w.dir, "my result.json")
	data, _ := json.Marshal(Request{Command: "install", Package: "wild", IndexURL: w.url, Context: w.ctx})
	os.WriteFile(reqPath, data, 0o644)
	if code, _, errs := w.call(t, "run", reqPath, resPath); code != 3 {
		t.Fatalf("exit %d, stderr %s", code, errs)
	}
	got, _ := os.ReadFile(resPath)
	if r := decode(t, string(got)); !reflect.DeepEqual(r.Confirm, []string{"unreviewed"}) {
		t.Errorf("response = %+v", r)
	}
}

func TestRunBadRequest(t *testing.T) {
	w := newWorld(t)
	reqPath, resPath := filepath.Join(w.dir, "bad.json"), filepath.Join(w.dir, "result.json")
	os.WriteFile(reqPath, []byte("{nope"), 0o644)
	code, _, _ := w.call(t, "run", reqPath, resPath)
	got, _ := os.ReadFile(resPath)
	if r := decode(t, string(got)); code != 1 || r.OK || r.Error == "" {
		t.Errorf("exit %d, response %+v", code, r)
	}
}

func TestOSCLogDuringInstall(t *testing.T) {
	w := newWorld(t)
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	w.ctx.OSCPort = conn.LocalAddr().(*net.UDPAddr).Port
	w.writeContext(t)

	if code, _, errs := w.call(t, "install", "fmx6"); code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errs)
	}
	var sawLog, sawProgress bool
	buf := make([]byte, 8192)
	for !(sawLog && sawProgress) {
		conn.SetReadDeadline(time.Now().Add(time.Second))
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			break
		}
		sawLog = sawLog || bytes.HasPrefix(buf[:n], []byte("/deken/log"))
		sawProgress = sawProgress || bytes.HasPrefix(buf[:n], []byte("/deken/progress"))
	}
	if !sawLog || !sawProgress {
		t.Errorf("log = %v, progress = %v", sawLog, sawProgress)
	}
}

func TestPlatformCommand(t *testing.T) {
	w := newWorld(t)
	data := func() map[string]any {
		_, out, _ := w.call(t, "platform")
		return decode(t, out).Data.(map[string]any)
	}
	if d := data(); d["api_known"] != true || d["key"] == "" || d["api"] != float64(3) {
		t.Errorf("3.14.1: %v", d)
	}
	w.ctx.SCVersion = "3.15.0-dev"
	w.writeContext(t)
	if d := data(); d["api_known"] != false || d["key"] != "" {
		t.Errorf("3.15.0-dev: %v", d)
	}
}

func TestDoctorNeedsNoIndex(t *testing.T) {
	w := newWorld(t)
	w.url = "http://127.0.0.1:1/index.json" // nothing listens here
	code, out, errs := w.call(t, "doctor")
	data, _ := decode(t, out).Data.(map[string]any)
	if code != 0 || data["items"] == nil {
		t.Errorf("exit %d, out %s, stderr %s", code, out, errs)
	}
}

func TestVersionCommand(t *testing.T) {
	w := newWorld(t)
	_, out, _ := w.call(t, "version")
	if d := decode(t, out).Data.(map[string]any); d["version"] != "dev" {
		t.Errorf("data = %v", d)
	}
}

// nothingInstalled reports whether the managed folder and state are untouched.
func nothingInstalled(w *world) bool {
	_, errDir := os.Stat(filepath.Join(w.ctx.ExtensionDir, "sc-deken"))
	_, errState := os.Stat(filepath.Join(w.ctx.AppSupportDir, "sc-deken", "state.json"))
	return os.IsNotExist(errDir) && os.IsNotExist(errState)
}

func TestInstallUnreviewedMainWritesNothingFirst(t *testing.T) {
	w := newWorld(t)
	code, out, _ := w.call(t, "install", "risky-app")
	r := decode(t, out)
	plan, _ := r.Data.(map[string]any)
	if code != 3 || !reflect.DeepEqual(r.Confirm, []string{"unreviewed"}) || plan["package"] != "risky-app" {
		t.Fatalf("exit %d, response %+v", code, r)
	}
	if !nothingInstalled(w) {
		t.Error("the reviewed dependency was installed before the main package was confirmed")
	}

	code, out, errs := w.call(t, "install", "risky-app", "--yes-unreviewed")
	results, _ := decode(t, out).Data.(map[string]any)["results"].([]any)
	if code != 0 || len(results) != 2 {
		t.Fatalf("exit %d, results %v, stderr %s", code, results, errs)
	}
	first := results[0].(map[string]any)["plan"].(map[string]any)["package"]
	last := results[1].(map[string]any)["plan"].(map[string]any)["package"]
	if first != "fmx6" || last != "risky-app" {
		t.Errorf("order = %v, %v; want dependency first, main last", first, last)
	}
}

func TestInstallUnreviewedDependencyNeedsConfirmation(t *testing.T) {
	w := newWorld(t)
	code, out, _ := w.call(t, "install", "safe-app")
	r := decode(t, out)
	plan, _ := r.Data.(map[string]any)
	deps, _ := plan["depends"].([]any)
	if code != 3 || !reflect.DeepEqual(r.Confirm, []string{"unreviewed"}) || plan["package"] != "safe-app" || len(deps) != 1 {
		t.Fatalf("exit %d, response %+v", code, r)
	}
	if conf, _ := deps[0].(map[string]any)["confirm"].([]any); len(conf) != 1 || conf[0] != "unreviewed" {
		t.Errorf("dependency plan does not carry its own confirm list: %v", deps[0])
	}
	if !nothingInstalled(w) {
		t.Error("something was installed before the dependency was confirmed")
	}
}

func TestInstallQuarkWithURLDependencyNeedsConfirmation(t *testing.T) {
	w := newWorld(t)
	code, out, _ := w.call(t, "install", "url-app")
	r := decode(t, out)
	plan, _ := r.Data.(map[string]any)
	deps, _ := plan["depends"].([]any)
	if code != 3 || !reflect.DeepEqual(r.Confirm, []string{"unreviewed"}) || plan["package"] != "url-app" || len(deps) != 1 {
		t.Fatalf("exit %d, response %+v", code, r)
	}
	dep := deps[0].(map[string]any)
	if dep["package"] != "Extra" || dep["repo"] != "https://github.com/other/Extra" || dep["route"] != "quark" || dep["trust"] != "unreviewed" {
		t.Errorf("dependency plan = %v", dep)
	}
	if conf, _ := dep["confirm"].([]any); len(conf) != 1 || conf[0] != "unreviewed" {
		t.Errorf("dependency confirm = %v", dep["confirm"])
	}
}

// runRequest sends req through a request file, as SuperCollider does, and
// returns the exit code and the response.
func (w *world) runRequest(t *testing.T, req Request) (int, Response) {
	t.Helper()
	req.IndexURL, req.Context = w.url, w.ctx
	reqPath, resPath := filepath.Join(w.dir, "request.json"), filepath.Join(w.dir, "result.json")
	data, _ := json.Marshal(req)
	if err := os.WriteFile(reqPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errs := w.call(t, "run", reqPath, resPath)
	got, err := os.ReadFile(resPath)
	if err != nil {
		t.Fatalf("no result (exit %d, stderr %s): %v", code, errs, err)
	}
	return code, decode(t, string(got))
}

const changedMessage = "The package list changed since you confirmed; please check again."

func TestInstallWithTheConfirmedCommits(t *testing.T) {
	w := newWorld(t)
	choices := install.Choices{Commits: map[string]string{"fmx6": strings.Repeat("2", 40)}}
	if code, r := w.runRequest(t, Request{Command: "install", Package: "fmx6", Choices: choices}); code != 0 || !r.OK {
		t.Fatalf("exit %d, response %+v", code, r)
	}
}

func TestInstallRefusesAChangedCommit(t *testing.T) {
	w := newWorld(t)
	choices := install.Choices{ConfirmUnreviewed: true, Commits: map[string]string{"wild": strings.Repeat("9", 40)}}
	code, r := w.runRequest(t, Request{Command: "install", Package: "wild", Choices: choices})
	plan, _ := r.Data.(map[string]any)
	if code != 3 || !reflect.DeepEqual(r.Confirm, []string{"changed"}) || r.Error != changedMessage || plan["package"] != "wild" {
		t.Fatalf("exit %d, response %+v", code, r)
	}
	if !nothingInstalled(w) {
		t.Error("something was installed")
	}
}

// A dependency the user never saw makes the plan a different one too.
func TestInstallRefusesAnUnconfirmedPackage(t *testing.T) {
	w := newWorld(t)
	choices := install.Choices{ConfirmUnreviewed: true, Commits: map[string]string{"risky-app": strings.Repeat("3", 40)}}
	code, r := w.runRequest(t, Request{Command: "install", Package: "risky-app", Choices: choices})
	if code != 3 || !reflect.DeepEqual(r.Confirm, []string{"changed"}) {
		t.Fatalf("exit %d, response %+v", code, r)
	}
}

func TestInstallChangedAndUnconfirmed(t *testing.T) {
	w := newWorld(t)
	choices := install.Choices{Commits: map[string]string{"wild": strings.Repeat("9", 40)}}
	code, r := w.runRequest(t, Request{Command: "install", Package: "wild", Choices: choices})
	if code != 3 || !reflect.DeepEqual(r.Confirm, []string{"unreviewed", "changed"}) {
		t.Fatalf("exit %d, response %+v", code, r)
	}
}

// installedBefore records packages as installed by sc-deken at version 0.9.0,
// older than the 1.0.0 the index offers.
func (w *world) installedBefore(t *testing.T, names ...string) {
	t.Helper()
	st := &state.State{Packages: map[string]state.Entry{}}
	for _, name := range names {
		dir := filepath.Join(w.ctx.ExtensionDir, "sc-deken", name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		st.Packages[name] = state.Entry{Name: name, Version: "0.9.0", Kind: "plugin", Path: dir, Route: state.Prebuilt}
	}
	if err := st.Save(w.ctx.Paths().State); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateEverything(t *testing.T) {
	w := newWorld(t)
	w.installedBefore(t, "fmx6")
	code, out, errs := w.call(t, "update")
	results, _ := decode(t, out).Data.(map[string]any)["results"].([]any)
	if code != 0 || len(results) != 1 {
		t.Fatalf("exit %d, results %v, stderr %s", code, results, errs)
	}
	if _, err := os.Stat(filepath.Join(w.ctx.ExtensionDir, "sc-deken", "fmx6", "FMX6.scx")); err != nil {
		t.Errorf("not updated: %v", err)
	}
}

func TestUpdateEverythingAsksFirst(t *testing.T) {
	w := newWorld(t)
	w.installedBefore(t, "fmx6", "wild")
	code, out, _ := w.call(t, "update")
	r := decode(t, out)
	plans, _ := r.Data.(map[string]any)["plans"].([]any)
	if code != 3 || !reflect.DeepEqual(r.Confirm, []string{"unreviewed"}) || len(plans) != 2 {
		t.Fatalf("exit %d, response %+v", code, r)
	}
	if _, err := os.Stat(filepath.Join(w.ctx.ExtensionDir, "sc-deken", "fmx6", "FMX6.scx")); !os.IsNotExist(err) {
		t.Error("fmx6 was updated before the user answered for wild")
	}
}

func TestUpdateOnePackage(t *testing.T) {
	w := newWorld(t)
	w.installedBefore(t, "fmx6", "wild")
	if code, _, errs := w.call(t, "update", "fmx6"); code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errs)
	}
}
