package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDetect checks the order of the detection: receipt, then location.
//
// Parameters:
//   - t: test to report to
func TestDetect(t *testing.T) {
	home := t.TempDir()
	local := filepath.Join(home, ".local", "bin", "tote")
	marker := filepath.Join(home, "install-method")

	if err := os.WriteFile(marker, []byte("install_method=apt\npackage=ppa\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	script := &Receipt{InstallMethod: "script", Path: local}
	apt := &Receipt{InstallMethod: "apt", Path: "/usr/bin/tote"}

	tests := []struct {
		name    string
		exe     string
		receipt *Receipt
		method  Method
		managed bool
		pkg     string
	}{
		{"script receipt", local, script, MethodScript, false, ""},
		{
			"script receipt under /usr", "/usr/local/bin/tote",
			&Receipt{InstallMethod: "script", Path: "/usr/local/bin/tote"}, MethodScript, false, "",
		},
		{"apt receipt", "/usr/bin/tote", apt, MethodApt, true, "ppa"},
		{"deb receipt value", "/usr/bin/tote", &Receipt{InstallMethod: "deb"}, MethodApt, true, "ppa"},
		{
			"brew receipt", "/opt/homebrew/bin/tote",
			&Receipt{InstallMethod: "brew", Path: "/opt/homebrew/bin/tote"}, MethodBrew, true, "",
		},
		{
			"manual receipt", local, &Receipt{InstallMethod: "manual", Path: local}, MethodScript, false, "",
		},
		{"no receipt, user directory", local, nil, MethodUnknown, false, ""},
		{"no receipt, /usr/bin", "/usr/bin/tote", nil, MethodApt, true, "ppa"},
		{"no receipt, /usr/sbin", "/usr/sbin/tote", nil, MethodApt, true, "ppa"},
		{"no receipt, /usr/local/bin", "/usr/local/bin/tote", nil, MethodUnknown, false, ""},
		{"no receipt, /usr itself", "/usr", nil, MethodUnknown, false, ""},
		{"no receipt, lookalike", "/usrx/bin/tote", nil, MethodUnknown, false, ""},
		{"no receipt, homebrew prefix", "/opt/homebrew/bin/tote", nil, MethodBrew, true, ""},
		{"no receipt, intel cellar", "/usr/local/Cellar/tote/1.0.0/bin/tote", nil, MethodBrew, true, ""},
		{"no receipt, linuxbrew", "/home/linuxbrew/.linuxbrew/bin/tote", nil, MethodBrew, true, ""},
		{"apt receipt for another binary", local, apt, MethodUnknown, false, ""},
		{"script receipt for another binary", "/usr/bin/tote", script, MethodApt, true, "ppa"},
		{
			"unknown method falls back to the location", "/usr/bin/tote", &Receipt{InstallMethod: "snap"},
			MethodApt, true, "ppa",
		},
	}

	for _, tt := range tests {
		got := Detect(tt.exe, tt.receipt, marker)
		if got.Method != tt.method || got.Managed != tt.managed || got.Package != tt.pkg {
			t.Errorf("%s: %+v, want %s managed=%v package=%q", tt.name, got, tt.method, tt.managed, tt.pkg)
		}

		switch got.Method {
		case MethodApt:
			if got.UpdateCommand != AptUpdateCommand || got.RemoveCommand != AptRemoveCommand {
				t.Errorf("%s: commands %q, %q", tt.name, got.UpdateCommand, got.RemoveCommand)
			}
		case MethodBrew:
			if got.UpdateCommand != "brew upgrade tote" || got.RemoveCommand != "brew uninstall tote" {
				t.Errorf("%s: commands %q, %q", tt.name, got.UpdateCommand, got.RemoveCommand)
			}
		default:
			if got.UpdateCommand != "" || got.RemoveCommand != "" {
				t.Errorf("%s: commands for an unmanaged install", tt.name)
			}
		}
	}
}

// TestDetectDebUpdate checks that the update hint follows the package kind of the marker: a .deb
// from a GitHub release is not followed by any apt repository, so it is told to install the new
// file, while the other kinds and a missing marker keep the apt command. Removal is always apt.
//
// Parameters:
//   - t: test to report to
func TestDetectDebUpdate(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		pkg  string
		want string
	}{
		{"deb", DebUpdateCommand},
		{"ppa", AptUpdateCommand},
		{"apt", AptUpdateCommand},
		{"", AptUpdateCommand},
		{"other", AptUpdateCommand},
	}

	for _, tt := range tests {
		marker := filepath.Join(dir, "marker-"+tt.pkg)
		text := "install_method=apt\n"

		if tt.pkg != "" {
			text += "package=" + tt.pkg + "\n"
		}

		if err := os.WriteFile(marker, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}

		got := Detect("/usr/bin/tote", nil, marker)
		if got.UpdateCommand != tt.want || got.RemoveCommand != AptRemoveCommand {
			t.Errorf("package %q: %q, %q, want %q",
				tt.pkg, got.UpdateCommand, got.RemoveCommand, tt.want)
		}
	}

	if !strings.Contains(DebUpdateCommand, "apt install ./") ||
		strings.Contains(DebUpdateCommand, "apt upgrade") {
		t.Errorf("DebUpdateCommand = %q", DebUpdateCommand)
	}
}

// TestDetectMarkerAbsent checks that a missing or hostile marker only drops the package kind.
//
// Parameters:
//   - t: test to report to
func TestDetectMarkerAbsent(t *testing.T) {
	dir := t.TempDir()
	huge := filepath.Join(dir, "huge")

	var lines []byte
	for i := 0; i < 100; i++ {
		lines = append(lines, "x=y\n"...)
	}

	lines = append(lines, "package=deb\n"...)

	if err := os.WriteFile(huge, lines, 0o600); err != nil {
		t.Fatal(err)
	}

	for name, marker := range map[string]string{
		"absent":    filepath.Join(dir, "none"),
		"directory": dir,
		"too long":  huge,
		"empty":     "",
	} {
		got := Detect("/usr/bin/tote", nil, marker)
		if !got.Managed || got.Package != "" {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

// TestDetectSymlinks checks that links are resolved before the location rules apply.
//
// Parameters:
//   - t: test to report to
func TestDetectSymlinks(t *testing.T) {
	dir := t.TempDir()
	cellar := filepath.Join(dir, "Cellar", "tote", "1.0.0", "bin")

	if err := os.MkdirAll(cellar, 0o755); err != nil {
		t.Fatal(err)
	}

	real := filepath.Join(cellar, "tote")
	if err := os.WriteFile(real, nil, 0o755); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(dir, "tote")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	if got := Detect(link, nil, ""); got.Method != MethodBrew {
		t.Errorf("link into a Cellar: %+v", got)
	}

	other := Receipt{Path: filepath.Join(dir, "x")}
	if !(Receipt{Path: link}).Describes(real) || other.Describes(real) {
		t.Error("Describes does not resolve links")
	}

	if !(Receipt{}).Describes(real) {
		t.Error("a receipt without a path describes any binary")
	}
}

// TestDetectGoInstall checks that a binary in GOBIN or in the bin folder of GOPATH is a go
// install, that GOBIN wins over GOPATH, and that a script receipt still takes precedence.
//
// Parameters:
//   - t: test to report to
func TestDetectGoInstall(t *testing.T) {
	root := t.TempDir()
	gobin := filepath.Join(root, "gobin")
	gopath := filepath.Join(root, "gopath")

	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", gopath)

	exe := filepath.Join(gopath, "bin", "tote")
	if got := Detect(exe, nil, ""); got.Method != MethodGo || !got.Managed ||
		got.UpdateCommand != GoUpdateCommand {
		t.Errorf("GOPATH install = %+v", got)
	}

	t.Setenv("GOBIN", gobin)

	if got := Detect(exe, nil, ""); got.Method != MethodUnknown {
		t.Errorf("GOBIN set, GOPATH binary = %+v, want unknown", got)
	}

	if got := Detect(filepath.Join(gobin, "tote"), nil, ""); got.Method != MethodGo {
		t.Errorf("GOBIN install = %+v", got)
	}

	scripted := &Receipt{InstallMethod: "script", Path: filepath.Join(gobin, "tote")}
	if got := Detect(filepath.Join(gobin, "tote"), scripted, ""); got.Method != MethodScript {
		t.Errorf("script receipt in GOBIN = %+v", got)
	}
}
