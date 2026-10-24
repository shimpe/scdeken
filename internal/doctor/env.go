package doctor

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Env describes the machine the doctor is checking.
type Env struct {
	OS      string // runtime.GOOS value: "linux", "darwin" (or "macos"), "windows"
	Distro  string // "debian", "arch", "fedora" or "" (unknown); Linux only
	VSWhere string // full path of vswhere.exe; Windows only, "" when not installed
}

// DetectEnv inspects the running machine.
func DetectEnv() Env {
	env := Env{OS: runtime.GOOS}
	switch env.OS {
	case "linux":
		env.Distro = distroFromFile("/etc/os-release")
	case "windows":
		env.VSWhere = findVSWhere()
	}
	return env
}

// distroFromFile reads ID and ID_LIKE from an os-release file.
func distroFromFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var ids []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), "=")
		if ok && (key == "ID" || key == "ID_LIKE") {
			ids = append(ids, strings.Fields(strings.Trim(value, `"'`))...)
		}
	}
	return familyOf(ids)
}

// familyOf maps distribution ids to the package-manager family we know.
func familyOf(ids []string) string {
	for _, id := range ids {
		switch id {
		case "debian", "ubuntu":
			return "debian"
		case "arch":
			return "arch"
		case "fedora", "rhel", "centos":
			return "fedora"
		}
	}
	return ""
}

// findVSWhere returns the path of vswhere.exe, or "" when it is not there.
func findVSWhere() string {
	base := os.Getenv("ProgramFiles(x86)")
	if base == "" {
		return ""
	}
	path := filepath.Join(base, "Microsoft Visual Studio", "Installer", "vswhere.exe")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}
