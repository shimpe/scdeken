package doctor

import (
	"fmt"

	"github.com/shimpe/scdeken/internal/recipe"
)

// platform is the kind of system a hint is written for.
type platform string

const (
	platMac     platform = "macos"
	platWindows platform = "windows"
	platDebian  platform = "debian"
	platArch    platform = "arch"
	platFedora  platform = "fedora"
	platUnknown platform = ""
)

// platformOf picks the hint platform for env.
func platformOf(env Env) platform {
	switch env.OS {
	case "darwin", "macos":
		return platMac
	case "windows":
		return platWindows
	case "linux":
		return linuxPlatform(env.Distro)
	}
	return platUnknown
}

func linuxPlatform(distro string) platform {
	switch p := platform(distro); p {
	case platDebian, platArch, platFedora:
		return p
	}
	return platUnknown
}

// buildTools installs the Windows C++ compiler.
const buildTools = `winget install --id Microsoft.VisualStudio.2022.BuildTools -e --override "--add Microsoft.VisualStudio.Workload.VCTools --includeRecommended --passive"`

var gitHints = map[platform]string{
	platMac:     "xcode-select --install",
	platWindows: "winget install --id Git.Git -e",
	platDebian:  "sudo apt install git",
	platArch:    "sudo pacman -S git",
	platFedora:  "sudo dnf install git",
}

var cmakeHints = map[platform]string{
	platMac:     "brew install cmake",
	platWindows: "winget install --id Kitware.CMake -e",
	platDebian:  "sudo apt install cmake",
	platArch:    "sudo pacman -S cmake",
	platFedora:  "sudo dnf install cmake",
}

var compilerHints = map[platform]string{
	platMac:     "xcode-select --install",
	platWindows: buildTools,
	platDebian:  "sudo apt install build-essential",
	platArch:    "sudo pacman -S base-devel",
	platFedora:  "sudo dnf install gcc-c++ make",
}

// hintFor returns the install command from table, or the generic advice.
func hintFor(table map[platform]string, env Env, label string) string {
	if cmd, ok := table[platformOf(env)]; ok {
		return cmd
	}
	return genericHint(label)
}

// genericHint is the advice when we do not know the system's package manager.
func genericHint(label string) string {
	return fmt.Sprintf("Install %s with your system's package manager.", label)
}

// requirementHint returns the install command for a recipe requirement.
func requirementHint(req recipe.Requirement, env Env, label string) string {
	p := platformOf(env)
	pkg := requirementPackage(req, p)
	if pkg == "" {
		return genericHint(label)
	}
	switch p {
	case platMac:
		return "brew install " + pkg
	case platWindows:
		return "winget install --id " + pkg + " -e"
	case platDebian:
		return "sudo apt install " + pkg
	case platArch:
		return "sudo pacman -S " + pkg
	default:
		return "sudo dnf install " + pkg
	}
}

// requirementPackage picks the package name that matches the platform.
func requirementPackage(req recipe.Requirement, p platform) string {
	switch p {
	case platMac:
		return req.Brew
	case platWindows:
		return req.Winget
	case platDebian:
		return req.Apt
	case platArch:
		return req.Pacman
	case platFedora:
		return req.Dnf
	}
	return ""
}
