package build

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/shimpe/scdeken/internal/run"
)

// Triplet returns the vcpkg triplet for an OS and architecture, or "" when
// vcpkg has no triplet this tool uses for that pair.
func Triplet(os, arch string) string {
	switch os + "/" + arch {
	case "linux/x86_64":
		return "x64-linux"
	case "linux/arm64":
		return "arm64-linux"
	case "macos/arm64":
		return "arm64-osx"
	case "macos/x86_64":
		return "x64-osx"
	case "windows/x86_64":
		return "x64-windows-static"
	}
	return ""
}

// nativeArch names the architecture this program runs on, in recipe terms.
func nativeArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "x86_64"
}

// vcpkgNames lists the vcpkg packages the recipe requires, sorted.
func (o Options) vcpkgNames() []string {
	var names []string
	for _, req := range o.Requires {
		if req.Vcpkg != "" {
			names = append(names, req.Vcpkg)
		}
	}
	sort.Strings(names)
	return names
}

// usesVcpkg reports whether this build installs libraries through vcpkg.
func (o Options) usesVcpkg() bool {
	return o.VcpkgRoot != "" && len(o.vcpkgNames()) > 0
}

// vcpkgExe is the path of the vcpkg program inside VcpkgRoot.
func (o Options) vcpkgExe() string {
	if o.OS == "windows" {
		return filepath.Join(o.VcpkgRoot, "vcpkg.exe")
	}
	return filepath.Join(o.VcpkgRoot, "vcpkg")
}

// vcpkgInstall installs the required libraries for one triplet.
func (o Options) vcpkgInstall(ctx context.Context, r run.Runner, triplet string) error {
	args := append([]string{"install"}, o.vcpkgNames()...)
	args = append(args, "--triplet", triplet)
	if err := r.Run(ctx, run.Cmd{Name: o.vcpkgExe(), Args: args, Log: o.Log}); err != nil {
		return fmt.Errorf("install libraries with vcpkg: %w", err)
	}
	return nil
}

// vcpkgCMakeArgs points CMake at the vcpkg toolchain for one triplet.
func (o Options) vcpkgCMakeArgs(triplet string) []string {
	toolchain := filepath.Join(o.VcpkgRoot, "scripts", "buildsystems", "vcpkg.cmake")
	args := []string{"-DCMAKE_TOOLCHAIN_FILE=" + toolchain, "-DVCPKG_TARGET_TRIPLET=" + triplet}
	if o.OS == "windows" {
		args = append(args, "-DCMAKE_MSVC_RUNTIME_LIBRARY=MultiThreaded")
	}
	return args
}
