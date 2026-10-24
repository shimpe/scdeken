package build

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/sc-deken/sc-deken/internal/run"
)

// compileUniversal builds arm64 and x86_64 separately, then merges them into
// OutDir with lipo.
func (o Options) compileUniversal(ctx context.Context, r run.Runner) error {
	if o.OS != "macos" {
		return fmt.Errorf("universal builds are only possible on macOS, not %s", o.OS)
	}
	arm := filepath.Join(o.WorkDir, "out-arm64")
	intel := filepath.Join(o.WorkDir, "out-x86_64")
	if err := o.buildArch(ctx, r, "arm64", arm); err != nil {
		return err
	}
	if err := o.buildArch(ctx, r, "x86_64", intel); err != nil {
		return err
	}
	return o.mergeUniversal(ctx, r, arm, intel)
}

// mergeUniversal copies the arm64 tree to OutDir and replaces every plugin
// file with the lipo of its arm64 and x86_64 builds.
func (o Options) mergeUniversal(ctx context.Context, r run.Runner, arm, intel string) error {
	plugins, err := matchingPlugins(arm, intel)
	if err != nil {
		return err
	}
	if err := copyTree(arm, o.OutDir); err != nil {
		return err
	}
	for _, rel := range plugins {
		cmd := run.Cmd{
			Name: "lipo",
			Args: []string{"-create", filepath.Join(arm, rel), filepath.Join(intel, rel), "-output", filepath.Join(o.OutDir, rel)},
			Log:  o.Log,
		}
		if err := r.Run(ctx, cmd); err != nil {
			return fmt.Errorf("merge %s into one universal file: %w", rel, err)
		}
	}
	return nil
}

// matchingPlugins returns the plugin files both trees contain, and fails
// naming any plugin that only one tree has.
func matchingPlugins(arm, intel string) ([]string, error) {
	armPlugins, err := pluginPaths(arm)
	if err != nil {
		return nil, err
	}
	intelPlugins, err := pluginPaths(intel)
	if err != nil {
		return nil, err
	}
	for _, rel := range armPlugins {
		if !slices.Contains(intelPlugins, rel) {
			return nil, fmt.Errorf("%s was built for arm64 but not for x86_64, so no universal file can be made", rel)
		}
	}
	for _, rel := range intelPlugins {
		if !slices.Contains(armPlugins, rel) {
			return nil, fmt.Errorf("%s was built for x86_64 but not for arm64, so no universal file can be made", rel)
		}
	}
	return armPlugins, nil
}

// copyTree copies every regular file under from into to, keeping the layout.
func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(p, target)
	})
}

// copyFile copies one file, keeping its permissions.
func copyFile(from, to string) (err error) {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)
	return err
}
