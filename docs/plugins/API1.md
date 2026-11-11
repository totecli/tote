# tote plugin API 1: reference

This is the reference for plugin API 1, as tote 1.0 implements it. It lists every key of
`plugin.toml`, every limit, every verb of `tote plugin`, every exit code and every JSON shape.

It is a reference, not a tutorial. To write a plugin step by step, read
[AUTHORING.md](AUTHORING.md). For what a plugin can and cannot do to you, and how tote defends
against it, read [SECURITY.md](SECURITY.md). The manifest is also described by a JSON Schema,
[plugin.schema.json](plugin.schema.json) (section 3.14). What stays the same in every 1.x release,
for the files, the JSON, the TSV and the exit codes as well as the manifest, is written in
[../compat.md](../compat.md).

Every command output below was produced by a real build of tote in a scratch home directory. Paths
are shortened to `/home/u/...`. A line that is cut says `...`. The `demo` plugin of this reference
is its own: the `demo` of [AUTHORING.md](AUTHORING.md) and of [SECURITY.md](SECURITY.md) are other
plugins, so their hashes and grant tokens differ.

Contents:

1. What a plugin is
2. Package and disk layout
3. The manifest
4. How a contribution runs
5. Plugin states
6. Consent
7. Install, update, enable, disable, remove
8. The command line
9. The Plugins screen
10. Not in 1.0
11. A complete example

## 1. What a plugin is

A plugin is a directory with a `plugin.toml` that tells tote which programs to run, with which
arguments, and what to show. tote 1.0 runs one tier: **declarative** (`tier = "declarative"`). A
declarative plugin has no program of its own. tote runs the programs it names (`git`, `curl`, `gft`)
with your user rights.

Three rules hold for every plugin:

- **No shell.** There is no `shell = true`. A value is substituted into one argument of a program,
  never into a command line.
- **Nothing runs before consent.** Installing, updating, listing, checking and reading a plugin
  execute no plugin code and no program a plugin names.
- **A plugin is never "safe because declarative".** The programs it names can themselves run other
  programs. The consent view prints every command in full for that reason.

The words **enforced** and **declared** appear throughout:

- **Enforced**: tote itself refuses what is not allowed. A plugin cannot cross it.
- **Declared**: the plugin states it and tote shows it at consent, but tote cannot stop a program
  that runs with your rights.

## 2. Package and disk layout

### 2.1 The package

A declarative package holds only these files:

| File                                 | Rule                                                               |
| ------------------------------------ | ------------------------------------------------------------------ |
| `plugin.toml`                        | Required, at the root. Regular file, UTF-8, TOML, at most 256 KiB. |
| `locales/en.toml`, `locales/fr.toml` | Optional. At most 128 KiB each. Only `en` and `fr`.                |
| `README*`, `LICENSE*`                | Optional, at the root.                                             |

Any other file is an error, `declarative_extra_file`, so that no program travels under the
declarative label.

A package is read as a directory or as a `.tar.gz` archive. The archive holds the package at its
root, with no folder around it. tote enforces these limits on a directory and on an archive:

- at most 16 MiB in all, 512 entries, paths of at most 6 segments (so at most 5 folders above a
  file) and paths of 255 bytes;
- regular files and folders only: no symbolic link, hard link, device or special file;
- each path segment matches `[A-Za-z0-9._][A-Za-z0-9._-]*` (it does not start with `-`), is not `.`
  or `..`, has no two names that differ only by case, and is not a duplicate;
- an archive is one gzip stream with one tar archive and nothing after it.

### 2.2 Hashes

tote computes three hashes. They are written in lower case hexadecimal.

| Hash             | What it covers                                                                                                                                                                                                                                                                                     |
| ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `tree_sha256`    | The files of the package. For each regular file in byte order of its path: path, size, content hash and whether it has the execute bit. Folders, times and owners do not count.                                                                                                                    |
| `surface_sha256` | What the plugin runs: for each contribution, its table, id, target, `run`, `open`, `env`, `cwd`, `allow_dash`, `output`, `pause`, `timeout`, `confirm` and the id, type, flag, pattern, choices and `required` of its fields. Labels, descriptions, keywords, hints and translations do not count. |
| `archive_sha256` | The bytes of a release or archive file. It is the value of `--sha256`.                                                                                                                                                                                                                             |

**A key of the surface counts only when it is set.** Each contribution is hashed as an object with
its `table` and `id`, and every other key is left out of it at its zero value (an empty string, an
empty list or table, `false`); a field is hashed with its `id` and `type`, and the same rule for the
rest. A later release that reads a new manifest key therefore keeps the `surface_sha256` of every
installed plugin that does not set it, and nobody has to consent again. The same rule binds that
later key: it counts in the hash only when a manifest sets it, and its zero value must mean exactly
what the plugin did before the key existed.

The tree hash of a package does not depend on how it was packed: a directory and an archive of the
same files give the same `tree_sha256`.

### 2.3 Where tote keeps things

The four base directories come from `TOTE_CONFIG_DIR`, `TOTE_DATA_DIR`, `TOTE_STATE_DIR` and
`TOTE_CACHE_DIR`, then from `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME` and
`XDG_CACHE_HOME`, then from the defaults under the home directory. `tote plugin info <id>` prints
the real paths:

```text
paths:
  installed files: /home/u/.local/share/tote/plugins/demo/01ddb77874bb4551a9bf53262545d7f8
  settings file: /home/u/.config/tote/plugins/demo.toml
  state directory: /home/u/.local/state/tote/plugins/demo
  cache directory: /home/u/.cache/tote/plugins/demo
  host directory: /home/u/.local/state/tote/plugins-host/demo
```

| Path                                           | Content                                                                                                                                                                           | Written by                                                      |
| ---------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------- |
| `<Config>/config.toml`, table `[plugins.<id>]` | `enabled = false` while the plugin is off                                                                                                                                         | tote and you                                                    |
| `<Config>/plugins/<id>.toml`                   | The values of the settings (section 3.11)                                                                                                                                         | you, and the terminal interface when you change a setting there |
| `<Data>/plugins/<id>/<tree32>/`                | One verified tree. `<tree32>` is the first 32 digits of `tree_sha256`.                                                                                                            | tote                                                            |
| `<Data>/plugins/.staging/`, `.mutex`           | An install in progress, and the lock that serialises changes                                                                                                                      | tote                                                            |
| `<State>/plugins.lock`                         | One TOML record per installed plugin                                                                                                                                              | tote                                                            |
| `<State>/plugin-procs`                         | The process groups of plugin runs that are alive, one line `<id> <session> <start>` each. A line with more fields is read for its first three, so a later release may append one. | tote                                                            |
| `<State>/plugins-host/<id>/`                   | Files that tote keeps for a plugin                                                                                                                                                | tote                                                            |
| `<State>/plugins/<id>/`                        | The state directory of a plugin                                                                                                                                                   | the plugin                                                      |
| `<Cache>/plugins/<id>/`                        | The cache directory of a plugin, disposable                                                                                                                                       | the plugin                                                      |

Directories are mode 0700 and files 0600. An installed tree is read-only: folders 0500, files 0400.
tote never writes into a directory that a plugin can write to.

The record of an installed plugin in `plugins.lock` looks like this (hashes and times shortened):

```toml
schema = 1

[[plugin]]
id = "demo"
version = "1.1.1"
tier = "declarative"
kind = "local"
source = "/home/u/work/demo"
origin = ""
archive_sha256 = "..."
tree_sha256 = "..."
surface_sha256 = "..."
installed_at = "2026-11-08T14:23:32Z"
updated_at = "2026-11-08T14:23:32Z"
caps = ["exec:date", "exec:echo", "exec:git", "fs:read:repo", "host:read:repos"]
consented_at = "2026-11-08T14:23:32Z"
consented_by = "grant"
disabled = false
failures = 0
last_failure_at = ""
last_reason = ""
quarantined = false
```

- `kind` is `release`, `local` or `bundled`.
- `origin` is the lower case `owner/repo` of a release, otherwise empty.
- `caps` are the capabilities you consented to (section 3.5), sorted.
- `consented_by` is `prompt`, `grant`, or empty when you never consented.
- A lock that does not parse, or has another `schema`, fails closed: no plugin runs, and
  `plugin list` says why. `config.toml` fails closed for a syntax error or another `schema` only. An
  unknown key or an invalid value in it is a warning and keeps the default of that setting. In
  `[plugins.<id>]`, an `enabled` that is not a boolean turns that plugin off, and any other key is
  ignored with a warning: the data of a plugin that a later release adds goes to `plugins.lock` or
  to `plugins/<id>.toml`, never there.

## 3. The manifest

### 3.1 Reading rules

tote reads a manifest in three steps.

1. **Size, encoding and structure.** At most 256 KiB, valid UTF-8, valid TOML, and bounded structure
   (a line of at most 70 KiB, nesting of at most 4 levels, keys of at most 8 parts, at most 4096
   table headers and 16384 keys). A failure here is `manifest_too_large`, `manifest_syntax` or
   `limit_exceeded`.
2. **Probe.** tote reads `api`, `tote`, `tier`, `requires_features`, `platforms` and `[[platform]]`
   leniently. A manifest that fails the probe is **incompatible** (section 3.4), and tote goes no
   further, so an older tote never calls a newer key unknown.
3. **Strict read.** Every unknown key is an error, and all of them are listed. A reserved table,
   key, type or permission is an error `reserved_not_available`. Every limit and every rule below is
   checked.

Errors carry the key path (`command[0].run[1]`) and the line and column. A manifest with errors is
never partly used.

Limits of the manifest:

| What                                                                         | Limit                                                    |
| ---------------------------------------------------------------------------- | -------------------------------------------------------- |
| Contributions in all (`command`, `item_action`, `action`, `list`, `setting`) | 64                                                       |
| Form fields per contribution                                                 | 16                                                       |
| `permissions`                                                                | 64                                                       |
| `[[requires]]`                                                               | 16                                                       |
| `requires_features`                                                          | 32                                                       |
| Arguments of a `run`                                                         | 64, of 1024 bytes each, checked again after substitution |
| `label`                                                                      | 60 runes                                                 |
| `title`                                                                      | 40 runes                                                 |
| `description`, `hint`, `confirm` text                                        | 200 runes                                                |
| `keywords`                                                                   | 8 words of 24 runes                                      |
| Static `choices`                                                             | 64 values of 64 runes                                    |
| `pattern`                                                                    | 256 bytes                                                |
| `homepage`                                                                   | 256 bytes                                                |
| `license`                                                                    | 128 bytes                                                |
| A text in a locale                                                           | 1024 bytes                                               |

Text values (`title`, `description`, `label`, `hint`, `keywords`, and the like) hold no control
character, no line or paragraph separator and no format character except the two joiners.

### 3.2 Top-level keys

| Key                                  | Type             | Required | Default               | Rule                                                                                                                                          |
| ------------------------------------ | ---------------- | -------- | --------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `api`                                | integer          | yes      |                       | `1`. Another value is incompatible.                                                                                                           |
| `id`                                 | string           | yes      |                       | Section 3.3. An empty id is `id_invalid`.                                                                                                     |
| `version`                            | string           | yes      |                       | Strict Semantic Version without `v`, at most 64 bytes. It equals the release tag. An empty version is `version_invalid`.                      |
| `tote`                               | string           | yes      |                       | Range of tote versions, section 3.4.                                                                                                          |
| `tier`                               | string           | yes      |                       | `declarative`. `exec` is reserved: a manifest that says `tier = "exec"` is incompatible with tote 1.0 (`reserved_not_available`), section 10. |
| `description`                        | string           | yes      |                       | One sentence, 1 to 200 runes.                                                                                                                 |
| `license`                            | string           | yes      |                       | SPDX-shaped: `^[A-Za-z0-9.+-]+( (AND\|OR\|WITH) [A-Za-z0-9.+-]+)*$`, at most 128 bytes.                                                       |
| `title`                              | string           | no       | the id                | Display text, 1 to 40 runes. Menus show the id first.                                                                                         |
| `homepage`                           | string           | no       | none                  | An `https` URL without user information, at most 256 bytes.                                                                                   |
| `requires_features`                  | array of strings | no       | `[]`                  | Section 3.4.                                                                                                                                  |
| `platforms`                          | array of strings | no       | `["linux", "darwin"]` | A subset of `linux` and `darwin`, no duplicate. There is no Windows.                                                                          |
| `permissions`                        | array of strings | no       | `[]`                  | Section 3.5. No duplicate.                                                                                                                    |
| `requires`                           | array of tables  | no       | none                  | `[[requires]]`, section 3.6.                                                                                                                  |
| `command`, `item_action`, `setting`  | arrays of tables | no       | none                  | Sections 3.8 to 3.11.                                                                                                                         |
| `i18n`                               | table            | no       | none                  | Section 3.12.                                                                                                                                 |
| `exec`, `platform`, `action`, `list` | tables           | no       | none                  | For the exec tier, which tote 1.0 does not have (section 10). In a declarative manifest they are an error.                                    |

A declarative manifest with `[exec]`, `[[platform]]`, `[[action]]` or `[[list]]` is an error,
`tier_mismatch`.

### 3.3 Identifiers

| Identifier                   | Grammar                               | Notes                                                                                                                                                                      |
| ---------------------------- | ------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Plugin id                    | `^[a-z0-9][a-z0-9-]*$`, 2 to 32 bytes | Names the install directory, the settings file, the lock record, the state and cache directories, and the variables `TOTE_PLUGIN_<ID>_...` (upper case, `-` becomes `__`). |
| Contribution id              | `^[a-z0-9][a-z0-9-]{0,31}$`           | Unique within its table.                                                                                                                                                   |
| Setting, field and column id | `^[a-z][a-z0-9_]{0,31}$`              | A field id is never `repo`, `conn`, `setting`, `item`, `cwd` or `home`. A setting id never holds `__`.                                                                     |

A plugin id is **reserved**, and refused with `id_invalid`, when it is:

- a verb of tote or a verb that tote keeps for later, a reserved word, or the name of a field of a
  tote record;
- any id that starts with `tote-`;
- the id of an official plugin (`gft`, `ghrel`, `ghsec`, `konsolai`, `ssh-tools`) unless the plugin
  is the copy that ships with tote or an official release (`totecli/<id>`, for that same id).

The reserved ids are:

```text
__complete a alias aliases all attach builtin bundled cmd co complete completion config conn
connections connexions core cwd desc doctor edit f fav find func funcs health help hidden history
host import init ip jump keep key kind mac man managed mn mode name needs_review none opener path
pick plugin plugins port ports proc process processes procs r recent refresh registry remote repo
repos restore schema scheme script session snippets start stats status sync tags target tote tunnel
tunnels uninstall update upgrade user version via when
```

A later release of tote may reserve more names (verbs and record fields grow). That never breaks an
installed plugin: the reservation is checked when a plugin is checked, installed or updated from a
new source, and not when an installed plugin is loaded. A plugin whose id is reserved after its
install keeps loading, running and updating under that id. A new install under it is refused with
`id_invalid`, and so is an update whose id is an official one that the plugin is not allowed to
carry. The fallback for an author who wants a new install is to rename the plugin.

An installed plugin lives in a directory named after its `id`. The directory of a source may have
any name. `tote plugin check --official` lets a check accept an official id.

### 3.4 Versions and compatibility

`api` (the contract), `version` (the plugin) and `tote` (the host range) are independent.

- **`api`.** tote 1.0 supports API 1. Another value is incompatible (`api_unsupported`).
- **`tote`.** Comparators `>=`, `>`, `<=`, `<` and `=` joined by commas, which mean AND. A bare
  version means `=`. A partial version is completed with zeros, so `<2` is `<2.0.0`. There is no
  `||`, `^` or `~`. A syntax error is `range_invalid`. A host that is out of range is incompatible
  (`tote_range`). tote compares its own version without pre-release and build parts. A development
  build satisfies every range: its version is `dev` (the default of `make build`), `0.0.0-dev` (what
  `make build VERSION=0.0.0-dev` gives) or the pseudo-version that `go build` writes for a checkout,
  such as `0.0.0-20261109205113-159407959e94`.
- **`requires_features`.** Feature names that the plugin needs. tote 1.0 announces these:

  ```text
  command item_action.repo setting.list field.bool open.url output.terminal
  ```

  Any other name makes the plugin incompatible (`feature_missing`). A feature name is
  `^[a-z][a-z0-9_.]{0,63}$`, and a later tote announces more of them: that is how a manifest uses a
  key that tote 1.0 does not have, and 1.0 then reports the plugin as incompatible and not as
  invalid. tote reserves more names for later versions, and a manifest that needs one is
  incompatible too. That includes `exec.list` and `exec.run`, which belong to the exec tier (section
  10): tote 1.0 does not announce them.

- **`platforms`.** A plugin that does not list the running system is incompatible
  (`platform_unsupported`).

Inside API 1, tote only gains optional things: a key, a table, a placeholder, a permission or an
enum value. Nothing is removed and nothing changes meaning. A deprecated key keeps working for two
minor releases with a warning (`deprecated_key`). An unknown key, permission, `output` or field type
is an error.

An **incompatible** manifest is not an invalid one. `plugin check` reports it with
`"incompatible": true` on the finding, and the plugin is listed in the state `incompatible`.

### 3.5 Permissions and capabilities

`permissions` is a closed vocabulary. An unknown entry is `perm_unknown`, a malformed one
`perm_invalid` and a duplicate `perm_invalid`. tote sorts and de-duplicates them before it hashes or
shows them.

| Entry                               | Grammar                                                                                           | Declarative tier                                                                                                                            |
| ----------------------------------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `exec:<program>`                    | A program name, never a path: `^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`                                | **Enforced.** The first word of every `run` must be covered. `exec:tote` and the base name of the running binary are refused (`exec_self`). |
| `env:<NAME>`                        | `^[A-Z_][A-Z0-9_]{0,63}$`                                                                         | **Enforced.** Only the declared names are passed to the program.                                                                            |
| `host:read:repos`                   |                                                                                                   | **Enforced.** `{repo.*}` placeholders and `target = "repo"` are refused without it (`perm_missing`).                                        |
| `host:open-url`                     |                                                                                                   | **Enforced.** An `open` key is refused without it (`perm_missing`).                                                                         |
| `net:<host>`                        | Lower case labels, at least 2, an optional leading `*.`. No port, IP address or user information. | **Declared.** Shown at consent.                                                                                                             |
| `fs:read:<zone>`, `fs:write:<zone>` | Zone: `home`, `cwd`, `repo`, `config`, `ssh`, `tmp` or `any`                                      | **Declared.** Shown at consent.                                                                                                             |

Three groups are never accepted:

- **Reserved**, `reserved_not_available`: `host:read:connections`, `host:handoff`, `host:clipboard`,
  `host:notify`, `host:editor`.
- **Refused**, `perm_refused`: `hook:intercept`, `host:write:settings`.
- **Blocked environment names**: an `env:` permission, and a key of an `env` table, may not name the
  variables that load code or change how programs resolve things. They are `PATH`, `HOME`, `SHELL`,
  `IFS`, `ENV`, `BASH_ENV`, `SHELLOPTS`, `BASHOPTS`, `PS4`, `PROMPT_COMMAND`, `CDPATH`,
  `GLOBIGNORE`, `JAVA_TOOL_OPTIONS`, `SSH_ASKPASS`, `PAGER`, `EDITOR`, `VISUAL`, `MANPAGER`,
  `MANOPT`, `BROWSER`, `LUA_INIT`, `OPENSSL_CONF`, `SSL_CERT_FILE`, `SSL_CERT_DIR`, `GCONV_PATH`,
  `LOCPATH`, `NLSPATH`, `HOSTALIASES`, the variables `XDG_CONFIG_HOME`, `XDG_CONFIG_DIRS`,
  `XDG_DATA_HOME` and `XDG_DATA_DIRS`, the Git variables that name a repository, a hook or a program
  (`GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_EXTERNAL_DIFF`, `GIT_PAGER`, `GIT_EDITOR`,
  `GIT_SEQUENCE_EDITOR`, `GIT_TEMPLATE_DIR`, `GIT_ALLOW_PROTOCOL`, `GIT_OBJECT_DIRECTORY`,
  `GIT_ALTERNATE_OBJECT_DIRECTORIES`, `GIT_NAMESPACE`, `GIT_COMMON_DIR`, `GIT_ATTR_SOURCE`,
  `GIT_REPLACE_REF_BASE`), and every name that starts with `LD_`, `DYLD_`, `TOTE_`, `PYTHON`,
  `PERL5`, `RUBY`, `NODE_`, `GIT_SSH`, `GIT_CONFIG`, `GIT_ASKPASS`, `GIT_EXEC_PATH`,
  `GIT_PROXY_COMMAND`, `BASH_FUNC_`, `GIT_TRACE`, `LESS` or `SSH_ASKPASS`. A key of an `env` table
  is `env_refused`. An `env:` permission is `perm_refused`, except `env:TOTE_TOKEN`, which is
  `reserved_not_available`.

**Effective capabilities** are the declared entries plus one that tote derives:

- `derived:tty` when any contribution has `output = "terminal"`: the program takes over the terminal
  and can read keys and draw anything.

Consent and escalation work on the effective set. A second derived capability, `derived:code`,
belongs to the exec tier, which tote 1.0 does not have (section 10).

**Covering.** A declarative plugin covers its programs: the first word of every `run` is a literal
word (no placeholder, no `/`) with `exec:<word>` declared, and every name in an `env` table has
`env:<NAME>` declared. An `exec:` entry that no command uses is a warning (`exec_unused`).

**Shells and launchers.** Declaring `exec:` for a shell or launcher is an error, `argv.shell`.
Matching is case-insensitive:
`sh bash zsh dash ksh fish csh tcsh env xargs sudo doas su nohup nice ionice time timeout setsid stdbuf watch find awk gawk perl python python3 ruby node lua php tclsh osascript eval`.
This list is advisory: a program like `git` or `ssh` can run other programs through its options.
`check` also warns, `argv.risky_flag`, when a `run` uses an option known to start another program:

| Program                       | Options                                  |
| ----------------------------- | ---------------------------------------- |
| `git`                         | `-c`, `--exec-path`, `--config-env`      |
| `ssh`, `scp`, `rsync`, `sftp` | `-o`, `-e`, `-F`                         |
| `curl`, `wget`                | `-K`, `--config`                         |
| `tar`                         | `--to-command`, `--use-compress-program` |

For `ssh`, `scp`, `rsync` and `sftp`, every argument that holds a placeholder must come after a
literal `--` (`argv.ssh_no_dashdash`, an error).

### 3.6 `[[requires]]`

A `[[requires]]` entry names a program that the plugin needs.

| Key        | Type    | Required | Rule                                                                                            |
| ---------- | ------- | -------- | ----------------------------------------------------------------------------------------------- |
| `bin`      | string  | yes      | A program name, 1 to 64 bytes, `^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`.                            |
| `hint`     | string  | no       | Up to 200 runes. Shown, never run, and labelled "suggested by the plugin, not checked by tote". |
| `optional` | boolean | no       | `false`. A missing optional program is a warning and hides nothing.                             |

tote looks the program up on the scrubbed `PATH` (section 4.2) with a `stat`. It never runs it. A
non-optional program that is missing puts the plugin in the state `missing-dependency`.

### 3.7 Placeholders

Placeholders are used in `run`, `open`, `cwd`, the values of `env`, the `default` of a field and the
text of `confirm`. The syntax is `{name}` or `{name?}`. A name is dotted segments that match
`^[a-z][a-z0-9_]*$`. `{{` and `}}` are literal braces. A stray brace, an empty `{}`, an unknown name
or a name that is out of scope is an error at load: `placeholder_syntax`, `placeholder_unknown` or
`placeholder_scope`.

| Placeholder                                                                   | Where                              | Value                                                                                 |
| ----------------------------------------------------------------------------- | ---------------------------------- | ------------------------------------------------------------------------------------- |
| `{<field>}`                                                                   | the contribution's own fields      | The submitted value. For a `bool` field, its `flag` when true and nothing when false. |
| `{setting.<id>}`                                                              | everywhere                         | The effective value of a setting.                                                     |
| `{repo.path}`, `{repo.name}`, `{repo.remote}`, `{repo.slug}`, `{repo.branch}` | everywhere                         | The repository of the call. Needs `host:read:repos`.                                  |
| `{cwd}`                                                                       | everywhere                         | The working directory of tote.                                                        |
| `{home}`                                                                      | everywhere                         | Your home directory.                                                                  |
| `{item.id}`, `{item.data.<key>}`                                              | `[[list.action]]` only (exec tier) | The row of a list.                                                                    |

The repository of a call is the one the item action was started on or, for a command, the saved
repository whose path contains the working directory of tote (the longest match on a folder
boundary).

- `repo.path` is the repository path with `~` expanded.
- `repo.name` is the saved name.
- `repo.remote` is the `origin` URL, or the only remote when there is exactly one. It loses its user
  information when it is an `https`, `ssh` or `git` URL. The user of an scp-like address
  (`git@github.com:example/demo.git`) is kept. It is empty unless the scheme is `https`, `ssh` or
  `git`, or the form is scp-like.
- `repo.slug` is `owner/name` when the host of `repo.remote` is `github.com`, otherwise empty.
- `repo.branch` is the current branch.

**Substitution** is per argument, in one pass, and never through a shell. The produced text must be
valid UTF-8, hold no control character (a tab included) and no bidirectional or zero-width
character, and stay within 1024 bytes. The rules for an empty value:

- `{x}` that is empty or unresolved makes the contribution **unavailable**. It is shown greyed, with
  the reason.
- `{x?}` that is empty drops the **whole argument**.
- A `list` setting must be a whole argument, `"{setting.folders}"`, and expands to zero or more
  arguments.

**The dash rule.** An argument that starts with `-` after substitution, when its template does not,
is refused (`dash_refused`). Three things are exempt: a placeholder named in the contribution's
`allow_dash`, the value of a `choice` field with static `choices`, and the `flag` of a `bool` field.

The first word of a `run` is a literal program name. A placeholder there, or a `/`, is an error,
`argv0_invalid`.

### 3.8 Form fields

`[[command.field]]` and `[[item_action.field]]` ask for values before a command runs. They share one
grammar and are asked in file order. Cancelling the form cancels the whole call.

| Key        | Type              | Required | Rule                                                                                                                                                                                               |
| ---------- | ----------------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`       | string            | yes      | Section 3.3. Unique within its contribution.                                                                                                                                                       |
| `type`     | string            | yes      | `text`, `choice` or `bool`.                                                                                                                                                                        |
| `label`    | string            | yes      | English text, up to 60 runes.                                                                                                                                                                      |
| `hint`     | string            | no       | Up to 200 runes.                                                                                                                                                                                   |
| `required` | boolean           | no       | `false`.                                                                                                                                                                                           |
| `default`  | string or boolean | no       | A template for `text` and `choice`, using earlier fields, `{repo.*}`, `{setting.*}`, `{cwd}` and `{home}`. A boolean for `bool`. An unresolved placeholder gives an empty default, never an error. |
| `pattern`  | string            | no       | `text` only. An RE2 expression of at most 256 bytes.                                                                                                                                               |
| `max_len`  | integer           | no       | `text` only. 1 to 1024. Default 256.                                                                                                                                                               |
| `choices`  | array of strings  | `choice` | `choice` only. 1 to 64 values of at most 64 runes, no duplicate. An empty value is allowed.                                                                                                        |
| `flag`     | string            | `bool`   | `bool` only. A literal that starts with `-`, 1 to 64 bytes, without space or tab.                                                                                                                  |

A submitted value is checked in this order: `required`, `max_len` (in runes), `pattern`, then the
dash rule at substitution. A value over 4096 bytes is refused. A failure is `input_invalid` with the
field id and the rule (`required`, `max_len`, `pattern`, `choice`, `bool`, and for the text itself
`length`, `utf8` or `control`). Each rule has a message in both languages, the catalog key
`plugin_reject_<rule>`, which `tote plugin check --argv` and the form print.

- A `choice` value must be one of the `choices`. An empty value passes only when the field is not
  required and `choices` holds an empty string.
- A `bool` field accepts `true`, `1`, `yes`, `on`, `false`, `0`, `no`, `off` in any case, or
  nothing. Its placeholder `{id}` gives the `flag` when true and nothing when false, so `{id?}`
  drops the argument.

A literal default of a `choice` must be one of the `choices`, and a literal default of a `text` must
fit `max_len` and `pattern`. A default that does not is `value_invalid`.

`pattern` is tried with `MatchString`, so it **must start with `^` and end with `$`**. Otherwise
`[0-9]+` would accept `1 --force`. An unanchored pattern is an error, `pattern.unanchored`.

The labels of a `choice` value have no English text in the manifest. Give them in `locales/en.toml`
(section 3.12).

Reserved: the keys `secret`, `remember` and `choices_from`, and the field types `connection`,
`connections`, `repo` and `path`. They give `reserved_not_available`.

### 3.9 Contributions: `[[command]]` and `[[item_action]]`

A **command** is a program that the user starts by name. An **item action** is the same for a saved
repository. Both share these keys.

| Key           | Type              | Required             | Default                                      | Rule                                                                                                                                                                                                                      |
| ------------- | ----------------- | -------------------- | -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`          | string            | yes                  |                                              | Section 3.3.                                                                                                                                                                                                              |
| `label`       | string            | yes                  |                                              | Up to 60 runes.                                                                                                                                                                                                           |
| `description` | string            | no                   | none                                         | Up to 200 runes. Shown in the footer.                                                                                                                                                                                     |
| `run`         | array of strings  | one of `run`, `open` |                                              | The program and its arguments, as templates. The first word is literal. At most 64 elements.                                                                                                                              |
| `open`        | string            | one of `run`, `open` |                                              | Instead of `run`: a template that must give an `https` URL (see `open` below).                                                                                                                                            |
| `output`      | string            | with `run`           |                                              | `pager`, `silent` or `terminal`.                                                                                                                                                                                          |
| `cwd`         | string            | no                   | `{cwd}`, or `{repo.path}` for an item action | A template for the working directory. It must give an existing absolute directory (`cwd_invalid`).                                                                                                                        |
| `env`         | table of strings  | no                   | `{}`                                         | Variable name to template. At most 16. Each name needs `env:<NAME>`.                                                                                                                                                      |
| `confirm`     | boolean or string | no                   | `false`                                      | `true` asks the generic question. A string is the English text of the question, with placeholders.                                                                                                                        |
| `pause`       | string            | no                   | `always`                                     | `terminal` output only: `always`, `error` (status 126 and above) or `never`.                                                                                                                                              |
| `timeout`     | string            | no                   | `5m`                                         | `^[0-9]+(s\|m)$`, at most `30m`. Not with `terminal`.                                                                                                                                                                     |
| `allow_dash`  | array of strings  | no                   | `[]`                                         | Names of placeholders of this `run` that may start with `-`. At most 16.                                                                                                                                                  |
| `key`         | string            | no                   | none                                         | A menu accelerator, one of `[a-z0-9]`. In the Tab menu of a repository, the key runs the action, or shows why it cannot run. The keys of the list itself win, and when two actions ask for the same key the first has it. |
| `keywords`    | array of strings  | no                   | `[]`                                         | `command` only. Up to 8 words of 24 runes, matched when the user searches for a command.                                                                                                                                  |
| `field`       | array of tables   | no                   | none                                         | Section 3.8.                                                                                                                                                                                                              |
| `target`      | string            | item action          |                                              | Item action only, required: `repo`.                                                                                                                                                                                       |

How the keys combine:

- `run` and `open` exclude each other (`key_conflict`). One of them is required.
- `output` is required with `run`.
- `pause` needs `output = "terminal"`. `timeout` is refused with `output = "terminal"`.
- With `open`, none of `cwd`, `env`, `output`, `pause`, `timeout` and `allow_dash` is allowed.
- Every name in `allow_dash` is a placeholder of the same `run`.
- `key` is a warning when two item actions use the same one (`key_clash`, the first claimant wins)
  or when it is a core key, `q j k h l g` (`key_reserved`, the core key wins).

**`open`.** No program of the plugin runs. tote asks `Open https://host/path?`, with the full host
and path and the focus on Cancel, then calls the platform opener (`xdg-open` or `open`) with the URL
as one argument. The plugin needs `host:open-url`. The template must start with `https://`, without
user information, and be at most 2048 bytes. A template that starts with a placeholder is checked
when it runs (`open_invalid`).

**`output`:**

| Value      | Streams                                                                     | Shown as                                                                     |
| ---------- | --------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `pager`    | stdin is `/dev/null`. stdout and stderr share one pipe, up to 8 MiB.        | A scrollable view.                                                           |
| `silent`   | stdin is `/dev/null`. stdout is dropped. The last 4 KiB of stderr are kept. | A short notice. The 4 KiB are shown on failure.                              |
| `terminal` | The terminal.                                                               | The program's own display, then a pause. Effective capability `derived:tty`. |

An item action is greyed, with the reason in the footer, when its directory is not a directory or a
placeholder is unresolved. For example `{repo.slug}` on a repository with no GitHub remote.

### 3.10 Where contributions appear

Only a plugin in the state `enabled` contributes. A command is offered as a command, and an item
action for a saved repository. Their labels and keywords come from the language of the user (section
3.12). The screens of tote's terminal interface that show them are not described here, and their key
bindings are not part of this reference.

### 3.11 `[[setting]]`

A setting is a typed value that the user can change. A plugin does not read the settings file and
does not write settings in API 1: it receives the effective values through `{setting.<id>}`.

| Key           | Type             | Required | Rule                                                                                                                                                                              |
| ------------- | ---------------- | -------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`          | string           | yes      | Section 3.3. `token_env` and `token_cmd` are reserved for a later version and refused; so are `schema` for good (it is the version key of the settings file) and an id with `__`. |
| `type`        | string           | yes      | `string`, `bool`, `int`, `enum`, `path` or `list`.                                                                                                                                |
| `label`       | string           | yes      | Up to 60 runes.                                                                                                                                                                   |
| `description` | string           | no       | Up to 200 runes.                                                                                                                                                                  |
| `default`     | typed            | no       | The type of the setting. Without it: `""`, `false`, `0`, the first choice for an `enum`, `""` for a `path`, `[]` for a `list`.                                                    |
| `pattern`     | string           | no       | `string` and `list` only. An RE2 expression, anchored like a field pattern.                                                                                                       |
| `max_len`     | integer          | no       | `string` and `list` items. 1 to 1024. Default 256.                                                                                                                                |
| `min`, `max`  | integer          | no       | `int` only. Default minus and plus 2^31.                                                                                                                                          |
| `choices`     | array of strings | `enum`   | 1 to 64 values of at most 64 runes.                                                                                                                                               |
| `must_exist`  | boolean          | no       | `path` only.                                                                                                                                                                      |
| `max_items`   | integer          | no       | `list` only. 1 to 500. Default 100.                                                                                                                                               |

A key that does not apply to the type is an error. A `default` must satisfy the setting itself: an
`enum` default is one of the `choices`, an `int` default is within `min` and `max`, a `string`
default matches its `pattern`, and `min` is not above `max`. Otherwise it is `value_invalid`. The
types `dir` and `secret_ref` are reserved. There is no password type and no secret setting: a
credential arrives through `env:<NAME>` or the program's own login.

**Precedence: environment, then file, then default.**

- **Environment.** The variable is `TOTE_PLUGIN_<ID>_<SETTING>` in the environment of tote. The id
  and the setting are upper-cased and each `-` of the id becomes `__`, so that no two pairs of
  plugin and setting share a variable: the plugin `my-tool` with the setting `x` reads
  `TOTE_PLUGIN_MY__TOOL_X`, and the plugin `my` with the setting `tool_x` reads
  `TOTE_PLUGIN_MY_TOOL_X`. No permission is needed. A `bool` is `1`, `true`, `yes`, `on` or `0`,
  `false`, `no`, `off` in any case. An `int` is decimal. A `list` is comma-separated and trimmed. An
  `enum` is one of its choices. An empty variable counts as unset.
- **File.** `<Config>/plugins/<id>.toml` is flat: one key per setting, and an optional `schema = 1`.
  A key that starts with `_` belongs to tote, never to a setting (a setting id starts with a
  letter): tote keeps such a key when it saves the file and does not report it as unknown. A file
  that is not valid TOML, is over 1 MiB or has another `schema` is ignored as a whole.
- **Default.** The `default` of the manifest, or the zero value of the type.

A value that does not fit its setting (type, length, pattern, range, choice, number of items, path)
is ignored for that setting and the next source applies. The rejection is never silent.
`plugin info` shows it as a warning:

```text
$ TOTE_PLUGIN_DEMO_GREETING='x!' tote plugin info demo
...
  ! setting greeting from the environment was ignored: does not match the pattern
```

With `--json` the same warning is
`{"code":"settings_ignored","args":["greeting","environment","pattern"],"message":"..."}`. A key of
the settings file that no setting declares is the warning `settings_unknown_key`. A file that cannot
be used is `settings_file`. The effective value always satisfies its setting, even when no source
does.

### 3.12 Translations

English is the language of the manifest. Other texts live in `locales/<lang>.toml` or in
`[i18n.<lang>]` tables of `plugin.toml`. Only `en` and `fr` are read: an `[i18n.<lang>]` table of
another language is `unknown_key`, and a `locales/<lang>.toml` file of another language is
`declarative_extra_file`. A locale file wins over the table. The language is chosen from
`TOTE_LANG`, then the `lang` setting of tote, then `LC_ALL`, `LC_MESSAGES` and `LANG`.

A text is looked up by key, in the language, then in English, then in the manifest. A missing key is
never an error at run time.

| Text         | Key                                                                                            |
| ------------ | ---------------------------------------------------------------------------------------------- |
| Plugin       | `title`, `description`                                                                         |
| Contribution | `<table>.<id>.label`, `.description`, `.keywords` (one comma-separated string), `.confirm`     |
| Field        | `<table>.<id>.field.<fid>.label`, `.hint`, `.choice.<value>` (`.choice._` for the empty value) |
| Setting      | `setting.<id>.label`, `.description`, `.choice.<value>`                                        |
| Requirement  | `requires.<bin>.hint`                                                                          |

Values are plain text. `{name}` is allowed where the English text may have it. There are no printf
verbs and no plurals. A locale file is a flat table of quoted keys, as in section 11. Nested tables
are read the same way.

Choices, enum values and columns have no English text in the manifest, so a plugin that has them
ships `locales/en.toml`.

`plugin check` compares the keys that the manifest needs with the keys of each locale. A missing key
is the warning `locale_missing_key` and an extra key is `locale_extra_key`. A locale file that does
not parse is `locale_invalid`.

### 3.13 Reserved names

These are reserved. A manifest that uses one is refused with `reserved_not_available`. tote 1.0
never accepts a feature that does nothing.

- **Tables:** `[[widget]]`, `[[panel]]`, `[[decorator]]`, `[[provider]]`, `[[connector]]`,
  `[[hook]]`, `[[job]]`, `[[screen]]`, `[[import]]`, `[[badge]]`, `[[search]]`.
- **Field keys and types:** `secret`, `remember`, `choices_from`; `connection`, `connections`,
  `repo`, `path`.
- **Setting types and ids:** `dir`, `secret_ref`; `token_env`, `token_cmd`.
- **Permissions:** section 3.5.

### 3.14 The JSON Schema

[plugin.schema.json](plugin.schema.json) is a JSON Schema (draft 2020-12) of the manifest as it
looks once TOML is converted to JSON. Use it in an editor to catch mistakes early:

- It checks keys, types, limits, enumerations, identifier grammars, the reserved ids, placeholders'
  syntax, the permission grammar and the blocked environment names. A feature name of
  `requires_features` is checked by its grammar, not against a list, so that a manifest of a later
  tote stays valid; whether this tote announces it is for `plugin check` (`feature_missing`).
- It does not check what needs the whole manifest or the machine: that a placeholder names a field
  or setting that exists (and that a list setting is a whole argument, `placeholder_list`), that
  every `run` is covered by an `exec:` permission, that an `env` table is covered by `env:`
  permissions (`env_uncovered`), that `host:read:repos` and `host:open-url` are declared where a
  feature needs them (`perm_missing`), that a pattern is anchored and compiles, that `allow_dash`
  names a placeholder of the `run`, that ids are unique within a table (`id_duplicate`), that a
  `default` satisfies its own field or setting (`value_invalid`: a choice default among the choices,
  an `int` default within `min` and `max`, a default that matches its pattern, `min` not above
  `max`), that there are at most 64 contributions in all, that the `tote` range is satisfiable, and
  everything about the package (files, hashes, translations).
- It accepts the manifest of `gft` that ships with tote, and it rejects the manifests that tote's
  reader rejects, key by key, apart from the rules of the previous bullet.
- It agrees with tote's reader on the empty values: an empty `id` (`id_invalid`), an empty `version`
  (`version_invalid`) and a `[[list]]` with an empty `column` array or an empty column
  (`value_invalid`, `key_missing`) are refused by both.

`tote plugin check` is the authority. Run it before you publish.

## 4. How a contribution runs

### 4.1 The sequence

For a command or an item action:

1. tote gathers the context: the selected record, the current repository and the settings.
2. It checks that the contribution is available (section 3.9).
3. It asks for the form, if there is one. Cancelling the form cancels everything.
4. It builds the arguments (section 3.7).
5. It **checks the state again** from the files on disk. The plugin must be `enabled`, or the call
   is refused with `not_runnable`. It then finds the first word on the scrubbed `PATH`. A program
   that is not installed is `dependency_missing`, with the hint of `[[requires]]`.
6. It resolves the working directory.
7. It asks for confirmation, if `confirm` is set. The question shows the exact command and its
   directory, with the focus on Cancel.
8. It builds the environment (section 4.2).
9. It checks the state once more just before it starts the program, then runs it according to
   `output`.
10. It shows the result: `done`, `failed (exit 1)` or `stopped by SIGTERM`.

A `disable` or `remove` in another terminal therefore stops the next start in a running tote, and
stops a running process (section 7). A declarative plugin cannot quit tote, change its directory or
type into the shell.

`tote plugin check <id> --argv --set <field>=<value> --repo <name>` runs steps 1 to 6 and prints the
command each contribution would run. No program that the plugin names starts. With `--repo`, tote
itself runs a read-only `git` to read the remote and the branch of that repository (SECURITY.md,
section 9). Section 8.9 gives an example.

### 4.2 The environment

The environment of a plugin program is built from nothing.

1. **Copied when set:** the scrubbed `PATH`, `HOME`, `USER`, `LOGNAME`, `LANG`, `LANGUAGE`, every
   `LC_*`, `TERM`, `COLORTERM`, `NO_COLOR`, `TZ`, `TMPDIR`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`,
   `XDG_STATE_HOME` and `XDG_CACHE_HOME`. `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` and `ALL_PROXY`,
   in either case, are copied only when the plugin declares a `net:` permission. `NO_COLOR=1` is set
   in the mono theme.
2. **Set by tote:** `TOTE_CONFIG_DIR`, `TOTE_DATA_DIR`, `TOTE_STATE_DIR` and `TOTE_CACHE_DIR` when
   they are set, so that `tote repo list --json` finds the same data.
3. **Added by tote:** `TOTE_API=1`, `TOTE_HOST_VERSION`, `TOTE_LANG`, `TOTE_BIN`, `TOTE_PLUGIN_ID`,
   `TOTE_PLUGIN_DIR` (read-only), `TOTE_PLUGIN_STATE`, `TOTE_PLUGIN_CACHE` and
   `PYTHONDONTWRITEBYTECODE=1`. A captured run also gets `GIT_TERMINAL_PROMPT=0`.
4. **Declared:** each `env:<NAME>` that is set in the environment of tote, then the `env` table of
   the contribution.

Nothing else is passed: no `SSH_AUTH_SOCK`, no `XDG_RUNTIME_DIR` unless `env:XDG_RUNTIME_DIR` is
declared, no token.

The **scrubbed `PATH`** is the `PATH` of tote without empty entries, relative entries, and entries
inside the plugin tree. The first word of `run` is resolved on it to an absolute path. A result
inside the plugin tree or the working directory is refused.

This list says what tote hands over. It is not a boundary for a program that runs with your rights.
[SECURITY.md](SECURITY.md) explains the limit.

### 4.3 Limits of a run

- At most **4** plugin processes at once in one tote process. A fifth start is refused at once with
  `busy`. There is no queue. Two tote processes count their own runs, and the interface itself
  starts one program at a time.
- `pager` and `silent` runs are limited by `timeout` (default 5 minutes, at most 30). On timeout or
  cancel tote sends SIGTERM to the process group, then SIGKILL after 2 seconds.
- A captured run ends when the direct child exits. tote drains the pipe until it has been silent for
  250 ms (5 seconds at most) after that. Other members of the process group are not killed, so a
  program that launches a browser can leave it running. Timeout, cancel, `disable` and `remove` do
  kill the group.
- Captured runs use their own session, so they have no controlling terminal.
- The pager shows at most 8 MiB, then a notice in the language of the interface ("output cut here,
  it was too long to keep"), which also covers a line longer than 32 KiB and more than 200000 lines.
  A run that writes more than 64 MiB is stopped (`output_limit`). The registry's own marker
  `[output truncated]` is in the event stream only, and the interface does not show it.
- The pager follows the end of the output while the program runs, until the user scrolls up. When
  the program ends well it goes back to the top of the output, when it fails it stays at the end. It
  has no search in 1.0.
- The pager keeps only colour: `ESC [ ... m` with the parameters
  `0 1 2 3 4 22 23 24 30-37 39 90-97`, and not in the mono theme. A colour sequence with any other
  parameter is removed whole. Every other escape sequence and control character, except line feed
  and tab, becomes `?`. A reset ends the text when a colour was kept.
- A `terminal` run uses the terminal of tote, flushes pending keys before the pause line, and then
  pauses according to `pause`.
- A declarative run never counts toward quarantine.

## 5. Plugin states

An installed plugin has one **state**, computed from the files at that moment. Nothing is stored.
The first row that matches wins. Every failing check is kept as a **reason**, so a disabled plugin
that is also broken lists both.

| #   | State                | When                                                                                                     | Reasons                                                                                                                                                                                                                                                                                              | Fix                                                                                                    |
| --- | -------------------- | -------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| 1   | `disabled`           | You turned it off, or `--no-plugins`                                                                     | `config_off`, `lock_off`, `session_off`                                                                                                                                                                                                                                                              | `plugin enable`                                                                                        |
| 2   | `broken`             | The tree, the record or the manifest cannot be trusted                                                   | `no_lock_record`, `lock_record_invalid`, `lock_unreadable`, `config_unreadable`, `manifest_invalid`, `lock_version`, `lock_tier`, `package_invalid`, `tree_missing`, `tree_symlink`, `tree_special`, `tree_name`, `tree_limit`, `tree_owner`, `tree_perms`, `tree_hash`, `tree_io`, `store_writable` | `plugin remove`, then install again; for `store_writable`, `chmod go-w` the directory the reason names |
| 3   | `incompatible`       | `api`, `tote`, `requires_features` or `platforms` rule it out, or the plugin is of the exec tier         | `api_unsupported`, `tote_range`, `feature_missing`, `platform_unsupported`, `platform_none`, `reserved_not_available` (`tier = "exec"`)                                                                                                                                                              | Upgrade tote or the plugin                                                                             |
| 4   | `needs-consent`      | You have not consented, or it now asks for more                                                          | `not_consented`, `caps_escalated`, `escalated`                                                                                                                                                                                                                                                       | `plugin enable`, or `--grant`                                                                          |
| 5   | `quarantined`        | Failures turned it off. A declarative run never counts as a failure, so this state does not occur in 1.0 | `quarantined`                                                                                                                                                                                                                                                                                        | `plugin enable`                                                                                        |
| 6   | `missing-dependency` | A non-optional `[[requires]]` program is not found                                                       | `missing_dependency`                                                                                                                                                                                                                                                                                 | Install the program                                                                                    |
| 7   | `enabled`            | None of the above                                                                                        |                                                                                                                                                                                                                                                                                                      |                                                                                                        |

Only `enabled` contributes. The other states keep the plugin listed, with its reasons, and add
nothing to the list of commands. A plugin that ships with tote and is not installed is listed as
`available`, which is not a state.

- `disabled` ranks first, so your intent wins.
- JSON and tab-separated output write the states as `needs-consent` and `missing-dependency`. The
  table form and the text of `info` write them as words: "consent needed" and "missing dependency".
- An optional program that is missing is a warning, `optional_missing`, and not a reason.
- A `revoked` state does not exist in 1.0.

A real example of a plugin whose program is not installed:

```text
$ tote plugin list
demo	1.1.1	enabled	local	Print a greeting and show the branch of a repository.	true
dep	0.1.0	missing-dependency	local	Needs a program that is not installed.	false
gft	1.0.0	available	bundled	GitHub releases and git stashes through the gft command.	false
$ tote plugin info dep
dep 0.1.0
description: Needs a program that is not installed.
tier: declarative (no program of its own)
kind: local
source: /home/u/work/dep
state: missing dependency
  - missing dependency: nosuchprog
  fix: install the missing program
...
```

## 6. Consent

Consent is what you accept, once, before a plugin runs. It covers the effective capabilities and the
command surface. [SECURITY.md](SECURITY.md) has the exact text of the screen and the reasoning. This
section lists the rules.

**The grant token** is the sorted effective capabilities joined by commas, then `@`, then the first
12 digits of `surface_sha256`:

```text
exec:date,exec:echo,exec:git,fs:read:repo,host:read:repos@058feb973609
```

It names exactly what you consent to. `--grant <token>` accepts only the current token, so a token
copied from an older review fails (`grant_mismatch`, exit 4).

**When consent is asked.**

| Why (`why` in JSON) | Case                                                                                                  |
| ------------------- | ----------------------------------------------------------------------------------------------------- |
| `new`               | A plugin that is not installed.                                                                       |
| `not_consented`     | An installed plugin that you never consented to.                                                      |
| `caps_added`        | An update asks for a capability the record does not cover.                                            |
| `surface`           | A command changed in a way that escalates (below).                                                    |
| `unverified`        | The installed copy could not be verified, so it cannot be compared.                                   |
| `forced`            | `--force` turned a refusal (same version, older version, pre-release, another source) into a warning. |

**What escalates the surface.** Something that runs changes in one of these ways:

- the `confirm` of a contribution that keeps its id is removed, or turned from `true` or a text to
  `false`;
- a name is added to `allow_dash`, on a contribution that keeps its id or on a new one;
- a name is added to the `env` table, or the value of a name changes, on a contribution that keeps
  its id; a new contribution that carries an `env` table asks too.

**What never asks.** A change of label, description, keywords, hints or translations. A change of
commands that does not escalate: it is shown in the screen as
`Commands: 0 added, 0 removed, 0 changed` and nothing more. A capability that is removed: it shows
as "no longer asked". When no consent is needed, `update` prints
`updated <id> from <old> to <new>, permissions unchanged`.

**What the screen shows.** For a new install: the source and origin class, the checksum, every
command in full as a template, the capabilities with `[enforced]` or `[declared]`, the programs that
are missing on this machine, and the reasons. A capability is marked as **broad** when it is file
access (`fs:read:` or `fs:write:`) to `home`, `ssh` or `any`, a wildcard `net:*.domain`, or an
`env:` name that looks like a secret (`SSH_AUTH_SOCK`, a name with `TOKEN`, `SECRET`, `PASSWORD`,
`CREDENTIAL`, `API_KEY` or `PRIVATE`, or a prefix `AWS_`, `AZURE_`, `GOOGLE_`, `GH_`, `GITHUB_`,
`GITLAB_`). This is the first part of the consent screen of the demo plugin, as `--dry-run` prints
it:

```text
$ tote plugin install ./demo --dry-run
Install plugin demo 1.1.1?

Source: /home/u/work/demo
Origin: local files, not verified
Checksum: sha256 01ddb77874bb, computed here, nothing to compare it with
No code of its own: tote runs only the programs listed below.

Description (written by the plugin, not checked by tote)
  | Print a greeting and show the branch of a repository.

Commands it will run:
  $ command greet: echo '{setting.greeting}' -n '{name}'
  $ command today: date +%F
  $ item action branch: git branch --show-current

This plugin will be able to:
  - run the program date [enforced]
  - run the program echo [enforced]
  - run the program git [enforced]
  - read files: the current repository [declared]
  - read your repository list (names, paths, remotes) [enforced]
    enforced: tote itself refuses anything else
    declared only: the plugin states it, tote cannot enforce it

! tote runs only the listed programs, but those programs can do whatever you can do with them.

consent is asked because:
  - it is a new plugin
Consent code exec:date,exec:echo,exec:git,fs:read:repo,host:read:repos@058feb973609

dry run: nothing was changed
to accept it without a terminal, run the command again without --dry-run and with --grant exec:date,exec:echo,exec:git,fs:read:repo,host:read:repos@058feb973609
```

The **origin class** in the screen is `official` (only a release of `totecli/<id>` for the plugin of
that id, checked by tote and never read from the manifest), `community` (any other release), `local`
(a directory or an archive on this machine) or `bundled` (the copy that ships with tote).

How consent is given:

- **In a terminal**, `install`, `update` and `enable` print the screen and ask `[y/N]` when consent
  is needed. Ctrl-C at the question exits 130, and so does SIGINT or SIGTERM at any other step, a
  download included (error code `interrupted`, nothing is changed).
- **Without a terminal**, there is no question. The verb refuses with `consent_needed` (exit 2), and
  the message holds the token. Review the output of `--dry-run`, then pass `--grant <token>`.
- `--grant` with the right token is consent. It is recorded as `consented_by = "grant"`.

## 7. Install, update, enable, disable, remove

### 7.1 Sources

`install` and `update` take a source:

| Source                               | Example                                                               | Needs `--sha256` |
| ------------------------------------ | --------------------------------------------------------------------- | ---------------- |
| A release URL                        | `https://github.com/example/demo/releases/download/1.1.1/demo.tar.gz` | yes              |
| A release shorthand                  | `example/demo@1.1.1`                                                  | yes              |
| The id of a plugin shipped with tote | `gft`                                                                 | no, and refused  |
| A directory                          | `./demo`, `/path/to/demo`, `~/demo`, `../demo`                        | no, and refused  |
| An archive file                      | `./demo.tar.gz`                                                       | optional         |

A path is `.`, `..`, `~`, or starts with `/`, `./`, `../` or `~/`. Any other bare word is never
taken for a directory: it is an id.

**Release rules.**

- The URL is exactly `https://github.com/<owner>/<repo>/releases/download/<tag>/<asset>`: `https`,
  host `github.com`, no port, user information, query or fragment, each of owner, repo, tag and
  asset within `[A-Za-z0-9._+-]` and none of them `.` or `..`.
- `owner/repo@tag` stands for the asset `<repo>.tar.gz` of that release. The tag is required.
- The **tag equals the `version`** of the manifest, without `v`. Otherwise `tag_mismatch`.
- Redirects are followed only to `github.com` and the GitHub download hosts, over `https`. A release
  that moved to another repository is refused (`origin_redirect`).
- At most 16 MiB are downloaded. tote sends no credential, so a download answered with status 401
  exits 3.
- The bytes are hashed while they are written and compared with `--sha256` **before anything is
  read**. A wrong pin is `hash_mismatch` (exit 5) and nothing is installed.
- `--sha256` takes 64 hexadecimal digits in either case (`hash_malformed` otherwise). A release
  without it is `hash_required`. For a directory or a shipped plugin it is `hash_unexpected`,
  because there is no archive to compare.

The pin proves that the file is the one the author published. It does not prove who the author is. A
hash copied from the page of the release gives integrity, not authenticity.

### 7.2 Update rules

`update <id> [<source>]` without a source uses the source of a local plugin as recorded, and the
copy shipped with tote for a bundled one. A plugin that came from a release has to be given the new
release: tote never asks the network what is new (`source_required`, exit 2). `install` of an id
that is already installed follows the same rules. tote refuses in these cases, and `--force` turns
each refusal into a warning that asks for consent again (`forced`):

| Refusal                                             | Code             | Exit |
| --------------------------------------------------- | ---------------- | ---- |
| The new version is not newer than the installed one | `downgrade`      | 1    |
| The new version is a pre-release                    | `prerelease`     | 1    |
| The source is not the one that installed the plugin | `source_changed` | 1    |

```text
$ tote plugin update demo ./demo
tote: error: version 1.1.1 is not newer than the installed 1.1.1; use --force
```

**An install that changes nothing.** `install <source>` of a plugin that is installed already from
the same source (the same origin and the same path or release URL) and with the same tree hash,
whose installed manifest can be read, is not an update and needs no consent. The version is not
compared on its own: the tree hash covers it. tote says so and does not touch the store or the lock
(it still reads the package, or downloads the release, into its staging area to compare the hashes):

```text
$ tote plugin install ./demo
plugin demo 1.1.1 is already installed (enabled)
```

When the plugin is not enabled, the line carries its state and is followed by the reasons and the
way out, as after an install, and `--dry-run` prints exactly that too. The exit code follows the
state: 0 for `enabled`, `disabled`, `quarantined` and `missing-dependency` (the plugin is installed
as it was asked for), and 4 for `needs-consent`, `broken` and `incompatible`, so that
`tote plugin install X --grant T && use X` does not go on with a plugin that cannot run; `--grant`
is not looked at, because nothing is installed (use `plugin enable`). With `--json` the object is
the one of an install with `"updated": false` and `"already_installed": true` (`"ok": false` with
the exit 4), and with `--dry-run --json` the dry-run object with `"already_installed": true` and
`"needs_consent": false` (every other install, update and dry-run object has
`"already_installed": false`). The name differs from the `unchanged` count that `plugin` carries
(section 8.7). Everything else keeps the rules above: another version is an update, another source
is `source_changed`, the same version with other files or an installed tree that was modified is
`downgrade`, `update` is never idempotent (the version is not newer), and `--force` asks for the
work and for the consent again.

A bundled plugin may be updated from its official release, `totecli/<id>`. There is no
`update --all`. The plugin in the source must have the id that you asked for: `id_mismatch` for the
id of a shipped plugin, and the manifest finding `id_invalid` with the reason `mismatch` for a
directory or an archive.

### 7.3 What an install does

1. It classifies the source and takes the lock for plugin changes.
2. It fetches or copies the package into a staging directory, checks the pin, extracts the archive
   and reads `plugin.toml` strictly. An invalid manifest is `manifest_invalid` (exit 1) and the
   first finding is shown.
3. It checks the package files (section 2.1) and computes the hashes.
4. It builds the consent view and asks (section 6).
5. On consent it moves the verified tree to `<Data>/plugins/<id>/<tree32>/`, makes it read-only and
   writes the lock record in one atomic write, so the version and the consent change together.
6. It removes what it replaced.

Nothing is installed when any step fails. Files that could not be deleted are reported as
`leftovers` and are not an error.

### 7.4 Enable, disable, remove

- **`disable`** sets a switch in the lock and writes `enabled = false` in `config.toml`. It keeps
  the files, the settings, the data and the consent. It stops the running processes of the plugin
  (`killed` in the JSON). Disabling twice is not an error (`already`).
- **`enable`** clears the switch, the quarantine and the failure counters. A plugin that needs
  consent is enabled only with a valid consent: in a terminal tote asks, otherwise
  `--grant <token>`. A broken or incompatible plugin is refused with `enable_refused`, and the
  reasons.
- **`remove`** deletes the files and the record, and clears `enabled = false` from the
  `[plugins.<id>]` table of `config.toml`: a plugin that is installed again is a new install, with a
  new consent, and starts enabled. It keeps the settings file and the state, cache and host
  directories. `--purge` deletes those too. The table itself is edited, not deleted, so an empty
  `[plugins.<id>]` header can stay in the file. Without a terminal, `--yes` is required
  (`confirm_needed`, exit 2). A symbolic link in the plugins directory is unlinked, and what it
  points to is not touched.
- **`--no-plugins`**, before the verb (`tote --no-plugins plugin list`), or `TOTE_NO_PLUGINS` set to
  anything but empty, `0`, `false`, `no` or `off`, turns every plugin off for that run. They show as
  `disabled` with the reason `session_off`.

The verbs that change plugins (`install`, `update`, `enable`, `disable`, `remove`) are refused with
`from_plugin` (exit 4) when `TOTE_PLUGIN_ID` is set, which it is in every program that tote starts
for a plugin, even with `--dry-run`. `list`, `info` and `check` still work:

```text
$ TOTE_PLUGIN_ID=demo tote plugin disable demo
tote: error: refused: this command changes tote and was started by a plugin
```

## 8. The command line

Only the exit codes and the JSON documents of this section are a promise for 1.x. The words of a
message, the output without `--json` and the detail of `check` are not (see
[../compat.md](../compat.md), section 8).

### 8.1 Verbs

```text
tote plugin [list] [--format table|tsv|json | --json]
tote plugin info <id> [--json]
tote plugin install <source> [--sha256 <hex>] [--grant <token>] [--dry-run] [--force] [--json]
tote plugin update <id> [<source>] [--sha256 <hex>] [--grant <token>] [--dry-run] [--force] [--json]
tote plugin enable <id> [--grant <token>] [--json]
tote plugin disable <id> [--json]
tote plugin remove <id> [--yes] [--purge] [--json]
tote plugin check [<id|dir|archive>] [--strict] [--official] [--argv [--set <field=value>] [--repo <name>]] [--json]
```

`plugins` is another name for `plugin`, and `ls` for `list`. `tote plugin` alone lists.
`tote help plugin` prints the usage. A bare word for `check` is an installed or shipped id. A
directory or an archive is written as a path (`./demo`).

| Flag                    | Verbs                         | Meaning                                                                             |
| ----------------------- | ----------------------------- | ----------------------------------------------------------------------------------- |
| `--json`                | all                           | Print one JSON object (section 8.3).                                                |
| `--format`              | `list`                        | `table`, `tsv` or `json`. The default is `table` on a terminal and `tsv` otherwise. |
| `--sha256 <hex>`        | `install`, `update`           | The checksum of the archive (section 7.1).                                          |
| `--grant <token>`       | `install`, `update`, `enable` | Consent without a question (section 6).                                             |
| `--dry-run`             | `install`, `update`           | Show what the plugin asks for and the token. Change nothing.                        |
| `--force`               | `install`, `update`           | Turn the refusals of section 7.2 into warnings that ask again.                      |
| `--yes`                 | `remove`                      | Do not ask.                                                                         |
| `--purge`               | `remove`                      | Also delete the settings, state, cache and host data.                               |
| `--strict`              | `check`                       | A warning about the package fails the check.                                        |
| `--official`            | `check`                       | Accept an official id.                                                              |
| `--argv`                | `check`                       | Allows `--set` and `--repo`. `check` always prints the commands.                    |
| `--set <field>=<value>` | `check`                       | The value of a field. Repeat it. Needs `--argv`.                                    |
| `--repo <name>`         | `check`                       | The saved repository to use. Needs `--argv`.                                        |

### 8.2 Exit codes

| Code | Meaning                                                                                                                                                                                                                                                                                                                                                                               |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0    | Done. This includes a `check` with only warnings, and `--dry-run`.                                                                                                                                                                                                                                                                                                                    |
| 1    | Failure: an invalid manifest, a plugin that is not installed, an update refused by section 7.2, a failed `check`, any failure without a more specific code.                                                                                                                                                                                                                           |
| 2    | Usage: a wrong flag or argument, `--sha256` missing or malformed, consent needed with no terminal, `--yes` needed, an unknown source or id.                                                                                                                                                                                                                                           |
| 3    | Authentication required: a download answered with status 401. tote sends no credential.                                                                                                                                                                                                                                                                                               |
| 4    | Refused by state or policy: `grant_mismatch`, `enable_refused`, an `install` of what is installed already when the plugin needs consent, is broken or is incompatible (section 7.2), `from_plugin`, `busy`, `stale`, and, for a package that an install or update refuses, `exec_self`, `exec_uncovered` and `perm_missing`. `not_runnable` is also 4, and no verb of 1.0 returns it. |
| 5    | Integrity: a hash does not match (`hash_mismatch`, `platform_hash`), an archive is unsafe or over a limit (`archive_*`), the tree on disk changed (`tree_*`), `too_large`, `origin_redirect`, `tree_collision`. An `enable` refused because the files changed is also 5.                                                                                                              |
| 127  | A program could not be started (`dependency_missing`, `start_failed`). No verb of 1.0 starts a plugin program, so no verb returns it.                                                                                                                                                                                                                                                 |
| 130  | Interrupted: Ctrl-C at the question of `install`, `update`, `enable` or `remove`, or SIGINT or SIGTERM during a download or any other step of them. The error code is `interrupted`; its message is translated and not a contract. SIGHUP (a closed terminal) is not caught: the process ends and what it left in `.staging` is removed by a later install, after one hour.           |

`check` exits 1 when it fails, and 5 when one of its errors is an integrity error. There is no
`plugin run` in 1.0, so a plugin program's own status never becomes the exit code of a plugin verb.

The codes 0 to 5, 10, 127 and 130 are tote's, here and in the rest of the command line, and they
keep their meaning in every 1.x release. A later release gives new conditions the codes 6 to 9 and
11 and above. `plugin run` and the exec tier will use 127 for a program that could not be started
and 4 (`not_runnable`) for a plugin that cannot run, as the table says, and they will never turn the
status of the program they started into the exit code of tote: that status goes into the JSON.

### 8.3 Output rules

- Text for people goes to standard output when it is the result and to standard error when it is a
  question or an error.
- With `--json`, the result is one JSON object on standard output, indented by 2 spaces. An error is
  a JSON object too (section 8.4), and **nothing goes to standard error** for it. The one exception
  is `list`, which still prints the problems of the plugin store (section 8.5) on standard error.
- Every object has `"schema": 1`. A list verb wraps its records: `{"schema": 1, "plugins": [...]}`.
- **Objects only gain fields.** A later release adds keys and never removes, renames or retypes one.
  The values of `state`, `kind`, `code` and the other enumerations are open: ignore a value that you
  do not know. The `tsv` of `list` has positional columns, and a later column is appended after the
  sixth (the rules for every list are in [../compat.md](../compat.md)).
- **A non-zero exit means: ignore any other output.** A script reads the exit code first and only
  then the object.
- Text values come from plugin files and are cleaned. Messages are in the language of the run
  (`TOTE_LANG`). The `code` and `args` of a finding or reason never change with the language.

### 8.4 The error object

```text
$ tote plugin info nope --json
{
  "schema": 1,
  "ok": false,
  "error": {
    "code": "not_installed",
    "message": "plugin \"nope\" is not installed",
    "exit": 1
  }
}
[exit 1]
```

`code` is stable. `message` is human text: it is in the language of the run and its words can change
in any release. `exit` repeats the exit code. A refused package is described by its first finding,
and the message ends with `(<n> more)`:

```text
$ tote plugin install ./bad --json --grant x
{
  "schema": 1,
  "ok": false,
  "error": {
    "code": "manifest_invalid",
    "message": "colour, line 8: unknown key (3 more)",
    "exit": 1
  }
}
[exit 1]
```

A wrong command line is `{"code": "usage", ...}` with exit 2. A failure that has no plugin code is
`failure`. A name that matches no record, for `check --repo`, is `not_found` with exit 1.

### 8.5 `list`

```text
$ tote plugin list
demo	1.1.1	enabled	local	Print a greeting and show the branch of a repository.	true
gft	1.0.0	available	bundled	GitHub releases and git stashes through the gft command.	false
$ tote plugin list --format table
NAME  VERSION  STATE      ORIGIN             DESCRIPTION
demo  1.1.1    enabled    local              Print a greeting and show the branch of a repository.
gft   1.0.0    available  shipped with tote  GitHub releases and git stashes through the gft command.
```

`tsv` has six columns: id, version, state, origin (the `origin` of a release, else the `kind`), the
first line of the description cut at 70 runes, and `true` or `false` for enabled. Installed and
available plugins are mixed and sorted by id. When the plugin store has a problem, such as an
install record that cannot be read, `list` still prints the plugins and writes
`tote: plugins: <problem>` on standard error, with `--json` too. The plugins that the problem
touches are listed as `broken`.

`--json` gives one record per plugin:

```json
{
  "schema": 1,
  "plugins": [
    {
      "id": "demo",
      "title": "Demo",
      "version": "1.1.1",
      "tier": "declarative",
      "kind": "local",
      "origin": "",
      "state": "enabled",
      "reasons": [],
      "enabled": true,
      "consented": true,
      "caps": ["exec:date", "exec:echo", "exec:git", "fs:read:repo", "host:read:repos"],
      "grant": "exec:date,exec:echo,exec:git,fs:read:repo,host:read:repos@058feb973609",
      "contributes": { "item_actions": 1, "commands": 2, "lists": 0, "settings": 1 }
    }
  ]
}
```

The real output indents the arrays of strings one item per line. Other records follow. The fields:

| Field                                    | Type             | Meaning                                                                                               |
| ---------------------------------------- | ---------------- | ----------------------------------------------------------------------------------------------------- |
| `id`, `title`, `version`, `tier`, `kind` | string           | From the record and the manifest. `title` is in the language of the run.                              |
| `origin`                                 | string           | `owner/repo` of a release, else empty.                                                                |
| `state`                                  | string           | A state of section 5, or `available`.                                                                 |
| `reasons`                                | array            | `{"code", "args", "message"}`, empty when enabled.                                                    |
| `enabled`                                | boolean          | `state` is `enabled`.                                                                                 |
| `consented`                              | boolean          | A consent is on record.                                                                               |
| `caps`                                   | array of strings | The effective capabilities of the installed version. For an available plugin, those it would ask for. |
| `grant`                                  | string           | The token of section 6.                                                                               |
| `contributes`                            | object           | The counts `item_actions`, `commands`, `lists` and `settings`.                                        |

### 8.6 `info`

```text
$ tote plugin info demo
demo 1.1.1 - Demo
description: Print a greeting and show the branch of a repository.
tier: declarative (no program of its own)
kind: local
source: /home/u/work/demo
state: enabled
license: MIT
homepage: https://example.org/demo
declared access:
  - exec:date
  - exec:echo
  - exec:git
  - fs:read:repo
  - host:read:repos
access you allowed:
  - exec:date
  - exec:echo
  - exec:git
  - fs:read:repo
  - host:read:repos
grant token: exec:date,exec:echo,exec:git,fs:read:repo,host:read:repos@058feb973609
requires:
  - echo: found at /usr/bin/echo
contributes:
  - command greet: Say hello
  - command today: Print the date
  - item action branch: Show the current branch
  - setting greeting: Greeting
settings:
  - greeting (string) = hello, from default
paths:
  installed files: /home/u/.local/share/tote/plugins/demo/01ddb77874bb4551a9bf53262545d7f8
  settings file: /home/u/.config/tote/plugins/demo.toml
  state directory: /home/u/.local/state/tote/plugins/demo
  cache directory: /home/u/.cache/tote/plugins/demo
  host directory: /home/u/.local/state/tote/plugins-host/demo
hashes:
  tree sha256: 01ddb77874bb4551a9bf53262545d7f854b7dd255fee379aa0cfaf6054b48349
  surface sha256: 058feb97360962bf20052fc7035d1084c2240be7950d9c789994a22bf6ca2852
```

`info` accepts an installed id and the id of a shipped plugin that is not installed. For the second,
the state is `available`, and nothing is read from disk but the copy inside tote.

`--json` has every field of a `list` record plus:

| Field                           | Type             | Meaning                                                                               |
| ------------------------------- | ---------------- | ------------------------------------------------------------------------------------- |
| `ok`                            | boolean          | `true`.                                                                               |
| `description`                   | string           | In the language of the run.                                                           |
| `contributions`                 | array            | `{"table", "id", "label"}` for each command, item action and setting, in file order.  |
| `license`, `homepage`, `source` | string           | Omitted when empty.                                                                   |
| `granted_caps`                  | array of strings | What you consented to.                                                                |
| `requires`                      | array            | `{"bin", "hint", "optional", "found", "path"}`.                                       |
| `settings`                      | array            | `{"id", "type", "value", "source"}`. `source` is `environment`, `file` or `default`.  |
| `paths`                         | object           | `tree`, `settings`, `state`, `cache`, `host`.                                         |
| `hashes`                        | object           | `archive_sha256`, `tree_sha256`, `surface_sha256`, each omitted when empty.           |
| `last_failure`                  | object           | `{"reason", "count", "at"}`. Only when failures are recorded.                         |
| `warnings`                      | array            | `{"code", "args", "message"}`: ignored settings, an unknown key in the settings file. |

### 8.7 `install` and `update`

```text
$ tote plugin install ./demo --grant exec:date,exec:echo,exec:git,fs:read:repo,host:read:repos@058feb973609
installed plugin demo 1.1.1 (enabled)
$ tote plugin install ./dep --grant exec:nosuchprog@845daba94637
installed plugin dep 0.1.0 (missing dependency)
plugin "dep" is not enabled (missing dependency):
  - missing dependency: nosuchprog
  fix: install the missing program
```

A plugin that is installed but not enabled is not an error: the exit is 0, and the state and reasons
tell why. The `--json` result:

```text
$ tote plugin install ./dep --json --grant exec:nosuchprog@845daba94637
{
  "schema": 1,
  "ok": true,
  "id": "dep",
  "version": "0.1.0",
  "updated": false,
  "state": "missing-dependency",
  "reasons": [
    {
      "code": "missing_dependency",
      "args": [
        "nosuchprog"
      ],
      "message": "missing dependency: nosuchprog"
    }
  ],
  "consented_by": "grant",
  "warnings": [],
  "leftovers": [],
  "already_installed": false
}
[exit 0]
```

| Field               | Type             | Meaning                                                                                                                                                                   |
| ------------------- | ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`, `version`     | string           | The installed plugin and version.                                                                                                                                         |
| `old_version`       | string           | The version it replaced. Only for an update.                                                                                                                              |
| `updated`           | boolean          | `true` for an update.                                                                                                                                                     |
| `already_installed` | boolean          | `true` when the plugin was installed already from the same source with the same files (section 7.2): nothing was written, and `state` and `reasons` are the current ones. |
| `state`, `reasons`  |                  | The state after the call, as in `list`.                                                                                                                                   |
| `consented_by`      | string           | `prompt`, `grant`, or empty when this call recorded no consent (an update that asked for nothing new).                                                                    |
| `warnings`          | array            | Findings of the manifest and the update rules that `--force` accepted.                                                                                                    |
| `leftovers`         | array of strings | Paths that could not be deleted. They wait for the next cleanup.                                                                                                          |

**`--dry-run`** prints the consent view, the token and `dry run: nothing was changed`. It reads the
package into a staging area, which is deleted when the command ends: on a machine that never
installed a plugin, the plugins directory it had to create goes away with it, and no file of tote is
left behind. Its JSON is:

```json
{
  "schema": 1,
  "ok": true,
  "dry_run": true,
  "plugin": {
    "id": "demo",
    "title": "Demo",
    "version": "1.1.1",
    "tier": "declarative",
    "kind": "local",
    "origin": "",
    "class": "local",
    "source": "/home/u/work/demo",
    "tree_sha256": "01ddb77874bb4551a9bf53262545d7f854b7dd255fee379aa0cfaf6054b48349",
    "surface_sha256": "058feb97360962bf20052fc7035d1084c2240be7950d9c789994a22bf6ca2852",
    "update": false,
    "caps": ["exec:date", "exec:echo", "exec:git", "fs:read:repo", "host:read:repos"],
    "added": ["exec:date", "exec:echo", "exec:git", "fs:read:repo", "host:read:repos"],
    "removed": [],
    "unchanged": 0,
    "commands": [
      { "table": "command", "id": "greet", "run": ["echo", "{setting.greeting}", "-n", "{name}"] },
      { "table": "command", "id": "today", "run": ["date", "+%F"] },
      { "table": "item_action", "id": "branch", "run": ["git", "branch", "--show-current"] }
    ],
    "missing": [],
    "warnings": [],
    "needs_consent": true,
    "why": ["new"],
    "grant": "exec:date,exec:echo,exec:git,fs:read:repo,host:read:repos@058feb973609"
  },
  "already_installed": false
}
```

The real output indents arrays one item per line. Next to `plugin` the object has
`"already_installed"`, which is `false` here and `true` when the plugin is installed already
(section 7.2). The fields of `plugin`:

| Field                                              | Meaning                                                                                                                                                                        |
| -------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `id`, `title`, `version`, `tier`, `kind`, `origin` | The new package.                                                                                                                                                               |
| `old_version`                                      | The installed version, only for an update.                                                                                                                                     |
| `class`                                            | `official`, `community`, `local` or `bundled`.                                                                                                                                 |
| `source`                                           | The source as recorded.                                                                                                                                                        |
| `archive_sha256`                                   | Only for a release or an archive.                                                                                                                                              |
| `tree_sha256`, `surface_sha256`                    | The hashes of section 2.2.                                                                                                                                                     |
| `update`                                           | `true` when a plugin with this id is installed.                                                                                                                                |
| `caps`                                             | The effective capabilities of the new version.                                                                                                                                 |
| `added`, `removed`, `unchanged`                    | Against the consent on record: capabilities to confirm, capabilities that are gone, and a count of the others.                                                                 |
| `commands`                                         | Each command in full: `table`, `id`, and `run` or `open`, plus `cwd`, `allow_dash` and `env` when set.                                                                         |
| `missing`                                          | Required programs that `PATH` does not have.                                                                                                                                   |
| `warnings`                                         | `{"code", "args", "message"}`: missing programs, same version, downgrade, pre-release, changed source. The `args` of `source_changed` are the recorded source and the new one. |
| `needs_consent`, `why`                             | Section 6.                                                                                                                                                                     |
| `grant`                                            | The token to pass to `--grant`.                                                                                                                                                |

A dry run of an update that asks for one more capability, from a real run:

```text
$ tote plugin update demo ./demo2 --force --dry-run
demo 1.2.0 asks for more access

Source: /home/u/work/demo2
Origin: local files, not verified
Checksum: sha256 51d94ab4b20d, computed here, nothing to compare it with
No code of its own: tote runs only the programs listed below.

Description (written by the plugin, not checked by tote)
  | Print a greeting and show the branch of a repository.

New access:
  + contact example.org (declared; tote cannot block it) [declared]
Unchanged: 5
No longer asked: 0
Commands: 0 added, 0 removed, 0 changed

! Installed from /home/u/work/demo, now offered by /home/u/work/demo2.

! tote runs only the listed programs, but those programs can do whatever you can do with them.

consent is asked because:
  - it asks for more access than you allowed
  - you forced a refusal with --force
Consent code exec:date,exec:echo,exec:git,fs:read:repo,host:read:repos,net:example.org@058feb973609

dry run: nothing was changed
to accept it without a terminal, run the command again without --dry-run and with --grant exec:date,exec:echo,exec:git,fs:read:repo,host:read:repos,net:example.org@058feb973609
```

### 8.8 `enable`, `disable`, `remove`

```text
$ tote plugin disable demo
plugin "demo" disabled, its settings, data and consent are kept
$ tote plugin enable demo
plugin "demo" enabled
$ tote plugin remove dep --yes
removed plugin "dep"
kept: state directory, cache directory (tote plugin remove --purge deletes them)
```

JSON results:

| Verb      | Fields besides `schema` and `ok`                                                                                          |
| --------- | ------------------------------------------------------------------------------------------------------------------------- |
| `enable`  | `id`, `state`, `reasons`, `already` (nothing changed), `consented` (this call recorded a consent), `config_file`          |
| `disable` | `id`, `already`, `killed` (process groups stopped), `config_file`                                                         |
| `remove`  | `id`, `killed`, `kept` (`settings`, `state`, `cache`), `leftovers`, `link` (the entry was a symbolic link), `config_file` |

`config_file` is the path of `config.toml` when tote could not edit it because you wrote the
`[plugins.<id>]` table in a layout it does not touch (dotted keys or an inline table). The command
still succeeded through the lock switch. A real `remove`:

```text
$ tote plugin remove execdemo --yes --json
{
  "schema": 1,
  "ok": true,
  "id": "execdemo",
  "killed": 0,
  "kept": [
    "state",
    "cache"
  ],
  "leftovers": [],
  "link": false
}
[exit 0]
```

### 8.9 `check`

The findings and the human output of `check` are for people and for diagnosis: the set of finding
codes, their order and the detail fields may change in a minor release. Only the exit codes and the
verdict are a promise (see [../compat.md](../compat.md), section 8).

`check` reads a plugin and reports; it changes nothing and starts no program that the plugin names
(`--repo` makes tote run a read-only `git` of its own, see section 4.1). The target is an installed
id, the id of a shipped plugin, a directory or an archive file. Without a target, it checks every
installed plugin and the plugin store.

```text
$ tote plugin check ./demo --strict
checking directory: ./demo
demo 1.1.1 (declarative)
what it runs:
  - command greet: echo hello -n world
    in /home/u/work
  - command today: date +%F
    in /home/u/work
  - item action branch: git branch --show-current
    cannot be built: repo is unavailable (unknown)
requires:
  - echo: found at /usr/bin/echo
access it asks for:
  - exec:date
  - exec:echo
  - exec:git
  - fs:read:repo
  - host:read:repos
hashes:
  tree sha256: 01ddb77874bb4551a9bf53262545d7f854b7dd255fee379aa0cfaf6054b48349
  surface sha256: 058feb97360962bf20052fc7035d1084c2240be7950d9c789994a22bf6ca2852
grant token: exec:date,exec:echo,exec:git,fs:read:repo,host:read:repos@058feb973609
./demo: no problem found
```

Each command is built with the **defaults** of its fields. A contribution that cannot be built says
why and is not a failure: here the item action needs a repository. `--argv` with `--set` and
`--repo` supplies the values:

```text
$ tote plugin check demo --argv --set name=Ada --repo proj
...
what it runs:
  - command greet: echo hello -n Ada
    in /home/u/work
  - command today: date +%F
    in /home/u/work
  - item action branch: git branch --show-current
    in /home/u/work/proj
...
$ tote plugin check demo --argv --set name=--force
...
  - command greet: echo '{setting.greeting}' -n '{name}'
    cannot be built: the value of name is not valid (does not match the pattern)
...
```

The second command shows that the validation of a field runs before the command is built. `--set`
names a field of any contribution. When none has it, `check` adds the warning `set_unknown` (scope
`request`), which does not fail the check.

A manifest with errors, with the findings in file order:

```text
$ tote plugin check ./bad
checking directory: ./bad
bad 0.1.0 (declarative)
  error: colour, line 8: unknown key
  error: command[0].run[1], line 16: {folder} is not a known placeholder
  error: command[0].run[0], line 16: the command runs ls, which no exec: permission covers
  error: hook: hook is not available in this version of tote (reserved for a later one)
hashes:
  tree sha256: dae9b9525ff7bd485327ab2bce26ee5a02296d590f61046e94b89d066421aae4
./bad: errors 4, warnings 0
[exit 1]
```

**Findings.** Each finding has a `scope`:

| Scope         | About                                                                                                         |
| ------------- | ------------------------------------------------------------------------------------------------------------- |
| `manifest`    | `plugin.toml`.                                                                                                |
| `package`     | The files of the package.                                                                                     |
| `locale`      | The translations.                                                                                             |
| `pin`         | A shipped plugin that differs from the checksum built into tote (`pin_mismatch`).                             |
| `state`       | The state of an installed plugin.                                                                             |
| `environment` | The machine: a program that `[[requires]]` names is not installed (`missing_dependency`, `optional_missing`). |
| `settings`    | The values and the file of the settings (`settings_ignored`, `settings_file`, `settings_unknown_key`).        |
| `request`     | Your command line (`set_unknown`).                                                                            |

A finding has a severity, `error` or `warning`. The warnings of the manifest are `exec_unused`,
`argv.risky_flag`, `key_clash`, `key_reserved` and `deprecated_key`, and those of the translations
are `locale_missing_key` and `locale_extra_key`. Every other code of the table below is an error.

**Codes of the manifest, the package and the translations.** The text of each is in the catalog key
`plugin_err_<code>`, where a dot of the code (`argv.shell`) is written `_` (`argv_shell`).

| Code                                                                                                         | Meaning                                                                                                                                                                                                                                                                             |
| ------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `manifest_too_large`, `manifest_syntax`                                                                      | `plugin.toml` is over its size limit, or is not valid TOML.                                                                                                                                                                                                                         |
| `manifest_missing`, `manifest_nested`                                                                        | There is no `plugin.toml` at the root of the package. With `manifest_nested` the package sits in one folder, whose name is the argument.                                                                                                                                            |
| `key_missing`                                                                                                | A required key is absent.                                                                                                                                                                                                                                                           |
| `unknown_key`                                                                                                | A key that API 1 does not have.                                                                                                                                                                                                                                                     |
| `type_invalid`                                                                                               | A value has the wrong TOML type.                                                                                                                                                                                                                                                    |
| `value_invalid`                                                                                              | A value of the right type that is not allowed: an enum value, a duration, an empty text or a text of spaces where one is required (title, description, labels, choices), an enum with no choice, an empty `column` array, a default that does not fit its field, `min` above `max`. |
| `text_invalid`                                                                                               | A text holds a control, bidirectional or zero-width character.                                                                                                                                                                                                                      |
| `limit_exceeded`                                                                                             | A count or a length is over a limit of section 3.                                                                                                                                                                                                                                   |
| `key_conflict`                                                                                               | Two keys that exclude each other, such as `run` and `open`.                                                                                                                                                                                                                         |
| `id_invalid`, `id_duplicate`                                                                                 | An identifier that does not follow section 3.3 (an empty plugin id included), or is used twice in one table.                                                                                                                                                                        |
| `version_invalid`                                                                                            | A version that is not a strict Semantic Version, an empty one included.                                                                                                                                                                                                             |
| `reserved_not_available`                                                                                     | A table, a key, a type, an event or a permission that is reserved (section 3.13), or `tier = "exec"`: the exec tier is not available in tote 1.0, so the manifest is incompatible (section 10).                                                                                     |
| `api_unsupported`, `range_invalid`, `tote_range`, `feature_missing`, `platform_unsupported`, `platform_none` | The compatibility rules of section 3.4.                                                                                                                                                                                                                                             |
| `tier_mismatch`                                                                                              | A table of the exec tier (`[exec]`, `[[platform]]`, `[[action]]`, `[[list]]`) in a declarative manifest.                                                                                                                                                                            |
| `perm_unknown`, `perm_invalid`, `perm_refused`                                                               | A permission that tote does not know, that is malformed, or that API 1 never grants.                                                                                                                                                                                                |
| `perm_missing`                                                                                               | A feature that needs a permission the manifest does not declare (`host:read:repos`, `host:open-url`).                                                                                                                                                                               |
| `exec_self`, `exec_uncovered`                                                                                | A command runs tote itself, or a program that no `exec:` permission names.                                                                                                                                                                                                          |
| `exec_unused`                                                                                                | Warning. An `exec:` permission that no command runs.                                                                                                                                                                                                                                |
| `env_uncovered`, `env_refused`                                                                               | An `env` table sets a name without an `env:` permission, or a name that is blocked.                                                                                                                                                                                                 |
| `placeholder_syntax`, `placeholder_unknown`, `placeholder_scope`, `placeholder_list`                         | A placeholder that is malformed, unknown, not available in that place, or a list used inside a longer argument.                                                                                                                                                                     |
| `argv0_invalid`                                                                                              | The first element of `run` is not a plain program name.                                                                                                                                                                                                                             |
| `argv.shell`, `argv.ssh_no_dashdash`, `pattern.unanchored`                                                   | A shell or launcher is run, `ssh` is run without `--` before the host, or a `pattern` is not anchored.                                                                                                                                                                              |
| `argv.risky_flag`                                                                                            | Warning. A program runs with an option that can start another program (`git -c`).                                                                                                                                                                                                   |
| `key_clash`, `key_reserved`                                                                                  | Warnings. An accelerator used twice, or a core key (section 3.9).                                                                                                                                                                                                                   |
| `deprecated_key`                                                                                             | Warning. A key that has a replacement.                                                                                                                                                                                                                                              |
| `declarative_extra_file`                                                                                     | A file other than `plugin.toml`, `locales/` and the documentation files in a declarative package.                                                                                                                                                                                   |
| `bin_missing`, `bin_not_executable`, `exec_undeclared`                                                       | Exec tier package files: a program that is declared and absent or not executable, or an executable file that is not declared. tote 1.0 never reaches them, because it refuses the exec tier first.                                                                                  |
| `locale_invalid`                                                                                             | A locale file that does not parse, or a value over its limits.                                                                                                                                                                                                                      |
| `locale_missing_key`, `locale_extra_key`                                                                     | Warnings, errors under `--strict`: a key the manifest has and a translation lacks, or the reverse.                                                                                                                                                                                  |

**The verdict.** A `check` fails (`"ok": false`, exit 1) when it has any error. Under `--strict` it
also fails on any warning about the package (`manifest`, `package`, `locale`, `pin`). A warning
about the machine, the state or your settings never fails a check, because the package is not at
fault. One rule for the labels: under `--strict` the translation warnings (`locale_missing_key`,
`locale_extra_key`) become errors, printed as `error` and counted as errors; every other warning
about the package stays a `warning` in the text and in the counts (`"errors": 0, "warnings": 1`),
but the check still fails (`"ok": false`, exit 1).

```text
$ tote plugin check ./lint
checking directory: ./lint
lint 0.1.0 (declarative)
  error: permissions[2], line 8: sh is a shell or a launcher: a command may not run it
  warning: permissions[4], line 8: the permission exec:cat is declared but no command runs it
  warning: permissions[3], line 8: the permission exec:ls is declared but no command runs it
  warning: permissions[2], line 8: the permission exec:sh is declared but no command runs it
  warning: command[0].run[1], line 13: git is run with -c, an option that can start another program
  error: command[1].run[1], line 19: ssh is run without -- before the host, so a host name could pass for an option
  error: command[1].field[0].pattern, line 25: the pattern [a-z]+ does not start with ^ and end with $
hashes:
  tree sha256: 870bb08a531e24a2cdfba947a5ea47cd59c044755273e7691dc2212414da2942
./lint: errors 3, warnings 4
[exit 1]
```

**An incompatible manifest** is reported with the flag `incompatible`:

```text
$ tote plugin check ./inc --json
{
  "schema": 1,
  "ok": false,
  "errors": 1,
  "warnings": 0,
  "findings": [
    {
      "scope": "manifest",
      "code": "tote_range",
      "severity": "error",
      "path": "tote",
      "line": 4,
      "column": 1,
      "args": [
        ">=2.0.0",
        "1.0.0"
      ],
      "incompatible": true,
      "message": "this plugin needs tote >=2.0.0, you have 1.0.0"
    }
  ],
  "target": "./inc",
  "kind": "directory",
  "id": "inc",
  "version": "0.1.0",
  "tier": "declarative",
  "strict": false,
  "tree_sha256": "eec60c61280f9da1be12fafdbb0ce6e07bb091813546abda82d366516cbe0dfe"
}
[exit 1]
```

**An archive** adds the line to publish next to a release:

```text
$ tote plugin check ./demo.tar.gz
...
hashes:
  archive sha256: d58ae94a79badae26cae83692e1510b38f003c7895afa41306cca4f411f2468c
  tree sha256: 01ddb77874bb4551a9bf53262545d7f854b7dd255fee379aa0cfaf6054b48349
  surface sha256: 058feb97360962bf20052fc7035d1084c2240be7950d9c789994a22bf6ca2852
grant token: exec:date,exec:echo,exec:git,fs:read:repo,host:read:repos@058feb973609
publish next to the release, as demo.tar.gz.sha256:
  d58ae94a79badae26cae83692e1510b38f003c7895afa41306cca4f411f2468c  demo.tar.gz
./demo.tar.gz: no problem found
```

**`--json` of one target** has these fields:

| Field                                             | Type    | Meaning                                                                                                                                                           |
| ------------------------------------------------- | ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `schema`, `ok`, `errors`, `warnings`              |         | `ok` is the verdict above.                                                                                                                                        |
| `findings`                                        | array   | `{"scope", "code", "severity", "path", "line", "column", "args", "incompatible", "message"}`. `path`, `line`, `column` and `incompatible` are omitted when empty. |
| `target`, `kind`                                  | string  | What was checked: `installed`, `directory`, `archive` or `bundled`.                                                                                               |
| `id`, `version`, `tier`, `title`                  | string  | From the manifest, when it could be read.                                                                                                                         |
| `state`                                           | string  | Only for an installed target.                                                                                                                                     |
| `strict`                                          | boolean | The flag.                                                                                                                                                         |
| `contributions`                                   | array   | What each contribution would run (below).                                                                                                                         |
| `requires`                                        | array   | `{"bin", "hint", "optional", "found", "path"}`.                                                                                                                   |
| `caps`, `grant`                                   |         | The effective capabilities and the token.                                                                                                                         |
| `archive_sha256`, `tree_sha256`, `surface_sha256` | string  | The hashes, when known.                                                                                                                                           |

A contribution of `contributions`:

| Field                         | Meaning                                                                                                                                          |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `table`, `id`, `label`        | `command` or `item_action`, its id and label.                                                                                                    |
| `output`                      | `pager`, `silent` or `terminal`.                                                                                                                 |
| `run`, `open`, `env`          | The templates, and the names of the `env` table.                                                                                                 |
| `argv`                        | The arguments after substitution. Only when they could be built.                                                                                 |
| `display`                     | `argv` written as a shell would print it, or the template when it could not be built.                                                            |
| `dir`, `url`                  | The working directory. The address an `open` would show.                                                                                         |
| `program`                     | The absolute path found on `PATH`. Empty when the program is not installed.                                                                      |
| `confirm_required`, `confirm` | Whether a confirmation is asked, and its text.                                                                                                   |
| `available`                   | `false` when it cannot be built.                                                                                                                 |
| `why`                         | `{"code", "args"}`: the reason when it is unavailable. `dependency_missing` for a program that is not installed (the arguments are still exact). |

**Without a target**, `check` returns the diagnosis of the plugin store:

```text
$ tote plugin check
installed plugins
demo 1.1.1 (enabled)
demo: no problem found
leftover: cache directory execdemo
leftover: state directory execdemo
plugins checked: 1
```

Its JSON is `{"schema", "ok", "strict", "problems", "plugins", "orphans", "leftovers", "findings"}`.
`plugins` is a list of the single-target results above. `orphans` are `{"kind", "name"}`: a file or
directory in the plugins directory that is not a plugin, or a record whose id is not valid.
`leftovers` are `{"kind", "id", "name", "age_seconds"}`: what a removal kept, an unfinished install,
deleted files waiting for removal. A leftover is information, never a failure.

### 8.10 Codes of a refused call

These are the `error.code` values of the verbs. A later release may add codes; a code that is listed
here keeps its meaning and its exit code. The exit codes are those of section 8.2. The codes of
section 8.9 appear as the code of an error only when an install or an update refuses a package and
its first error is one that the `manifest_invalid` row names; any other first error gives
`manifest_invalid`.

| Code                                                                                 | Exit                     | Meaning                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| ------------------------------------------------------------------------------------ | ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `usage`                                                                              | 2                        | The command line is wrong.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `not_found`                                                                          | 1                        | `check --repo` names no saved repository.                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `failure`                                                                            | 1                        | A failure without a plugin code.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `not_installed`                                                                      | 1                        | No plugin has this id.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `bad_id`                                                                             | 2                        | The id is not a valid plugin id.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `bad_source`                                                                         | 2                        | The source is none of the forms of section 7.1, or the path does not exist.                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `source_unsupported`                                                                 | 2                        | `check` was given a release URL or `owner/repo@tag`. It never downloads. Install it, or check the archive file.                                                                                                                                                                                                                                                                                                                                                                                                   |
| `source_required`                                                                    | 2                        | An update of a plugin from a release needs the new release as its source.                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `no_embedded`                                                                        | 2                        | No plugin of this name ships with tote.                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `source_unsafe`, `source_limits`                                                     | 1                        | A directory source that is unsafe or over a limit.                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `hash_required`, `hash_malformed`, `hash_unexpected`                                 | 2                        | Problems with `--sha256`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| `consent_needed`                                                                     | 2                        | Consent is needed and there is no terminal.                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `confirm_needed`                                                                     | 2                        | `--yes` is needed and there is no terminal. On the run path, which no verb of 1.0 reaches, it also means that a command with `confirm` was started before the screen reported the user's confirmation.                                                                                                                                                                                                                                                                                                            |
| `grant_mismatch`                                                                     | 4                        | The `--grant` token is not the current one.                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `enable_refused`                                                                     | 4, or 5                  | The plugin cannot be enabled. 5 when the files changed.                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `from_plugin`                                                                        | 4                        | A plugin started the command.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `busy`                                                                               | 4                        | Another tote is changing plugins.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `stale`                                                                              | 4                        | The plugin changed while the call ran.                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `manifest_invalid`                                                                   | 1                        | The manifest has errors, and the first one has no code of its own for an install. When the first error is `id_invalid`, `perm_unknown`, `perm_refused`, `api_unsupported`, `tote_range`, `platform_none`, `reserved_not_available`, `declarative_extra_file`, `exec_self`, `exec_uncovered`, `perm_missing`, `manifest_too_large` or `bin_missing`, the error has that code instead (exit 4 for the three of `exec_self`, `exec_uncovered` and `perm_missing`, 1 for the others). The message is the first error. |
| `input_invalid`, `dash_refused`, `no_entry`                                          | 2                        | Only on the run path, which no verb of 1.0 reaches.                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `id_mismatch`, `downgrade`, `prerelease`, `source_changed`                           | 1                        | An update rule of section 7.2.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `tag_mismatch`                                                                       | 1                        | The tag is not the version.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `hash_mismatch`, `platform_hash`                                                     | 5                        | The bytes do not match their pin.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `archive_format`, `archive_unsafe`, `archive_limits`                                 | 5                        | The file is not a valid `.tar.gz`, an entry is unsafe, or a limit of section 2.1 is crossed.                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `too_large`, `origin_redirect`, `tree_collision`                                     | 5                        | The download is over 16 MiB, the release moved, two trees collide.                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `net_status`                                                                         | 3 for status 401, else 1 | The download was answered with a status.                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `net_offline`, `net_timeout`, `net_tls`, `net_redirect`, `net_truncated`             | 1                        | A download failed.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `tree_hash`, `tree_perms`, `tree_owner`, `tree_symlink`, `tree_special`, `tree_name` | 5                        | The installed files changed.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `store_corrupt`, `disk`                                                              | 1                        | The lock cannot be read, or a write failed. Nothing changed.                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| `store_writable`                                                                     | 1                        | The plugins directory, or the directory of a plugin, can be written by other users: run `chmod go-w` on it. Plugins are off until then.                                                                                                                                                                                                                                                                                                                                                                           |
| `source_read`                                                                        | 1                        | A file or a folder of the source cannot be read. The arguments are the path and the reason.                                                                                                                                                                                                                                                                                                                                                                                                                       |

## 9. The Plugins screen

The terminal interface has a Plugins screen. It lists the installed plugins and the plugins that
ship with tote, with their state and the reasons for it, and it works on the same operations as the
verbs of section 8. The command line is the reference: this document does not describe the screen or
its key bindings, and a script should use the verbs.

## 10. Not in 1.0

These names are reserved. tote 1.0 reads some of them, checks them, and refuses or ignores them as
described. None of them works, and nothing in this reference promises that it will.

- **The exec tier.** A plugin that runs a program of its own (`tier = "exec"`, with an `[exec]`
  table, `[[platform]]` builds, `[[action]]` and `[[list]]`) is not available in tote 1.0. A
  manifest that says `tier = "exec"` is refused with `reserved_not_available` on the key `tier`, as
  an incompatibility, and nothing else of it is read. `tote plugin check` reports it, `install` and
  `update` refuse it (exit 1, nothing is staged or changed), and a plugin of that tier that is
  already in the store, left by an earlier tote, is listed as `incompatible` with that reason: it
  cannot be enabled and it never runs. The features `exec.list` and `exec.run` are not announced, so
  a manifest that needs one is incompatible (`feature_missing`). A declarative manifest with an
  `[exec]` table, `[[platform]]`, `[[action]]` or `[[list]]` is `tier_mismatch`. Do not write or
  publish a plugin of the exec tier for 1.0.
- **`plugin run`, `plugin search`, a registry and revocation.** The verbs are unknown:

  ```text
  $ tote plugin run demo greet --json
  {
    "schema": 1,
    "ok": false,
    "error": {
      "code": "usage",
      "message": "unknown subcommand \"run\"",
      "exit": 2
    }
  }
  [exit 2]
  ```

  tote never asks the network what exists or what is new. There is no `revoked` state: `disable` and
  `remove` are the recall.

- **Secrets for plugins.** There is no secret field or setting.
- **Reserved names:** section 3.13.
- **Everything else that a later release adds** follows the rules of [../compat.md](../compat.md):
  new optional keys only, a key counts in `surface_sha256` only when it is set, and a manifest that
  needs one says so with a `tote` range or a `requires_features` name.
- **`ghrel` and `ghsec`.** Only `gft` ships with tote 1.0.

## 11. A complete example

This is the plugin used for the examples above. It has a setting, a command with a form, a command
without one, an item action, a required program with a hint, and a French translation.

`plugin.toml`:

```toml
api = 1
id = "demo"
version = "1.1.1"
tote = ">=1.0.0, <2"
title = "Demo"
description = "Print a greeting and show the branch of a repository."
license = "MIT"
homepage = "https://example.org/demo"
tier = "declarative"
permissions = ["exec:date", "exec:echo", "exec:git", "fs:read:repo", "host:read:repos"]

[[requires]]
bin = "echo"
hint = "Install the coreutils package."

[[setting]]
id = "greeting"
type = "string"
label = "Greeting"
description = "The word that the greet command prints first."
default = "hello"
pattern = "^[A-Za-z ]{1,20}$"

[[command]]
id = "greet"
label = "Say hello"
run = ["echo", "{setting.greeting}", "-n", "{name}"]
output = "pager"

[[command.field]]
id = "name"
type = "text"
label = "Name"
default = "world"
required = true
pattern = "^[A-Za-z]{1,20}$"

[[command]]
id = "today"
label = "Print the date"
run = ["date", "+%F"]
output = "silent"

[[item_action]]
id = "branch"
target = "repo"
label = "Show the current branch"
run = ["git", "branch", "--show-current"]
output = "pager"
```

`locales/fr.toml`:

```toml
"title" = "Demo"
"description" = "Affiche un message et la branche d'un depot."
"requires.echo.hint" = "Installez le paquet coreutils."
"setting.greeting.label" = "Salutation"
"setting.greeting.description" = "Le mot que la commande greet affiche en premier."
"command.greet.label" = "Dire bonjour"
"command.greet.field.name.label" = "Nom"
"command.today.label" = "Afficher la date"
"item_action.branch.label" = "Afficher la branche courante"
```

Check it with `tote plugin check ./demo --strict`: the output is the one in section 8.9, and the
exit status is 0. The plugin asks for five permissions. Three are `exec:` and enforced, one is
`host:read:repos` and enforced because of `{repo.*}` and `target = "repo"`, and `fs:read:repo` is
declared only.
