# Writing a tote plugin

This is a tutorial. You write a small plugin called `demo`, check it, install it, change it, pack it
for a release and remove it. Every command and every output below was run with a real build of tote;
only the paths differ from yours. An output that is cut keeps the lines that matter, and `...` marks
the cut.

Only the declarative tier exists in 1.0. A declarative plugin has no code of its own: it is a folder
with a `plugin.toml` that tells tote which programs to run and which form to ask first. If you need
your own program, read [Not in 1.0](#not-in-10) first.

The `demo` plugin of this tutorial is its own: the `demo` of the other two documents is another
plugin, so their hashes and grant tokens differ from the ones here.

Companion documents:

- [API1.md](API1.md) is the reference: every key, type, limit and error.
- [SECURITY.md](SECURITY.md) is the security statement: what a plugin can do and what tote checks.
- [../compat.md](../compat.md) lists what every 1.x release keeps (the formats, the reserved names,
  the JSON output and the exit codes) and what it does not promise.

## 1. What a plugin is

A plugin is a folder:

```
demo-plugin/
  plugin.toml        the manifest, required
  locales/en.toml    optional: English texts that the manifest cannot carry
  locales/fr.toml    optional: French texts
  README.md          optional (README* and LICENSE* are allowed)
  LICENSE
```

Nothing else may be in it. A script, a binary or any other file is refused
(`declarative_extra_file`), so no program travels under the declarative label.

The manifest declares **contributions**. A declarative plugin has three kinds:

| Table             | What it is                                           | Where it shows up                                                      |
| ----------------- | ---------------------------------------------------- | ---------------------------------------------------------------------- |
| `[[command]]`     | A program to run, with an optional form.             | The commands that tote offers.                                         |
| `[[item_action]]` | The same, on a saved repository (`target = "repo"`). | The actions offered for a saved repository.                            |
| `[[setting]]`     | A typed value that the user can change.              | The settings of the plugin; read by your commands as `{setting.<id>}`. |

`[[action]]` and `[[list]]` belong to the exec tier, which tote 1.0 does not have. A declarative
manifest that holds one is refused (`tier_mismatch`). The "list" of this tutorial is a **list
setting**: a setting of `type = "list"` that expands into zero or more arguments.

Three rules explain most of what follows.

1. **tote builds the command line, never a shell.** Each element of `run` is one argument. A value
   typed by the user is substituted into one element and stays one argument, whatever it contains.
2. **Nothing runs without consent.** The user sees every command you wrote, in full, before the
   plugin is installed.
3. **The programs run with the rights of the user.** tote runs only what you list, but `grep`, `git`
   or `curl` can do whatever the user can do with them. Declare honestly what you use.

## 2. Set up a sandbox

Try everything with a private set of directories so that your real configuration stays alone:

```sh
export SANDBOX=~/tote-sandbox
mkdir -p $SANDBOX
export TOTE_CONFIG_DIR=$SANDBOX/.config/tote
export TOTE_DATA_DIR=$SANDBOX/.local/share/tote
export TOTE_STATE_DIR=$SANDBOX/.local/state/tote
export TOTE_CACHE_DIR=$SANDBOX/.cache/tote
tote plugin
```

```
gft	1.0.0	available	bundled	GitHub releases and git stashes through the gft command.	false
```

`gft` ships inside tote and is listed as `available`: it is not installed until you ask. The columns
are name, version, state, origin, description and whether it is enabled. The outputs in this
tutorial were captured through a pipe, so they are tab-separated lines. On a terminal tote prints a
table instead; `--format tsv` gives the lines.

The plugin needs a repository to work on. Make a small one with neutral content and save it in the
sandbox:

```sh
mkdir -p /tmp/tdoc/example-repo && cd /tmp/tdoc/example-repo
git init -q .
git remote add origin https://github.com/example/demo.git
printf 'package main\n\n// TODO: handle the empty list\nfunc main() {}\n// FIXME: slow\n' > main.go
tote repos add example /tmp/tdoc/example-repo
```

```
added repository "example"
```

Then make the plugin folder:

```sh
mkdir -p /tmp/tdoc/demo-plugin && cd /tmp/tdoc/demo-plugin
```

The outputs below were captured in that folder, so you will see `/tmp/tdoc/demo-plugin` where yours
shows your own path.

## 3. Step 1: the smallest plugin

Write `plugin.toml`:

```toml
api = 1
id = "demo"
version = "0.1.0"
tote = ">=1.0.0, <2"
description = "Search folders with grep and measure them with du."
license = "MIT"
tier = "declarative"
permissions = ["exec:grep", "fs:read:cwd"]

[[command]]
id = "search"
label = "Search for a word"
run = ["grep", "-rn", "-e", "TODO", "--", "."]
output = "pager"
```

The top-level keys:

| Key           | Meaning                                                                                                                                                                                                |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `api`         | The contract version. It is `1`. Any other value is `incompatible`.                                                                                                                                    |
| `id`          | Lower case letters, digits and dashes, 2 to 32 characters. It names the folders tote creates for the plugin. Some ids are reserved (tote verbs, record field names, `gft`...): see [API1.md](API1.md). |
| `version`     | Strict SemVer, no `v`. For a release it must equal the tag.                                                                                                                                            |
| `tote`        | The range of tote versions that can run the plugin: comparators `>= > <= < =` joined by commas. A development build of tote satisfies every range.                                                     |
| `description` | One sentence, up to 200 characters.                                                                                                                                                                    |
| `license`     | An SPDX-shaped expression such as `MIT` or `MIT OR Apache-2.0`.                                                                                                                                        |
| `tier`        | `declarative`.                                                                                                                                                                                         |
| `permissions` | What the plugin uses. Closed list, see below.                                                                                                                                                          |

The command has `id` (unique in its table), `label` (up to 60 characters), `run` (the program and
its arguments) and `output`. `output = "pager"` shows what the program prints in a scrollable view.
The other values are `silent` (a notice when done) and `terminal` (the program owns the terminal
while it runs).

The first element of `run` is the program. It must be a plain name, never a placeholder and never a
path, and a `permissions` entry `exec:<name>` must cover it. That is why `exec:grep` is declared.

Check it. `check` never starts a program that your plugin names: it reads the folder and prints what
tote would run. (With `--repo`, tote itself runs a read-only `git` to read the remote and the branch
of a saved repository; see [SECURITY.md](SECURITY.md).)

```sh
tote plugin check .
```

```
checking directory: .
demo 0.1.0 (declarative)
what it runs:
  - command search: grep -rn -e TODO -- .
    in /tmp/tdoc/demo-plugin
access it asks for:
  - exec:grep
  - fs:read:cwd
hashes:
  tree sha256: 7f70c8174bb5423c2beec3971075fac97bbfb5e52391ddfbe7971aa80a6eafe0
  surface sha256: 8bd6e468300c71f69b51a6c38cd50d2c67d6445f43b5b65ae1c578461886367a
grant token: exec:grep,fs:read:cwd@8bd6e468300c
.: no problem found
```

How to read it:

- `what it runs` is the exact command and the directory it runs in. Read it as the user will.
- `access it asks for` is the effective permission set. `check` can add entries of its own, such as
  `derived:tty` for a `terminal` command.
- `tree sha256` is the hash of the files. `surface sha256` is the hash of everything the plugin
  runs. The `grant token` is the sorted permissions, `@`, and the first 12 characters of the surface
  hash. You will use it to install without a terminal.

`check` exits 0 when it finds no error and 1 otherwise. A directory target is `.`, `..`, or starts
with `/`, `./`, `../` or `~/`; any other bare word is the id of an installed plugin.

The folder that you check, and later install, is the package: `tote` copies all of it. Keep in it
only `plugin.toml`, `locales/` and the files you want to ship (`README*`, `LICENSE*`). A folder that
is also a git repository (`.git`, `.github`, `.gitignore`, a changelog, tests) fails with one error
per file that a declarative plugin may not ship, and a real `.git` goes over a limit of the tree.
Put the plugin in a folder of its own inside the repository, or copy it to a clean folder (a staging
folder) before you check it.

### Permissions

`permissions` is a closed list. An unknown or duplicate entry is an error.

| Entry                               | Use                                                                      | Does tote enforce it?                                  |
| ----------------------------------- | ------------------------------------------------------------------------ | ------------------------------------------------------ |
| `exec:<name>`                       | A program the plugin runs. A name, never a path.                         | Yes: the first element of every `run` must be covered. |
| `env:<NAME>`                        | An environment variable passed to the program.                           | Yes: only declared names are passed.                   |
| `host:read:repos`                   | Read the saved repositories: `{repo.*}` and `item_action`.               | Yes.                                                   |
| `host:open-url`                     | An `open` contribution (opens an https address).                         | Yes.                                                   |
| `fs:read:<zone>`, `fs:write:<zone>` | Files the program touches. Zones: `home cwd repo config ssh tmp any`.    | No. Declared only.                                     |
| `net:<host>`                        | Hosts the program talks to, such as `api.github.com` or `*.example.org`. | No. Declared only.                                     |

"Declared only" means that tote shows the line at consent and records it, but cannot stop the
program from doing more. Declare what you really do, because the user decides on that text. See
[SECURITY.md](SECURITY.md).

## 4. Step 2: a form

A command can ask questions before it runs. Replace the command with:

```toml
[[command]]
id = "search"
label = "Search for a word"
run = ["grep", "-rn", "{ignore_case?}", "-e", "{word}", "--", "."]
output = "pager"
[[command.field]]
id = "word"
type = "text"
label = "Word to search"
required = true
pattern = "^[^ ]{1,64}$"
[[command.field]]
id = "ignore_case"
type = "bool"
label = "Ignore case"
flag = "-i"
```

Each `[[command.field]]` is one question, asked in file order. The field types are:

| Type     | Value                                          | Keys                                                                               |
| -------- | ---------------------------------------------- | ---------------------------------------------------------------------------------- |
| `text`   | A string.                                      | `max_len` (256 by default, at most 1024), `pattern`, `default`, `required`, `hint` |
| `choice` | One of a fixed list, the empty string allowed. | `choices` (1 to 64 strings)                                                        |
| `bool`   | On or off.                                     | `flag`: a literal starting with `-`, required                                      |

`default` is a template: it may use the fields above it, `{repo.*}`, `{setting.*}`, `{cwd}` and
`{home}`, and a value that cannot be resolved gives an empty default, never an error. `hint` is a
line of help under the field.

A field is written into `run` as a **placeholder** between braces:

| Placeholder                                                               | Result                                                        |
| ------------------------------------------------------------------------- | ------------------------------------------------------------- |
| `{word}`                                                                  | The value. If it is empty, the command is unavailable.        |
| `{word?}`                                                                 | The value. If it is empty, the **whole argument** is dropped. |
| `{ignore_case?}`                                                          | For a `bool`: its `flag` when on, nothing when off.           |
| `{setting.<id>}`                                                          | The effective value of a setting (step 3).                    |
| `{repo.path}` `{repo.name}` `{repo.remote}` `{repo.slug}` `{repo.branch}` | The repository (step 5).                                      |
| `{cwd}` `{home}`                                                          | The working directory of tote, the home directory.            |
| `{{` and `}}`                                                             | A literal brace.                                              |

An unknown placeholder, a stray brace or `{}` is an error when the manifest is read. Substitution
happens per argument and in one pass, so a value that contains `{x}` stays as typed.

Two rules protect the command line from the values:

- **Anchor every pattern.** A `pattern` is matched with a regular expression (RE2) against the whole
  value, so it must start with `^` and end with `$`. Without the anchors `[0-9]+` would accept
  `1 --force`. `check` refuses an unanchored pattern (`pattern.unanchored`).
- **The dash rule.** If a substituted argument starts with `-` and its template did not, tote
  refuses the command (`dash_refused`). Exceptions: a `bool` flag, a value from a `choice`, and the
  placeholders you list in `allow_dash`. Put `--` before user text where the program supports it, as
  `grep -e {word} -- .` does.

Test it without running anything: `check --argv` accepts `--set <field>=<value>`, any number of
times, and prints the command the form would build. A `bool` takes `true 1 yes on` or
`false 0 no off`.

```sh
tote plugin check . --argv --set word=main --set ignore_case=true
```

```
checking directory: .
demo 0.1.0 (declarative)
what it runs:
  - command search: grep -rn -i -e main -- .
    in /tmp/tdoc/demo-plugin
access it asks for:
  - exec:grep
  - fs:read:cwd
hashes:
  tree sha256: 2a284de1e6c8b7fa6b70c85e9a730b70f578fe7c7c040869d4d8b9b1e313d75f
  surface sha256: 4b20ed8d419f44a3fc580eb9f7ada9d1963689f515ae20ab76c9738547fe20cf
grant token: exec:grep,fs:read:cwd@4b20ed8d419f
.: no problem found
```

`-i` came from the switch, and `{word}` became the single argument `main`. Now the refusals. The
outputs below keep only the lines that matter.

```sh
tote plugin check . --argv --set word=-rf
```

```
  - command search: grep -rn '{ignore_case?}' -e '{word}' -- .
    cannot be built: the value of word starts with a dash and the command does not allow it
```

```sh
tote plugin check . --argv --set 'word=two words'
```

```
  - command search: grep -rn '{ignore_case?}' -e '{word}' -- .
    cannot be built: the value of word is not valid (does not match the pattern)
```

Without `--set` the required field is empty, and `check` says so:

```
  - command search: grep -rn '{ignore_case?}' -e '{word}' -- .
    cannot be built: the value of word is not valid (a value is required)
```

These are the same messages the user will see in the form. A failing value does not make `check`
fail: it tells you what the form would answer.

Validation order for a field is `required`, `max_len`, `pattern`, then the dash rule when the
command is built.

## 5. Step 3: settings and a list

A setting is a value that the user changes once and your commands read many times. Add two before
the command:

```toml
[[setting]]
id = "folders"
type = "list"
label = "Folders to search"
description = "Folders that the search and the sizes commands look at."
default = ["."]
max_items = 10

[[setting]]
id = "context"
type = "int"
label = "Lines of context"
default = 0
min = 0
max = 5
```

and use them in the command:

```toml
run = ["grep", "-rn", "{ignore_case?}", "-C", "{setting.context}", "-e", "{word}", "--", "{setting.folders}"]
```

The setting types are `string`, `bool`, `int`, `enum`, `path` and `list`. They share `id`, `type`,
`label`, `description` and `default`; the others depend on the type:

| Type             | Extra keys                                                                                     |
| ---------------- | ---------------------------------------------------------------------------------------------- |
| `string`, `list` | `pattern` (anchored), `max_len` (256 by default); a list also has `max_items` (100 by default) |
| `int`            | `min`, `max`                                                                                   |
| `enum`           | `choices` (1 to 64)                                                                            |
| `path`           | `must_exist`                                                                                   |
| `bool`           | none                                                                                           |

A list setting must be a whole argument of `run`. It expands into zero or more arguments, one per
item: `"{setting.folders}"` with `["src", "docs"]` becomes `src docs`. Each item goes through the
dash rule like any other value: `-x` as an item makes the command unavailable (`dash_refused`).

There is no password type and no secret setting. A credential arrives through `env:<NAME>` or the
login of the program itself.

### Where a value comes from

Three sources, strongest first: the environment, the settings file, the default.

- Environment: `TOTE_PLUGIN_<ID>_<SETTING>` in upper case, a dash of the id becoming `__` (the
  plugin `my-tool` with the setting `x` reads `TOTE_PLUGIN_MY__TOOL_X`; a setting id never holds
  `__`, and `schema` is not a setting id). A list is comma separated and trimmed. A `bool` accepts
  `1 true yes on` and `0 false no off`. No permission is needed: it is tote's own environment.
- File: `plugins/<id>.toml` in the tote configuration directory, flat, with `schema = 1` and one key
  per setting.
- Default: the `default` of the manifest, else the zero value (`""`, `false`, `0`, `[]`).

Try it with the file and with the environment:

```sh
mkdir -p $TOTE_CONFIG_DIR/plugins
printf 'schema = 1\nfolders = ["src", "docs"]\ncontext = 2\n' > $TOTE_CONFIG_DIR/plugins/demo.toml
```

```sh
tote plugin check . --argv --set word=main
```

```
  - command search: grep -rn -C 2 -e main -- src docs
    in /tmp/tdoc/demo-plugin
```

`check` reads your settings, so the command shows the effective values. Move the file out of the
way, so that the rest of the tutorial runs with the defaults:

```sh
mv $TOTE_CONFIG_DIR/plugins/demo.toml $SANDBOX/demo-settings.toml
```

With no file, the same command is:

```
  - command search: grep -rn -C 0 -e main -- .
    in /tmp/tdoc/demo-plugin
```

A value that is invalid, from the environment or from the file, is ignored for that setting with a
warning, and the next source applies. `check` repeats the warning:

```sh
TOTE_PLUGIN_DEMO_CONTEXT=9 tote plugin check . --argv --set word=main
```

```
checking directory: .
demo 0.1.0 (declarative)
  warning: setting.context: setting context from the environment was ignored: out of range
what it runs:
  - command search: grep -rn -C 0 -e main -- .
    in /tmp/tdoc/demo-plugin
...
.: errors 0, warnings 1
```

A plugin never reads the settings file and never writes settings: it receives the effective values
and nothing else.

## 6. Step 4: a program that needs the terminal

`output = "terminal"` gives the terminal to the program, which is what a pager-less tool such as
`du` or an interactive tool needs. Add:

```toml
[[command]]
id = "sizes"
label = "Show folder sizes"
run = ["du", "-sh", "--", "{setting.folders}"]
output = "terminal"
```

and add `exec:du` to `permissions`. A terminal run adds the derived permission `derived:tty` to the
consent text: the program can read keys and draw anything while it runs. By default tote waits for a
key when the program ends (`pause = "always"`); `pause = "error"` waits only when the program fails
(status 126 or more), `pause = "never"` does not wait.

The other keys of a command, all optional:

| Key           | Meaning                                                                                                                                                                                    |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `description` | A line shown in the footer. Up to 200 characters.                                                                                                                                          |
| `keywords`    | Up to 8 words (24 characters each) that tote matches when the user searches for a command.                                                                                                 |
| `confirm`     | `true` for a generic question, or a text with placeholders: `"Delete {n}?"`. The user sees the exact command and the directory, with the focus on Cancel. Use it for anything destructive. |
| `cwd`         | The directory to run in, a template. Default `{cwd}` for a command, `{repo.path}` for a repository action. It must be an existing absolute directory.                                      |
| `env`         | A table of variables to set. Each name needs `env:<NAME>`; loader and interpreter variables (`PATH`, `LD_*`, `GIT_SSH*`...) are refused.                                                   |
| `timeout`     | `30s`, `2m`... up to `30m`. Default `5m`. For `pager` and `silent`.                                                                                                                        |
| `allow_dash`  | Placeholders that may start with `-`.                                                                                                                                                      |
| `key`         | An accelerator letter `[a-z0-9]` in the action menu.                                                                                                                                       |
| `open`        | Instead of `run`: an `https` address built from a template. Needs `host:open-url`. No program runs: tote asks `Open https://...?` and calls the system opener.                             |

## 7. Step 5: an action on a repository

An `[[item_action]]` works on a saved repository. It needs `host:read:repos` and runs by default in
the directory of the repository. Add:

```toml
[[item_action]]
id = "marks"
target = "repo"
label = "List the marked lines"
key = "m"
run = ["grep", "-rn", "--exclude-dir=.git", "-e", "{mark}", "--", "."]
output = "pager"
[[item_action.field]]
id = "mark"
type = "choice"
label = "Mark"
choices = ["TODO", "FIXME", "XXX"]
```

and `host:read:repos` to `permissions`. A `choice` value is never refused for starting with a dash,
because it can only be one of your strings.

Check it against the saved repository with `--repo`, and give the field a value:

```sh
tote plugin check . --argv --repo example --set mark=FIXME
```

```
  - item action marks: grep -rn --exclude-dir=.git -e FIXME -- .
    in /tmp/tdoc/example-repo
```

Without `--repo` the action cannot be built, since there is no repository:

```
  - item action marks: grep -rn --exclude-dir=.git -e '{mark}' -- .
    cannot be built: repo is unavailable (unknown)
```

A value that is not one of the choices is refused:

```
    cannot be built: the value of mark is not valid (not one of the choices)
```

The repository members you can use are `{repo.path}`, `{repo.name}`, `{repo.remote}`, `{repo.slug}`
(`owner/name`, only for a github.com remote) and `{repo.branch}`. In a `[[command]]` they refer to
the current repository: the saved repository that contains the directory tote was started in. An
entry whose placeholder has no value is greyed in the menu with the reason.

## 8. Step 6: the requirement

A plugin that needs a program can say so and tell the user how to get it:

```toml
[[requires]]
bin = "grep"
hint = "Install grep with your package manager."
```

If the program is not on the `PATH`, the plugin is in the state `missing dependency`: it contributes
nothing, and `info` shows your hint labelled "suggested by the plugin, not checked by tote". tote
never runs the hint. With `optional = true` a missing program is only a warning and hides only the
entries that run it.

## 9. Step 7: translations

The manifest is in English. French texts live in `locales/fr.toml`, English texts that the manifest
cannot hold in `locales/en.toml`. Only `en` and `fr` are read. A text is looked up in the language
of the user, then English, then the manifest. A missing key is never an error at run time.

The French file translates two display keys that the manifest does not have yet. Add them first. At
the top level, after `version`:

```toml
title = "Demo"
```

`title` is display text only, up to 40 characters. Menus show the id first, so a plugin cannot pass
for another by its title. In the `search` command, after `label`:

```toml
keywords = ["grep", "find"]
```

`keywords` are extra words that tote matches when the user searches for a command.

Run `check` now:

```sh
tote plugin check .
```

```
checking directory: .
demo 0.1.0 (declarative)
  warning: locales.en.item_action.marks.field.mark.choice.TODO: the translation lacks the key item_action.marks.field.mark.choice.TODO
  warning: locales.en.item_action.marks.field.mark.choice.FIXME: the translation lacks the key item_action.marks.field.mark.choice.FIXME
  warning: locales.en.item_action.marks.field.mark.choice.XXX: the translation lacks the key item_action.marks.field.mark.choice.XXX
...
.: errors 0, warnings 3
```

The labels of choices are the one text with no English literal in the manifest: a choice value is
the program's word (`TODO`), and its label is what the menu shows. Give them in `locales/en.toml`:

```toml
# The labels of the choices have no English text in plugin.toml, so they are given here.
"item_action.marks.field.mark.choice.TODO" = "To do"
"item_action.marks.field.mark.choice.FIXME" = "To fix"
"item_action.marks.field.mark.choice.XXX" = "Needs attention"
```

The French file needs every translatable key, because a French user who lacks one reads English:

```toml
"title" = "Démo"
"description" = "Cherche dans des dossiers avec grep et les mesure avec du."
"requires.grep.hint" = "Installez grep avec votre gestionnaire de paquets."
"setting.folders.label" = "Dossiers à parcourir"
"setting.folders.description" = "Dossiers que les commandes de recherche et de tailles examinent."
"setting.context.label" = "Lignes de contexte"
"command.search.label" = "Chercher un mot"
"command.search.keywords" = "grep, trouver"
"command.search.field.word.label" = "Mot à chercher"
"command.search.field.ignore_case.label" = "Ignorer la casse"
"command.sizes.label" = "Afficher la taille des dossiers"
"item_action.marks.label" = "Lister les lignes marquées"
"item_action.marks.field.mark.label" = "Marque"
"item_action.marks.field.mark.choice.TODO" = "À faire"
"item_action.marks.field.mark.choice.FIXME" = "À corriger"
"item_action.marks.field.mark.choice.XXX" = "À examiner"
```

The keys are derived from the manifest:

| Text         | Key                                                                                            |
| ------------ | ---------------------------------------------------------------------------------------------- |
| Plugin       | `title`, `description`                                                                         |
| Contribution | `<table>.<id>.label`, `.description`, `.keywords` (one comma separated string), `.confirm`     |
| Field        | `<table>.<id>.field.<fid>.label`, `.hint`, `.choice.<value>` (`.choice._` for the empty value) |
| Setting      | `setting.<id>.label`, `.description`, `.choice.<value>`                                        |
| Requirement  | `requires.<bin>.hint`                                                                          |

Values are plain text. Placeholders `{name}` are allowed where the English text may have them
(`confirm`), but there are no printf verbs and no plurals. The examples write each key as one quoted
string; nested tables are read the same way.

`check` reports a missing or extra key as a warning. Under `--strict` those two warnings become
errors, and any other warning about the package (an unused `exec:` permission, for example) still
prints as `warning` but fails the check. Warnings about your machine, your settings or the state of
an installed plugin never fail it. Use `--strict` in your CI:

```sh
tote plugin check . --strict
```

With both locale files in place, the output ends with `.: no problem found` and the exit status
is 0. The same check proves the translations: after adding a command and forgetting its French
label, `check` says

```
  warning: locales.fr.command.count.label: the translation lacks the key command.count.label
```

and `check --strict` says `error:` instead of `warning:` and exits 1.

To see the French texts, set the language of the run. `check` shows the manifest and the commands,
so use `info` once the plugin is installed (section 11, "Look at it").

The language comes from `TOTE_LANG`, the `lang` setting of tote, then `LC_ALL`, `LC_MESSAGES` and
`LANG`.

## 10. The complete manifest

This is the `plugin.toml` you have built. The `locales` folder holds the two files of step 7.

```toml
api = 1
id = "demo"
version = "0.1.0"
tote = ">=1.0.0, <2"
title = "Demo"
description = "Search folders with grep and measure them with du."
license = "MIT"
tier = "declarative"

permissions = [
    "exec:du",
    "exec:grep",
    "fs:read:cwd",
    "host:read:repos",
]

[[requires]]
bin = "grep"
hint = "Install grep with your package manager."

[[setting]]
id = "folders"
type = "list"
label = "Folders to search"
description = "Folders that the search and the sizes commands look at."
default = ["."]
max_items = 10

[[setting]]
id = "context"
type = "int"
label = "Lines of context"
default = 0
min = 0
max = 5

# A command with a form: the word to search and a switch.
[[command]]
id = "search"
label = "Search for a word"
keywords = ["grep", "find"]
run = ["grep", "-rn", "{ignore_case?}", "-C", "{setting.context}", "-e", "{word}", "--", "{setting.folders}"]
output = "pager"
[[command.field]]
id = "word"
type = "text"
label = "Word to search"
required = true
pattern = "^[^ ]{1,64}$"
[[command.field]]
id = "ignore_case"
type = "bool"
label = "Ignore case"
flag = "-i"

# A command that needs the terminal: du prints, then tote waits for a key.
[[command]]
id = "sizes"
label = "Show folder sizes"
run = ["du", "-sh", "--", "{setting.folders}"]
output = "terminal"

# An action on a saved repository. The program runs in the repository.
[[item_action]]
id = "marks"
target = "repo"
label = "List the marked lines"
key = "m"
run = ["grep", "-rn", "--exclude-dir=.git", "-e", "{mark}", "--", "."]
output = "pager"
[[item_action.field]]
id = "mark"
type = "choice"
label = "Mark"
choices = ["TODO", "FIXME", "XXX"]
```

Its final check, with a repository and values for every field:

```sh
tote plugin check . --argv --repo example --set word=TODO --set mark=FIXME --set ignore_case=yes
```

```
checking directory: .
demo 0.1.0 (declarative)
what it runs:
  - command search: grep -rn -i -C 0 -e TODO -- .
    in /tmp/tdoc/demo-plugin
  - command sizes: du -sh -- .
    in /tmp/tdoc/demo-plugin
  - item action marks: grep -rn --exclude-dir=.git -e FIXME -- .
    in /tmp/tdoc/example-repo
requires:
  - grep: found at /usr/bin/grep
access it asks for:
  - derived:tty
  - exec:du
  - exec:grep
  - fs:read:cwd
  - host:read:repos
hashes:
  tree sha256: 8a3e91cf809b6e101e4c5058f5fe1f3a828528a3cd3c85ab4c084d3239b63e1e
  surface sha256: 80a62f7605796b41de066cc95fa2ae457816b7a7342a15040eb4705546375133
grant token: derived:tty,exec:du,exec:grep,fs:read:cwd,host:read:repos@80a62f760579
.: no problem found
```

(The `-C 0` and `.` come from the defaults, because this run had no settings file.)

## 11. Install it

Installing from a directory **copies** it. tote never runs the folder you wrote: it runs the copy,
checked against its hash each time. So editing your folder changes nothing until you update (section
12).

### See what the user will see

`--dry-run` prints the consent view and changes nothing:

```sh
tote plugin install . --dry-run
```

```
Install plugin demo 0.1.0?

Source: /tmp/tdoc/demo-plugin
Origin: local files, not verified
Checksum: sha256 8a3e91cf809b, computed here, nothing to compare it with
No code of its own: tote runs only the programs listed below.

Description (written by the plugin, not checked by tote)
  | Search folders with grep and measure them with du.

Commands it will run:
  $ command search: grep -rn '{ignore_case?}' -C '{setting.context}' -e '{word}' -- '{setting.folders}'
  $ command sizes: du -sh -- '{setting.folders}'
  $ item action marks: grep -rn --exclude-dir=.git -e '{mark}' -- .

This plugin will be able to:
  ! take over the terminal while some actions run (it can read keys and draw anything) [enforced]
  - run the program du [enforced]
  - run the program grep [enforced]
  - read files: the current directory [declared]
  - read your repository list (names, paths, remotes) [enforced]
    enforced: tote itself refuses anything else
    declared only: the plugin states it, tote cannot enforce it

! tote runs only the listed programs, but those programs can do whatever you can do with them.

consent is asked because:
  - it is a new plugin
Consent code derived:tty,exec:du,exec:grep,fs:read:cwd,host:read:repos@80a62f760579

dry run: nothing was changed
to accept it without a terminal, run the command again without --dry-run and with --grant derived:tty,exec:du,exec:grep,fs:read:cwd,host:read:repos@80a62f760579
```

This is your plugin as the user judges it. Each command is shown in full, with its placeholders. The
description is in a box because tote did not write it and did not check it. Every permission is
marked `enforced` or `declared`. A `!` marks what deserves attention. Write your commands so that
this screen is honest and short.

### Install with a terminal

On a terminal, `install` prints the same view on standard error and asks one question. The default
is No:

```sh
tote plugin install .
```

```
Install plugin demo 0.1.0?

...

Install and enable plugin "demo"? [y/N] n
tote: cancelled, nothing changed
```

A refusal exits 1. Ctrl-C exits 130, and so does SIGINT or SIGTERM during a download.

### Install without a terminal

There is no `--yes` for consent. A script passes `--grant` with the token it read from `--dry-run`,
`check` or `info`. Without a terminal and without the token:

```sh
tote plugin install .
```

```
tote: error: consent needed: run in a terminal, or pass --grant derived:tty,exec:du,exec:grep,fs:read:cwd,host:read:repos@80a62f760579 after reviewing what the plugin asks for
```

The exit status is 2. With a token that does not match the plugin, the exit status is 4. A token
that matched an older version of the plugin does not match the next one: a script cannot be widened
by accident.

```sh
tote plugin install . --grant 'exec:grep@80a62f760579'
```

```
tote: error: the --grant token does not match what this plugin asks for: the current token is in tote plugin info, or in the output of --dry-run
```

With the right token:

```sh
tote plugin install . --grant 'derived:tty,exec:du,exec:grep,fs:read:cwd,host:read:repos@80a62f760579'
```

```
installed plugin demo 0.1.0 (enabled)
```

### Look at it

```sh
tote plugin
```

```
demo	0.1.0	enabled	local	Search folders with grep and measure them with du.	true
gft	1.0.0	available	bundled	GitHub releases and git stashes through the gft command.	false
```

```sh
tote plugin info demo
```

```
demo 0.1.0 - Demo
description: Search folders with grep and measure them with du.
tier: declarative (no program of its own)
kind: local
source: /tmp/tdoc/demo-plugin
state: enabled
license: MIT
declared access:
  - derived:tty
  - exec:du
  - exec:grep
  - fs:read:cwd
  - host:read:repos
access you allowed:
  - derived:tty
  - exec:du
  - exec:grep
  - fs:read:cwd
  - host:read:repos
grant token: derived:tty,exec:du,exec:grep,fs:read:cwd,host:read:repos@80a62f760579
requires:
  - grep: found at /usr/bin/grep
contributes:
  - command search: Search for a word
  - command sizes: Show folder sizes
  - item action marks: List the marked lines
  - setting folders: Folders to search
  - setting context: Lines of context
settings:
  - folders (list) = ., from default
  - context (int) = 0, from default
paths:
  installed files: ~/.local/share/tote/plugins/demo/8a3e91cf809b6e101e4c5058f5fe1f3a
  settings file: ~/.config/tote/plugins/demo.toml
  state directory: ~/.local/state/tote/plugins/demo
  cache directory: ~/.cache/tote/plugins/demo
  host directory: ~/.local/state/tote/plugins-host/demo
hashes:
  tree sha256: 8a3e91cf809b6e101e4c5058f5fe1f3a828528a3cd3c85ab4c084d3239b63e1e
  surface sha256: 80a62f7605796b41de066cc95fa2ae457816b7a7342a15040eb4705546375133
```

(In the capture, the sandbox folder was the home directory, so `$SANDBOX` is shown as `~`. Your
paths start with `$SANDBOX`.)

The tab-separated listing is a stable format for scripts; the table for people is not. Add `--json`
for the full records. Every list verb wraps its array in an object that carries the schema, and
every JSON object has `"schema": 1`:

```sh
tote plugin --json
```

```json
{
  "schema": 1,
  "plugins": [
    {
      "id": "demo",
      "title": "Demo",
      "version": "0.1.0",
      "tier": "declarative",
      "kind": "local",
      "origin": "",
      "state": "enabled",
      "reasons": [],
      "enabled": true,
      "consented": true,
      "caps": [
        "derived:tty",
        "exec:du",
        "exec:grep",
        "fs:read:cwd",
        "host:read:repos"
      ],
      "grant": "derived:tty,exec:du,exec:grep,fs:read:cwd,host:read:repos@80a62f760579",
      "contributes": {
        "item_actions": 1,
        "commands": 2,
        "lists": 0,
        "settings": 2
      }
    },
    ...
  ]
}
```

(`gft` follows in the real output, with the same fields.) `tote plugin info demo --json` adds the
contributions, the settings with their value and source, the requirements and the paths.

A failure under `--json` is an object on standard output and nothing on standard error. **A non-zero
exit status means the standard output is not a result**: ignore it and read `error` instead.

```sh
tote plugin info nosuch --json
```

```json
{
  "schema": 1,
  "ok": false,
  "error": {
    "code": "not_installed",
    "message": "plugin \"nosuch\" is not installed",
    "exit": 1
  }
}
```

To see the French texts of an installed plugin, set the language of the run:

```sh
TOTE_LANG=fr tote plugin info demo
```

```
demo 0.1.0 - Démo
description : Cherche dans des dossiers avec grep et les mesure avec du.
niveau : déclaratif (aucun programme propre)
...
apporte :
  - commande search : Chercher un mot
  - commande sizes : Afficher la taille des dossiers
  - action d’élément marks : Lister les lignes marquées
  - réglage folders : Dossiers à parcourir
  - réglage context : Lignes de contexte
...
```

### Use it

An enabled plugin contributes to tote's own interface: its commands, its repository actions and its
settings. The terminal interface also has a Plugins screen that lists the installed and the
available plugins. This tutorial does not describe the screens or their key bindings: the command
line is the reference and the one to use in scripts. A declarative plugin cannot quit tote, change
the directory or type into the shell.

While you write, `check --argv` is the way to see what a form would run. `tote plugin run` does not
exist in 1.0.

### Disable, enable, no plugins

```sh
tote plugin disable demo
tote plugin
tote plugin enable demo
```

```
plugin "demo" disabled, its settings, data and consent are kept
demo	0.1.0	disabled	local	Search folders with grep and measure them with du.	false
gft	1.0.0	available	bundled	GitHub releases and git stashes through the gft command.	false
plugin "demo" enabled
```

`disable` keeps the files, the settings and the consent. A disabled plugin contributes nothing. To
turn every plugin off for one run, put `--no-plugins` before the verb, or set `TOTE_NO_PLUGINS=1`:

```sh
tote --no-plugins plugin
```

```
demo	0.1.0	disabled	local	Search folders with grep and measure them with du.	false
gft	1.0.0	available	bundled	GitHub releases and git stashes through the gft command.	false
```

### The states

One state per installed plugin, the first that matches. Only `enabled` contributes.

| State                | Meaning                                                                                                                     | What to do                                                  |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| `disabled`           | You turned it off, or `--no-plugins`.                                                                                       | `tote plugin enable <id>`                                   |
| `broken`             | Missing or modified files, an invalid manifest.                                                                             | Remove it and install it again.                             |
| `incompatible`       | The `api`, `tote` range, features or platform do not fit this tote.                                                         | Upgrade tote or the plugin.                                 |
| `needs-consent`      | Not allowed yet, or it now asks for more or runs something else.                                                            | `tote plugin enable <id>` on a terminal, or with `--grant`. |
| `quarantined`        | Turned off after failures. A declarative run never counts as a failure, so it cannot happen to a declarative plugin in 1.0. | `tote plugin enable <id>`                                   |
| `missing-dependency` | A `[[requires]]` program is not found.                                                                                      | Install the program.                                        |
| `enabled`            | None of the above.                                                                                                          |                                                             |

For example, when `grep` cannot be found on the `PATH` of the run (point `PATH` at a folder that
holds `tote` and none of the programs you need, and call tote by its full path):

```
demo	0.1.0	missing-dependency	local	Search folders with grep and measure them with du.	false
...
state: missing dependency
  - missing dependency: grep
  fix: install the missing program
requires:
  - grep: not found
    suggested by the plugin, not checked by tote: Install grep with your package manager.
```

`tote plugin check` with no argument checks every installed plugin and lists what is left on disk:

```
installed plugins
demo 0.1.0 (enabled)
demo: no problem found
leftover: cache directory demo2
leftover: state directory demo2
plugins checked: 1
```

The two `leftover` lines come from a test plugin, `demo2`, that was removed without `--purge`: its
state and cache directories stayed. `check` also reports orphan files, staging folders, settings of
plugins that are not installed, and rejected setting values.

## 12. Change a plugin and update it

Edit your folder, raise the `version`, and run `update`. For a plugin installed from a directory it
reads the recorded path again:

```sh
tote plugin update demo
```

With the same version it refuses:

```
tote: error: version 0.1.0 is not newer than the installed 0.1.0; use --force
```

Make a harmless change: set `version = "0.1.1"` and shorten the `pattern` of `word` to
`"^[^ ]{1,40}$"`. First look at what it does, then do it:

```sh
tote plugin update demo --dry-run
```

```
Update plugin demo from 0.1.0 to 0.1.1?

Source: /tmp/tdoc/demo-plugin
Origin: local files, not verified
Checksum: sha256 9ca86a9657a8, computed here, nothing to compare it with
No code of its own: tote runs only the programs listed below.

Description (written by the plugin, not checked by tote)
  | Search folders with grep and measure them with du.

Unchanged: 5
No longer asked: 0
Commands: 0 added, 0 removed, 0 changed

! tote runs only the listed programs, but those programs can do whatever you can do with them.
```

There is no "consent is asked because" block and no `Consent code` line: no consent is needed. The
new pattern is a change to what the plugin runs, and it is not a reason to ask (see below).

```sh
tote plugin update demo
```

```
updated demo from 0.1.0 to 0.1.1, permissions unchanged
```

The settings, the state, the cache and the enabled flag survive an update.

### When consent is asked again

An update asks again, and a script must pass a new `--grant`, in two cases:

1. **It asks for more access.** A new `exec:`, `net:`, `env:` or `host:` entry, a first terminal
   command (`derived:tty`), or any new permission. Dropped permissions never ask.
2. **It runs something it could not before.** A `confirm` removed or turned off on an entry that
   kept its id, an `allow_dash` entry added (a new entry that carries one asks too), an `env` name
   or value added or changed (a new entry that carries an `env` table asks too). Any other change to
   a command, even to its program or arguments, is shown in the diff and is **not** asked. This is a
   choice, and a limit: the user reads the diff but is not stopped by it.

Try the first case. Raise the version to `0.2.0`, add `"exec:wc"` to `permissions` and add:

```toml
[[command]]
id = "count"
label = "Count lines"
run = ["wc", "-l", "--", "{setting.folders}"]
output = "pager"
```

```sh
tote plugin update demo --dry-run
```

```
demo 0.2.0 asks for more access

Source: /tmp/tdoc/demo-plugin
Origin: local files, not verified
Checksum: sha256 39038f7be7dc, computed here, nothing to compare it with
No code of its own: tote runs only the programs listed below.

Description (written by the plugin, not checked by tote)
  | Search folders with grep and measure them with du.

New access:
  + run the program wc [enforced]
Unchanged: 5
No longer asked: 0
Commands: 1 added, 0 removed, 0 changed
  + command count: wc -l -- '{setting.folders}'

! tote runs only the listed programs, but those programs can do whatever you can do with them.

consent is asked because:
  - it asks for more access than you allowed
Consent code derived:tty,exec:du,exec:grep,exec:wc,fs:read:cwd,host:read:repos@5ace30628ec0

dry run: nothing was changed
to accept it without a terminal, run the command again without --dry-run and with --grant derived:tty,exec:du,exec:grep,exec:wc,fs:read:cwd,host:read:repos@5ace30628ec0
```

On a terminal `update` shows this view and asks; declining leaves the old version running. With an
old token it exits 4:

```
tote: error: the --grant token does not match what this plugin asks for: the current token is in tote plugin info, or in the output of --dry-run
```

That `wc` command is not kept in the demo. Put the folder back as it was for the release: set
`version = "0.1.1"` again, remove `"exec:wc"` from `permissions` and delete the `count` command. The
rest of the tutorial uses the 0.1.1 you installed above.

Other `update` rules:

- A lower or equal version, or a pre-release, is refused without `--force`.
- The source must be the same as the recorded one. A different one is `source_changed` and needs
  `--force`.
- `tote plugin install ./demo` of what is installed already (the same source, the same version, the
  same files) is not an update: it prints `plugin demo 0.1.1 is already installed (enabled)`, exits
  0, asks nothing and writes nothing (it exits 4 if the plugin cannot run as it is: it needs
  consent, or is broken). With `--json` the result has `"already_installed": true`.
- tote never updates by itself and never asks the network what is new.

## 13. Release it

A release plugin is a `.tar.gz` that a user installs from GitHub with a checksum that you publish.

### The archive

The archive holds the package at its root: `plugin.toml`, `locales/`, and optionally `README*` and
`LICENSE*`. No folder around it: an archive made with `tar czf demo.tar.gz demo-plugin/`, or the
"Source code" archive that a hosting site generates, has the package in a folder and is refused with
`plugin.toml: not at the root of the package: it is in the folder demo-plugin`. Name the files as
below, from inside the folder, so that the archive holds only the package: `.git`, `.github`,
`.gitignore`, a changelog or a test folder do not belong to it, and a declarative plugin may not
ship them.

```sh
mkdir -p /tmp/tdoc/dist
cd /tmp/tdoc/demo-plugin
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime='2026-11-01 00:00Z' \
    -czf /tmp/tdoc/dist/demo.tar.gz plugin.toml locales
tar tzf /tmp/tdoc/dist/demo.tar.gz
```

```
plugin.toml
locales/
locales/en.toml
locales/fr.toml
```

Rules that tote enforces when it extracts:

- gzip tar only; at most 16 MiB in all and per file, 512 entries, paths of at most 6 segments, up to
  255 bytes;
- regular files and folders only: no symbolic link, hard link, device or special file;
- each path segment matches `[A-Za-z0-9._-]`, is not `.` or `..` and does not start with `-`; no
  absolute path, no duplicate, no two names that differ only by case;
- `plugin.toml` at the root. When it is not there, the error says so (`manifest_missing`); when the
  only `plugin.toml` of the archive is in one folder, the error names the folder
  (`manifest_nested`).

`tote plugin check <archive>` prints the checksum to publish next to the release only when the
archive has no error.

The repository of the release is `owner/name`. The tag **must equal the plugin version** (`0.1.1`,
no `v`); a release whose manifest says another version is refused (`tag_mismatch`). The asset of the
`owner/repo@tag` shorthand is named after the repository: `example/demo@0.1.1` means
`https://github.com/example/demo/releases/download/0.1.1/demo.tar.gz`. A full release URL may name
the asset as you like.

### The checksum

`check` on the archive prints the line to publish next to the asset:

```sh
cd /tmp/tdoc/dist
tote plugin check ./demo.tar.gz
```

```
checking archive: ./demo.tar.gz
demo 0.1.1 (declarative)
what it runs:
...
hashes:
  archive sha256: 7cbd956e6076252984e4df37dbae3eded5eb9bd671ab647b1dde0b7b13ff2616
  tree sha256: 9ca86a9657a8943f52e8bbdbd259855eac686d238fedcba80a3f8b153e9bccb7
  surface sha256: 70b702eff43cf98b9fa01b583b5333a14d16bb4e6fe897de54bf1a12b6950db6
grant token: derived:tty,exec:du,exec:grep,fs:read:cwd,host:read:repos@70b702eff43c
publish next to the release, as demo.tar.gz.sha256:
  7cbd956e6076252984e4df37dbae3eded5eb9bd671ab647b1dde0b7b13ff2616  demo.tar.gz
./demo.tar.gz: no problem found
```

The last two lines of the hashes are not what you publish. Publish the **archive** hash, in the
`sha256sum` format: put `7cbd...2616  demo.tar.gz` in `demo.tar.gz.sha256`, attach both files to the
release, and also print the hash in the release notes. `sha256sum demo.tar.gz` gives the same line.

### What the user does

Every remote install and every remote update needs the pin:

```sh
tote plugin install example/demo@0.1.1 --sha256 7cbd956e6076252984e4df37dbae3eded5eb9bd671ab647b1dde0b7b13ff2616
```

Without it, the answer is:

```
tote: error: a remote plugin needs --sha256, the checksum published by its author
```

with exit status 2 (and `hash_required` in the `--json` error object). A pin that is not 64 hex
characters is `hash_malformed`, also 2. tote compares the pin with the bytes it downloaded before it
reads anything else. A wrong pin stops the install with exit 5 and `Nothing was installed.`:

```sh
tote plugin install ./demo.tar.gz --sha256 0000000000000000000000000000000000000000000000000000000000000000
```

```
tote: error: checksum mismatch: expected 0000000000000000000000000000000000000000000000000000000000000000, got 7cbd956e6076252984e4df37dbae3eded5eb9bd671ab647b1dde0b7b13ff2616. Nothing was installed.
```

`--sha256` also works on a local archive. It is refused for a directory and for a bundled plugin
(`hash_unexpected`, exit 2), because there is no archive to compare.

`demo` is still installed from your folder, and an install refuses a source that is not the recorded
one (exit 1, `use --force to replace it`). Remove it first (section 14 tells more), then install the
archive with the right pin:

```sh
tote plugin remove demo --yes
```

```
removed plugin "demo"
kept: state directory, cache directory (tote plugin remove --purge deletes them)
```

```sh
tote plugin install ./demo.tar.gz --sha256 7cbd956e6076252984e4df37dbae3eded5eb9bd671ab647b1dde0b7b13ff2616 \
    --grant 'derived:tty,exec:du,exec:grep,fs:read:cwd,host:read:repos@70b702eff43c'
```

```
installed plugin demo 0.1.1 (enabled)
```

Say this in your release notes: **the pin proves that the file is the one you published, not who
published it.** A hash copied from the page of the release gives integrity, not authenticity. Ask
your users to take it from a second place (your README, a message, a review) when it matters.

The release URL and the redirects that tote accepts are strict: `https` on `github.com`, a path of
exactly `/<owner>/<repo>/releases/download/<tag>/<asset>`, redirects only to `github.com` or the
GitHub download hosts, and a redirect to another repository is refused (`origin_redirect`). See
[SECURITY.md](SECURITY.md). tote sends no credential: a download answered with status 401 (a private
release, for example) exits 3.

### Before you tag

A checklist:

1. `tote plugin check . --strict` has no error and no warning.
2. `tote plugin check . --argv --set ... --repo ...` builds every command you expect, and refuses
   the ones you want refused.
3. `tote plugin install . --dry-run` reads well: short, honest, no surprise.
4. `version` equals the tag.
5. `tote plugin check ./demo.tar.gz` on the archive you attach, and the `.sha256` file is its line.
6. If the manifest uses a key or a feature that only a later tote has, `tote` says so
   (`tote = ">=1.1.0"`, or a name in `requires_features`): tote 1.0 then reports the plugin as
   `incompatible`, which is what you want, and not as invalid.

## 14. Remove it

```sh
tote plugin remove demo
```

Without a terminal and without `--yes` this exits 2:

```
tote: error: demo: remove asks for confirmation and there is no terminal: pass --yes
```

```sh
tote plugin remove demo --yes
```

```
removed plugin "demo"
kept: state directory, cache directory (tote plugin remove --purge deletes them)
```

`remove` stops the processes that the plugin started, deletes the plugin and its consent, and clears
the `enabled = false` that `disable` wrote in `config.toml`, so that installing it again gives an
enabled plugin. It keeps your settings, state and cache. `--purge` deletes those as well; an empty
`[plugins.demo]` header can stay in `config.toml`.

## 15. gft, the complete example

`gft` is the plugin that ships inside tote. Its source is the folder
`internal/plugin/bundled/plugins/gft/`: a `plugin.toml` of 188 lines, `locales/en.toml`,
`locales/fr.toml` and a `LICENSE`. Read it after this tutorial; it uses everything you saw. It runs
the external `gft` program, which shows GitHub releases and manages git stashes.

Install it the way you installed `demo`:

```sh
tote plugin install gft --dry-run
```

```
Install plugin gft 1.0.0?

Origin: shipped with tote
Checksum: sha256 f56d20b069f5, covered by the signature of this tote release
No code of its own: tote runs only the programs listed below.

Description (written by the plugin, not checked by tote)
  | GitHub releases and git stashes through the gft command.

Commands it will run:
  $ command get: gft '{slug}' '{tag?}' --get '{pattern}' '{verify?}'
  $ command install: gft '{slug}' '{tag?}' --install
  $ command open: open https://github.com/{slug}/releases
  $ command show: gft '{slug}' '{tag?}' '{view?}'
...
```

What each part teaches:

| In gft                                                                             | Teaches                                                                                                         |
| ---------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| `[[requires]] bin = "gft"` with a `hint`                                           | A missing program is a state, not a crash.                                                                      |
| `[[item_action]]` with `target = "repo"` and `run = ["gft", "{repo.slug}"]`        | Actions on a saved repository. Greyed when the repository has no GitHub remote, because `{repo.slug}` is empty. |
| `stash-list` has `key = "s"`                                                       | An accelerator.                                                                                                 |
| `stash-drop` has `confirm = "Drop stash {n} of {repo.name}?"` and `--yes` in `run` | Destructive actions confirm first; the program's own prompt is turned off because tote asked.                   |
| `stash-show` runs `gft stash show {n?}` with `default = "0"`                       | An optional field: empty drops the argument.                                                                    |
| `[[command]] show` with a `choice` whose first value is `""`                       | An empty choice, labelled by `choice._` in the catalogs.                                                        |
| `[[command]] open` with `open = "https://github.com/{slug}/releases"`              | An `open` contribution: no program runs; the user sees the full address first. Needs `host:open-url`.           |
| `[[command]] get` with a `bool` field with `flag = "--checksum"`                   | A switch that adds a flag.                                                                                      |
| `output = "terminal"` on `get` and `install`                                       | The program owns the terminal. It adds `derived:tty`.                                                           |
| Every field has an anchored `pattern`                                              | See below.                                                                                                      |
| The comment above `permissions`                                                    | The honest declaration of a wide program.                                                                       |

### Its patterns

`gft` reads the words `update`, `delete`, `uninstall` and `stash` anywhere in its arguments as its
own subcommands and has no `--`. A tag typed as `stash` would change what the program does. So the
pattern of the tag cannot match a bare word:

```toml
pattern = "^([vV]?[0-9][A-Za-z0-9._+-]{0,62}|[A-Za-z]+[0-9._+-][A-Za-z0-9._+-]{0,62})?$"
```

A tag starts with a digit, optionally after `v`, or holds a digit or a symbol after its letters. The
slug has its own pattern, `^[A-Za-z0-9][A-Za-z0-9-]*/\\.?[A-Za-z0-9_-][A-Za-z0-9._-]*$`, which
cannot start with a dash. When a program has no `--`, the patterns are your whole defence: write
them against what the program does with the value, and test them with `check --argv --set`.

### Its permissions

```toml
permissions = [
    "env:GITHUB_TOKEN",
    "exec:gft",
    "fs:read:cwd",
    "fs:write:any",
    "fs:write:cwd",
    "host:open-url",
    "host:read:repos",
    "net:api.github.com",
    "net:github.com",
    "net:*.githubusercontent.com",
]
```

`gft --get` writes to the working directory, `--install` copies a binary to a `bin` directory, and
the cache lives in `~/.config/gft`. The manifest says so with `fs:write:any`. The consent view marks
the wide ones with `!`: sensitive variables such as `GITHUB_TOKEN`, `fs:*:any` and `net:*.domain`.

### Its French

`gft` follows the rules of step 7, and `check --strict` is the gate that keeps it that way. Look at
what French needs:

```toml
"item_action.stash-drop.label" = "Supprimer un stash"
"item_action.stash-drop.confirm" = "Supprimer le stash {n} de {repo.name} ?"
"item_action.stash-drop.field.n.label" = "Numéro du stash"
"command.show.keywords" = "release, version, assets, notes"
"command.show.field.view.choice._" = "Infos de la release"
"command.show.field.view.choice.--assets" = "Assets"
"command.open.field.slug.label" = "Dépôt"
```

- The `{n}` and `{repo.name}` of `confirm` are copied as they are: they must exist in that entry.
- `keywords` is one comma separated string.
- A choice with an empty value uses `choice._`; a choice with a value such as `--assets` uses it in
  the key.
- French typography is yours to write: a non-breaking space before `?` and `:`, and `’`.
- `locales/en.toml` has the five labels of the choices of `View`, the only English text that the
  manifest cannot carry:

```toml
"command.show.field.view.choice._" = "Release info"
"command.show.field.view.choice.--releases" = "All releases"
"command.show.field.view.choice.--assets" = "Assets"
"command.show.field.view.choice.--detect" = "Asset for this machine"
"command.show.field.view.choice.--notes" = "Release notes"
```

Look at the result in French. `TOTE_LANG=fr tote plugin check gft` prints:

```
vérification (plugin livré avec tote) : gft
gft 1.0.0 (déclaratif)
ce qu’il lance :
  - commande show : gft '{slug}' '{tag?}' '{view?}'
    ne peut pas être construit : la valeur de slug n’est pas valide (une valeur est obligatoire)
...
```

### Its pins

A plugin shipped with tote carries two hashes compiled into the binary, in
`internal/plugin/bundled/pins.toml`:

```toml
[[plugin]]
id = "gft"
version = "1.0.0"
tree_sha256 = "f56d20b069f52ab5d1e18bc9e8a45ce30de964f55b0801e8208decd21e623098"
surface_sha256 = "aa61a534bc43b24875969ea4188bb8afff5637e2f8287a6717e4f97938491e6e"
```

They are the two hashes that `check` prints for any plugin:

```sh
tote plugin check gft --strict
```

```
hashes:
  tree sha256: f56d20b069f52ab5d1e18bc9e8a45ce30de964f55b0801e8208decd21e623098
  surface sha256: aa61a534bc43b24875969ea4188bb8afff5637e2f8287a6717e4f97938491e6e
grant token: derived:tty,env:GITHUB_TOKEN,exec:gft,fs:read:cwd,fs:write:any,fs:write:cwd,host:open-url,host:read:repos,net:*.githubusercontent.com,net:api.github.com,net:github.com@aa61a534bc43
gft: no problem found
```

The tree hash covers every file (so the permissions and the commands). The surface hash covers only
what runs: the commands, their working directories, environments, fields and confirmations, but not
the labels, descriptions or translations. A bundled plugin is installed by extraction, then verified
like any other. The install compares the version and both hashes with the pin and stops on a
difference, `check gft` reports one as `pin_mismatch`, and a test fails when a file, a version or a
hash moves without the matching edit in `pins.toml`. A tote developer who updates a bundled plugin
vendors the release of the plugin with `scripts/vendor.sh`, which checks the archive's hash, runs
`check --strict --official` and rewrites the pin. A new bundled plugin is the same two things and no
Go code: its folder `internal/plugin/bundled/plugins/<id>/` and its block in `pins.toml`, which the
script writes in alphabetical order.

For your own plugin the equivalents are the archive hash that you publish and the grant token. They
play the same part: the first says "these are the bytes", the second says "this is what they run".
The `official` ids (`gft`, `ghrel`, `ghsec`, `konsolai`, `ssh-tools`) are reserved: a plain
`install` of a directory or an archive refuses them, and only the copy that ships with tote and the
official releases (`totecli/<id>`) may use them. `--official` is a flag of `check` for tote's own
developers, to check a directory as if it were such a copy.

## 16. The errors you will meet

`check` prints each problem with its key path, and with its line when it has one. `check --json`
gives `findings[]` with a `code` that keeps its meaning (the set of codes can grow, and the words of
a message can change); the catalogs are keyed `plugin_err_<code>`, a dot or a dash of the code
becoming `_`. Here is what each of these looked like.

| You wrote                            | `check` says                                                                                         | Code                                                  |
| ------------------------------------ | ---------------------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| `permissions` without `exec:grep`    | `command[0].run[0], line 14: the command runs grep, which no exec: permission covers`                | `exec_uncovered`                                      |
| `{wrod}` in `run`                    | `command[0].run[3], line 14: {wrod} is not a known placeholder`                                      | `placeholder_unknown`                                 |
| `pattern = "[a-z]+"`                 | `command[0].field[0].pattern, line 21: the pattern [a-z]+ does not start with ^ and end with $`      | `pattern.unanchored`                                  |
| `confrim = true`                     | `command[0].confrim, line 16: unknown key`                                                           | `unknown_key`                                         |
| `run = ["sh", "-c", "..."]`          | `sh is a shell or a launcher: a command may not run it`                                              | `argv.shell`                                          |
| a command with no `output`           | `command[0].output, line 11: this key is required`                                                   | `key_missing`                                         |
| `net:example.org:8080`               | `permissions[1], line 9: invalid permission "net:example.org:8080" (host)`                           | `perm_invalid`                                        |
| `[[hook]]`                           | `hook: hook is not available in this version of tote (reserved for a later one)`                     | `reserved_not_available`                              |
| `tier = "exec"`                      | `tier, line 5: tier = "exec" is not available in this version of tote (reserved for a later one)`    | `reserved_not_available` (the plugin is incompatible) |
| `id = ""`                            | `id, line 2: the id "" is not allowed: use lowercase letters, digits and dashes, 2 to 32 characters` | `id_invalid`                                          |
| `version = ""`                       | `version, line 3: the version "" is not a strict semantic version`                                   | `version_invalid`                                     |
| `run = ["{tool}", ...]`              | `command[0].run[0], line 14: the program "" of a command is not a plain name`                        | `argv0_invalid`                                       |
| a `[[list]]` in a declarative plugin | `list: [list] is for exec plugins, this plugin is declarative`                                       | `tier_mismatch`                                       |
| `id = "repo"`                        | `id, line 2: the id "repo" is not allowed: it is a tote command`                                     | `id_invalid`                                          |
| `run.sh` next to `plugin.toml`       | `run.sh: a declarative plugin may not ship the file run.sh`                                          | `declarative_extra_file`                              |
| a choice with no English label       | `locales.en....choice.TODO: the translation lacks the key ...`                                       | `locale_missing_key` (warning)                        |
| a permission nothing uses            | `the permission exec:grep is declared but no command runs it`                                        | `exec_unused` (warning)                               |
| `version = "1.0"`                    | `version, line 3: the version "1.0" is not a strict semantic version`                                | `version_invalid`                                     |
| `tote = ">=2.0.0"` on tote 1.0.3     | `tote, line 4: this plugin needs tote >=2.0.0, you have 1.0.3`                                       | `tote_range`                                          |

Two notes. A `dev` build of tote satisfies every range, so `tote_range` only shows on a release
build. And when several things are wrong, `check` lists them all; the install commands report the
first.

The same kind of line exists for the errors of the verbs. The exit status tells the family:

| Exit | Meaning                                                                | Examples                                                                                                                                                                 |
| ---- | ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 0    | Done.                                                                  |                                                                                                                                                                          |
| 1    | Failure.                                                               | `not_installed`, a failed `check`, `downgrade`, `source_changed`, a refused package                                                                                      |
| 2    | Usage, or a flag is needed.                                            | `bad_source`, `hash_required`, `hash_malformed`, `hash_unexpected`, `consent_needed`, `bad_id`, `source_required`, `source_unsupported`, `confirm_needed`, `no_embedded` |
| 3    | Authentication required.                                               | `net_status` with status 401                                                                                                                                             |
| 4    | Refused by state or policy.                                            | `grant_mismatch`, `enable_refused`, `from_plugin`, `busy`, `stale`, and a package refused with `exec_self`, `exec_uncovered` or `perm_missing`                           |
| 5    | Integrity.                                                             | `hash_mismatch`, `too_large`, `tree_hash`, `platform_hash`, `origin_redirect`, and every `archive_*`                                                                     |
| 130  | Interrupted: Ctrl-C at the question, or SIGINT or SIGTERM at any step. | `interrupted`                                                                                                                                                            |

`plugin check` exits 5 when a finding is one of the integrity codes and 1 for any other failure. An
install or an update reports a refused package with the code of its first error when it is one of a
short list (an id, a permission, the API, the `tote` range, a reserved name, an extra file, the
program rule), and `manifest_invalid` otherwise. [API1.md](API1.md) section 8 is the complete list
of exit codes and codes, and lists every finding code of `check`.

## Not in 1.0

These exist in the manifest grammar or in the plans, and are **reserved**. Do not build on them.

- **The exec tier.** `tier = "exec"` (plugins with an `[exec]` program or `[[platform]]` builds,
  `[[action]]`, `[[list]]`, a JSON-lines protocol) is not available in tote 1.0. A manifest with
  `tier = "exec"` is refused with `reserved_not_available` on the key `tier`: `plugin check` reports
  it, `plugin install` and `plugin update` refuse it, and the plugin counts as incompatible with
  this tote. A plugin of that tier that an earlier tote left in the store is listed as
  `incompatible`, cannot be enabled and never runs. The features `exec.list` and `exec.run` are not
  announced, so `requires_features` cannot name them, and there is no `tote plugin run`
  (`tote: error: unknown subcommand "run"`). Do not write or publish one for 1.0.
- **A registry, search and revocation.** tote never asks the network what exists or what is new.
  Every install and update is a deliberate act with a hash you typed. There is no `update --all`.
- **Reserved tables and types**: `[[widget]]`, `[[panel]]`, `[[decorator]]`, `[[provider]]`,
  `[[connector]]`, `[[hook]]`, `[[job]]`, `[[screen]]`, `[[import]]`, `[[badge]]`, `[[search]]`; the
  field types `connection`, `connections`, `repo` and `path`; the keys `secret`, `remember` and
  `choices_from`; the setting types `dir` and `secret_ref`; the permissions `host:read:connections`,
  `host:handoff`, `host:clipboard`, `host:notify`, `host:editor`. A manifest that uses one is
  refused with `reserved_not_available`, so that nobody ships a feature that does nothing.
- **`ghrel` and `ghsec`** are not part of 1.0.
