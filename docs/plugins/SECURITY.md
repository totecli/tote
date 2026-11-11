# Security of tote plugins

This document is the security statement for tote 1.0 and plugin API 1. It is written for the people
who install plugins and for the people who write them. It says what tote protects, how, and where
the protection stops.

Only the declarative tier is available in 1.0. A declarative plugin has no code of its own: it is a
`plugin.toml` that tells tote which programs to run. Section 10 lists what is reserved and not
available.

Where an example shows output, it is the real output of a build of tote 1.0 in a throwaway home
directory, with neutral data (a plugin called `demo`, the repository `example/demo`). The `demo` of
each document in this folder is a different plugin, so hashes and grant tokens differ between them.
Long paths are shortened to `/work`. Section 11 names the tests behind each protection. The other
documents are [API1.md](API1.md), the reference, [AUTHORING.md](AUTHORING.md), the tutorial, and
[../compat.md](../compat.md), which lists what every 1.x release keeps.

## 1. The short version

- A plugin makes tote run programs with your rights. It is not sandboxed. If a program the plugin
  lists can run other programs, the plugin can run anything you can run.
- tote shows the exact command line templates before you agree, and nothing runs before you agree.
- A plugin that asks for more later has to ask you again. A plugin that changes what its commands
  do, without asking for more access, does not always ask again (section 4.5). Read the update diff.
- A plugin from a release is installed only if you type the sha256 checksum of the archive. That
  proves you got the bytes the author published. It does not prove who the author is.
- A plugin from a directory on your machine is not verified at all.
- You can switch any plugin off with one command, and tote refuses to start a plugin whose files
  changed after you agreed.

## 2. How to read this document

Two words are used with a precise meaning, and the consent screen uses the same two as tags.

- **Enforced**: tote itself checks or refuses. The plugin cannot go beyond it without a bug in tote.
- **Declared**: the plugin states it, the consent screen shows it, and tote cannot check it. An
  honest plugin keeps to it. A hostile one is not stopped.

In 1.0, `exec:` and `env:` permissions are enforced for a declarative plugin (tote builds the
command and the environment). `net:` and `fs:` permissions are declared only: tote does not watch
what the program it started does with the network or the files.

## 3. What a declarative plugin is, and what it can do

### 3.1 What it is

A declarative package holds `plugin.toml`, optionally `locales/en.toml` and `locales/fr.toml`, and
files named `README*` or `LICENSE*` at the root. Any other file makes the package invalid
(`declarative_extra_file`), so no program travels under the declarative label.

```
$ tote plugin install ./extra --dry-run
tote: error: run.sh: a declarative plugin may not ship the file run.sh
```

### 3.2 What it can do

- Ask tote to start the programs it lists in `exec:` permissions, with arguments built from its own
  templates, the values you type in its forms, and its settings. There is no shell: each argument is
  one element of an argument list.
- Ask for the terminal for the duration of a command (`output = "terminal"`). The program then reads
  your keys and draws on your screen. The consent screen lists this as a strong line.
- Show program output in a pager, cleaned (section 7).
- Ask tote to open an `https` address it built and checked, if it holds `host:open-url`.
- Read, through placeholders, the name, path, cleaned remote, slug and branch of your saved
  repositories, if it holds `host:read:repos`.
- Receive the environment variables it names in `env:` permissions, if they are set.

### 3.3 What it cannot do

| It cannot                                                                                       | Why                                                                                                                        | Level                                                                                                                                |
| ----------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| Start a program that no `exec:` permission names                                                | the name at the start of every command must be covered (`exec_uncovered`)                                                  | enforced                                                                                                                             |
| Start tote itself as the program of a command                                                   | `exec:tote` is refused, and so is any file that is the tote binary under another name (`exec_self`)                        | enforced for the first word of a command; a listed program such as `git` can still start tote through an alias (section 3.4)         |
| Start a shell or launcher by name (`sh`, `bash`, `env`, `xargs`, `python`, `find`, `sudo`, ...) | refused when the manifest is read and again when the command is built                                                      | enforced, but the list is advisory (section 3.4)                                                                                     |
| Put text on your shell prompt, write the clipboard, read your connections                       | `host:handoff`, `host:clipboard`, `host:read:connections` and the like are reserved and refused (`reserved_not_available`) | enforced                                                                                                                             |
| Receive your ssh agent socket, your runtime directory, tokens or tote's hand-off file           | the environment is built from nothing (section 6.1)                                                                        | enforced for the environment that tote builds, not against a program that reads tote's own environment through `/proc` (section 3.4) |
| Change its own consent, the lock, `config.toml` or another plugin through tote                  | only the host writes those files                                                                                           | enforced by tote's code, not by the system (section 8)                                                                               |
| Install, update, enable, disable or remove a plugin through tote                                | tote refuses those verbs when it was started by a plugin                                                                   | best effort (section 8.3)                                                                                                            |
| Ship a program inside the package                                                               | extra files are refused                                                                                                    | enforced                                                                                                                             |
| Make tote contact the network                                                                   | only the install and update of a release do, and only to GitHub                                                            | enforced for tote, declared for the programs it runs                                                                                 |

### 3.4 The central limit

tote runs the listed programs with your rights. Whatever a listed program can do, the plugin can do
through it. Programs that start other programs are common. A plugin that lists only `git` can still
write this:

```
run = ["git", "-c", "alias.x=!id", "x"]
```

`plugin check` warns about this particular option, and the plugin is accepted, because no list of
options can be complete. The only control that works is the one tote gives you: it prints the
command, in full, before you agree.

```
$ tote plugin install ./risky --dry-run
...
Commands it will run:
  $ command go: git -c 'alias.x=!id' x

This plugin will be able to:
  - run the program git [enforced]
    enforced: tote itself refuses anything else

! tote runs only the listed programs, but those programs can do whatever you can do with them.
```

When you read a consent screen, read the commands first. Treat a declarative plugin as code that you
have reviewed, not as a permission list.

Once a plugin gets a program to run arbitrary code, tote's own restrictions no longer bind it. Such
a program is a child of tote under your account. It can read tote's environment through `/proc`,
open your terminal, and reach your ssh agent through a socket path it finds. tote 1.0 does not try
to hide itself from its children.

### 3.5 The repository is untrusted too

A command or an item action runs in a directory, and for a repository action that directory is a
repository you saved, often one you cloned from someone else. A program that the plugin runs there,
such as `git`, `make` or a build tool, reads the configuration, the hooks and the build files of
that repository, and they can run code. For example a `.git/config` can set `core.fsmonitor` or
`core.sshCommand`. tote hardens only its own `git` helper (section 6.3). It does not harden the
programs a plugin runs, and consent does not cover what a hostile repository makes them do.

If you run a plugin action in a repository you do not trust, read the command first and keep in mind
that the repository decides part of what runs. As an author, prefer commands that do not read
repository configuration, give `git` explicit `-c` overrides where that is enough, and say in the
`description` that the command works inside the repository.

## 4. Consent

### 4.1 When tote asks

| Moment                                                                                        | Consent                        |
| --------------------------------------------------------------------------------------------- | ------------------------------ |
| Install of a plugin that is not installed                                                     | asked                          |
| Update that asks for more access, or whose commands changed in a way listed in section 4.5    | asked                          |
| Update to another source, an older version, the same version or a pre-release, with `--force` | asked                          |
| `enable` of a plugin that has no consent on record, or whose consent no longer covers it      | asked                          |
| Update that asks for nothing more                                                             | not asked, one line is printed |
| `disable`, `remove`, `check`, `info`, `list`                                                  | not asked                      |

Official and bundled plugins are asked too. "Official" is a label that tote computes from the origin
of a release: `totecli/<id>`, for the plugin of that same id. It is never read from the manifest,
and it is never a right.

Ids are restricted so that a plugin cannot pass for tote. An id may not be a tote verb, a reserved
word (`tote`, `core`, `host`, ...), a name starting with `tote-`, or one of the official ids, which
only the copy shipped with tote and the official release of that id (`totecli/<id>`) may use. The
rule is an exact match: the origin must be `totecli/` followed by the id, and the id must be one of
the official ones. A release of `totecli/ghrel` that claims the id `gft` is not official,
`totecli/registry` is official for nothing, and the former name `totecli/plugin-gft` no longer is.
The archive of a release is still called `plugin-<id>.tar.gz`; only the repository is named `<id>`.

```
$ tote plugin install ./fakegft --dry-run
tote: error: id, line 2: the id "gft" is not allowed: it is reserved for the official plugins
```

### 4.2 What the screen shows

The command line renders one structure, built in `internal/plugin`, and a screen of the terminal
interface is meant to show the same one. In order:

1. The title (install, update from one version to another, or "asks for more access").
2. The source, and where it comes from: `official`, `community`, `local files, not verified` or
   `shipped with tote`.
3. A short checksum and where it came from: typed by you, computed on this machine with nothing to
   compare it with, or compiled into this tote.
4. The code line: "No code of its own: tote runs only the programs listed below."
5. The description, in a box labelled "written by the plugin, not checked by tote".
6. Every command, in full: each `run` and `open` template, with its `cwd`, its `allow_dash` names
   and its `env` table.
7. What the plugin will be able to do: one line per capability, grouped as terminal, programs,
   network, files, environment and host. Each line is tagged `[enforced]` or `[declared]`. Broad or
   sensitive lines are marked `!`: files in the home or the ssh directory or anywhere, a wildcard
   network host, an environment variable that looks like a credential, the terminal.
8. Warnings: a required program that is missing (with the plugin's hint, which is never run), a
   changed source, a downgrade, a pre-release.
9. The closing sentence and the consent code.

Real output, for the plugin `demo` (its `plugin.toml` is at the end of section 5.2):

```
$ tote plugin install ./demo --dry-run
Install plugin demo 1.0.0?

Source: /work/demo
Origin: local files, not verified
Checksum: sha256 9455857586bb, computed here, nothing to compare it with
No code of its own: tote runs only the programs listed below.

Description (written by the plugin, not checked by tote)
  | Show the last commits of a repository.

Commands it will run:
  $ command log: git log --oneline -n '{count}'

This plugin will be able to:
  - run the program git [enforced]
    enforced: tote itself refuses anything else

! tote runs only the listed programs, but those programs can do whatever you can do with them.

consent is asked because:
  - it is a new plugin
Consent code exec:git@e32982c86ba8

dry run: nothing was changed
to accept it without a terminal, run the command again without --dry-run and with --grant exec:git@e32982c86ba8
```

### 4.3 Answering

On a terminal, tote prints the screen on standard error and asks one line. The default is No.
Pressing Enter cancels. There is no one-key accept and no `--yes` for consent. Ctrl-C ends the wait
and exits 130; SIGINT or SIGTERM during a download exits 130 too, with nothing changed.

```
Install and enable plugin "demo3"? [y/N]
tote: cancelled, nothing changed
```

Without a terminal, tote needs `--grant TOKEN`. The token is the sorted list of effective
capabilities, joined by commas, then `@`, then the first 12 hex digits of the surface hash (section
5.2). `--dry-run`, `plugin info` and `plugin check` print it. The token binds what the plugin may
reach and what it runs. A script written for one version therefore stops working when the plugin
asks for more, instead of agreeing for you.

```
$ tote plugin install ./demo < /dev/null
tote: error: consent needed: run in a terminal, or pass --grant exec:git@e32982c86ba8 after reviewing what the plugin asks for
$ echo $?
2
$ tote plugin update esc --grant 'exec:git@92db25a2862a'    # the plugin now also wants exec:curl
tote: error: the --grant token does not match what this plugin asks for: the current token is in tote plugin info, or in the output of --dry-run
$ echo $?
4
```

A token that does not match is refused even when you answered yes at the terminal. A token is
checked only when consent is asked: an update that asks for nothing more goes through with the token
of an older version, because it cannot widen anything.

### 4.4 Escalation

Let `old` be the capabilities on record and `new` those of the new version. Capabilities are the
declared permissions plus the derived ones: `derived:tty` (some command has `output = "terminal"`)
and, for the exec tier that tote 1.0 does not have, `derived:code`. The new version asks again when
some capability of `new` is not covered by `old`.

- A capability is covered if it is in `old`, or if it is `net:H` and `old` has `net:*.D` with `H` a
  strict subdomain of `D`.
- Nothing else is implied. `fs:write:cwd` does not cover `fs:read:cwd`. For capabilities, tote may
  ask too often, and it never asks too rarely. Changed commands are another matter (section 4.5).
- A capability that is dropped never asks, and the new, smaller set is recorded.

A command that now needs the terminal, a new `exec:` program, a new `net:` host, a new `env:` name
and a new `host:` entry are all escalations.

```
$ tote plugin update esc --dry-run          # 1.3.0 adds exec:curl
esc 1.3.0 asks for more access
...
New access:
  + run the program curl [enforced]
Unchanged: 1
No longer asked: 0
Commands: 0 added, 0 removed, 0 changed
...
consent is asked because:
  - it asks for more access than you allowed
```

If you decline, nothing changes: the old version stays installed and keeps running.

### 4.5 Changed commands, and what is not asked

A changed command asks again only in four cases. This is a trade-off that was made on purpose, and
it leaves a residual risk.

Asked again, for an entry that keeps its id:

- its `confirm` is removed or turned off;
- a name is added to its `allow_dash` list, or a new entry arrives with such a name;
- a name is added to its `env` table, or the value of an existing name changes (a value can start
  another program);
- a `[[platform]]` hash changes while the version stays the same (exec tier, not in 1.0).

```
$ tote plugin update esc --dry-run          # 1.1.0 removes confirm = true from "drop"
esc 1.1.0 asks for more access
...
  ! command drop no longer asks for confirmation
...
consent is asked because:
  - what it runs changed in a way that asks again
```

Not asked, shown in the diff of commands:

```
$ tote plugin update esc --dry-run          # 1.2.0 changes "git stash drop" into "git stash clear"
Update plugin esc from 1.0.0 to 1.2.0?
...
Commands: 0 added, 0 removed, 1 changed
  - command drop: git stash drop
  + command drop: git stash clear

dry run: nothing was changed
$ tote plugin update esc < /dev/null          # the same update, for real, without a terminal
updated esc from 1.0.0 to 1.2.0, permissions unchanged
```

So a plugin you allowed to run `git` can, in a later version and without asking, run other `git`
commands, new commands included. The update diff is the only place that tells you. Each update is
compared with the version installed before it, not with the first one you read. An update is always
a command you type, so read the diff with `--dry-run` first.

### 4.6 Enable, disable, remove

`disable` sets the kill switch (section 8.2). `enable` clears it, and asks for consent when none is
on record or when the consent no longer covers the plugin. `enable` refuses a plugin that is broken
or incompatible and tells why. When the reason is that the files changed on disk, the exit code
is 5.

## 5. Where the bytes come from

### 5.1 Sources

| Source            | Written as                                                                              | What is checked                                                   |
| ----------------- | --------------------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| Shipped with tote | `tote plugin install gft` (a bare id)                                                   | tree and surface hashes compiled into the tote binary; no network |
| Release           | `https://github.com/<owner>/<repo>/releases/download/<tag>/<asset>` or `owner/repo@tag` | the sha256 you type, required on every install and every update   |
| Local archive     | a `.tar.gz` path starting with `/`, `./`, `../` or `~/`                                 | the sha256 only if you give one                                   |
| Local directory   | a directory path starting the same way                                                  | nothing; tote computes the hashes                                 |

For the bundled plugin, the pin is part of the tote binary. Whoever can replace that binary can
replace the plugin. The consent screen says "covered by the signature of this tote release": release
binaries are signed with minisign, so the plugin pin is as trustworthy as the tote binary you
verified, and no more. The official release of that id (`totecli/<id>`) is accepted as an update of
the bundled copy.

`--sha256` is refused for a directory or a bundled plugin (`hash_unexpected`, exit 2): a checksum
that is not checked must not look like one that was.

### 5.2 Hashes

| Name             | What it covers                                                                                                              | Used for                                               |
| ---------------- | --------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| `archive_sha256` | the bytes of the downloaded archive, as they came on the wire                                                               | what you pin with `--sha256`                           |
| `tree_sha256`    | every regular file of the package                                                                                           | identity of the installed files; checked at every load |
| `surface_sha256` | everything the plugin runs: commands, fields, `env`, `cwd`, `open`, `allow_dash`, `confirm`, output mode, `pause`, timeouts | the grant token; detects a change of commands          |

`tree_sha256` is the sha256 of one line per regular file, in byte order of the path:
`path NUL size NUL sha256-of-content NUL x-or-dash LF`. The execute bit is the only mode bit that
counts. Directories, dates and owners are not in it. It covers the manifest, so it covers the
permissions. The installed tree is stored in `<data>/plugins/<id>/<first 32 hex digits>/`. The full
hash decides identity, never the directory name.

`surface_sha256` leaves out labels, descriptions, hints and translations. It answers one question:
does this version run the same commands.

A key of the surface counts only when it is set. The hash is computed over one object per
contribution that holds its table and id, and each other key only when it is not at its zero value
(empty, `false`). Adding a manifest key in a later release therefore never changes the hash of an
installed plugin, which would otherwise ask for its consent again and invalidate every stored
`--grant` token. The same rule binds the later key: it counts in the hash only when a manifest sets
it, and its zero value must mean the behaviour of version 1.0, never a different one. Tests pin the
hash of a manifest that uses every key (`TestSurfaceHashPinned`) and fail when a zero-valued key is
written (`TestSurfaceZeroValuesAbsent`).

An author can compute all of them before publishing:

```
$ tote plugin check ./demo.tar.gz
...
hashes:
  archive sha256: eafea225c3cdd133335d0e2a0e75a9cb3f6bb04ee6d0fa4b0cd09b9d9aae64d5
  tree sha256: 9455857586bbaed292c2d3fd50f5bcf524ab68d8e88de66216d7feb45a1701cf
  surface sha256: e32982c86ba8e412322a178c92a085c077813fd4fd688e77f2b7697bae34203d
grant token: exec:git@e32982c86ba8
publish next to the release, as demo.tar.gz.sha256:
  eafea225c3cdd133335d0e2a0e75a9cb3f6bb04ee6d0fa4b0cd09b9d9aae64d5  demo.tar.gz
```

The `plugin.toml` of the `demo` plugin used in the examples:

```toml
api = 1
id = "demo"
version = "1.0.0"
tote = ">=1.0.0, <2"
title = "Demo"
description = "Show the last commits of a repository."
license = "MIT"
tier = "declarative"
permissions = ["exec:git"]

[[requires]]
bin = "git"
hint = "Install git"

[[command]]
id = "log"
label = "Show the last commits"
run = ["git", "log", "--oneline", "-n", "{count}"]
output = "pager"
[[command.field]]
id = "count"
type = "text"
label = "Count"
default = "5"
pattern = "^[0-9]{1,3}$"
```

### 5.3 What a pin proves

A checksum you copy from the page that hosts the archive proves integrity: the bytes you got are the
bytes on that page. It does not prove authenticity. If someone controls the page, they control the
checksum next to it. Take the checksum from a channel other than the download, for example a message
from the author, or a reviewed repository.

The `<asset>.sha256` file that an author may publish is for you to compare with a checksum obtained
elsewhere. tote never uses it as a pin.

A wrong pin is an integrity failure and nothing is installed. The pin is compared before anything is
parsed.

```
$ tote plugin install ./demo.tar.gz --sha256 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
tote: error: checksum mismatch: expected aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, got eafea225c3cdd133335d0e2a0e75a9cb3f6bb04ee6d0fa4b0cd09b9d9aae64d5. Nothing was installed.
$ echo $?
5
```

A release without `--sha256` is refused before any network access:

```
$ tote plugin install example/demo@1.0.0
tote: error: a remote plugin needs --sha256, the checksum published by its author
$ echo $?
2
```

The version in the manifest must also equal the tag of the release (`tag_mismatch`), so a release
cannot pass an old package off as a new one.

### 5.4 Fetching a release

In the plugin code, only `internal/plugin/fetch.go` imports `net/http`, and a test enforces it.
Network is used only by `install` and `update` of a release. Never at start-up, in `list`, `info`,
`check`, `enable`, `disable` or `remove`.

- The URL is parsed with `net/url`, never by slicing text. The scheme is `https`. The host is
  exactly `github.com`. There is no port, no user information, no query, no fragment and no `%`. The
  path has exactly six segments (`/<owner>/<repo>/releases/download/<tag>/<asset>`), each made of
  `A-Z a-z 0-9 . _ + -`, none of them `.` or `..`.
- At most 3 redirects. Each must be `https` on `github.com`, `objects.githubusercontent.com` or
  `release-assets.githubusercontent.com`, with the default port and no user information. On
  `github.com` the target must be the same tag and asset, with no query.
- A redirect to another owner or repository of `github.com` (a repository that was renamed or
  transferred) is `origin_redirect`, exit 5. There is no `--force` for it: install again from the
  new URL.
- No variable of the environment changes the list of hosts. The trust roots and the proxy do come
  from your environment (`SSL_CERT_FILE`, `SSL_CERT_DIR`, `HTTPS_PROXY`). The `--sha256` pin is what
  protects the bytes, whatever the network does.
- The request carries `User-Agent: tote/<version>` and nothing that identifies you. No
  `Authorization`, no cookie, so there is nothing to leak. A private release answers 401, which tote
  reports as "authentication required" (exit 3).
- Only status 200 is accepted. TLS 1.2 or later, the system roots, never `InsecureSkipVerify`. The
  proxy of your environment is used. A proxy can see where you go and can block it, and it cannot
  change the bytes without the hash failing.
- Compression is not decoded: the bytes that are hashed are the bytes of the asset.
- Limits: 16 MiB. A `Content-Length` over the limit stops the download before the body is read, and
  a counting reader stops one that lies. A body shorter than announced is `net_truncated`.
- Timeouts: 10 s to connect, 20 s for headers, 30 s without data, 5 minutes in total.
- The body is written to a file in the staging directory (mode 0600, created with `O_EXCL`) while it
  is hashed. The file is read again for extraction and its digest is compared a second time, so a
  file swapped in between does not pass for the pinned archive.

### 5.5 Extracting an archive and copying a directory

The same rules apply to an archive and to a directory source, and they are applied before anything
is created for an entry. Files are written through an `os.Root` opened on a fresh 0700 staging
directory, so a mistake in the checks still cannot write outside it.

- Format: one gzip stream, found by its magic bytes, holding one tar archive and nothing after it. A
  second stream, trailing bytes, a truncated stream and non-zero data after the end of the tar are
  errors.
- Types: regular files and directories only. A symbolic link, a hard link, a device, a pipe, a
  sparse file and a PAX header that sets a path are refused.
- Names: each segment matches `[A-Za-z0-9._][A-Za-z0-9._-]*` (no leading `-`), is not `.` or `..`,
  and the name has no absolute path, no backslash, no NUL. No duplicate, no two names that differ
  only in case, no file where a directory is.
- Creation: `O_CREATE|O_EXCL`, so an existing path is an error. Directories are 0700 and files 0600
  during the copy, and the tree is made read-only at commit. setuid, setgid and sticky never
  survive. Owners and dates of the archive are ignored.
- Limits, counted while streaming and never trusted from a header: 16 MiB for the archive, 16 MiB
  for the extracted files together and for one file, 512 entries (implied parent directories count),
  depth 6, a path of 255 bytes. The decompressed stream is capped as well, so a bomb ends early.
  `plugin.toml` is at most 256 KiB, a locale file 128 KiB.
- A directory source is opened through `os.Root`, every entry is looked at with `Lstat`, and the
  file that is opened must be the file that was listed. A pipe put in the place of a file cannot
  stall the copy.

The first violation aborts the whole install and names the entry. Archives exit 5, a directory
source exits 1.

```
$ tote plugin install ./parent.tar.gz        # an entry named ../evil
tote: error: unsafe entry in the archive: ../evil (parent path (..))
$ echo $?
5
$ tote plugin install ./bomb.tar.gz          # 41 KiB that expand to 40 MiB
tote: error: the archive exceeds the limits: big (too many bytes)
$ tote plugin install ./linkdir              # a symbolic link to /etc
tote: error: etc: symbolic link
```

### 5.6 Commit

What is installed is the copy that was hashed and that you agreed to, not what the source holds at
commit time. A source that changes after the consent changes nothing.

Under a lock on `<data>/plugins/.mutex` (10 seconds, then `busy`), the staged copy is hashed again
and then:

1. the staging directory is renamed to `<data>/plugins/<id>/<tree32>`. If that directory exists it
   is accepted only after its full hash is recomputed and equal (`tree_collision`, exit 5,
   otherwise);
2. one atomic write of the lock record switches the version, the tree, the capabilities, the surface
   and the consent;
3. the state and cache directories are created;
4. the previous tree is moved away and deleted.

A failure before step 2 leaves the previous version running. A crash after step 1 leaves a tree that
nothing runs and that a retry reuses. After step 2 the new version runs and the old tree waits for
the next cleanup.

### 5.7 Checking at every load and before every start

Every time tote lists plugins it checks each installed one. A plugin that fails is `broken` and
never runs.

- The directory of the id is a real directory owned by you, not writable by group or others, and so
  is the plugins directory above it. A directory of yours that others can write is `store_writable`,
  and the message names it and the command that fixes it, `chmod go-w`; `plugin remove` and
  `install` refuse with the same code until it is fixed.
- Every entry of the tree belongs to you. Directories are mode 0500, files 0400 and a program 0500,
  with no other bit. A different mode is `tree_perms`, a link is `tree_symlink`, and so on.
- The tree hash is recomputed (cached by the identity of every file, so a changed byte or mode is
  seen) and must equal the record. The manifest is parsed from the bytes that were hashed, not from
  a second read.
- The record's version and tier equal the manifest's. The consent covers the capabilities and the
  surface.

Real output after one byte of an installed `plugin.toml` was changed:

```
$ tote plugin list
demo	1.0.0	broken	local		false
$ tote plugin enable demo
tote: error: cannot enable demo: the files were changed on disk (expected 9455857586bbaed292c2d3fd50f5bcf5, found 383d40a43d1563cddc3d40cda9db9e69) and the plugin was not run; remove it and install it again
$ echo $?
5
```

Before every start of a program, the check that precedes it reads the lock and `config.toml` again
and computes the state from the files as they are at that moment, so a `disable` made in another
terminal is seen. The state is checked once more immediately before the program starts and once more
after it was recorded in `plugin-procs`. The program found on the path must be the same file (device
and inode) as the one that check found, else the run is refused as `stale`.

This is tamper detection: a sync tool, a backup restore, a program with fewer rights, a disk fault.
It is not protection against another process of your own account that races the check (section 12).

## 6. Running a plugin

### 6.1 The environment

The environment of a program is built from nothing.

- Copied when set: `USER`, `LOGNAME`, `LANG`, `LANGUAGE`, the `LC_*` family, `TERM`, `COLORTERM`,
  `NO_COLOR`, `TZ`, `TMPDIR`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`,
  `XDG_CACHE_HOME`. `HOME` is set. `PATH` is copied after it is cleaned (below).
- The proxy variables (`HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY`, `ALL_PROXY`, either case) only when
  the plugin holds a `net:` permission.
- `TOTE_CONFIG_DIR`, `TOTE_DATA_DIR`, `TOTE_STATE_DIR` and `TOTE_CACHE_DIR` when set, so that a
  program that calls `tote repo list --json` reads the same store.
- Set by tote: `TOTE_API`, `TOTE_HOST_VERSION`, `TOTE_LANG`, `TOTE_BIN`, `TOTE_PLUGIN_ID`,
  `TOTE_PLUGIN_DIR`, `TOTE_PLUGIN_STATE`, `TOTE_PLUGIN_CACHE`, `PYTHONDONTWRITEBYTECODE=1`, and
  `GIT_TERMINAL_PROMPT=0` for captured runs.
- Each `env:<NAME>` permission, when the variable is set, then the `env` table of the command.

Not passed: `SSH_AUTH_SOCK`, `XDG_RUNTIME_DIR` (it holds agent sockets and the hand-off file of the
shell integration), `TOTE_HANDOFF`, tokens and everything the plugin did not declare.

The names that no manifest may request or set are refused: `PATH`, `HOME`, `SHELL`, `IFS`,
`BASH_ENV`, the loader variables (`LD_*`, `DYLD_*`), every `TOTE_*` name, the interpreter variables
(`PYTHON*`, `PERL5*`, `RUBY*`, `NODE_*`), the variables through which git, ssh, a pager or an editor
starts a program (`GIT_SSH*`, `GIT_CONFIG*`, `GIT_EXEC_PATH`, `GIT_PAGER`, `PAGER`, `EDITOR`,
`LESS*`, ...). The list cannot be complete. The consent screen prints the `env` table with its
values, and that is the real gate.

```
$ tote plugin check ./envt        # [command.env] with GIT_PAGER = "id"
  error: command[0].env.GIT_PAGER, line 16: the environment variable GIT_PAGER may not be passed to a plugin
```

`env:SSH_AUTH_SOCK` is allowed and is marked as sensitive at consent, as is any name containing
`TOKEN`, `SECRET`, `PASSWORD`, `PASSWD`, `CREDENTIAL`, `API_KEY` or `PRIVATE`, and the prefixes
`AWS_`, `AZURE_`, `GOOGLE_`, `GH_`, `GITHUB_`, `GITLAB_`.

### 6.2 Finding the program

The name at the start of a command is resolved on your `PATH` after it is cleaned: empty and
relative entries are dropped, and so is the plugins directory with everything under it, the working
directory, and the directory the command runs in. A repository that carries a file called `git`, or
a plugin that tried to plant one, is not found. The resolved file must not be the tote binary.

### 6.3 Arguments

- Each argument is built from one template element, never through a shell. At most 64 arguments of
  1024 bytes.
- A value that starts with `-` is refused when its template does not start with `-`, unless the
  placeholder is named in `allow_dash`, or it is a `choice` field or the flag of a `bool` field. A
  value can therefore not become an option by accident or by attack.

```
$ tote plugin check ./dash --argv --set rev=--output=/tmp/x
  - command show: git show '{rev}'
    cannot be built: the value of rev starts with a dash and the command does not allow it
```

- Text that substitution produces must be valid UTF-8, with no control, bidirectional or zero-width
  character, no tab, and within the limit (4096 bytes for a value).
- A `pattern` must be anchored (`^` and `$` on every alternative), because it is matched against the
  whole value. An unanchored pattern is an error when the manifest is read.
- A command that starts `ssh`, `scp`, `rsync` or `sftp` must put a literal `--` before any
  placeholder.
- A repository's remote has the user information of an `https`, `ssh` or `git` URL removed. The user
  of an scp-like address (`git@github.com:example/demo.git`) is kept. The remote is empty unless it
  is one of those forms. `ext::`, `file:` and the `<transport>::<address>` form are dropped.
- The repository path is saved data that tote expands. A hostile repository can carry its own git
  configuration: tote derives `{repo.remote}` and `{repo.branch}` with `git config --local` and
  `core.fsmonitor=false`, from a clean environment, in at most 1.5 seconds.
- An `open` template must produce an `https` address with a host, no user information, no space or
  control character, at most 2048 bytes. When the template names the host literally, the result must
  have exactly that host, so that no value can move the link. The consent line says that you confirm
  each address. That confirmation is made by the screen that runs the entry, not by the plugin
  layer, which checks the address only.
- A command with `confirm` set is refused (`confirm_needed`) until the screen that runs it reports
  that you confirmed it. The question shows the exact command and directory, with focus on Cancel.

### 6.4 Limits of a run

- At most 4 plugin processes at a time in one tote process. A fifth is refused at once (`busy`).
  Each tote process counts its own runs.
- A captured run (pager or silent) has no terminal and no standard input, and runs in a session of
  its own. Its timeout is the `timeout` key, or 5 minutes. The pager keeps 8 MiB. More than 64 MiB
  of output stops the run (`output_limit`).
- A captured run ends when the program exits. What it left behind may hold the pipe until it has
  been silent for 250 ms (5 seconds at most), and is then cut off. Timeout, cancel, `disable` and
  `remove` send SIGTERM to the process group and SIGKILL 2 seconds later. If tote itself is killed
  with SIGKILL, the process group of a run keeps going: it is stopped by `tote plugin disable` or
  `remove`, which read `plugin-procs`.
- A `terminal` run gives the program your terminal and has no timeout. It belongs to the plugin
  until it exits.

## 7. Strings that come from a plugin

Everything a plugin writes is data. It is cleaned when it is read, cut to a limit, and shown with
its origin.

- Manifest strings, translations, hints and the arguments of every error pass through a cleaner:
  invalid UTF-8 becomes U+FFFD, and every control character, escape, C1 character, bidirectional
  control, zero-width and tag character becomes `?`. A line feed in a one-line field becomes `?`.
- The consent screen is generated by tote. Text from the plugin sits in a labelled box ("written by
  the plugin, not checked by tote"), or is tagged as suggested by the plugin. A hint is never
  executed.
- Program output shown in the pager passes a filter. Only the colour sequences of a short list
  survive (reset, bold, faint, italic, underline, and the 16 foreground colours). Blink, reverse,
  conceal, backgrounds and the 256-colour and truecolor forms are dropped, because they can hide
  text or paint over the frame. A colour sequence with any other parameter is removed whole. Every
  other escape sequence and control character becomes `?`. A reset ends the text so that a colour
  cannot leak. Under the mono theme no colour is kept.
- Secrets are masked in program output: the exact values of the `env:` variables handed over (4
  bytes or more) and of the `env` table (8 bytes or more), the well-known shapes of credentials
  (tokens of code hosts, chat services and cloud providers, JWTs, bearer values, user information in
  URLs) and whole private key blocks. The shapes are a best effort. Do not rely on masking.
- A `terminal` run is not filtered. The program owns the tty.

## 8. What the host writes, and the kill switch

### 8.1 Where

| File or directory                                                                    | Written by                                                      |
| ------------------------------------------------------------------------------------ | --------------------------------------------------------------- |
| `<data>/plugins/<id>/<tree32>/` (read-only tree)                                     | the host, at install                                            |
| `<state>/plugins.lock` (installs, consent, kill switch)                              | the host                                                        |
| `<config>/config.toml` (`[plugins.<id>] enabled`)                                    | you, and the host for `enable` and `disable`                    |
| `<config>/plugins/<id>.toml` (settings)                                              | you, and the terminal interface when you change a setting there |
| `<state>/plugin-procs` (running process groups)                                      | the host                                                        |
| `<state>/plugins-host/<id>/`                                                         | the host only                                                   |
| `<state>/plugins/<id>/` and `<cache>/plugins/<id>/` (own state and cache, mode 0700) | the programs of the plugin                                      |

The host never writes into the two directories that a plugin's programs write to, and it never
follows a link it finds there. A file that is the host's own goes in `plugins-host/<id>`, a sibling
that a plugin cannot turn into a link. The host files are opened without following links, must be
private regular files of yours with one link, and the directory is checked on every write. Settings
files are treated as hostile input: a value that does not fit its declared type is ignored with a
visible warning.

The system does not enforce this. A program of your own account can write anywhere you can. The rule
protects the host from a plugin that plants a link, not you from a plugin that edits your files.

### 8.2 Disable and remove

`disable` is the kill switch and does not depend on the layout of any file you edit.

1. It sets a flag in the lock record, in one atomic write.
2. It writes `enabled = false` for the plugin in `config.toml`. If you wrote that table in a layout
   tote does not edit (dotted keys, an inline table), the command still succeeds, tells you, and the
   flag alone keeps the plugin off.
3. It sends SIGTERM and then SIGKILL to the process groups recorded in `plugin-procs`, whichever
   tote started them, and to every process of those sessions on Linux.

It keeps the files, the settings and the consent. `--no-plugins` (or `TOTE_NO_PLUGINS=1`) turns
every plugin off for one run.

`remove` checks the id against the grammar before any file is touched (`../data` is a usage error),
refuses when there is no terminal and no `--yes`, stops the processes of the plugin, deletes the
lock record, and then removes the tree with walks that never follow links. A failure leaves the rest
for the next cleanup, and says so.

```
$ tote plugin remove ../data --yes
tote: error: invalid plugin id "../data": use lowercase letters, digits and dashes, 2 to 32 characters
$ echo $?
2
```

A descendant that called `setsid` again leaves the recorded session and survives. tote does not
track it. Stopping is best effort.

### 8.3 A plugin cannot call the verbs that change plugins

`install`, `update`, `enable`, `disable` and `remove` refuse to run when `TOTE_PLUGIN_ID` is set, or
when the calling process is in a session that `plugin-procs` records. The second test survives
removing the variable.

```
$ TOTE_PLUGIN_ID=other tote plugin install ./demo --grant 'exec:git@e32982c86ba8'
tote: error: refused: this command changes tote and was started by a plugin
$ echo $?
4
```

A descendant that called `setsid`, and a `terminal` run (which shares tote's own session) escape the
second test. This guard stops honest and careless plugins, and accidents of a script. It is not a
boundary.

## 9. What `plugin check` and `--dry-run` execute

Nothing the plugin names. `check`, `info`, `list`, `install --dry-run`, `enable` and `disable` read
files, hash them and look programs up on the path with a stat. The `hint` of a missing program is
text.

The only program tote starts on its own during these verbs is `git`, to derive `{repo.remote}`,
`{repo.slug}` and `{repo.branch}` of a saved repository when `check --argv` or a command needs them.
It is read-only, as described in section 6.3. In the plugin code, programs are started in two files
only, `run.go` (a prepared run, and the platform opener) and `git.go` (this helper), and a test
enforces it.

Checked with a program on the path that writes a marker file when it runs, and a plugin that
requires it and lists it in `exec:`:

```
$ tote plugin check ./noexec --argv
$ tote plugin install ./noexec --dry-run
$ tote plugin install ./noexec --grant exec:marker@...
$ tote plugin disable noexec
$ tote plugin enable noexec
$ ls MARKER.txt
ls: cannot access 'MARKER.txt': No such file or directory
```

A directory given to `check` is copied by the rules of section 5.5 into a private temporary
directory, examined there and deleted before the command returns.

## 10. Not in 1.0

These are reserved. The 1.0 documentation never describes them as working.

- **The exec tier** (`tier = "exec"`, a plugin with its own program that speaks JSON lines). It is
  not available in 1.0, so no program of a plugin ever starts. What the code does:
  - a manifest with `tier = "exec"` is refused with `reserved_not_available` on the key `tier`, as
    an incompatibility, at the one place where the reader decides the tier: `plugin check`, a local,
    archive or release `install` and an `update` all go through it, so nothing remains in staging,
    in the store or in the lock, and nothing is consented to (the package is copied into the private
    staging area to be read, and a release is downloaded, before the refusal);
  - a plugin of that tier that is in the store already (an earlier tote installed it) is read the
    same way: it is `incompatible` with that reason, `enable` refuses it and no command of it is
    offered or run;
  - the features `exec.list` and `exec.run` are not announced, so a manifest that needs one is
    incompatible (`feature_missing`).

  The tier comes back in a later version behind the same switch; nothing here promises a date.

- **`plugin run`, `plugin search`, a registry, revocation, an audit log and `plugin verify`.** The
  verbs are unknown and exit 2 (`unknown subcommand`). There is no `revoked` state: a malicious
  plugin you installed cannot be recalled remotely. `disable` and `remove` are the recall.
- **Secrets for plugins.** `env:TOTE_TOKEN` is refused (`reserved_not_available`). There is no
  secret field type.
- **Reserved permissions**: `host:read:connections`, `host:handoff`, `host:clipboard`,
  `host:notify`, `host:editor` (`reserved_not_available`). **Never grantable in API 1**:
  `hook:intercept`, `host:write:settings` (`perm_refused`).
- **Reserved tables and keys**: `widget`, `panel`, `decorator`, `provider`, `connector`, `hook`,
  `job`, `screen`, `import`, `badge`, `search`, and the field types and events listed in the
  reference (`reserved_not_available`).
- **Kernel isolation.** No namespace, no seccomp, no Landlock. tote does not call `prctl` to make
  itself non-dumpable.
- **Packages other than gft.** Only `gft` is shipped.

## 11. Attack, defence, where it is tested

Test names are in `internal/plugin` unless a path is given. Table-driven tests list their cases as
subtests (`-run 'TestName/case'`).

| Attack                                                                              | Defence                                                                                                                                                                                                    | Tested by                                                                                                                                                                     |
| ----------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Entry `../evil`, `/etc/x`, `a\b` in an archive                                      | names checked, writes through `os.Root`                                                                                                                                                                    | `TestHostileArchives` (the cases of parent, absolute and backslash names), `TestEntryNames`                                                                                   |
| Symlink, hard link, device or pipe in an archive                                    | refused by type                                                                                                                                                                                            | `TestHostileArchives` (the link, device and pipe cases), `TestInstallReleaseHostileArchives`                                                                                  |
| Symlink or pipe in a directory source                                               | `Lstat` of each entry, file identity checked at copy                                                                                                                                                       | `TestInstallHostileSource`, `TestSecReviewHostileSources`                                                                                                                     |
| Decompression bomb, huge entry, too many entries, depth, long path                  | limits counted while streaming                                                                                                                                                                             | `TestUnpackBomb`, `TestHostileArchives` (the limit cases), `TestUnpackOneByteOver`                                                                                            |
| Second gzip stream, trailing data, truncation                                       | one stream and one tar, nothing after                                                                                                                                                                      | `TestHostileArchives` (the second-stream case)                                                                                                                                |
| Download larger than allowed, or lying about its size                               | `Content-Length` and counting reader                                                                                                                                                                       | `TestDownloadSizeCap`, `TestDownloadLengthLies`                                                                                                                               |
| Redirect to another host, scheme, port or user information                          | host allow-list, `https` only                                                                                                                                                                              | `TestDownloadRedirectRules`, `TestDownloadRedirectHosts`, `TestDownloadRedirectLimit`                                                                                         |
| Redirect to another repository of the same host                                     | `origin_redirect`, exit 5                                                                                                                                                                                  | `TestDownloadRedirectRules` (to another owner, to another repository)                                                                                                         |
| Bytes that are not the ones you pinned                                              | sha256 compared before any parse, and again after the file is re-read                                                                                                                                      | `TestInstallReleaseWrongPinIsCheckedFirst`, `TestInstallReleaseNeedsThePin`, `TestInstallReleaseSwappedDownload`                                                              |
| A hash typed for a thing that has no archive                                        | refused (`hash_unexpected`)                                                                                                                                                                                | `TestInstallPinIsRefusedForADirectory`                                                                                                                                        |
| A release whose manifest version is not its tag                                     | `tag_mismatch`                                                                                                                                                                                             | `TestInstallReleaseTagMismatch`                                                                                                                                               |
| One byte of an installed file changed, a file added, a mode changed, a link planted | tree hash and modes checked at every load; state `broken`                                                                                                                                                  | `TestTamper`, `TestCacheSeesTamper`                                                                                                                                           |
| Staged copy altered between consent and commit                                      | hashed again under the lock                                                                                                                                                                                | `TestInstallTamperedStaging`                                                                                                                                                  |
| Source changed after the consent                                                    | the copy is installed, not the source                                                                                                                                                                      | `TestInstallIsTheCopy`                                                                                                                                                        |
| Update that asks for more access                                                    | escalation rule, grant token                                                                                                                                                                               | `TestUpdateEscalationMatrix`, `TestUpdateReleaseEscalation`, `TestEscalationMatrix`, `TestPluginExitCodes` (in `cmd/tote`)                                                    |
| Update that removes a `confirm`, adds `allow_dash` or `env`                         | surface escalation                                                                                                                                                                                         | `TestSurfaceEscalations`, `TestUpdateReleaseEscalation`                                                                                                                       |
| Update from another source, older, same version, pre-release                        | refused; with `--force` consent is still asked                                                                                                                                                             | `TestUpdateRules`, `TestUpdateSourceChanged`, `TestUpdateReleaseRules`, `TestForcedUpdateAsksForConsent`                                                                      |
| The same plugin installed again                                                     | nothing is asked and nothing is written; any difference (version, source, files) keeps the update rules                                                                                                    | `TestInstallIdempotent`, `TestPluginInstallIdempotent` (`cmd/tote`)                                                                                                           |
| A plugin of the exec tier, new or already in the store                              | refused as incompatible (`reserved_not_available`), never enabled or run                                                                                                                                   | `TestExecTierRefused`, `TestExecTierRefusedEverywhere`, `TestPluginExecTierRefused` (`cmd/tote`)                                                                              |
| A script that agrees for you                                                        | no `--yes`; `--grant` must equal the token                                                                                                                                                                 | `TestInstallConsent`, `TestPluginEnableConsent` (in `cmd/tote`)                                                                                                               |
| A plugin that installs or removes plugins                                           | verbs refused from a plugin session                                                                                                                                                                        | `TestReviewMutatingVerbsRefuseFromAPluginSession`, `TestPluginFromPlugin` (in `cmd/tote`)                                                                                     |
| A plugin that runs a program it did not declare, tote or a shell                    | covered by `exec:`, never tote, never a launcher                                                                                                                                                           | `TestProgramChecks`, `TestShellNames`                                                                                                                                         |
| An id that passes for tote or for an official plugin                                | reserved verbs, words, prefix and official ids; not applied to a plugin that is already installed, so a later reservation cannot break it                                                                  | `TestReservedID`, `TestReservationKeepsInstalled`, `TestInstallOfficialIDOnlyBundled`, `TestPluginReservedIDs` (in `cmd/tote`)                                                |
| A value that becomes an option, or an injected character                            | dash rule, anchored patterns, produced-text rules                                                                                                                                                          | `TestExpandArgv`, `TestExpandHostileValues`, `TestPrepareAllowDash`                                                                                                           |
| Environment leak (agent socket, tokens, loader variables, hand-off file)            | built from nothing; blocked names                                                                                                                                                                          | `TestRunIsolation`, `TestEnvironmentFromNothing`, `TestSecReviewEnvTableCodeExecution`, `TestSecReviewCodeEnvNamesRefused`, `TestReviewEnvTableBlockedNames`                  |
| A program planted in the plugins directory or the working directory                 | lookup refuses those places                                                                                                                                                                                | `TestScrubPath`, `TestLookBinName`                                                                                                                                            |
| Escape sequences in program output or in a manifest                                 | pager filter, cleaner                                                                                                                                                                                      | `TestFilterEscapeInjection`, `TestRunEscapes`, `TestCleanLine`                                                                                                                |
| Credential in program output                                                        | exact-value and shape masking                                                                                                                                                                              | `TestRedactor`, `TestRunRedacts`                                                                                                                                              |
| A plugin plants links in its own directories to make the host write elsewhere       | host files in `plugins-host`, no link followed                                                                                                                                                             | `TestHostWritesNothingInPluginDirectories`, `TestWriteHostFileRefuses`                                                                                                        |
| `remove ../data`, a linked id directory, a linked plugins directory                 | id grammar, `Lstat` walks                                                                                                                                                                                  | `TestRemoveBadIDs`, `TestRemoveSymlinkedID`, `TestRemoveTreeHostileLayouts`, `TestRemoveReleaseNeverFollowsLinks`                                                             |
| A disabled plugin that keeps running                                                | state rechecked before every start; process groups stopped                                                                                                                                                 | `TestRunsOnlyWhenEveryConditionHolds`, `TestRunStateIsRechecked`, `TestKillSwitchSurvivesAnyConfigLayout`, `TestRemoveStopsRunningPlugin`, `TestRemoveSetsTheKillSwitchFirst` |
| A corrupt or forged lock or `config.toml`                                           | the lock is parsed strictly and fails closed; `config.toml` fails closed for a syntax error or another schema, an unknown key is a warning, and a plugin switch that is not a boolean turns the plugin off | `TestFailClosed`, `TestLockFailsClosed`, `TestSecReviewLockForgeryFailsClosed`                                                                                                |
| A hostile or huge manifest                                                          | structure limits before the TOML library runs                                                                                                                                                              | `TestHostileManifests`, `TestShapeIssues`                                                                                                                                     |
| A program shipped in a declarative package                                          | extra files refused                                                                                                                                                                                        | `TestCheckPackageDeclarative`                                                                                                                                                 |
| An `open` address that leaves the declared host or the scheme                       | checked address, fixed literal host                                                                                                                                                                        | `TestOpenDefenses`, `TestLiteralHost`                                                                                                                                         |
| Output flood                                                                        | 8 MiB in the pager, 64 MiB stops the run                                                                                                                                                                   | `TestRunBigOutput`                                                                                                                                                            |
| `check` that starts a program                                                       | nothing is started                                                                                                                                                                                         | `TestCheckRunsNothing`                                                                                                                                                        |
| A crash or an I/O error in the middle of an install                                 | atomic switch, tree reuse                                                                                                                                                                                  | `TestInstallCrashAtEveryPoint`, `TestInstallFailureAtEveryPoint`, `TestCommitTreeCollision`                                                                                   |
| A bundled plugin that no longer matches its pin                                     | pin test in the build                                                                                                                                                                                      | `TestBundledPins` (in `internal/plugin/bundled`)                                                                                                                              |
| Network code outside `fetch.go`                                                     | import rule                                                                                                                                                                                                | `TestLayoutPluginImports`                                                                                                                                                     |
| Reserved verbs such as `plugin run`                                                 | unknown subcommand, exit 2                                                                                                                                                                                 | `TestPluginRunIsReserved` (in `cmd/tote`)                                                                                                                                     |

To run them:

```
go test -p 1 ./internal/plugin ./internal/plugin/bundled ./cmd/tote
```

## 12. Limits, stated plainly

1. A declarative plugin makes tote run programs with your rights, with the argument templates shown
   at consent. If a listed program can run other programs, the plugin can too. The consent screen is
   the control. The lists of launchers and risky options are advisory.
2. `net:` and `fs:` permissions are declared only. tote does not stop a listed program from reaching
   the network or any file you can reach.
3. A changed command under an unchanged set of permissions does not ask again, new commands
   included, apart from the cases of section 4.5. An update is compared with the version before it.
4. A checksum proves the bytes, not the author. A directory or a local archive without a pin is not
   verified. The bundled plugin is as trustworthy as the tote binary you run.
5. Consent, the lock and the hashes detect tampering by accident, by a sync tool, or by a process
   with fewer rights. Another process of your own account can edit the same files, and a person who
   edits the lock has granted what they wrote.
6. A `terminal` run gives the program your terminal, with no filtering and no timeout.
7. The kill switch stops the recorded process groups and the processes of their sessions. A
   descendant that calls `setsid` again survives. The guard against plugin-initiated installs has
   the same hole.
8. No program run by a plugin is isolated from tote or from your account. tote does not hide its own
   environment from its children.
9. There is no revocation and no remote recall in 1.0.
10. The masking of secrets in output is a best effort.

If you find a way around a protection listed as enforced, it is a bug in tote.
