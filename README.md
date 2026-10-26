# tote

Your terminal tools, in one bag.

tote is a terminal launcher for SSH connections, Git repositories, shell aliases and shell
functions, with a full-screen interface built on Bubble Tea. It is the Go successor of
[mn](https://github.com/20syldev/mn).

**Status:** version 1.0.0 is the first release. What comes later is marked as planned.

## What it is

You keep four kinds of records, each in a plain TOML file:

| Kind         | What Enter does in the interface                                                |
| ------------ | ------------------------------------------------------------------------------- |
| Connections  | Opens an `ssh` session, optionally through a `via` command such as a jump host. |
| Repositories | Changes the directory of your shell to the repository.                          |
| Aliases      | Puts the command on your prompt, ready to run.                                  |
| Functions    | Puts the function name on your prompt, ready to run.                            |

The same records are available as commands (`tote co`, `tote repos`, ...), as real shell aliases and
functions loaded by `tote init`, and through a palette that searches all of them at once.

How it differs from mn:

- One static Go binary instead of a Bash program. Linux and macOS (amd64 and arm64) are the targets.
- Data in TOML files with a schema, written atomically, instead of `:::` separated lines.
- A new interface: lists with a detail panel, a palette, settings, mouse, themes, English and
  French.
- Plugins are rewritten, not ported. In 1.0 they are declarative only (a manifest, no code of their
  own) and nothing runs before you have read the exact commands and agreed.
- There is no migration inside the program: a separate script moves your data from mn, see
  [Moving your data from mn](#moving-your-data-from-mn).

## The interface

`tote` with no argument, run in a terminal, opens the full-screen interface; in a pipe or a script
it prints the help. `tote co` and `tote config` with no argument open it on the Connections list and
on the settings.

The welcome page is a boxed greeting with the number of records of each kind and a menu:
Connections, Repos, Aliases, Functions, Plugins, Settings. A list shows one kind in columns that
disappear from the right as the terminal narrows, with a filter line and a panel with the exact
command Enter would run (or the path of a repository and its Git branch). The palette searches every
record and a few actions from anywhere; the settings are changed in place and saved at once; the
Plugins screen lists installed and bundled plugins with their state; `?` lists every key that
applies.

| Key                                         | Where                             | Action                                                                                                                                                                                                                                        |
| ------------------------------------------- | --------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `1` to `4`, and `5` when the plugins are on | Welcome                           | Open the kind or Plugins, in menu order. Settings has no digit: use `,`.                                                                                                                                                                      |
| Up, Down, `k`, `j`                          | Everywhere                        | Move. `h` and `l` (or Left and Right) switch kind on a list.                                                                                                                                                                                  |
| Enter                                       | Lists                             | Use the record: connect, go to the repository, put the command on the prompt.                                                                                                                                                                 |
| `/`                                         | Lists                             | Filter: names and aliases that start with the text come first. Enter applies it and leaves the typing; on Connections the cursor lands on the row whose name or alias is exactly the text, and a second Enter uses the record. Esc clears it. |
| `n`, `e`, `d`, `v`, `C`                     | Lists                             | New, edit, delete (asks first), details, copy. On the Plugins screen `d` removes.                                                                                                                                                             |
| `v`                                         | Settings                          | The whole description, values and note of the selected setting: the panel under the rows cuts long text.                                                                                                                                      |
| `u`, `U`, `c`                               | Connections                       | Next login user, choose a user, put the `ssh` command on the prompt.                                                                                                                                                                          |
| `o`                                         | Repos                             | Open the repository in your editor.                                                                                                                                                                                                           |
| Tab                                         | Repos, Plugins                    | Menu of actions for the row (Repos: the actions that plugins add).                                                                                                                                                                            |
| `Ctrl+K`                                    | Everywhere                        | Palette. Prefixes narrow it: `c ` connections, `r ` repositories, `a ` aliases, `d ` functions, `>` actions.                                                                                                                                  |
| `,`                                         | Everywhere                        | Settings.                                                                                                                                                                                                                                     |
| `?`, `F1`                                   | Everywhere                        | Help overlay.                                                                                                                                                                                                                                 |
| `Ctrl+R`                                    | Welcome, lists, Settings, Plugins | Reload the files from disk.                                                                                                                                                                                                                   |
| `u`, `U`                                    | Welcome, Settings                 | Show the update notice, check for an update now.                                                                                                                                                                                              |
| Space                                       | Plugins                           | Enable or disable the plugin.                                                                                                                                                                                                                 |
| `a`, `s`, `i`, `d`, `c`                     | Plugins                           | Allow (consent), settings, install, remove, check.                                                                                                                                                                                            |
| Esc                                         | Everywhere                        | Go back, close an overlay.                                                                                                                                                                                                                    |
| `q`                                         | Everywhere                        | Quit (not while typing in a field).                                                                                                                                                                                                           |
| `Ctrl+C`                                    | Everywhere                        | Go back, never quits at once (twice on the welcome page quits).                                                                                                                                                                               |
| `Ctrl+D`, `Ctrl+Z`                          | Everywhere                        | Quit, suspend.                                                                                                                                                                                                                                |

In forms, Tab goes to the next field, `Ctrl+S` saves and Esc cancels.

Mouse: pointing highlights, a click selects, a double click opens, the wheel scrolls and a click on
a key of the footer presses it. Dragging selects text and copies it when you release the button;
Shift+drag (Option+drag in iTerm2) lets the terminal select instead. `ui.mouse` turns it all off.

Looks: the theme is `auto` (follows the terminal background), `dark` (for a dark terminal), `light`
(for a light one) or `mono`; tote paints no background of its own, so a theme set against the
terminal can be unreadable, and the Settings screen warns about it when the terminal has said what
its background is (`d` goes back to `auto`); the welcome page is `box`, `compact` or `off`; below 70
columns or 20 rows the welcome box is replaced by a one-line header; the page width can be capped at
120 or 160 columns. Languages: English and French, from `TOTE_LANG`, then the `lang` setting, then
`LC_ALL`, `LC_MESSAGES` and `LANG`.

## Install

tote 1.0.0 is the first release. The channels:

| Channel            | State                    | Notes                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ------------------ | ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Install script     | 1.0.0                    | Downloads the archive for your system from GitHub Releases, checks its SHA-256 against the signed checksums file, verifies the minisign signature when a verifier is available, installs to `~/.local/bin` and writes an install receipt. Never uses `sudo`, never edits your startup files. Run `curl -fsSL https://dropr.sylvain.sh/bash/tote@latest/install.sh \| sh`, or `... \| sh -s -- 1.0.0` for a given version. |
| Release archives   | 1.0.0                    | `tote_<version>_<os>_<arch>.tar.gz` for Linux and macOS, amd64 and arm64, with `tote_<version>_checksums.txt` and its minisign signature.                                                                                                                                                                                                                                                                                 |
| `.deb`             | 1.0.0                    | `tote_<version>_linux_<arch>.deb` on GitHub Releases. No apt repository follows it: to update, download the new file and run `sudo apt install ./tote_<version>_linux_<arch>.deb`. tote never updates itself.                                                                                                                                                                                                             |
| apt and Ubuntu PPA | planned for 1.1          | Signed apt repository and Launchpad package. A tote installed from them will be updated with `sudo apt update && sudo apt upgrade tote`.                                                                                                                                                                                                                                                                                  |
| `go install`       | 1.0.0                    | Needs Go 1.26 or later.                                                                                                                                                                                                                                                                                                                                                                                                   |
| From source        | works now, in a checkout | See below.                                                                                                                                                                                                                                                                                                                                                                                                                |

With Go 1.26 or later:

```sh
go install github.com/totecli/tote/cmd/tote@latest
```

Or from a clone of the repository:

```sh
git clone https://github.com/totecli/tote.git && cd tote
make build VERSION=1.0.0-local        # writes bin/tote
```

A build from source or `go install` has no release key: `tote update` refuses to install anything
(`tote update --check` still looks for a release; in the Go bin folder it prints the `go install`
command instead) and `tote doctor` warns. That is expected. The public minisign keys of the
releases:

```sh
# current: RWTTj50yYWk72oCPtVJZ8hrpENxKKpQHDtQl3l1JkOxauemhwepIFON3    next (kept offline): RWS7eI0zAwZGD3nDouKjNMVgt3opeJ9Lf3K3RNRKeNYIV1Hm9L0UirQT
sha256sum -c tote_1.0.0_checksums.txt --ignore-missing      # macOS: shasum -a 256 -c, on your line
minisign -V -H -P <public key> -m tote_1.0.0_checksums.txt  # reads the .minisig next to it
```

## Shell integration

A program cannot change the directory of the shell that started it, nor type on its prompt. tote
does both through a small shell function that you load once:

For bash, add this line to `~/.bashrc`:

```sh
eval "$(tote init bash)"
```

For zsh, add this one to `~/.zshrc`:

```sh
eval "$(tote init zsh)"
```

`tote init <bash|zsh>` prints the code and writes nothing. It gives you:

- A `tote` function around the binary. It hands the binary a private temporary file (`TOTE_HANDOFF`)
  and then does what the binary left in it: `cd` into a repository, put a command on your prompt
  (bash `read -e -i`, zsh `print -z`), reload the definitions, or exit. This is why Enter on a
  repository changes your directory.
- Your aliases and functions as real shell aliases and functions. Each repository also becomes an
  alias that runs `tote repo open <name>`, so typing its name goes there.
- The `bin` folder of the data directory added to your `PATH`.
- Tab completion of commands, connection names, plugin ids and the main flags, for bash and for zsh
  (when `compinit` is loaded).
- A reload after you change records in the interface, so new aliases work without a new shell.

`tote init <shell> --defs` prints only the definitions (aliases, functions, `PATH` line and
completion), which is what the reload runs. Without the integration the program still works, but
`cd` cannot happen: `tote repos open` then only prints the path. `tote doctor` tells you whether the
line is in place. Names that the shell or tote needs (`tote`, `source`, `eval`, keywords, ...) are
refused for a record.

The private file is made in `$XDG_RUNTIME_DIR`, else `$TMPDIR`, else `/tmp`; if none can be used,
the function says so once and runs the binary without `cd` and without putting a command on the
prompt. A relative path handed back for `cd` is made explicit (`./name`), so `CDPATH` is never
consulted.

## Commands

`tote help` lists them and `tote help <command>` prints the usage. Commands that list records print
a table on a terminal and tab separated lines in a pipe; `--format table|tsv|json` chooses and
`--json` means `--format json`. The tab separated columns are positional and a later release only
appends columns; every JSON document starts with `"schema": 1` and only gains fields (see
[docs/compat.md](docs/compat.md)). A name must be unique across aliases, repositories and functions,
because each becomes a word of your shell.

The commands are `co` (also `connections`), `alias` (`a`), `repos` (`repo`, `r`), `funcs` (`f`),
`plugin` (`plugins`), `config`, `doctor`, `update`, `uninstall`, `init`, `version` and `help`. With
no command, `tote` opens the interface, and `--ascii` draws it with plain ASCII symbols.

`--no-plugins` (before the verb) turns every plugin off for one run, like `TOTE_NO_PLUGINS=1`.

### co: connections

```
tote co -l [--format table|tsv|json | --json]
tote co <name|alias> [user]
tote co add <name> --target <host> [--via <command>] [--user <user>] [--alias <a,b>] [--ip <ip>] [--desc <text>]
tote co rm <name> [--yes]
```

`tote co <name>` replaces tote by `ssh`. A unique prefix of a name or alias works once it has
`connect.prefix_min` characters (2 by default), and a second argument overrides the login user.
`--via` is a command using `{user@}`, `{user}`, `{host}`, `{ip}` and `{name}`; empty means plain
`ssh`. A plain `ssh` gets keepalive options unless your ssh configuration sets its own. The command
is split on spaces and runs without a shell: quotes are not interpreted (and are refused), so put
options such as `ProxyCommand` in `~/.ssh/config`. Its first word is a command name found through
`PATH`: it starts with a letter, a digit or `_`, so an absolute path (`/usr/bin/ssh`) and `./ssh`
are refused. `tote doctor` warns when that program is not found.

```sh
tote co add jump --target 10.0.0.5 --desc "behind the bastion" \
    --via 'ssh -t admin@jump.example.org {user@}{host}'
tote co add web --target host1.example.org --user admin --alias w,www
tote co web deploy      # same connection, other login user
```

### alias: shell aliases

```
tote alias [list] [--format table|tsv|json | --json]
tote alias add <name> [--desc <text>] <command>
tote alias rm <name> [--yes]
```

```sh
tote alias add ll --desc "long listing" "ls -la"
```

### repos: repositories

```
tote repos [list] [--format table|tsv|json | --json]
tote repos open <name>
tote repos add <name> <path> [--desc <text>]
tote repos rm <name> [--yes]
```

A `<path>` must be absolute or start with `~/`; a relative one such as `work/app` is looked up under
`repos_root` and used when that folder exists there. `tote repos open` changes the directory (with
the shell integration) or prints the path. In the interface, `o` opens the repository in `editor`
(else `$VISUAL`, `$EDITOR`, `vim`) and `repos.after_open` decides what follows: `return` to the
list, `quit` or `cd`.

```sh
tote repos add app ~/work/app --desc "main application"
```

### funcs: functions and scripts

```
tote funcs [list] [--format table|tsv|json | --json]
tote funcs add <name> --script <path> [--kind exec|source] [--desc <text>] [--managed]
tote funcs rm <name> [--yes]
```

`--kind source` (the default) defines a shell function that sources the script, so it can change
your shell. `--kind exec` only reserves the name: tote lists it and defines nothing, and the program
itself lives in the `bin` folder of the data directory or in `PATH`. `add` does not check that it
exists, and `tote doctor` does not look at it. `tote doctor` does warn about a repository whose path
is not an existing directory and about a `source` function whose script is missing. A function name
may contain `-` (`fn-x`); bash in POSIX mode (`bash --posix`, `sh` linked to bash,
`POSIXLY_CORRECT=1`) refuses such a name, so use letters, digits and `_` only if your startup file
is read in that mode. `--managed` records that the script belongs to tote; the flag is stored and
shown, and nothing in 1.0 deletes or moves the file. A later release may act only on scripts under
the `bin` folder of the data directory (see [docs/compat.md](docs/compat.md)).

```sh
tote funcs add deploy --script ~/scripts/deploy.sh --desc "push to production"
```

### plugin: plugins

```
tote plugin [list] [--format table|tsv|json | --json]
tote plugin info <id> [--json]
tote plugin install <source> [--sha256 <hex>] [--grant <token>] [--dry-run] [--force] [--json]
tote plugin update <id> [<source>] [--sha256 <hex>] [--grant <token>] [--dry-run] [--force] [--json]
tote plugin enable <id> [--grant <token>] [--json]
tote plugin disable <id> [--json]
tote plugin remove <id> [--yes] [--purge] [--json]
tote plugin check [<id|dir|archive>] [--strict] [--official] [--argv [--set <field=value>] [--repo <name>]] [--json]
```

Exit codes: 0 done, 1 failure, 2 usage or a flag is needed, 3 authentication required, 4 refused
(state, policy, `--grant` mismatch), 5 integrity; `update --check` adds 10, 127 means a program
could not be started and 130 an interruption (Ctrl-C at the question, or SIGINT or SIGTERM at any
step, a download included; nothing is changed). Codes 6 to 9 and 11 and above are kept for later
releases. See [Plugins](#plugins) and [docs/compat.md](docs/compat.md).

### The others

```
tote config                       # opens the settings on a terminal
tote config path                  # the directory of config.toml and the record files
tote doctor [--json]              # checks, with a fix for each warning
tote doctor --reset               # restores a broken terminal, nothing else
tote update [--check] [--json] [--beta] [--force]
tote uninstall [--yes] [--shell] [--purge] [--dry-run]
tote init <bash|zsh> [--defs]
tote version [--json]
tote help [command]
```

- `doctor` runs more than twenty checks (version, install receipt, `PATH`, directories and files,
  name clashes, shell integration, `ssh` and `git`, `TERM`, locale, colors, mouse, clipboard, size,
  tmux, the update key). Its exit code is 0 when no check failed and 1 when one did; warnings do not
  change it.
- `update --check` exits 0 when up to date and 10 when a release is available; `--beta` also offers
  pre-releases and `--force` installs even an older or the same release. With `--json` an error is
  `{"schema": 1, "ok": false, "status": "error", "error": {"code", "message", "exit"}}` on standard
  output, with `code` one of `offline`, `rate_limited`, `no_release`, `bad_signature`,
  `unknown_key`, `bad_checksum`, `bad_key`, `interrupted` and so on. A misuse of the command (an
  unknown option, an extra word) is the same object with `code` `usage` and exit 2, also when
  `--json` comes before or after the bad option, but not after `--`.
- `uninstall` removes the program and keeps your data. `--purge` deletes the data too, that is the
  record files, `config.toml`, the plugins with their consent and settings, the plugin lock, the
  install receipt and the update cache, and nothing else (a folder of your own, or a backup next to
  the files, stays); `--shell` also removes the line that loads tote from your startup files,
  `--yes` skips the questions, `--dry-run` prints the plan and removes nothing. The exact list is in
  [docs/compat.md](docs/compat.md).

## Configuration and data

Directories follow the XDG rules; a `TOTE_*_DIR` variable overrides one and must be absolute.

| Purpose                                  | Variable          | Then                    | Default               |
| ---------------------------------------- | ----------------- | ----------------------- | --------------------- |
| Config: records and settings             | `TOTE_CONFIG_DIR` | `$XDG_CONFIG_HOME/tote` | `~/.config/tote`      |
| Data: plugins, a `bin` folder for `PATH` | `TOTE_DATA_DIR`   | `$XDG_DATA_HOME/tote`   | `~/.local/share/tote` |
| State: install receipt, plugin lock      | `TOTE_STATE_DIR`  | `$XDG_STATE_HOME/tote`  | `~/.local/state/tote` |
| Cache: update check, plugin caches       | `TOTE_CACHE_DIR`  | `$XDG_CACHE_HOME/tote`  | `~/.cache/tote`       |

The config directory holds `config.toml` (settings and `[plugins.<id>]` tables), `connections.toml`,
`repos.toml`, `aliases.toml`, `funcs.toml`, and `plugins/<id>.toml` (the settings of a plugin).
Every file that tote writes starts with `schema = 1`, is plain text and safe to edit while tote is
not writing; a `.lock` file next to it serialises writers. `tote doctor` reports a file that does
not parse (with line and column) and warns when the directory or a file can be changed by other
users. A record file that does not parse, or has another schema, is read-only. `config.toml` is
lenient: an unknown key or an invalid value is reported on standard error with its line and the
default applies; only a syntax error or a schema other than 1 sets the whole file aside. Data that a
plugin keeps goes to `plugins.lock` or `plugins/<id>.toml`, never to the `[plugins.<id>]` tables of
`config.toml`. Formats stay compatible within 1.x, see [docs/compat.md](docs/compat.md). A record is
a `[[connection]]` (or `[[repo]]`, `[[alias]]`, `[[func]]`) table, for example `name = "web"`,
`target = "host1.example.org"`, `user = "admin"`, `aliases = ["w", "www"]`, `desc = "Web host"`.

`config.toml` with every setting at its default (all optional):

```toml
schema = 1
lang = ""                   # "en", "fr", or empty to follow the environment
editor = ""                 # empty: $VISUAL, then $EDITOR, then vim
repos_root = ""             # folder offered for a new repository, base of relative paths
[connect]
prefix_min = 2              # characters needed before a prefix connects
after_disconnect = "menu"   # "menu" or "exit"
keepalive = true
[repos]
after_open = "return"       # "return", "quit" or "cd"
[shell]
compat_mn = false           # also define a function named mn
extra_names = []            # more function names to expose
[update]
check = true                # look for a release in the background, at most once a day
[ui]
theme = "auto"              # "auto", "dark", "light" or "mono"
welcome = "box"             # "box", "compact" or "off"
compact = "auto"            # "auto", "always" or "never"
width = "auto"              # "auto", "120" or "160": caps the page at that many columns
mouse = true
```

| Variable                                                               | Effect                                                                                                                                    |
| ---------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- |
| `TOTE_CONFIG_DIR`, `TOTE_DATA_DIR`, `TOTE_STATE_DIR`, `TOTE_CACHE_DIR` | Directory overrides.                                                                                                                      |
| `TOTE_LANG`                                                            | Language of the interface (`en` or `fr`), before the `lang` setting.                                                                      |
| `TOTE_COLOR`                                                           | `truecolor`, `256`, `16`, `none` (also `never`, `off`), `always` or `auto`.                                                               |
| `NO_COLOR`                                                             | Any non-empty value turns the colors off; `TOTE_COLOR` wins over it.                                                                      |
| `TOTE_NO_PLUGINS`                                                      | Any value other than empty, `0`, `false`, `no` or `off` turns every plugin off for the run.                                               |
| `TOTE_PLUGIN_<ID>_<SETTING>`                                           | The value of a plugin setting (each hyphen of the id becomes two underscores: `my-tool` and `x` give `TOTE_PLUGIN_MY__TOOL_X`).           |
| `TOTE_UPDATE_BASE`, `TOTE_UPDATE_PUBKEY`                               | Read releases from a mirror (`https://` or `file://`); the key override works only with a `file://` mirror and never for root. For tests. |

`TOTE_HANDOFF` and `TOTE_SHELL` are set by the shell function and are not meant to be set by hand.

## Plugins

A plugin adds commands to the palette and actions to the rows of a list. In tote 1.0 a plugin is
**declarative**: a folder with a `plugin.toml` and optional translations, with no code of its own.
The manifest lists the programs to run (`git`, `curl`, `gft`), their arguments and the form to ask
first. tote builds each command line itself, one argument per element, and never runs a shell. The
**exec tier**, where a plugin ships its own program, is reserved for a later version: a manifest
that asks for it is refused, and `tote plugin run` does not exist in 1.0.

Consent comes first. Before a plugin is installed, or when an update asks for more, tote shows every
command in full and what the plugin declares (programs, files, network, environment variables, the
terminal), marking what tote enforces and what is only declared. Nothing runs before you agree, a
plugin whose files changed after you agreed is refused, and any plugin can be switched off with one
command. The programs a plugin names still run with your rights: read the list.

The plugin that ships with tote is **gft**. It is embedded in the binary, listed as `available`
until you install it, and needs the `gft` program (GitHub releases and Git stashes). On a saved
repository with a GitHub remote (Tab on the row) it adds: show the latest release, its assets and
its notes; list, show, drop and delete stashes. In the palette (`>`) it adds: show a GitHub release,
open its page, download an asset, install a release binary.

Look first, nothing changes:

```sh
tote plugin                           # list: gft is "available"
tote plugin install gft --dry-run     # read what it asks for
tote plugin check gft                 # the commands it would run, and any problem
```

Then, one at a time:

```sh
tote plugin install gft               # asks on the terminal
tote plugin disable gft               # settings and consent are kept
tote plugin remove gft --purge        # also delete its settings and data
```

From another source, with the digest you were given:

```sh
tote plugin install example/demo@1.0.0 --sha256 <64 hex digits>
```

`remove` also forgets the `enabled = false` of a plugin you had switched off, so a later install
starts enabled; `--purge` adds the settings, state, cache and data.

Without a terminal, consent is a token: `--dry-run` prints the `--grant` value to pass to `install`,
which fails if the plugin asks for anything else. A release is a `github.com` release URL or
`owner/repo@tag` and needs the SHA-256 its author published; a folder on your machine
(`./my-plugin`) is not verified at all. The states are `enabled`, `disabled`, `needs-consent`,
`missing-dependency`, `incompatible`, `broken`, `quarantined` and `available`. Quarantine turns a
plugin off after failures; a declarative run never counts as one, so it does not occur in 1.0, and
`tote plugin enable` clears it.

- [docs/plugins/AUTHORING.md](docs/plugins/AUTHORING.md): write, check, install and release a
  plugin, step by step.
- [docs/plugins/API1.md](docs/plugins/API1.md): the reference of plugin API 1.
- [docs/plugins/SECURITY.md](docs/plugins/SECURITY.md): what a plugin can do, and what tote checks.
- [docs/plugins/plugin.schema.json](docs/plugins/plugin.schema.json): JSON Schema of the manifest.
- [docs/compat.md](docs/compat.md): what stays compatible in the 1.x line (files, JSON, exit codes).

## Updates and signatures

`tote update` reads the releases of `totecli/tote` over https, downloads the archive for your
system, checks it against the checksums file, verifies the minisign signature of that file with the
release keys built into the binary, checks that the new binary announces the expected version, and
replaces the running one atomically. A signature that does not verify is always an error. Versions
are SemVer; stable is the default channel and pre-releases (`1.1.0-beta.1`) are offered only with
`--beta`.

An install receipt (`install.json` in the state directory) records how tote was installed, the
version, the channel and the signed timestamp of the release, and is used to refuse a downgrade. It
stays at schema 1: later releases only add optional fields, which an older tote keeps. A tote owned
by a package manager (apt, Homebrew, `go install`) never updates or removes itself: `tote update`
and `tote uninstall` print the command to run. With `update.check` on, tote looks for a release in
the background at most once a day, and `u` on the welcome page reads the notice; nothing is
installed without `tote update`.

## Terminal requirements

- At least 24 columns by 7 rows; below that the interface asks for a larger terminal. The full
  layout needs about 70 by 20, smaller terminals get the compact one.
- A UTF-8 locale for the symbols. With another locale, in the Linux console or with `--ascii`, tote
  draws plain ASCII and unaccented text.
- Colors are detected: truecolor is optional, and `TOTE_COLOR`, `NO_COLOR` or the `mono` theme turn
  them down or off.
- Clipboard: selecting text or pressing `C` copies it. tote tries `wl-copy` (Wayland), `xclip`, then
  `xsel` (X11), and otherwise sends an OSC 52 sequence to the terminal. GNOME Terminal and other VTE
  based terminals ignore OSC 52: install `xclip`, `xsel` or `wl-clipboard` there, or select with
  Shift+drag and copy with Ctrl+Shift+C. `wl-copy` is used only when `WAYLAND_DISPLAY` is set,
  `xclip` and `xsel` only when `DISPLAY` is set. On macOS there is no `pbcopy` path: copying uses
  OSC 52, so use a terminal that supports it (iTerm2, kitty, WezTerm, Ghostty) or select with
  Option+drag and use the terminal's own copy. Inside tmux, OSC 52 needs `set -g set-clipboard on`.
  `tote doctor` shows what applies to your session.

## Troubleshooting

```sh
tote doctor
```

Each line is `OK`, `WARN` or `FAIL`, and every warning prints its fix (shell integration line
missing, directory not in `PATH`, a file that does not parse, a name used twice, no clipboard tool,
...).

If a program left your terminal in a bad state (no echo, garbage after a crash), run
`tote doctor --reset`: it restores the terminal and does nothing else, and needs a terminal on
standard output. If the interface looks wrong, try `tote --ascii`, `TOTE_COLOR=none tote` or the
`mono` theme; to rule a plugin out, run `tote --no-plugins`. `tote config path` shows where your
files are.

## Known limits

- **Records edited by hand.** A record whose `via` holds a quote, or whose text field (`cmd`,
  `path`, `script`, `target`, `via`, ...) is longer than 4096 bytes, is refused by `add` and by the
  forms, and when it is already in a file it loads as _invalid_: the file stays usable,
  `tote doctor` names the line, the shell skips the record and `tote co <name>` refuses it until you
  edit it. A record file is also refused for writing when it would pass 4 MiB.
- **Inserted bash lines.** The line that tote puts on your prompt runs inside the `tote` function,
  so a `declare` or `typeset` at its start is local to that function. Assignments, `export`, `cd`
  and the rest work as usual. Fixing it needs the line to run at the top level of the shell; that is
  planned for 1.1.
- **Function names with a dash** are valid, but bash in POSIX mode refuses to define them.
- **Names are checked, not locked.** The rule that a name is unique across aliases, repositories and
  functions is checked before each `add`, not under one lock: two `tote` processes adding the same
  name at the same moment can both succeed. `tote doctor` then reports the clash and the shell keeps
  the first one.
- **Closed terminal.** SIGINT and SIGTERM stop an install or an update cleanly (exit 130, nothing
  changed). SIGHUP, a closed terminal, ends the process at once; what it left in the `.staging`
  folder of the plugins is removed by a later install, after one hour.
- **Translations.** Most refusals are translated, but a few messages stay in English: the free text
  of a syntax error in a TOML file, an I/O error of the system and some details of `tote doctor`.
- **Exec plugins.** A manifest that asks for the exec tier is refused and `tote plugin run` does not
  exist: both are planned for 1.1, with the `ghrel` and `ghsec` plugins.

## Moving your data from mn

The program has no import command. A separate Bash script (bash 4 or later), `scripts/import.sh`,
does the move. It is in the repository only, not in the release archives or the packages: clone
`https://github.com/totecli/tote.git`, or download `scripts/import.sh` from the tag of your version
(`https://raw.githubusercontent.com/totecli/tote/v1.0.0/scripts/import.sh`). It is a dry run unless
you pass `--apply`:

```sh
bash scripts/import.sh                  # dry run: shows what would be imported, writes nothing
```

When the result is what you expect, run it for real:

```sh
bash scripts/import.sh --apply
```

`--from DIR` and `--to DIR` choose other locations than the defaults.

Other options: `--home DIR`, `--system-bin DIR`, `--verbose` and `--help`, the full reference. By
default it reads `.config/mn` in the home of your account and writes to the tote configuration
directory.

- It moves connections, aliases, repositories and functions. The language and editor files, the
  repository list file, the plugins, the docs and the files mn generates are left behind on purpose.
  A function is only referenced by its script (`.local/bin/NAME`, else `/usr/local/bin/NAME`), which
  is never moved, copied or managed.
- Every value is checked with the rules of tote. A record that fails is skipped with its reason; the
  exit status is 1 when something was skipped. Aliases, repositories and functions share one
  namespace in tote, so when a name clashes the first one wins, in that order.
- It never writes below the mn directory, never changes an existing tote record, can be run again
  without importing anything twice, and refuses to run as root.
- With `--apply`, each tote file it changes is first copied to `FILE.bak-<date>-<time>`, and the new
  records are checked in the files afterwards.

Read the dry run first, then check the result with `tote co -l`, `tote repos` and so on.

## Contributing

Read the [Conventions](#conventions) below first. The Makefile targets:

| Target                    | What it does                                                                                                                                                                                                                                             |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `make build`              | Build `bin/tote`; takes `VERSION=` and, for a release build, `KEYS=` (the minisign public keys that `tote update` trusts).                                                                                                                               |
| `make test`               | Everything, in several minutes: unit tests, view frames against `internal/ui/testdata`, terminal injection probes and the tmux end-to-end tests (skipped without tmux, unless `TOTE_REQUIRE_TOOLS=1` or `CI=true`, which make a missing tool a failure). |
| `make short`, `make e2e`  | The same without tmux, and only the tmux end-to-end tests.                                                                                                                                                                                               |
| `make race`               | The race detector on the interface, updates, command line and end-to-end tests; needs a C toolchain and takes about half an hour (each package has a 30 minute limit). CI does not race the interface tests: run it before every release.                |
| `make check`              | `gofmt`, `scripts/docs.sh` (Markdown), `go vet`, `golangci-lint`, the tests and `govulncheck`; `golangci-lint` and `govulncheck` must be installed.                                                                                                      |
| `make cover`, `make slow` | Coverage of `internal/ui`; the single core end-to-end passes.                                                                                                                                                                                            |
| `make golden`             | Rewrite the reference frames; review them with `git diff internal/ui/testdata`.                                                                                                                                                                          |
| `make snapshot`           | Release dry run in `dist/`, unsigned and uploaded nowhere; needs goreleaser v2. See [docs/RELEASING.md](docs/RELEASING.md).                                                                                                                              |

The tools that `make check` and `make snapshot` call are not part of the repository. Install them
once (the binaries land in `$(go env GOPATH)/bin`, which must be in your `PATH`):

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
go install github.com/goreleaser/goreleaser/v2@latest
export PATH="$(go env GOPATH)/bin:$PATH"
```

`go install` needs the network, and so does `govulncheck`, which reads the vulnerability database.

## Licence

MIT, see [LICENSE](LICENSE).

## Conventions

- **Language**: everything is in English first (code, comments, commits, documentation, releases,
  visuals). The interface is English by default, with French as a second language.
- **Branch**: work happens on `master`.
- **Commits**: a single subject line, `SEVERITY: scope: Description`, no body. Severities `MAJOR`,
  `MEDIUM`, `MINOR`, `BUILD`, `DOC`; scopes `cmd`, `ui`, `config`, `store`, `shell`, `plugin`,
  `i18n`, `update`, `scripts` (the folder name); `BUILD` and `DOC` have no scope. One scope per
  commit, signed.
- **Commit check**: CI runs `scripts/commits.sh` on every push to `master` and on pull requests
  (format and single line). It can be run locally on any range, for example
  `scripts/commits.sh origin/master..HEAD`. There is no git hook.
- **Markdown**: prose lines stay within 100 columns (tables and fenced code are left alone).
  `scripts/docs.sh` checks it, in `make check` and in CI.
- **Releases**: signed tag with a `v` prefix (`v1.0.0`, as `go install` requires), empty release
  title. Betas (`1.1.0-beta.1`) are GitHub pre-releases, installed with the script or, once tote is
  installed, with `tote update --beta`; never published to apt.
- **Dependencies**: only Bubble Tea v2, Lip Gloss v2, a few Bubbles packages (`key` and
  `textinput`), `x/ansi`, `charmbracelet/colorprofile` (color detection, Charm family), a TOML
  library, `golang.org/x/crypto` (the `blake2b` package only, to verify update signatures) and
  `golang.org/x/sys/unix` (terminal size for `tote doctor`). Charm imports, `colorprofile` included,
  stay inside `internal/ui/`.
- **No migration in the app**: moving data over from mn is a separate script described in this
  README, never a command of tote.
- **Instructions for contributors live in this README** (this section), not in a per-tool
  instruction file.
