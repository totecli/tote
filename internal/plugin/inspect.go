package plugin

import (
	"errors"
	"io"
	"io/fs"
	"os"
)

// inspect validates a package that was copied into a staging directory, in the order that makes
// the hash final: the manifest is read from the copy, the files it declares as programs get the
// execute bit (the only mode bit the hash covers), and only then is the tree hashed. The same
// checks as Load follow, so that an install refuses a package that Load would call broken.
//
// Parameters:
//   - dst: the staging directory, opened as a root
//   - dir: the staging directory
//   - want: id the manifest must carry, empty to accept any
//   - official: true when the plugin may use an official id (a bundled one)
//   - origin: lower case owner/repo of the release, empty for any other source
//   - installed: true when a plugin of that id is already installed and the reservations of ids
//     must not apply to it (DecodeOptions.Installed)
//
// Returns the hashed tree and the parsed manifest, or an *Error that wraps a *ManifestError.
func (r *Registry) inspect(dst *os.Root, dir, want string, official bool, origin string,
	installed bool,
) (*Tree,
	*parsedManifest, error,
) {
	tree, pm, err := r.examine(dst, dir, want, official, origin, installed)

	switch {
	case err != nil:
		return nil, nil, err
	case pm.m == nil:
		return nil, nil, rejection(pm.rep.Errors())
	case len(pm.pkg) > 0:
		return nil, nil, rejection(pm.pkg)
	}

	return tree, pm, nil
}

// examine reads, completes and hashes a staged package and reports everything it finds instead
// of stopping at the first refusal. inspect refuses on what examine finds; check lists it.
//
// Parameters:
//   - dst: the staging directory, opened as a root
//   - dir: the staging directory
//   - want: id the manifest must carry, empty to accept any
//   - official: true when the plugin may use an official id (a bundled one)
//   - origin: lower case owner/repo of the release, empty for any other source
//   - installed: true when a plugin of that id is already installed (DecodeOptions.Installed)
//
// Returns the hashed tree and the parsed manifest, whose m is nil when the manifest was refused
// (the report holds why) and whose pkg lists the findings about the files. The error is set only
// when the tree itself cannot be read or the staging directory cannot be changed.
func (r *Registry) examine(dst *os.Root, dir, want string, official bool, origin string,
	installed bool,
) (*Tree,
	*parsedManifest, error,
) {
	pm := &parsedManifest{official: official}

	data, err := readStaged(dst, manifestName)
	if err != nil {
		pm.rep = Report{Issues: []Issue{absentManifest(dst, err)}}
	} else {
		pm.m, pm.rep = Decode(data, DecodeOptions{
			Host: r.opts.Host, WantID: want,
			Official: official, Origin: origin, Installed: installed,
		})
	}

	if pm.m != nil {
		for _, rel := range ExecFiles(pm.m) {
			if err := dst.Chmod(rel, 0o700); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, nil, wrapError(CodeDisk, err, rel)
			}
		}
	}

	sc, terr := scanTree(dir)
	if terr != nil {
		return nil, nil, treeFailure(terr)
	}

	tree, terr := sc.hash(dir, keepPackageText)
	if terr != nil {
		return nil, nil, treeFailure(terr)
	}

	if pm.m != nil {
		r.finishParse(pm, tree)
	}

	return tree, pm, nil
}

// absentManifest builds the issue of a package that has no readable plugin.toml at its root. A
// package wrapped in one folder (an archive made of the folder, a source tarball of a hosting
// site) is the common cause, and the issue then names the folder.
//
// Parameters:
//   - dst: the staging directory, opened as a root
//   - err: why the manifest could not be read
func absentManifest(dst *os.Root, err error) Issue {
	if !errors.Is(err, fs.ErrNotExist) {
		return Issue{Code: CodeKeyMissing, Path: manifestName}
	}

	var found []string

	if f, oerr := dst.Open("."); oerr == nil {
		names, _ := f.Readdirnames(MaxTreeEntries + 1)
		_ = f.Close()

		for _, name := range names {
			info, serr := dst.Stat(name + "/" + manifestName)
			if serr == nil && info.Mode().IsRegular() {
				found = append(found, name)
			}
		}
	}

	if len(found) == 1 {
		return Issue{Code: CodeManifestNested, Path: manifestName, Args: []string{found[0]}}
	}

	return Issue{Code: CodeManifestMissing, Path: manifestName}
}

// readStaged reads the manifest of a staging directory, one byte past its limit so that Decode
// can report a manifest that is too large.
//
// Parameters:
//   - dst: the staging directory, opened as a root
//   - name: the file
func readStaged(dst *os.Root, name string) ([]byte, error) {
	f, err := dst.Open(name)
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	return io.ReadAll(io.LimitReader(f, MaxManifestSize+1))
}

// treeFailure turns a finding about a staged tree into an *Error.
//
// Parameters:
//   - terr: the finding
func treeFailure(terr *TreeError) *Error {
	switch terr.Code {
	case CodeTreeLimit:
		return newError(CodeSourceLimits, terr.Path, terr.Err.Error())
	case CodeTreeSymlink:
		return newError(CodeSourceUnsafe, terr.Path, UnsafeSymlink)
	case CodeTreeSpecial:
		return newError(CodeSourceUnsafe, terr.Path, UnsafeSpecial)
	case CodeTreeName:
		return newError(CodeSourceUnsafe, terr.Path, UnsafeName)
	}

	return wrapError(CodeDisk, terr, terr.Path)
}
