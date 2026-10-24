package install

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shimpe/scdeken/internal/archive"
	"github.com/shimpe/scdeken/internal/build"
	"github.com/shimpe/scdeken/internal/headers"
	"github.com/shimpe/scdeken/internal/platform"
	"github.com/shimpe/scdeken/internal/recipe"
	"github.com/shimpe/scdeken/internal/run"
	"github.com/shimpe/scdeken/internal/state"
)

// Choices are the user's answers to the confirmations a plan asked for.
type Choices struct {
	ConfirmUnreviewed bool   `json:"confirm_unreviewed"`
	ConfirmRetracted  bool   `json:"confirm_retracted"`
	OnDuplicate       string `json:"on_duplicate"` // "replace" | "keep" | ""
}

// ConfirmationError lists the confirmations still missing.
type ConfirmationError struct{ Needed []string }

func (e *ConfirmationError) Error() string {
	return "needs confirmation: " + strings.Join(e.Needed, ", ")
}

// Result is what Apply did.
type Result struct {
	Plan        Plan     `json:"plan"`
	InstalledTo string   `json:"installed_to,omitempty"`
	BackedUp    []string `json:"backed_up,omitempty"`
	Kept        bool     `json:"kept,omitempty"` // user chose keep-existing
}

// Apply carries out a plan without any confirmations.
func (s *Service) Apply(ctx context.Context, p Plan) (Result, error) {
	return s.ApplyWith(ctx, p, Choices{})
}

// ApplyWith carries out a plan. Files are built in a staging folder and
// moved into place only when complete, so a failure leaves nothing behind.
func (s *Service) ApplyWith(ctx context.Context, p Plan, c Choices) (Result, error) {
	if err := c.Validate(); err != nil {
		return Result{}, err
	}
	if missing := p.Missing(c); len(missing) > 0 {
		return Result{}, &ConfirmationError{Needed: missing}
	}
	res := Result{Plan: p}
	if p.Route == RouteQuark {
		return res, nil // sclang runs Quarks.install
	}
	if c.OnDuplicate == "keep" && len(p.Duplicates) > 0 {
		return s.keepExisting(res)
	}
	var dups []Duplicate
	if c.OnDuplicate == "replace" {
		dups = p.Duplicates
	}
	target, backups, err := s.install(ctx, p, dups)
	if err != nil {
		return res, err // install undoes its own work, so nothing is installed
	}
	res.InstalledTo = target
	for _, b := range backups {
		res.BackedUp = append(res.BackedUp, b.dst)
	}
	if err := s.record(p, target); err != nil {
		return res, err // the files are in place even though state is not saved
	}
	s.progress(1, "Done")
	return res, nil
}

// Validate rejects an OnDuplicate answer that is neither empty, "replace"
// nor "keep".
func (c Choices) Validate() error {
	switch c.OnDuplicate {
	case "", "replace", "keep":
		return nil
	}
	return fmt.Errorf("unknown choice for existing copies: %q (use replace or keep)", c.OnDuplicate)
}

// Missing returns the confirmations this plan asks for (not those of its
// Depends) that the choices do not give.
func (p Plan) Missing(c Choices) []string {
	given := map[string]bool{
		ConfirmUnreviewed: c.ConfirmUnreviewed,
		ConfirmRetracted:  c.ConfirmRetracted,
		ConfirmDuplicates: c.OnDuplicate != "",
	}
	var missing []string
	for _, need := range p.Confirm {
		if !given[need] {
			missing = append(missing, need)
		}
	}
	return missing
}

// keepExisting records the user's own copy instead of installing.
func (s *Service) keepExisting(res Result) (Result, error) {
	entry := s.entryFor(res.Plan, res.Plan.Duplicates[0].Path)
	// The version of a copy the user made is unknown; never claim the plan's.
	entry.Route, entry.Platform, entry.Version, entry.Commit = state.External, "", "", ""
	if err := s.save(entry); err != nil {
		return Result{}, err
	}
	res.Kept = true
	return res, nil
}

// install fills a staging folder, moves dups out of the way and swaps the
// staging folder into the managed folder. It returns the package folder and
// the backups made. On error nothing has changed and the staging folder is
// gone.
func (s *Service) install(ctx context.Context, p Plan, dups []Duplicate) (target string, backups []backup, err error) {
	name := strings.ToLower(p.Package)
	staging := filepath.Join(s.Paths.Managed, ".staging", name)
	target = filepath.Join(s.Paths.Managed, name)
	if err := os.RemoveAll(staging); err != nil {
		return "", nil, fmt.Errorf("clear old staging folder %s: %w", staging, err)
	}
	defer func() {
		os.RemoveAll(staging)
		os.Remove(filepath.Dir(staging)) // only succeeds when empty
	}()
	if err := os.MkdirAll(filepath.Dir(staging), 0o755); err != nil {
		return "", nil, fmt.Errorf("create staging folder: %w", err)
	}
	if err := s.stage(ctx, p, staging); err != nil {
		return "", nil, err
	}
	if backups, err = s.backUp(dups); err != nil {
		return "", nil, err
	}
	fmt.Fprintf(s.logger(), "Installing %s into %s\n", p.Package, target)
	if err := swapIn(staging, target); err != nil {
		return "", nil, errors.Join(err, s.restore(backups))
	}
	return target, backups, nil
}

// stage builds the finished package in staging.
func (s *Service) stage(ctx context.Context, p Plan, staging string) error {
	if err := s.fill(ctx, p, staging); err != nil {
		return err
	}
	if p.Kind == recipe.KindPluginQuark {
		if err := stripClasses(staging); err != nil {
			return err
		}
	}
	s.clearQuarantine(ctx, staging)
	return nil
}

// fill puts the package's plugin files into staging.
func (s *Service) fill(ctx context.Context, p Plan, staging string) error {
	switch p.Route {
	case RoutePrebuilt:
		return s.fillPrebuilt(ctx, p, staging)
	case RouteLocalBuild:
		return s.fillBuilt(ctx, p, staging)
	}
	return fmt.Errorf("cannot install %s: unknown route %q", p.Package, p.Route)
}

// fillPrebuilt downloads the archive (checking its hash) and unpacks it.
func (s *Service) fillPrebuilt(ctx context.Context, p Plan, staging string) error {
	if p.Build == nil {
		return fmt.Errorf("%s has no prebuilt archive to download", p.Package)
	}
	if err := os.MkdirAll(s.Paths.Cache, 0o755); err != nil {
		return fmt.Errorf("create download folder: %w", err)
	}
	zip := filepath.Join(s.Paths.Cache, fmt.Sprintf("%s-%s-%s.zip", p.Package, p.Version, p.Platform))
	fmt.Fprintf(s.logger(), "Downloading %s %s\n", p.Package, p.Version)
	err := s.Fetch.Download(ctx, p.Build.URL, zip, p.Build.SHA256, func(done, total int64) {
		if total > 0 {
			s.progress(0.8*float32(done)/float32(total), "Downloading "+p.Package)
		}
	})
	if err != nil {
		return err
	}
	s.progress(0.9, "Unpacking "+p.Package)
	return archive.Unzip(zip, staging)
}

// fillBuilt compiles the package from source into staging.
func (s *Service) fillBuilt(ctx context.Context, p Plan, staging string) error {
	opts, err := s.buildOptions(p, staging)
	if err != nil {
		return err
	}
	sc, err := s.Ctx.Version()
	if err != nil {
		return err
	}
	if err := os.RemoveAll(opts.WorkDir); err != nil {
		return fmt.Errorf("clear work folder %s: %w", opts.WorkDir, err)
	}
	defer os.RemoveAll(opts.WorkDir)
	fmt.Fprintf(s.logger(), "Building %s %s on this computer\n", p.Package, p.Version)
	s.progress(0.1, "Fetching SuperCollider headers")
	h, err := headers.Ensure(ctx, s.Runner, s.Paths.Headers, sc, s.Log)
	if err != nil {
		return err
	}
	opts.HeadersDir = h.Dir
	s.progress(0.3, "Building "+p.Package)
	if _, err := build.Run(ctx, s.Runner, opts); err != nil {
		return fmt.Errorf("build %s: %w", p.Package, err)
	}
	s.progress(0.9, "Unpacking "+p.Package)
	return nil
}

// buildOptions gathers the recipe data and folders for a local build.
func (s *Service) buildOptions(p Plan, staging string) (build.Options, error) {
	pkg, ok := s.Index.Find(p.Package)
	if !ok || pkg.Build == nil {
		return build.Options{}, fmt.Errorf("%s has no build recipe", p.Package)
	}
	arch, err := s.Ctx.Arch()
	if err != nil {
		return build.Options{}, err
	}
	return build.Options{
		RepoURL:  pkg.Repo,
		Commit:   p.Commit,
		Build:    *pkg.Build,
		Requires: pkg.Requires,
		WorkDir:  filepath.Join(s.Paths.Work, p.Package+"-"+p.Version),
		OutDir:   staging,
		OS:       platform.HostOS(),
		Arch:     arch,
		Log:      s.Log,
	}, nil
}

// clearQuarantine removes macOS' download quarantine flag so the plugins
// load. Errors are ignored: the flag may simply not be there.
func (s *Service) clearQuarantine(ctx context.Context, staging string) {
	if platform.HostOS() != "macos" {
		return
	}
	_ = s.Runner.Run(ctx, run.Cmd{Name: "xattr", Args: []string{"-dr", "com.apple.quarantine", staging}})
}

// stripClasses removes sclang sources and help from dir, so the quark part
// is the only source of classes and no class is defined twice.
func stripClasses(dir string) error {
	return walkRemove(dir, func(path string, isDir bool) bool {
		if isDir {
			return filepath.Base(path) == "HelpSource"
		}
		ext := strings.ToLower(filepath.Ext(path))
		return ext == ".sc" || ext == ".schelp"
	})
}

// walkRemove deletes every file or folder under dir for which drop is true.
func walkRemove(dir string, drop func(path string, isDir bool) bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("clean %s: %w", dir, err)
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		switch {
		case drop(path, e.IsDir()):
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("remove %s: %w", path, err)
			}
		case e.IsDir():
			if err := walkRemove(path, drop); err != nil {
				return err
			}
		}
	}
	return nil
}

// entryFor is the state record of p installed at path.
func (s *Service) entryFor(p Plan, path string) state.Entry {
	entry := state.Entry{
		Name: p.Package, Version: p.Version, Commit: p.Commit,
		Platform: p.Platform, Path: path, Kind: string(p.Kind),
		InstalledAt: time.Now().UTC(),
	}
	if pkg, ok := s.Index.Find(p.Package); ok {
		entry.Repo = pkg.Repo
	}
	return entry
}

// record notes a finished install in the state file.
func (s *Service) record(p Plan, target string) error {
	entry := s.entryFor(p, target)
	entry.Route = state.Route(p.Route)
	return s.save(entry)
}

// save stores entry in memory and on disk; memory is restored on failure.
func (s *Service) save(entry state.Entry) error {
	key := strings.ToLower(entry.Name)
	prev, had := s.State.Packages[key]
	s.State.Packages[key] = entry
	if err := s.State.Save(s.Paths.State); err != nil {
		if had {
			s.State.Packages[key] = prev
		} else {
			delete(s.State.Packages, key)
		}
		return err
	}
	return nil
}

// backup is one existing copy moved aside.
type backup struct{ orig, dst string }

// backUp moves each duplicate under Backup/<timestamp>/. Nothing is ever
// deleted. If one cannot be moved, the ones already moved are put back and
// the error names the path that failed.
func (s *Service) backUp(dups []Duplicate) ([]backup, error) {
	if len(dups) == 0 {
		return nil, nil
	}
	dir := filepath.Join(s.Paths.Backup, time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create backup folder: %w", err)
	}
	var done []backup
	for _, d := range dups {
		dst := freePath(filepath.Join(dir, filepath.Base(d.Path)))
		if err := moveTree(d.Path, dst); err != nil {
			err = fmt.Errorf("Couldn't move %s out of the way (%w). It may need administrator rights, or SuperCollider may be using it.", d.Path, err)
			return nil, errors.Join(err, s.restore(done))
		}
		fmt.Fprintf(s.logger(), "Moved your old copy %s to %s\n", d.Path, dst)
		done = append(done, backup{orig: d.Path, dst: dst})
	}
	return done, nil
}

// restore puts backed-up copies back where they were.
func (s *Service) restore(backups []backup) error {
	var errs []error
	for i := len(backups) - 1; i >= 0; i-- {
		b := backups[i]
		if err := moveTree(b.dst, b.orig); err != nil {
			errs = append(errs, fmt.Errorf("your copy of %s is now in %s: %w", b.orig, b.dst, err))
		}
	}
	return errors.Join(errs...)
}

// freePath returns path, or path-2, path-3... when it is taken.
func freePath(path string) string {
	candidate := path
	for n := 2; ; n++ {
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", path, n)
	}
}

func (s *Service) logger() io.Writer {
	if s.Log == nil {
		return io.Discard
	}
	return s.Log
}

func (s *Service) progress(fraction float32, label string) {
	if s.Progress != nil {
		s.Progress(fraction, label)
	}
}
