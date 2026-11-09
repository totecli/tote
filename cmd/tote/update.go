package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/totecli/tote/internal/config"
	"github.com/totecli/tote/internal/shell"
	"github.com/totecli/tote/internal/update"
)

const (
	// exitUpdate is the exit code of "tote update --check" when a newer release exists, so
	// that a script can tell it from a failure (1) and from an up to date install (0).
	exitUpdate = 10

	// sourceEnv names the variable that points the update to a mirror, https:// or file://.
	sourceEnv = "TOTE_UPDATE_BASE"

	// pubkeyEnv names the variable that replaces the embedded release keys; it is honoured
	// only together with a file:// source, as install.sh does.
	pubkeyEnv = "TOTE_UPDATE_PUBKEY"
)

var (
	// errPubkeyEnv reports TOTE_UPDATE_PUBKEY used with a source that is not a local mirror.
	errPubkeyEnv = errors.New("public key override outside a local mirror")

	// errPubkeyRoot reports TOTE_UPDATE_PUBKEY used by root, who must only trust the keys
	// embedded in the program.
	errPubkeyRoot = errors.New("public key override refused for root")

	// errBadKeyEnv reports a TOTE_UPDATE_PUBKEY that holds no valid minisign key.
	errBadKeyEnv = errors.New("invalid public key override")

	// errExe reports a running program that cannot be located.
	errExe = errors.New("cannot find the running program")
)

// installCtx describes the running program and how it was installed.
//
// exe is the path of the binary with links resolved, paths the tote directories (zero when they
// cannot be resolved), receipt the install receipt when it describes this binary and nil
// otherwise, foreign true when a receipt exists but describes another binary, and install the
// result of the detection.
type installCtx struct {
	exe     string
	paths   config.Paths
	receipt *update.Receipt
	foreign bool
	install update.Install
}

// updateInfo is the JSON form of "tote update --json" when it worked.
//
// Status is one of up-to-date, available, updated, dev, newer or managed. A failure is an
// updateFailure instead.
type updateInfo struct {
	Schema    int    `json:"schema"`
	OK        bool   `json:"ok"`
	Status    string `json:"status"`
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Channel   string `json:"channel,omitempty"`
	Available bool   `json:"available"`
	Updated   bool   `json:"updated"`
	Path      string `json:"path,omitempty"`
	ManagedBy string `json:"managed_by,omitempty"`
	Command   string `json:"command,omitempty"`
}

// updateFailure is the JSON form of "tote update --json" when it failed.
//
// Error has the shape of the error of the plugin commands: the stable name of the failure, such
// as offline, rate_limited, bad_signature, unknown_key or bad_checksum, its message in the
// language of the user and the exit code of the process. Status is always error.
type updateFailure struct {
	Schema int       `json:"schema"`
	OK     bool      `json:"ok"`
	Status string    `json:"status"`
	Error  errorBody `json:"error"`
}

// updateFlags holds the options of "tote update".
type updateFlags struct {
	check bool
	json  bool
	beta  bool
	force bool
}

// notifyContext returns a context that a Ctrl-C, a termination signal or the closing of the
// terminal cancels, so that a download stopped that way cleans up like one stopped by Ctrl-C.
func notifyContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM,
		syscall.SIGHUP)
}

// context returns the context of a long operation.
func (a *app) context() (context.Context, context.CancelFunc) {
	if a.Context != nil {
		return a.Context()
	}

	return notifyContext()
}

// executable returns the path of the running binary with its links resolved.
//
// Returns the path, or an error when it cannot be found or no longer exists.
func (a *app) executable() (string, error) {
	fn := a.Executable
	if fn == nil {
		fn = os.Executable
	}

	p, err := fn()
	if err != nil {
		return "", err
	}

	return filepath.EvalSymlinks(p)
}

// isRoot reports whether tote runs with the rights of root.
func (a *app) isRoot() bool {
	euid := os.Geteuid
	if a.Geteuid != nil {
		euid = a.Geteuid
	}

	return euid() == 0
}

// underSudo reports whether tote runs as root on behalf of another user, through sudo or doas.
//
// Writing the install receipt then would leave a file owned by root in the home of that user.
func (a *app) underSudo() bool {
	return a.isRoot() && (a.Getenv("SUDO_USER") != "" || a.Getenv("DOAS_USER") != "")
}

// installContext finds out which binary runs and how it was installed.
//
// Returns the description. A receipt that cannot be read is reported on stderr and ignored; the
// directories being unresolvable only means that there is no receipt.
func (a *app) installContext() (installCtx, error) {
	exe, err := a.executable()
	if err != nil {
		return installCtx{}, err
	}

	ic := installCtx{exe: exe}

	paths, err := a.cfgPaths()
	if err == nil {
		ic.paths = paths

		r, rerr := update.ReadReceipt(paths.ReceiptFile())

		switch {
		case rerr == nil && r.Describes(exe):
			ic.receipt = &r
		case rerr == nil:
			ic.foreign = true
		case !errors.Is(rerr, os.ErrNotExist):
			a.say("cli_update_receipt_bad", sanitize(rerr.Error()))
		}
	}

	ic.install = update.Detect(exe, ic.receipt, update.MarkerFile)

	return ic, nil
}

// channelFor chooses the release channel of an install.
//
// Parameters:
//   - f: options of the command
//   - ic: description of the install
//
// Returns beta for --beta, for an install whose receipt says beta and for a pre-release build,
// else stable.
func (a *app) channelFor(f updateFlags, ic installCtx) update.Channel {
	return channelOf(a.Version, ic.receipt, f.beta)
}

// channelOf chooses the release channel from what is known about a build.
//
// Parameters:
//   - version: release string of the running program
//   - r: install receipt that describes it, nil when there is none
//   - beta: true when the user asked for the beta channel
//
// Returns beta when asked, when the receipt says beta or when the build is a pre-release, else
// stable.
func channelOf(version string, r *update.Receipt, beta bool) update.Channel {
	if beta || (r != nil && r.Channel == string(update.ChannelBeta)) {
		return update.ChannelBeta
	}

	if v, err := update.ParseVersion(version); err == nil && v.Prerelease() {
		return update.ChannelBeta
	}

	return update.ChannelStable
}

// updateSource builds the release source from the environment.
//
// Returns GitHub, or the mirror named by TOTE_UPDATE_BASE, with the HTTP client of the Updater
// template when it has one; an error wraps update.ErrInsecure when the base is neither
// https:// nor file://.
func (a *app) updateSource() (update.Source, error) {
	src := update.Source{
		Base: a.Getenv(sourceEnv), Client: a.Updater.Source.Client, Agent: "tote/" + a.Version,
	}

	return src, src.Validate()
}

// releaseKeys returns the keys that releases are checked with, the ones of the Updater template
// when it has some.
func (a *app) releaseKeys() ([]update.PublicKey, error) {
	if a.Updater.Keys != nil {
		return a.Updater.Keys, nil
	}

	return update.ReleaseKeys()
}

// updateKeys returns the keys that releases must be signed with.
//
// Parameters:
//   - src: release source; the override of the keys is honoured only for a local mirror
//
// Returns the embedded keys, or the ones of TOTE_UPDATE_PUBKEY, or an error. The override is
// refused for root, who must only trust the keys embedded in the program.
func (a *app) updateKeys(src update.Source) ([]update.PublicKey, error) {
	env := a.Getenv(pubkeyEnv)
	if env == "" {
		return update.ReleaseKeys()
	}

	if !src.Local() {
		return nil, errPubkeyEnv
	}

	if a.isRoot() {
		return nil, errPubkeyRoot
	}

	keys, err := update.ParsePublicKeys(env)
	if err != nil || len(keys) == 0 {
		return nil, errBadKeyEnv
	}

	return keys, nil
}

// newUpdater fills the updater template with what this run knows.
//
// Parameters:
//   - f: options of the command
//   - ic: description of the install
//   - src: release source
func (a *app) newUpdater(f updateFlags, ic installCtx, src update.Source) *update.Updater {
	u := a.Updater
	u.Version = a.Version
	u.Channel = a.channelFor(f, ic)
	u.Force = f.force
	u.Source = src
	u.Target = ic.exe

	// A receipt written for another binary is left alone. Under sudo it is still read, to keep
	// the guard against an older release, but never written: it would belong to root.
	if ic.paths.State != "" && !ic.foreign {
		u.Receipt = ic.receipt

		if !a.underSudo() {
			u.ReceiptPath = ic.paths.ReceiptFile()
		}
	}

	return &u
}

// runUpdate implements "tote update [--check] [--json] [--beta] [--force]".
//
// Parameters:
//   - args: arguments after the verb
//
// Returns the exit code of the verb: 0 on success, 10 for --check when a release is available,
// 1 on a failure, 2 for a misuse and 130 after a Ctrl-C.
func (a *app) runUpdate(args []string) int {
	o, err := parseOptions(args, []opt{
		{long: "check"}, {long: "json"}, {long: "beta"}, {long: "force"},
	})
	if err == nil {
		err = wantArgs(o.pos, 0, 0)
	}

	if err != nil {
		return a.updateUsage(args, err)
	}

	if o.help {
		return a.verbHelp("update")
	}

	f := updateFlags{
		check: o.set["check"], json: o.set["json"], beta: o.set["beta"], force: o.set["force"],
	}

	ic, err := a.installContext()
	if err != nil {
		return a.updateFail(f, ic, fmt.Errorf("%w: %v", errExe, err))
	}

	if ic.install.Managed {
		return a.reportManaged(f, ic)
	}

	src, err := a.updateSource()
	if err != nil {
		return a.updateFail(f, ic, err)
	}

	u := a.newUpdater(f, ic, src)

	if !f.check {
		if u.Keys == nil {
			u.Keys, err = a.updateKeys(src)
		}

		if err != nil {
			return a.updateFail(f, ic, err)
		}

		if !f.json {
			u.Progress = a.progress()
		}
	}

	ctx, stop := a.context()
	defer stop()

	var res update.Result

	if f.check {
		res, err = u.Check(ctx)
	} else {
		res, err = u.Apply(ctx)
	}

	if err != nil {
		return a.updateFail(f, ic, err)
	}

	return a.reportUpdate(f, res)
}

// progress returns the function that tells the user which step an update is at.
func (a *app) progress() func(update.Step) {
	return func(s update.Step) {
		switch s {
		case update.StepDownload:
			a.say("cli_update_step_download")
		case update.StepVerify:
			a.say("cli_update_step_verify")
		case update.StepInstall:
			a.say("cli_update_step_install")
		}
	}
}

// reportManaged tells that a package manager owns the binary and what to run instead.
//
// Parameters:
//   - f: options of the command
//   - ic: description of the install
//
// Returns exitOK: nothing is wrong, nothing was touched.
func (a *app) reportManaged(f updateFlags, ic installCtx) int {
	if f.json {
		info := updateInfo{
			Schema: listSchema, OK: true,
			Status: string(update.StatusManaged), Current: a.Version,
			ManagedBy: string(ic.install.Method), Command: ic.install.UpdateCommand,
		}

		return a.printUpdateJSON(info)
	}

	fmt.Fprintln(a.Stdout, a.t("cli_update_managed", ic.install.Method, ic.install.UpdateCommand))

	return exitOK
}

// reportUpdate prints the outcome of a check or of an update.
//
// Parameters:
//   - f: options of the command
//   - res: result of the updater
//
// Returns the exit code.
func (a *app) reportUpdate(f updateFlags, res update.Result) int {
	if f.json {
		info := updateInfo{
			Schema: listSchema, OK: true,
			Status: string(res.Status), Current: res.Current, Latest: res.Latest,
			Channel: string(res.Channel), Available: res.Status == update.StatusAvailable,
			Updated: res.Status == update.StatusUpdated, Path: res.Path,
		}

		rc := a.printUpdateJSON(info)
		if rc == exitOK && f.check && info.Available {
			return exitUpdate
		}

		a.warnReceipt(res)

		return rc
	}

	w := a.Stdout

	switch res.Status {
	case update.StatusAvailable:
		fmt.Fprintln(w, a.t("cli_update_available", res.Latest, res.Current, res.Channel))
		fmt.Fprintln(w, a.t("cli_update_available_hint"))

		return exitUpdate
	case update.StatusUpToDate:
		fmt.Fprintln(w, a.t("cli_update_up_to_date", res.Current))
	case update.StatusDev:
		fmt.Fprintln(w, a.t("cli_update_dev", res.Latest))
	case update.StatusNewer:
		fmt.Fprintln(w, a.t("cli_update_newer", res.Current, res.Latest))
	case update.StatusUpdated:
		fmt.Fprintln(w, a.updatedText(res))
		a.warnReceipt(res)
	}

	return exitOK
}

// updatedText describes an install that just happened.
//
// Parameters:
//   - res: result with the status StatusUpdated
func (a *app) updatedText(res update.Result) string {
	if res.Current == res.Latest {
		return a.t("cli_update_reinstalled", res.Latest, sanitize(res.Path))
	}

	return a.t("cli_update_done", sanitize(res.Current), res.Latest, sanitize(res.Path))
}

// warnReceipt tells that the update worked but the receipt could not be written.
//
// Parameters:
//   - res: result of the update
func (a *app) warnReceipt(res update.Result) {
	if res.ReceiptErr != nil {
		a.say("cli_update_receipt_failed", sanitize(res.ReceiptErr.Error()))
	}
}

// printUpdateJSON writes the JSON form of a result on stdout.
//
// Parameters:
//   - info: what to print
//
// Returns exitOK, or exitFail when the output cannot be written.
func (a *app) printUpdateJSON(info updateInfo) int {
	if err := writeJSON(a.Stdout, info); err != nil {
		return a.fail(err)
	}

	return exitOK
}

// updateFail reports a failed check or update.
//
// Parameters:
//   - f: options of the command
//   - ic: description of the install, used to name the program in a hint
//   - err: the failure, one of the errors of internal/update or of this file
//
// Returns exitFail, or exitInterrupted after a Ctrl-C. With --json the failure is an updateFailure
// on stdout and nothing goes to standard error, as for the plugin commands.
func (a *app) updateFail(f updateFlags, ic installCtx, err error) int {
	key, args := a.updateErrorMessage(err, ic)
	msg := a.t(key, args...)

	rc := exitFail
	if errors.Is(err, context.Canceled) {
		rc = exitInterrupted
	}

	if f.json {
		code := update.Code(err)
		if errors.Is(err, errPubkeyEnv) || errors.Is(err, errBadKeyEnv) ||
			errors.Is(err, errPubkeyRoot) {
			code = "bad_key"
		}

		doc := updateFailure{
			Schema: listSchema, Status: "error",
			Error: errorBody{Code: code, Message: msg, Exit: rc},
		}
		if werr := writeJSON(a.Stdout, doc); werr != nil {
			return exitFail
		}

		return rc
	}

	a.say("cli_error", msg)

	return rc
}

// updateUsage reports a misuse of "tote update". With --json among the arguments the error is
// the JSON object of every other failure of the verb (code "usage") and nothing goes to standard
// error, so that a script reads one shape whatever went wrong.
//
// Parameters:
//   - args: arguments after the verb
//   - err: what the parser refused
//
// Returns exitUsage.
func (a *app) updateUsage(args []string, err error) int {
	var oerr *optError

	if !errors.As(err, &oerr) || !slices.Contains(args[:dashes(args)], "--json") {
		return a.usageErr(err, "update")
	}

	doc := updateFailure{
		Schema: listSchema, Status: "error",
		Error: errorBody{
			Code: codeUsage, Message: a.t(oerr.Key, a.optArgs(oerr)...), Exit: exitUsage,
		},
	}
	if werr := writeJSON(a.Stdout, doc); werr != nil {
		return exitFail
	}

	return exitUsage
}

// dashes finds where the options of a command line end.
//
// Parameters:
//   - args: arguments after the verb
//
// Returns the index of the first "--", or len(args) when there is none.
func dashes(args []string) int {
	if i := slices.Index(args, "--"); i >= 0 {
		return i
	}

	return len(args)
}

// updateErrorMessage chooses the catalog message for a failure.
//
// Parameters:
//   - err: the failure
//   - ic: description of the install
//
// Returns the catalog key and the values of its directives.
func (a *app) updateErrorMessage(err error, ic installCtx) (string, []any) {
	var rl *update.RateLimitError

	var (
		nw  *update.NotWritableError
		ins *update.InsecureError
		rb  *update.RollbackError
		cm  *update.CommentError
	)

	switch {
	case errors.As(err, &rl):
		if !rl.Reset.IsZero() {
			return "cli_update_rate_limited_at", []any{rl.Reset.Local().Format(time.TimeOnly)}
		}

		return "cli_update_rate_limited", nil
	case errors.As(err, &nw):
		return "cli_update_not_writable", []any{sanitize(nw.Dir), sanitize(shell.Quote(ic.exe))}
	case errors.As(err, &ins):
		if ins.Redirect {
			return "cli_update_insecure_redirect", []any{sanitize(scrubURLs(ins.URL))}
		}

		return "cli_update_insecure", []any{sanitize(scrubURLs(ins.URL))}
	case errors.As(err, &rb):
		return "cli_update_rollback", []any{stamp(rb.Signed), stamp(rb.Installed)}
	case errors.As(err, &cm):
		return "cli_update_bad_comment", []any{sanitize(cm.Field)}
	}

	table := []struct {
		target error
		key    string
		detail bool
	}{
		{context.Canceled, "cli_update_interrupted", false},
		{errExe, "cli_update_exe", true},
		{errPubkeyEnv, "cli_update_pubkey_env", false},
		{errPubkeyRoot, "cli_update_pubkey_root", false},
		{errBadKeyEnv, "cli_update_bad_key", false},
		{update.ErrNoKey, "cli_update_no_key", false},
		{update.ErrKeyFormat, "cli_update_bad_release_key", false},
		{update.ErrOffline, "cli_update_offline", true},
		{update.ErrCertificate, "cli_update_certificate", true},
		{update.ErrServer, "cli_update_server", true},
		{update.ErrNoRelease, "cli_update_no_release", true},
		{update.ErrNoAsset, "cli_update_no_asset", true},
		{update.ErrInsecure, "cli_update_insecure", true},
		{update.ErrUnsupported, "cli_update_unsupported", true},
		{update.ErrTooLarge, "cli_update_too_large", false},
		{update.ErrBadChecksum, "cli_update_bad_checksum", false},
		{update.ErrBadSignature, "cli_update_bad_signature", false},
		{update.ErrUnknownKey, "cli_update_unknown_key", false},
		{update.ErrSignatureFormat, "cli_update_bad_signature", false},
		{update.ErrLegacySignature, "cli_update_bad_signature", false},
		{update.ErrBadArchive, "cli_update_bad_archive", false},
		{update.ErrBadBinary, "cli_update_bad_binary", false},
	}

	for _, e := range table {
		if errors.Is(err, e.target) {
			if !e.detail {
				return e.key, nil
			}

			return e.key, []any{trimmed(err, e.target)}
		}
	}

	return "cli_update_failed", []any{sanitize(scrubURLs(err.Error()))}
}

// stamp formats a Unix time as a local date and time for a message.
//
// Parameters:
//   - unix: seconds since 1970
func stamp(unix int64) string {
	return time.Unix(unix, 0).Local().Format("2006-01-02 15:04")
}

// trimmed returns the text of an error without the sentence of the error it wraps, which the
// message of the catalog already says.
//
// Parameters:
//   - err: the failure
//   - target: the error whose text starts the failure, such as update.ErrOffline
func trimmed(err, target error) string {
	return sanitize(scrubURLs(strings.TrimPrefix(err.Error(), target.Error()+": ")))
}

// updateNotice returns what the status line needs to announce an update, with no network
// access: it only reads the result of the last check.
//
// Nothing is announced when the user turned the check off with update.check = false, for a
// development build, or for an install owned by a package manager, which is updated by the
// package manager and not from the latest GitHub release.
//
// Returns the notice and true when an update is available.
func (a *app) updateNotice() (update.Notice, bool) {
	ic, ok := a.noticeContext()
	if !ok {
		return update.Notice{}, false
	}

	n, ok := update.ReadNotice(
		filepath.Join(ic.paths.Cache, update.CacheFile), a.Version, a.channelFor(updateFlags{}, ic),
	)

	return n, ok && n.Available
}

// refreshUpdateNotice asks the release server for the status line, at most once a day.
//
// It is meant to run in a goroutine after the interface has started. It does nothing under the
// same conditions as updateNotice, nothing while the last check is less than a day old, and no
// request when the build has no usable release key.
//
// Parameters:
//   - ctx: cancels the request when the interface closes
//
// Returns the notice, and the error of the check when it failed.
func (a *app) refreshUpdateNotice(ctx context.Context) (update.Notice, error) {
	ic, ok := a.noticeContext()
	if !ok {
		return update.Notice{}, nil
	}

	src, err := a.updateSource()
	if err != nil {
		return update.Notice{}, err
	}

	u := a.newUpdater(updateFlags{}, ic, src)

	if u.Keys == nil {
		// A build without release keys could not install what the notice would announce.
		if u.Keys, err = a.updateKeys(src); err != nil {
			return update.Notice{}, err
		}
	}

	return u.RefreshNotice(ctx, filepath.Join(ic.paths.Cache, update.CacheFile), time.Now())
}

// noticeContext checks the conditions under which an update may be announced.
//
// Returns the description of the install and true when the user did not opt out, the build is a
// release and no package manager owns it.
func (a *app) noticeContext() (installCtx, bool) {
	if !a.cfg().Update.Check {
		return installCtx{}, false
	}

	if _, err := update.ParseVersion(a.Version); err != nil {
		return installCtx{}, false
	}

	ic, err := a.installContext()
	if err != nil || ic.install.Managed || ic.paths.Cache == "" {
		return installCtx{}, false
	}

	return ic, true
}
