# Releasing tote

How a release is built, signed and published, and what the maintainer does by hand. Nothing in the
pipeline publishes by itself: the workflow stops at a **draft** GitHub release that the maintainer
reviews and publishes.

## How it works

- `.goreleaser.yaml` (GoReleaser v2) builds `linux` and `darwin` on `amd64` and `arm64` with
  `CGO_ENABLED=0 -trimpath`, injects the version (`-X main.version`) and the release public keys
  (`-X .../internal/update.releaseKeys`), and produces:
  - four archives `tote_<version>_<os>_<arch>.tar.gz` (owner `root:root`, modes `0644` and `0755`,
    times of the tagged commit);
  - two `.deb` files `tote_<version>_linux_<arch>.deb` (package version `1.1.0~beta.1-1` for the
    SemVer `1.1.0-beta.1`, `Recommends: git, openssh-client, xclip | xsel | wl-clipboard`, MIT
    copyright, `/usr/share/tote/install-method` so that a packaged tote never updates itself: the
    file holds `install_method=apt` and `package=deb`, and with `package=deb` (the file of a GitHub
    release, which no apt repository follows) tote tells the user to download the new file and run
    `sudo apt install ./tote_<version>_linux_<arch>.deb`; `package=ppa` and `package=apt` get
    `sudo apt update && sudo apt upgrade tote`. Once the apt repository ships those `.deb` files,
    they must carry `package=apt` in `packaging/deb/install-method`, or they keep the GitHub hint
    (see step 7 of "Each release");
  - `tote_<version>_checksums.txt` (SHA-256) and its minisign signature `.minisig`, whose trusted
    comment holds `project`, `version`, `tag`, `channel` (`stable` or `beta`), `timestamp` and
    `file`, separated by tabs.
- `packaging/preflight.sh` runs first and refuses a real release that lacks release keys, the
  signing key file, its password or the GitHub token, listing every problem at once. It also makes a
  dry signature with the key file and the password and verifies it against the release keys, so a
  wrong password or a secret that is not one of the two keys stops the run before anything is
  uploaded, and it refuses a `README.md` that still holds a `PLACEHOLDER` (see "Before the first
  release", step 7).
- `.github/workflows/release.yml` runs on a push of a tag `v*`, in the GitHub environment `release`
  (the maintainer approves each run). It builds with the latest patch of the Go 1.26 line
  (`go-version: "1.26"`, `check-latest`; `go.mod` says `go 1.26.0`, which alone would give the first
  release of the line, without its security fixes) and without any build cache, since the job holds
  the signing secrets. It checks the tag name and that the tagged commit is on `master`, writes the
  signing key to `RUNNER_TEMP` (umask 077), runs GoReleaser, runs `packaging/verify.sh dist`
  (minisign signature against the release keys, trusted comment, six expected files, `sha256sum -c`,
  and the linux amd64 binary of the archive: it embeds the release keys and reports
  `tote <version>`), and removes the key file.
- The release is created as a **draft**; a tag with a pre-release part (`v1.1.0-beta.1`) makes a
  pre-release. Release notes are never generated: the maintainer writes them in the draft. A second
  run for the same tag reuses the draft (found by its name, the tag), keeps its notes and replaces
  the assets already uploaded to it (`replace_existing_artifacts`).
- `.github/workflows/ci.yml` tests with the minimum Go of `go.mod` and runs three more jobs: `vuln`
  (`govulncheck` with the latest 1.26 patch, the toolchain of the release), `release-dry-run`
  (`goreleaser check` and a snapshot build, nothing signed or uploaded) and `macos` (builds tote
  natively, runs it and tests every package but the interface and the tmux end-to-end tests; it does
  not block the run). On CI the tests that need `tmux`, `zsh`, `minisign` and the like fail when the
  program is missing (`CI=true`; set `TOTE_REQUIRE_TOOLS=1` to get the same elsewhere), they do not
  skip.
- `install.sh` is not part of the pipeline. Its draft stays outside the repository and is published
  by the maintainer (see below); it is checked against the same signed checksums file.

## Dry run

No key, no token, nothing uploaded; the output goes to `dist/` (git-ignored):

```sh
make snapshot        # goreleaser release --snapshot --skip=sign --clean
```

The binary then carries the placeholder keys, so `tote update` refuses and `tote doctor` says so.
`--skip=sign` is for snapshots only: a real release always needs the signing setup.

To test signing as well, create throwaway keys with `minisign -G`, export `TOTE_RELEASE_KEYS` (their
public keys), `MINISIGN_KEY_FILE`, `MINISIGN_PASSWORD`, a dummy `GITHUB_TOKEN` and
`TOTE_MAINTAINER`, then build as a real release without publishing, on a fake tag, and verify the
result. While the `README.md` still holds its placeholders, the preflight refuses the run unless
`TOTE_ALLOW_PLACEHOLDER_README=1` is set (for such a test run only, never for a real release):

```sh
TOTE_ALLOW_PLACEHOLDER_README=1 GORELEASER_CURRENT_TAG=v1.0.0 \
    goreleaser release --clean --skip=publish,validate,announce
GITHUB_REF_NAME=v1.0.0 packaging/verify.sh dist
```

A snapshot is not suitable for this: its version has a pre-release part but GoReleaser reports no
pre-release for it, so its signature says `channel:stable` and `verify.sh` expects `beta`.

## Try the install without publishing

The whole chain (build, signature, checksums, `install.sh`, `tote doctor`) can be played on one
machine with throwaway keys and a folder that stands for the GitHub release page. Nothing is
published and nothing outside the throwaway folders is touched. You need `goreleaser` v2 and
`minisign` (see the README for the `go install` line), a copy of `install.sh` with the real keys,
and about five minutes (the build is most of it).

```sh
SIM=$(mktemp -d) && cd "$SIM"
# 1. two throwaway keys, never the real ones (the password is read from the pipe)
for k in a b; do printf 'simpass\nsimpass\n' | minisign -G -p $k.pub -s $k.key; done
# 2. build and sign a v1.0.0 from a clean clone: only committed files are released
git clone --local <path of your checkout> src && cd src
export TOTE_RELEASE_KEYS="$(sed -n 2p ../a.pub) $(sed -n 2p ../b.pub)" \
       MINISIGN_KEY_FILE=$SIM/a.key MINISIGN_PASSWORD=simpass GITHUB_TOKEN=dummy \
       TOTE_MAINTAINER="Name <address>" GORELEASER_CURRENT_TAG=v1.0.0
goreleaser release --clean --skip=publish,validate,announce
GITHUB_REF_NAME=v1.0.0 packaging/verify.sh dist        # must end with "binary ok"
# 3. the folder that stands for the release page: <base>/download/v<version>/<asset>
mkdir -p "$SIM/root/download/v1.0.0" && cp dist/tote_1.0.0_* "$SIM/root/download/v1.0.0/"
# 4. install into a throwaway HOME with the key of the simulation
mkdir -p "$SIM/home/.local/bin"
env -i PATH=/usr/bin:/bin HOME="$SIM/home" TOTE_INSTALL_BASE="file://$SIM/root" \
    TOTE_INSTALL_PUBKEY="$(sed -n 2p $SIM/a.pub)" TOTE_INSTALL_YES=1 \
    sh <path of install.sh> 1.0.0
# 5. use it
HOME="$SIM/home" PATH="$SIM/home/.local/bin:/usr/bin:/bin" tote doctor
```

The version must be given (`1.0.0`): a `file://` base has no `latest` redirect to follow. The
installer must say "Signature verified with minisign", and `tote doctor` must show the receipt
(`installed with script`) and no FAIL; the only warning is the missing shell integration, which a
throwaway HOME does not have. Delete `$SIM` by hand when you are done.

## Before the first release (once, in this order)

1. **Install minisign 0.10 or later** on the maintainer machine (`apt install minisign`,
   `brew install minisign`).
2. **Create the signing keys**, run by the maintainer only, never by CI and never on a shared
   machine:
   - the release key: `minisign -G -p tote.pub -s tote.key`;
   - the next key, kept for a later rotation: `minisign -G -p tote-next.pub -s tote-next.key`, with
     a different password; move `tote-next.key` offline right away.

   Keep both passwords in a password manager. The secret keys never enter this repository. To print
   the two public keys: `sed -n 2p tote.pub` and `sed -n 2p tote-next.pub`.

3. **Write the public keys down.** The public key is the second line of each `.pub` file (it starts
   with `RW`). Two keys, current then next, separated by one space:
   - GitHub repository variable `TOTE_RELEASE_KEYS` (Settings, Secrets and variables, Actions,
     Variables). It is injected into the binary at build time and used by `verify.sh`;
   - the two placeholder lines `RW_CURRENT_RELEASE_KEY_PLACEHOLDER` and
     `RW_NEXT_RELEASE_KEY_PLACEHOLDER` of `TOTE_MINISIGN_KEYS` in the copy of `install.sh` that is
     published (the installer refuses to run with placeholders);
   - the `README.md` of the repository, see step 7;
   - for a local release build only: `make build VERSION=1.0.0 KEYS="<current> <next>"`. The source
     default of `internal/update/key.go` stays a placeholder on purpose.
4. **Create the GitHub environment `release`** in `totecli/tote` (Settings, Environments):
   - required reviewers: the maintainer;
   - deployment branches and tags: tags matching `v*` only;
   - environment secrets `MINISIGN_SECRET_KEY` (the whole content of `tote.key`) and
     `MINISIGN_PASSWORD` (its password). Environment secrets are readable only by approved runs.

   Set the variable `TOTE_MAINTAINER` (`Name <address>`), the `Maintainer` field of the `.deb`; the
   release preflight refuses to run without it, and a snapshot says `maintainer@example.org`. With
   the `gh` command line, from the directory that holds the key files:

   ```sh
   KEYS="$(sed -n 2p tote.pub) $(sed -n 2p tote-next.pub)"
   gh variable set TOTE_RELEASE_KEYS --repo totecli/tote --body "$KEYS"
   gh variable set TOTE_MAINTAINER --repo totecli/tote --body "Name <address>"
   gh api -X PUT repos/totecli/tote/environments/release --input - <<EOF
   {"reviewers":[{"type":"User","id":$(gh api user --jq .id)}],
    "deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true}}
   EOF
   gh api -X POST repos/totecli/tote/environments/release/deployment-branch-policies \
       -f name='v*' -f type=tag
   gh secret set MINISIGN_SECRET_KEY --env release --repo totecli/tote < tote.key
   gh secret set MINISIGN_PASSWORD --env release --repo totecli/tote   # asks for the password
   ```

5. **Optional hardening**: a tag ruleset on `v*` (only the maintainer may create or move them) and a
   signed-commits rule on `master`.
6. **Check CI is green on `master`** and run `make snapshot` once.
7. **Write the keys into the README, before tagging.** The archives ship `README.md` as it is at the
   tag, so a release made with the placeholders would publish them for good, and the keys would be
   published nowhere out of band. In the "Install" section: replace
   `RW_CURRENT_RELEASE_KEY_PLACEHOLDER` and `RW_NEXT_RELEASE_KEY_PLACEHOLDER` by the two public
   keys, write the real install command (the address of the published `install.sh`), change the
   "planned for 1.0.0" states of the status table to what is true, and replace the sentence "Nothing
   is released yet". The two keys can be written with
   `sed -i "s|RW_CURRENT_RELEASE_KEY_PLACEHOLDER|$(sed -n 2p tote.pub)|; s|RW_NEXT_RELEASE_KEY_PLACEHOLDER|$(sed -n 2p tote-next.pub)|" README.md`.
   Commit it, then tag. `packaging/preflight.sh` refuses a release whose `README.md` still contains
   `PLACEHOLDER`.

## Each release

1. Make sure the commit to release is on `master`, CI is green (including `vuln` and
   `release-dry-run`) and `make race` passes on your machine: CI does not run the race detector on
   `internal/ui`, whose frame tests take over 30 minutes with it (the target sets `-timeout 30m`).
2. Create the signed tag and push it. Nothing else is pushed by the pipeline:
   ```sh
   git tag -s v1.0.0 -m v1.0.0
   git push <remote> v1.0.0     # the name of your remote for totecli/tote
   ```
3. Approve the run of the workflow `release` in the Actions tab. It must end green, including the
   step "Verify the signature and the checksums".
4. Open the draft release `v1.0.0`: it holds the six archives and packages, the checksums file and
   its `.minisig`. Write the notes (French), empty the title field, and publish. Do not re-run the
   workflow for that tag after emptying the title: the draft is found by its name, so a second draft
   would be created.
5. Check the published files from outside, with the public key file:
   ```sh
   minisign -V -H -p tote.pub -m tote_1.0.0_checksums.txt
   sha256sum -c tote_1.0.0_checksums.txt
   ```
6. Stable versions only: publish `install.sh` (with the real keys, see step 3 above) in the place it
   is served from, under the version, and test `curl ... | sh` in a clean container.
7. Stable versions the maintainer judges important: add the two `.deb` files to the signed apt
   repository and build the PPA source package, both with the maintainer's own scripts and keys. The
   `.deb` files for the repository must have `package=apt` in `packaging/deb/install-method` (the
   PPA package says `package=ppa`); the files attached to the GitHub release keep `deb`. Betas are
   never published to apt or to the PPA; they ship as GitHub pre-releases, installed with
   `install.sh <version>` or, from a tote that is already installed, with `tote update --beta`. A
   tote on the stable channel stays on it unless `--beta` is given, its install receipt says beta or
   the running build is itself a pre-release.

## Where the install script lives

`install.sh` stays out of the repository on purpose: it is a self-contained POSIX script published
on the maintainer's file host, and its two key lines are filled in at publication. The draft is kept
with the maintainer's other private distribution drafts (together with `keys.sh`, `publish-apt.sh`
and the PPA scripts). Keep its behaviour in step with `internal/update` (archive and checksums
names, trusted comment fields, key format).

## Rotating the key

A signature names the key that made it (its key id), and tote and `install.sh` accept every key they
carry. A tote that is already installed carries the keys of the release it came from and learns no
other: the key list cannot be extended from the outside. One rule follows, the bridge release rule:

> A release may only be signed with a key that every client still supported already embeds. A new
> key reaches the clients through a bridge release, which embeds it before anyone signs with it.

Two keys are always shipped, the current one (A) and the next one (B), and `preflight.sh` and
`verify.sh` refuse a build that has fewer than two different keys. Every client embeds both, so a
release signed with B is accepted by all of them. The steps:

1. Create the key C that follows B (`minisign -G`) and keep its secret part offline.
2. Build the bridge release: it embeds B and C (`TOTE_RELEASE_KEYS="<B> <C>"`, or all three during
   the overlap, `<A> <B> <C>`) and is signed with A or B, never with C.
3. Wait until the supported clients have taken the bridge release. A client that never did and meets
   a release signed with a key it does not embed says
   `signed with a key this version of tote does not know` (error name `unknown_key`, not
   `bad_signature`) and tells the user to update with `install.sh` or with the package manager.
4. Only then sign with the key the clients now embed as their next one, and drop A from
   `TOTE_RELEASE_KEYS` once no supported client needs it. Put the signing key file in
   `MINISIGN_SECRET_KEY` and its password in `MINISIGN_PASSWORD`.

A compromise of the current key means a release signed with the next key at once (every client
already embeds it), then the bridge release for the key that follows, and a new `install.sh`.

## What a release must keep, release after release

These are contracts with every installed tote, with `install.sh` and with the packages. Changing one
breaks clients that cannot be fixed afterwards.

- Asset names: `tote_<version>_<os>_<arch>.tar.gz` for the archives, `tote_<version>_checksums.txt`
  for the checksums and `tote_<version>_checksums.txt.minisig` for the signature.
- The trusted comment of the signature: the tab separated fields `project`, `version`, `tag`,
  `channel`, `timestamp` and `file`.
- The first line printed by `tote version` is `tote <semver>`; the updater checks the downloaded
  binary with it.
- The install receipt `install.json` keeps `"schema": 1`. A later release only adds optional fields,
  and a binary that does not know a field keeps it when it rewrites the file.
- The JSON documents of the command line start with `"schema": 1` and only gain fields; the exit
  codes 0, 1, 2, 3, 4, 5, 10, 127 and 130 keep their meaning and new codes use 6 to 9 and 11 and
  above.
- `release.yml` holds the signing secrets, so its actions are pinned by commit SHA, with the tag in
  a comment. To move one, resolve the new tag with
  `gh api repos/<owner>/<repo>/commits/<tag> --jq .sha` and read what changed before editing the
  workflow. `ci.yml` and `commits.yml` have no secrets and use the moving major tag (`@v7`).

## When something fails

| Symptom                                                 | Cause                                                                                                               |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| `preflight: ... is not set`                             | a secret, a variable or the token is missing; the message names it                                                  |
| `map has no entry for key "TOTE_RELEASE_KEYS"`          | the variable is not set and the preflight was skipped                                                               |
| `README.md still holds a PLACEHOLDER`                   | step 7 of "Before the first release" was not done: write the keys into the README and commit before tagging         |
| `is not the secret key of any key of TOTE_RELEASE_KEYS` | `MINISIGN_SECRET_KEY` is another key than the two public keys of the variable, or the next key is not yet listed    |
| `cannot sign with MINISIGN_PASSWORD`                    | the password does not open the key, or the secret is not a minisign secret key                                      |
| `does not embed the release key`                        | the binary of the archive was built without `TOTE_RELEASE_KEYS`: the variable is not the one the build used         |
| `no release key verifies ...minisig`                    | the signing key does not match `TOTE_RELEASE_KEYS`                                                                  |
| `unexpected trusted comment`                            | the tag, version or channel in the signature differ from the tag pushed                                             |
| `does not point at a commit of master`                  | the tag was made on another branch                                                                                  |
| `is not a version tag`                                  | the tag is not strict SemVer with a `v` prefix (no leading zeros)                                                   |
| `Wrong password for that key`                           | `MINISIGN_PASSWORD` does not belong to the key in `MINISIGN_SECRET_KEY`                                             |
| the verify step fails after the upload                  | the draft exists with its assets: delete the draft (and the tag if it must move), fix the cause, push the tag again |
