# Compatibility rules of the 1.0 formats

This page is normative. It lists what tote 1.0 writes and reads, and what every later 1.x release
keeps. A rule says "must" for the release that changes a format and "can rely on" for what a script,
a plugin or a hand-edited file may count on. The plugin manifest has its own details in
[plugins/API1.md](plugins/API1.md); the release contracts are in [RELEASING.md](RELEASING.md).
Section 8 says what is not promised.

## 1. What "schema 1" guarantees

Every file tote writes starts with `schema = 1` (the record files, `config.toml`,
`plugins/<id>.toml`, `plugins.lock`; a file written by hand may omit it, which reads as 1), and so
does every JSON document of the command line and the install receipt. Inside schema 1 a release may
only **add optional things**: a key, a field, a table, an enum value. It never removes, renames,
retypes or re-interprets one, and the absence of a new key means what it meant in 1.0. A change that
cannot be made that way is schema 2, and then a 1.x tote leaves the file alone: a record file or
`plugins.lock` with another schema is read-only (every plugin is `broken`), `config.toml` is ignored
with a `FAIL` of `tote doctor`.

A reader **keeps what it does not know**. Every write of tote 1.0 leaves the unknown keys of a
record, of a `plugins.lock` record and of a settings file where they are, with the comments of the
file, and so it does a top-level table that the file does not know (a later `[via.mosh]` next to the
`[[connection]]` records). The install receipt, which is JSON, keeps its unknown fields too, after
the known ones and in alphabetical order.

## 2. Records: extension keys and reserved fields

A plugin or a later release stores its own data in a record as a **dotted key or an inline table
under the plugin id, inside the record block**:

```toml
[[connection]]
name = "web"
target = "host1.example.org"
konsolai.layout = "tall"            # dotted key
# konsolai = { layout = "tall" }    # or an inline table
```

A sub-table header (`[connection.konsolai]`) is never used. tote 1.0 reads a record file with
`[connection.konsolai]` as a syntax error (`sub-tables of connection are not supported`) and makes
the file read-only. The id is a valid bare TOML key, since a plugin id is `[a-z0-9-]`. tote 1.0
reads and writes no extension key itself, and keeps all of them.

The **reserved fields** are names that 1.0 does not read yet and keeps like any unknown key. Their
types are fixed now:

| Fields                                                              | Type                |
| ------------------------------------------------------------------- | ------------------- |
| `tags`                                                              | array of strings    |
| `fav`, `hidden`                                                     | boolean             |
| `port`                                                              | integer, 1 to 65535 |
| `key`, `jump`, `mac`, `attach`, `session`, `mode`, `when`, `remote` | string              |
| `opener`                                                            | string, or a table  |

The fields of 1.0 itself (`name`, `via`, `target`, `user`, `aliases`, `ip`, `desc`, `needs_review`,
`path`, `cmd`, `kind`, `script`, `managed`) are strict: a wrong type makes the file read-only, with
the line and the column. A **reserved field with a wrong type is not that**: a release that starts
reading it must treat it as unknown, warn, and keep the file usable. tote 1.0 ignores such a field
without a warning. `scheme`, `start`, `health` and `cwd` are reserved names too.

The text fields (`cmd`, `path`, `script`, `target`, `via`, ...) hold 4096 bytes at most, and a `via`
holds no quote, because it is split on spaces and runs without a shell. A record that breaks one of
these rules in a file is **invalid** and not a syntax error: the file stays usable, `tote doctor`
names the record, the shell skips it and the record is kept as it is when tote rewrites the file.

`managed` on a function is stored and shown and has no other effect in 1.0. A later release may act
on a managed script only when it lies under the `bin` folder of the data directory, never anywhere
else.

## 3. Names reserved for later

- **Record names.** No connection (name or alias), alias or repository is named
  `add rm list ls open edit import find status tunnel tunnels snippets sync refresh restore keep pick export tags fav`,
  compared without regard to case, nor starts with `__tote_` or `_tote`. A function may not start
  with those prefixes. `add` and the forms refuse them; a hand-edited file keeps loading and
  `tote doctor` warns. A later reservation only widens the refusal of `add`.
- **File names.** `snippets.toml`, `tunnels.toml` and `ports.toml` (each with its `.lock`) are
  reserved in the config directory, and `tote uninstall --purge` already covers them.
- **Plugin ids.** The verbs and words of tote, the planned ones, the record fields above, the prefix
  `tote-` and the official ids (`gft`, `ghrel`, `ghsec`, `konsolai`, `ssh-tools`), listed in
  [plugins/API1.md](plugins/API1.md) section 3.3. **A name reserved later never breaks installed
  data**: an installed plugin keeps loading, running and updating under its id; only a check, a new
  install and an update from a new source are refused.
- **Plugin settings.** `TOTE_PLUGIN_<ID>_<SETTING>`: each `-` of the id becomes `__`, and a setting
  id never holds `__`, so no two pairs share a variable. In `plugins/<id>.toml`, `schema` and every
  root key that starts with `_` belong to tote.

## 4. The plugin manifest

- A key added later is optional, and its zero value means the behaviour of 1.0. A manifest that
  needs it says so with a `tote` range or a `requires_features` name; tote 1.0 then reports the
  plugin as **incompatible** (`tote_range`, `feature_missing`) and not as invalid.
- `surface_sha256` counts a key only when it is set, so a new key leaves the hash, the consent and
  every stored `--grant` token of an installed plugin alone.
- `plugin.schema.json` checks a feature name by its grammar (`^[a-z][a-z0-9_.]{0,63}$`) and not by a
  list, so a manifest of a later release stays valid for an older schema.

## 5. Output

- **JSON.** A document is one object whose first key is `"schema": 1`. A later release only adds
  keys; a key keeps its type and its meaning. The `error` of a failed call is always the object
  `{"code", "message", "exit"}`, for `tote plugin` and for `tote update --json`, which then prints
  nothing on standard error. A misuse of `tote update --json` (an unknown option, an extra word) is
  that object too, with `"code": "usage"` and exit 2. The values of `state`, `reason`, `kind`,
  `status`, finding codes and error codes are **open**: a script must tolerate a value it does not
  know. `message` and the human text change with the language and the release and are not a contract
  (section 8). After a non-zero exit ignore every other output.
- **TSV** (`--format tsv`, the default in a pipe). No header, one line per record, columns are
  **positional** and new ones are only appended at the end. A tab, a newline and a carriage return
  in a cell are written `\t`, `\n` and `\r`.

  | List             | Columns                                                                     |
  | ---------------- | --------------------------------------------------------------------------- |
  | `co`             | name, via, target, user, aliases (comma separated), ip, desc                |
  | `alias`, `repos` | name, cmd or path, desc                                                     |
  | `funcs`          | name, kind, script, managed (`true` or `false`), desc                       |
  | `plugin`         | id, version, state, origin or kind, description (70 runes at most), enabled |

  `needs_review` of a connection is in the JSON only.

- **Exit codes.** 0 done, 1 failure, 2 usage, 3 authentication required (a download answered 401), 4
  refused by state or policy, 5 integrity, 10 `update --check` found a release, 127 a program could
  not be started, 130 interrupted (Ctrl-C or SIGTERM, also during a download; the error code is
  `interrupted`). These keep their meaning. New codes use 6 to 9 and 11 and above. The program that
  a later `plugin run` starts never has its status turned into the exit code of tote: tote reports
  it in the JSON and returns a code of its own. `tote co <name>` replaces tote by the connection
  command, so there the status is the command's.
- **Hand-off.** The shell function reads the first line `v1 <action>[,<flag>]` of `TOTE_HANDOFF`,
  ignores an action it does not know, and matches a flag as a whole word, so a flag can be named
  `reload2`. A function that a running shell has loaded cannot be replaced.

## 6. Where tote keeps things

- The four directories come from `TOTE_CONFIG_DIR`, `TOTE_DATA_DIR`, `TOTE_STATE_DIR` and
  `TOTE_CACHE_DIR`, then from `XDG_*_HOME`, then from the defaults. A file keeps its name and its
  directory for the whole of 1.x.
- **`config.toml`** holds the settings and, per plugin, `[plugins.<id>]` with `enabled` only. Per
  plugin data of a later release goes to `plugins.lock` or to `plugins/<id>.toml`, never to the
  `[plugins.<id>]` tables. An unknown key or an invalid value is a warning (stderr and
  `tote doctor`), the setting keeps its default and the rest of the file applies. Only a syntax
  error, a schema other than 1, a file too deep, too big (64 KiB) or not regular is refused as a
  whole.
- **The install receipt** `install.json` of the state directory stays `"schema": 1`; a later release
  only adds optional fields and tote keeps the ones it does not know when it rewrites it.
  `plugin-procs` lines are `<id> <session> <start>` and a reader ignores any further field.
- **`tote uninstall --purge`** removes only the names of `config.Paths.Owned()`: the five record
  files and `config.toml` with their `.lock` files, the reserved file names of section 3, the
  `plugins` folders (settings, installed files, state, cache, host files), `plugins.lock`,
  `plugin-procs`, the receipt, the update cache and `bin`. That list grows with the files a release
  adds. In a directory not named `tote`, a folder is removed only when it is empty.

## 7. Release signing keys

A release is signed with a key that every supported client already embeds. A new key reaches the
clients only through a bridge release that embeds it first, and a build ships at least two keys. A
client that meets an unknown key says `unknown_key`, not `bad_signature`. See RELEASING.md,
"Rotating the key".

## 8. What is not promised

Sections 1 to 7 are the whole promise of 1.x: the file formats (schema 1), the plugin manifest, the
exit codes, and the JSON documents (their keys, types and meanings, `tote plugin list` and
`tote plugin info` included). Nothing else is, and in particular:

- **The words of a message.** The text of any message, hint or error may change in a minor release,
  and not only with the language. The `code` and the `exit` of an error, the `args` of a finding and
  the keys of the JSON stay. A script matches codes, never words.
- **The human-readable output.** Without `--json`, the output of every verb (a table, a summary, the
  lines of `tote plugin check`, the text of `tote doctor`) can change its layout, order, labels and
  wording. Only `--json` and the TSV forms of section 5 are covered.
- **The detail of `tote plugin check`.** The set of finding codes may grow in a minor release, and a
  new check can report something about a package that passed before; a code that exists keeps its
  meaning. The order of the findings and the entries of `contributions` and `requires` may change.
  The nine keys of a finding (`scope`, `code`, `severity`, `path`, `line`, `column`, `args`,
  `incompatible`, `message`; `message` is human text, see above) keep their names and types. The
  exit codes and the verdict stay: a check fails (`"ok": false`, exit 1, or 5 for an integrity
  error) when it has an error.
- **New refusal codes.** The codes of a refused call (API1.md, section 8.10) may gain new ones. A
  code that exists keeps its meaning and its exit code; a script treats an unknown code by its exit
  code.
