package plugin

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestCheckUnreadableSource checks that a file or a directory of a source that cannot be read is
// reported as a read failure that names it, not as a write failure or a source that changed.
//
// Parameters:
//   - t: test to report to
func TestCheckUnreadableSource(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}

	for _, rel := range []string{"README.md", "locales"} {
		t.Run(rel, func(t *testing.T) {
			l := chkLab(t)
			files := chkPlugin([]string{"exec:tool"}, cmdWith("go", ""),
				map[string]string{"README.md": "# demo\n", "locales/en.toml": "x = \"y\"\n"})
			dir := l.srcDir("unreadable", files)

			must(t, os.Chmod(filepath.Join(dir, rel), 0o000))
			t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, rel), 0o755) })

			res, err := l.reg(nil).Check(context.Background(), CheckRequest{Target: dir})
			must(t, err)

			f := res.find(CodeSourceRead)
			if f == nil || len(f.Args) != 2 || f.Args[0] != rel ||
				!strings.Contains(f.Args[1], "permission denied") {
				t.Fatalf("findings %+v", res.Findings)
			}

			if res.Has(CodeDisk) || res.Has(CodeSourceUnsafe) {
				t.Errorf("a read failure is told as another one: %+v", res.Findings)
			}
		})
	}
}

// TestCheckWrappedArchive checks the answers to a package that sits in a folder: the error names
// the folder, a package with nothing at all says where tote looked, and the checksum to publish is
// not printed for an archive that cannot be installed.
//
// Parameters:
//   - t: test to report to
func TestCheckWrappedArchive(t *testing.T) {
	l := chkLab(t)
	r := l.reg(nil)

	manifestText := manifest("hx", "1.0.0", []string{"exec:tool"}, cmdWith("go", ""))

	for _, c := range []struct {
		name string
		ents []ent
		code string
		args []string
	}{
		{
			"one folder",
			[]ent{dirEnt("hx/"), file("hx/plugin.toml", manifestText)},
			CodeManifestNested,
			[]string{"hx"},
		},
		{"no manifest", []ent{file("README.md", "x")}, CodeManifestMissing, nil},
		{
			"two folders",
			[]ent{
				file("a/plugin.toml", manifestText), file("b/plugin.toml", manifestText),
			},
			CodeManifestMissing, nil,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			data := tgz(t, c.ents...)
			path := filepath.Join(l.root, strings.ReplaceAll(c.name, " ", "-")+".tar.gz")
			must(t, os.WriteFile(path, data, 0o600))

			res, err := r.Check(context.Background(), CheckRequest{Target: path})
			must(t, err)

			f := res.find(c.code)
			if f == nil || f.Path != "plugin.toml" || !slices.Equal(f.Args, c.args) {
				t.Fatalf("findings %+v", res.Findings)
			}

			if res.ArchiveSHA256 != "" {
				t.Errorf("the checksum of a broken archive is offered: %q", res.ArchiveSHA256)
			}

			_, err = r.Plan(context.Background(), InstallRequest{Source: path})
			if e := asError(t, err); e.Code != CodeManifestInvalid {
				t.Errorf("Plan = %v", e)
			}
		})
	}
}

// TestWritableStoreIsNamed checks that a plugins directory that others can write is named for
// what it is: the reason of the plugin and the refusal of a change both say that the directory is
// writable by others, and they do not send the user to a remove that cannot work.
//
// Parameters:
//   - t: test to report to
func TestWritableStoreIsNamed(t *testing.T) {
	l, r := removeLab(t)

	must(t, os.Chmod(l.paths.PluginsDir(), 0o770))

	p, ok := r.Recheck("demo")
	if !ok || p.State != StateBroken {
		t.Fatalf("Recheck = %v %+v", ok, p)
	}

	var got *Reason

	for i := range p.Reasons {
		if p.Reasons[i].Code == CodeStoreWritable {
			got = &p.Reasons[i]
		}
	}

	if got == nil || len(got.Args) != 2 || got.Args[0] != l.paths.PluginsDir() {
		t.Fatalf("reasons %+v", p.Reasons)
	}

	for _, do := range []func() error{
		func() error { _, err := r.Remove("demo", RemoveOptions{}); return err },
		func() error {
			req := InstallRequest{Source: demoSource(l, "demo", "2.0.0")}
			_, err := r.Plan(context.Background(), req)

			return err
		},
	} {
		if e := asError(t, do()); e.Code != CodeStoreWritable || len(e.Args) != 2 {
			t.Errorf("got %s %v, want %s", e.Code, e.Args, CodeStoreWritable)
		}
	}
}
