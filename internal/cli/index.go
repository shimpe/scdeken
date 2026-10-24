package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sc-deken/sc-deken/internal/advisory"
	"github.com/sc-deken/sc-deken/internal/indexer"
	"github.com/sc-deken/sc-deken/internal/recipe"
	"github.com/sc-deken/sc-deken/internal/sign"
)

// Environment variables that hold the signing key in CI.
const (
	signingKeyEnv      = "SCDEKEN_SIGNING_KEY"
	signingPasswordEnv = "SCDEKEN_SIGNING_PASSWORD"
)

// The commands that build, sign and check the package index.
func init() {
	commands["index-build"] = command{
		usage: "index build --recipes <folder> --out <file> [--advisories <folder>] [--sc-versions <file>] [--previous <url|file>] [--releases-repo OWNER/NAME] [--directory <url|file>] [--limit N]",
		flags: func(fs *flag.FlagSet, req *Request) {
			m := &req.Maint
			fs.StringVar(&m.Recipes, "recipes", "", "folder of recipe files")
			fs.StringVar(&m.Advisories, "advisories", "", "folder of advisory files")
			fs.StringVar(&m.SCVersions, "sc-versions", "", "sc-versions.toml")
			fs.StringVar(&m.Previous, "previous", "", "last published index (address or file), reused as a scan cache")
			fs.StringVar(&m.Directory, "directory", "", "quarks directory (address or file); default the official one")
			fs.StringVar(&m.ReleasesRepo, "releases-repo", "", "GitHub repository (OWNER/NAME) with the prebuilt release assets")
			fs.StringVar(&req.Out, "out", "", "index file to write")
			fs.IntVar(&m.Limit, "limit", 0, "only look at the first N directory entries and topic repositories (0 = all)")
		},
		run: runIndexBuild,
	}
	commands["index-sign"] = command{
		usage: "index sign --in <index.json>",
		flags: func(fs *flag.FlagSet, req *Request) {
			fs.StringVar(&req.Maint.In, "in", "", "index file to sign")
		},
		run: runIndexSign,
	}
	commands["index-keygen"] = command{
		usage: "index keygen --out-dir <folder> [--password-env NAME]",
		flags: func(fs *flag.FlagSet, req *Request) {
			fs.StringVar(&req.Maint.OutDir, "out-dir", "", "folder for scdeken.pub and scdeken.key")
			fs.StringVar(&req.Maint.PasswordEnv, "password-env", "", "environment variable holding the password (default: ask)")
		},
		run: runIndexKeygen,
	}
	commands["index-new-versions"] = command{
		usage: "index new-versions --recipes <folder>",
		flags: func(fs *flag.FlagSet, req *Request) {
			fs.StringVar(&req.Maint.Recipes, "recipes", "", "folder of recipe files")
		},
		run: runIndexNewVersions,
	}
	commands["advisories-validate"] = command{
		usage: "advisories validate --dir <folder>",
		flags: func(fs *flag.FlagSet, req *Request) {
			fs.StringVar(&req.Maint.Dir, "dir", "", "folder of advisory files")
		},
		run: runAdvisoriesValidate,
	}
}

// indexBuildResult is the answer to "index build".
type indexBuildResult struct {
	Out      string `json:"out"`
	Packages int    `json:"packages"`
}

func runIndexBuild(ctx context.Context, req Request, s *session) (any, error) {
	if req.Maint.Recipes == "" || req.Out == "" {
		return nil, usageError{"index build needs --recipes and --out"}
	}
	in, err := gatherInputs(ctx, req.Maint, s)
	if err != nil {
		return nil, err
	}
	ix, err := indexer.Build(ctx, indexer.GitScanner{Runner: s.deps.Runner}, in)
	if err != nil {
		return nil, err
	}
	data, err := ix.Encode()
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(req.Out, append(data, '\n')); err != nil {
		return nil, err
	}
	return indexBuildResult{Out: req.Out, Packages: len(ix.Packages)}, nil
}

func runIndexSign(_ context.Context, req Request, _ *session) (any, error) {
	if req.Maint.In == "" {
		return nil, usageError{"index sign needs --in <index file>"}
	}
	key, password := os.Getenv(signingKeyEnv), os.Getenv(signingPasswordEnv)
	if key == "" || password == "" {
		return nil, fmt.Errorf("signing needs the environment variables %s (the encrypted key file's text) and %s", signingKeyEnv, signingPasswordEnv)
	}
	msg, err := os.ReadFile(req.Maint.In)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", req.Maint.In, err)
	}
	sig, err := sign.Sign([]byte(key), password, msg)
	if err != nil {
		return nil, err
	}
	out := req.Maint.In + ".minisig"
	if err := os.WriteFile(out, sig, 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", out, err)
	}
	return map[string]string{"signature": out}, nil
}

func runIndexKeygen(_ context.Context, req Request, s *session) (any, error) {
	dir := req.Maint.OutDir
	if dir == "" {
		return nil, usageError{"index keygen needs --out-dir <folder>"}
	}
	pubPath, keyPath := filepath.Join(dir, "scdeken.pub"), filepath.Join(dir, "scdeken.key")
	for _, p := range []string{pubPath, keyPath} {
		if _, err := os.Stat(p); err == nil {
			return nil, fmt.Errorf("%s already exists; sc-deken never overwrites a signing key", p)
		}
	}
	password, err := keyPassword(req.Maint.PasswordEnv, s)
	if err != nil {
		return nil, err
	}
	pub, encrypted, err := sign.GenerateKey(password)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	if err := writeNewFile(pubPath, []byte(pub+"\n"), 0o644); err != nil {
		return nil, err
	}
	if err := writeNewFile(keyPath, encrypted, 0o600); err != nil {
		return nil, err
	}
	return map[string]string{"public_key": pubPath, "private_key": keyPath}, nil
}

// keyPassword reads the new key's password from the environment variable env
// when given, else from one line on standard input.
func keyPassword(env string, s *session) (string, error) {
	password := ""
	if env != "" {
		if password = os.Getenv(env); password == "" {
			return "", fmt.Errorf("the environment variable %s is empty or not set", env)
		}
		return password, nil
	}
	fmt.Fprint(s.deps.Stderr, "Password for the new signing key: ")
	line, err := bufio.NewReader(s.deps.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no password was given")
	}
	if password = strings.TrimRight(line, "\r\n"); password == "" {
		return "", errors.New("the password must not be empty")
	}
	return password, nil
}

// writeNewFile creates path and fails if it already exists.
func writeNewFile(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	return f.Close()
}

// newVersionsResult is the answer to "index new-versions".
type newVersionsResult struct {
	Proposals []indexer.Proposal `json:"proposals"`
	Moved     []indexer.MovedTag `json:"moved"`
}

func runIndexNewVersions(ctx context.Context, req Request, s *session) (any, error) {
	if req.Maint.Recipes == "" {
		return nil, usageError{"index new-versions needs --recipes"}
	}
	recipes, err := recipe.LoadDir(req.Maint.Recipes)
	if err != nil {
		return nil, err
	}
	proposals, moved, err := indexer.NewVersions(ctx, indexer.GitScanner{Runner: s.deps.Runner}, recipes)
	if err != nil {
		fmt.Fprintf(s.log, "Some repositories could not be checked:\n%v\n", err)
	}
	return jsonText{newVersionsResult{Proposals: nonNilList(proposals), Moved: nonNilList(moved)}}, nil
}

func nonNilList[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}

func runAdvisoriesValidate(_ context.Context, req Request, _ *session) (any, error) {
	if req.Maint.Dir == "" {
		return nil, usageError{"advisories validate needs --dir"}
	}
	advs, err := advisory.LoadDir(req.Maint.Dir)
	if err != nil {
		return nil, err
	}
	return map[string]int{"valid": len(advs)}, nil
}
