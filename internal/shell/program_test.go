package shell

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/totecli/tote/internal/store"
)

// TestLookProgram checks which files of a PATH count as a program: executable regular files in
// absolute directories, the first one winning, and a name with a slash taken as it stands.
//
// Parameters:
//   - t: test to report to
func TestLookProgram(t *testing.T) {
	bin := t.TempDir()
	other := t.TempDir()
	plain := t.TempDir()

	for _, p := range []string{
		filepath.Join(bin, "jmp"), filepath.Join(other, "jmp"),
		filepath.Join(other, "other"),
	} {
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(plain, "jmp"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Mkdir(filepath.Join(plain, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Chdir(bin)

	tests := []struct {
		name string
		path string
		prog string
		want string
		ok   bool
	}{
		{"found", bin, "jmp", filepath.Join(bin, "jmp"), true},
		{"first directory wins", bin + ":" + other, "jmp", filepath.Join(bin, "jmp"), true},
		{"later directory", plain + ":" + other, "jmp", filepath.Join(other, "jmp"), true},
		{"not executable", plain, "jmp", "", false},
		{"directory", plain, "dir", "", false},
		{"missing", bin, "nothing", "", false},
		{"relative entry", "." + ":" + "", "jmp", "", false},
		{"empty path", "", "jmp", "", false},
		{"empty name", bin, "", "", false},
		{
			"slash as it stands", "", filepath.Join(other, "other"), filepath.Join(other, "other"),
			true,
		},
		{"slash relative to the directory", "", "./jmp", "./jmp", true},
		{"slash missing", bin, "nowhere/jmp", "", false},
	}

	for _, tt := range tests {
		got, ok := LookProgram(tt.path, tt.prog)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%s: LookProgram(%q, %q) = %q, %v, want %q, %v", tt.name, tt.path, tt.prog,
				got, ok, tt.want, tt.ok)
		}
	}
}

// TestViaProgram checks the program found for each shape of via.
//
// Parameters:
//   - t: test to report to
func TestViaProgram(t *testing.T) {
	tests := []struct {
		via  string
		want string
	}{
		{"", ""},
		{"ssh", ""},
		{"jmp", "jmp"},
		{"mosh --ssh=ssh", "mosh"},
		{"ssh -4", "ssh"},
		{"ssh -t admin@jump.example.org {user@}{host}", "ssh"},
		{"jmp -t {host}", "jmp"},
		{"bin/jmp {host}", "bin/jmp"},
		{"{host}", ""},
		{"x{name} -v", ""},
		{"  ", ""},
	}

	for _, tt := range tests {
		if got := ViaProgram(store.Connection{Name: "a", Via: tt.via}); got != tt.want {
			t.Errorf("ViaProgram(%q) = %q, want %q", tt.via, got, tt.want)
		}
	}
}

// TestViaProgramMatchesConnectArgv checks that the program named for the doctor is the one that
// ConnectArgv starts, for every via that names one.
//
// Parameters:
//   - t: test to report to
func TestViaProgramMatchesConnectArgv(t *testing.T) {
	for _, via := range []string{
		"jmp", "mosh", "mosh --ssh=ssh", "jmp -t {host}",
		"ssh -t admin@jump.example.org {user@}{host}", "bin/jmp {user@}{host}",
	} {
		c := store.Connection{Name: "a", Via: via, Target: "h"}

		argv, err := ConnectArgv(c, "")
		if err != nil {
			t.Fatalf("%q: %v", via, err)
		}

		if got := ViaProgram(c); got != argv[0] {
			t.Errorf("%q: ViaProgram = %q, ConnectArgv starts %q", via, got, argv[0])
		}
	}
}

// TestIsMissingProgram checks which errors of starting a program count as a program that cannot
// be found or run.
//
// Parameters:
//   - t: test to report to
func TestIsMissingProgram(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not in PATH", &exec.Error{Name: "jmp", Err: exec.ErrNotFound}, true},
		{"wrapped", fmt.Errorf("run: %w", &exec.Error{Name: "jmp", Err: exec.ErrNotFound}), true},
		{
			"no such file", &fs.PathError{Op: "fork/exec", Path: "bin/jmp", Err: syscall.ENOENT},
			true,
		},
		{"bare errno", syscall.ENOENT, true},
		{"permission", &fs.PathError{Op: "fork/exec", Path: "bin/jmp", Err: syscall.EACCES}, true},
		{"permission wrapped", &exec.Error{Name: "jmp", Err: fs.ErrPermission}, true},
		{
			"bad format", &fs.PathError{Op: "fork/exec", Path: "bin/jmp", Err: syscall.ENOEXEC},
			false,
		},
		{"other", errors.New("boom"), false},
	}

	for _, tt := range tests {
		if got := IsMissingProgram(tt.err); got != tt.want {
			t.Errorf("%s: IsMissingProgram(%v) = %v, want %v", tt.name, tt.err, got, tt.want)
		}
	}
}

// TestIsMissingProgramReal checks the error that the system gives for a program that is not
// there, and for a file that cannot be executed.
//
// Parameters:
//   - t: test to report to
func TestIsMissingProgramReal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)

	plain := filepath.Join(dir, "plain")
	if err := os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"tote-no-such-program", plain, filepath.Join(dir, "gone")} {
		err := exec.Command(name).Run()
		if err == nil || !IsMissingProgram(err) {
			t.Errorf("%q: error %v is not a missing program", name, err)
		}
	}
}
