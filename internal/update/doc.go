// Package update checks for new releases of tote, installs them and removes an installation.
//
// A release is read from the GitHub Releases of totecli/tote over https (a file:// mirror is
// accepted for tests). Its checksums file must carry a minisign signature that verifies against
// a public key embedded in the binary, the archive must match the checksum listed there, and
// the unpacked binary must announce the expected version before it atomically replaces the
// running one. Nothing here runs a privileged command: a target that cannot be written is
// reported to the caller.
//
// The release keys are the variable releaseKeys. A release build replaces its value with the
// minisign public keys of the project, separated by spaces:
//
//	go build -ldflags "-X github.com/totecli/tote/internal/update.releaseKeys='RWxxx RWyyy'"
//
// Until then the placeholder makes every update fail with ErrNoKey instead of trusting nothing.
package update
