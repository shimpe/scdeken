package indexer

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/shimpe/scdeken/internal/recipe"
	"github.com/shimpe/scdeken/internal/run"
)

// MatrixRow is one package version CI has to build.
type MatrixRow struct {
	Recipe  string `json:"recipe"`
	Package string `json:"package"`
	Version string `json:"version"`
}

// Matrix lists the approved versions in the recipes of dir that the git
// revision base did not have yet. A recipe that did not exist at base counts
// as having none, so all its versions are new. git runs inside dir.
func Matrix(ctx context.Context, r run.Runner, dir, base string) ([]MatrixRow, error) {
	recipes, err := recipe.LoadDir(dir)
	if err != nil {
		return nil, err
	}
	rows := []MatrixRow{}
	for _, rc := range recipes {
		file := rc.Name + ".toml"
		old, err := recipeAt(ctx, r, dir, base, file)
		if err != nil {
			return nil, err
		}
		for _, v := range rc.ApprovedVersions() {
			if _, had := old.Versions.Approved[v]; !had {
				rows = append(rows, MatrixRow{Recipe: filepath.ToSlash(filepath.Join(dir, file)), Package: rc.Name, Version: v})
			}
		}
	}
	return rows, nil
}

// recipeAt reads a recipe file as it was at revision base. A file that did
// not exist then gives an empty recipe; an unknown revision is an error.
func recipeAt(ctx context.Context, r run.Runner, dir, base, file string) (recipe.Recipe, error) {
	out, err := r.Output(ctx, run.Cmd{Name: "git", Dir: dir, Args: []string{"show", base + ":./" + file}})
	if err != nil {
		return recipe.Recipe{}, checkRevision(ctx, r, dir, base, err)
	}
	old, err := recipe.Parse([]byte(out))
	if err != nil {
		return recipe.Recipe{}, fmt.Errorf("%s at %s: %w", file, base, err)
	}
	return old, nil
}

// checkRevision turns a failed "git show" into nil (the file is new) when
// base is a real revision, and into an error otherwise.
func checkRevision(ctx context.Context, r run.Runner, dir, base string, showErr error) error {
	_, err := r.Output(ctx, run.Cmd{Name: "git", Dir: dir, Args: []string{"rev-parse", "--verify", "--quiet", base + "^{commit}"}})
	if err != nil {
		return fmt.Errorf("find revision %s: %w", base, showErr)
	}
	return nil
}
