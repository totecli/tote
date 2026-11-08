package update

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Method is how a tote binary was installed.
type Method string

const (
	// MethodScript is an install made by install.sh or by tote update.
	MethodScript Method = "script"

	// MethodApt is a binary owned by a Debian or Ubuntu package.
	MethodApt Method = "apt"

	// MethodBrew is a binary owned by Homebrew.
	MethodBrew Method = "brew"

	// MethodGo is a binary made by go install.
	MethodGo Method = "go"

	// MethodUnknown is a binary with no receipt that no package manager owns.
	MethodUnknown Method = "unknown"
)

const (
	// AptUpdateCommand is what to run to update an apt installation.
	AptUpdateCommand = "sudo apt update && sudo apt upgrade tote"

	// DebUpdateCommand is what to do to update a .deb installed from the GitHub release, which
	// no apt repository follows: install the new file over it.
	DebUpdateCommand = "sudo apt install ./tote_<version>_linux_<arch>.deb " +
		"(download it from https://github.com/totecli/tote/releases)"

	// AptRemoveCommand is what to run to remove an apt installation.
	AptRemoveCommand = "sudo apt remove tote"

	// BrewUpdateCommand is what to run to update a Homebrew installation.
	BrewUpdateCommand = "brew upgrade tote"

	// BrewRemoveCommand is what to run to remove a Homebrew installation.
	BrewRemoveCommand = "brew uninstall tote"

	// GoUpdateCommand is what to run to update a go install installation.
	GoUpdateCommand = "go install github.com/totecli/tote/cmd/tote@latest"

	// GoRemoveCommand is what to run to remove a go install installation.
	GoRemoveCommand = `rm "$(go env GOPATH)/bin/tote"`

	// MarkerFile is the file the .deb packages ship, two lines: install_method=apt and
	// package=deb for the file of a GitHub release, package=ppa for the Launchpad package and
	// package=apt for a file published in an apt repository, the last two being updated by apt.
	MarkerFile = "/usr/share/tote/install-method"
)

// Install describes how the running binary was installed.
//
// Managed is true when a package manager owns the binary, in which case tote must not replace
// or remove it and UpdateCommand and RemoveCommand say what to run instead. Package is "deb" or
// "ppa" when the package marker says so.
type Install struct {
	Method        Method
	Managed       bool
	UpdateCommand string
	RemoveCommand string
	Package       string
}

// Detect finds out how a binary was installed.
//
// The order is the receipt, when it describes this binary, then the location: a path under
// /usr other than /usr/local belongs to apt, a path in a Homebrew prefix or Cellar to Homebrew
// and a path in GOBIN or in the bin folder of GOPATH to go install. A receipt that says script
// or manual does not override a location of a package manager. No process is started and
// /var/lib/dpkg is never read.
//
// Parameters:
//   - exe: path of the running binary, symbolic links resolved
//   - r: install receipt, nil when there is none
//   - marker: path of the package marker, MarkerFile in production
//
// Returns the installation.
func Detect(exe string, r *Receipt, marker string) Install {
	exe = resolve(exe)

	if r != nil && r.Describes(exe) {
		switch strings.ToLower(r.InstallMethod) {
		case "apt", "deb", "ppa":
			return aptInstall(marker)
		case "brew", "homebrew":
			return brewInstall()
		case string(MethodScript), "manual":
			// A receipt cannot claim a place that only a package manager writes to.
			if !brewPath(exe) && !aptPath(exe) {
				return Install{Method: MethodScript}
			}
		}
	}

	switch {
	case brewPath(exe):
		return brewInstall()
	case aptPath(exe):
		return aptInstall(marker)
	case goPath(exe):
		return goInstall()
	}

	return Install{Method: MethodUnknown}
}

// aptInstall builds the description of an apt installation.
//
// Parameters:
//   - marker: path of the package marker, read for the kind of package, which decides how an
//     update is made
func aptInstall(marker string) Install {
	pkg := readMarker(marker)["package"]

	in := Install{
		Method: MethodApt, Managed: true, Package: pkg,
		UpdateCommand: AptUpdateCommand, RemoveCommand: AptRemoveCommand,
	}

	if pkg == "deb" {
		in.UpdateCommand = DebUpdateCommand
	}

	return in
}

// brewInstall builds the description of a Homebrew installation.
func brewInstall() Install {
	return Install{
		Method: MethodBrew, Managed: true,
		UpdateCommand: BrewUpdateCommand, RemoveCommand: BrewRemoveCommand,
	}
}

// goInstall builds the description of a go install installation. The Go tool owns the binary,
// so tote leaves it alone and says which command to run.
func goInstall() Install {
	return Install{
		Method: MethodGo, Managed: true,
		UpdateCommand: GoUpdateCommand, RemoveCommand: GoRemoveCommand,
	}
}

// goPath reports whether a path is a folder where go install puts binaries.
//
// Parameters:
//   - p: absolute cleaned path; the folders are GOBIN, then the bin folder of each GOPATH entry,
//     then ~/go/bin when GOPATH is unset
func goPath(p string) bool {
	dir := filepath.Dir(p)

	if bin := os.Getenv("GOBIN"); bin != "" {
		return dir == filepath.Clean(bin)
	}

	paths := filepath.SplitList(os.Getenv("GOPATH"))
	if len(paths) == 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}

		paths = []string{filepath.Join(home, "go")}
	}

	for _, gp := range paths {
		if gp != "" && dir == filepath.Join(filepath.Clean(gp), "bin") {
			return true
		}
	}

	return false
}

// aptPath reports whether a path is where a package puts a binary.
//
// Parameters:
//   - p: absolute cleaned path; /usr/local is the administrator's, not a package's
func aptPath(p string) bool {
	return strings.HasPrefix(p, "/usr/") && !strings.HasPrefix(p, "/usr/local/")
}

// brewPath reports whether a path is inside a Homebrew prefix or Cellar.
//
// Parameters:
//   - p: absolute cleaned path
func brewPath(p string) bool {
	return strings.Contains(p, "/Cellar/") || strings.HasPrefix(p, "/opt/homebrew/") ||
		strings.HasPrefix(p, "/home/linuxbrew/.linuxbrew/")
}

// readMarker reads the key=value lines of the package marker.
//
// Parameters:
//   - path: marker file
//
// Returns the pairs; a missing or unreadable file gives an empty map. At most 20 lines are read.
func readMarker(path string) map[string]string {
	out := map[string]string{}

	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return out
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for i := 0; i < 20 && sc.Scan(); i++ {
		if k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "="); ok {
			out[k] = v
		}
	}

	return out
}
