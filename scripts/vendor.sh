#!/usr/bin/env bash
# Vendor an official plugin: replace the copy embedded in the tote binary by a published release
# and rewrite its pin.
#
#   scripts/vendor.sh <id> <tag> <sha256>
#   scripts/vendor.sh gft 1.0.1 3f0c...e9
#
# What it does, in this order, and stops at the first failure:
#   1. downloads https://github.com/totecli/<id>/releases/download/<tag>/plugin-<id>.tar.gz
#      (the repository is named <id>, the archive plugin-<id>.tar.gz) over https only, with a
#      size cap;
#   2. checks the sha256 you pass against the archive;
#   3. lists the archive and refuses absolute paths, "..", links, devices and control characters;
#   4. extracts it into a new directory and runs `tote plugin check --strict --official` on it;
#   5. moves the old tree out of the way (it is moved, never deleted), moves the new one into
#      internal/plugin/bundled/plugins/<id> and rewrites the block of <id> in pins.toml, or inserts
#      it there in alphabetical order of the id. That is all a new bundled plugin takes: the
#      directory and the block, no Go edit.
#
# It never runs by itself: not from a test, not from a build, not from CI. Review the diff, run
# `go test ./internal/plugin/...` and commit it yourself. The directory it works in is printed and
# left behind; delete it when you no longer need the previous tree.
#
# Environment:
#   TOTE_BIN   tote binary that has the `plugin check` verb (default: tote in PATH)
set -euo pipefail

readonly base_url="https://github.com/totecli"
readonly max_bytes=16777216

die() { printf 'vendor: %s\n' "$*" >&2; exit 1; }

usage() {
    printf 'usage: %s <id> <tag> <sha256>\n' "${0##*/}" >&2
    exit 2
}

[ "$#" -eq 3 ] || usage
id=$1
tag=$2
want=$3

[[ $id =~ ^[a-z0-9][a-z0-9-]{1,31}$ ]] || die "invalid plugin id: $id"
[[ $tag =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]] || die "invalid tag: $tag"
[[ $want =~ ^[0-9a-f]{64}$ ]] || die "the sha256 must be 64 lower case hex digits"

for tool in curl tar awk sed grep mktemp mv; do
    command -v "$tool" >/dev/null 2>&1 || die "$tool is required"
done

if command -v sha256sum >/dev/null 2>&1; then
    sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
    sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
    die "sha256sum or shasum is required"
fi

tote=${TOTE_BIN:-tote}
command -v "$tote" >/dev/null 2>&1 || die "no tote binary: set TOTE_BIN"

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
bundled="$root/internal/plugin/bundled"
[ -f "$bundled/pins.toml" ] || die "not the tote repository: $bundled/pins.toml is missing"

work=$(mktemp -d "${TMPDIR:-/tmp}/vendor.XXXXXX")
archive="$work/plugin-$id.tar.gz"
stage="$work/new"
mkdir "$stage"
printf 'working in %s\n' "$work"

# 1. Download.
url="$base_url/$id/releases/download/$tag/plugin-$id.tar.gz"
printf 'downloading %s\n' "$url"
final=$(curl --fail --silent --show-error --location \
    --proto '=https' --proto-redir '=https' --tlsv1.2 --max-redirs 3 \
    --max-filesize "$max_bytes" --max-time 120 \
    --write-out '%{url_effective}' \
    --output "$archive" "$url") || die "download failed"

# The redirects of a release download may only land on the hosts of the specification (7.2).
host=${final#https://}
host=${host%%[/?#]*}
case $host in
    github.com | objects.githubusercontent.com | release-assets.githubusercontent.com) ;;
    *) die "the download ended on an unexpected host: $host" ;;
esac

size=$(wc -c <"$archive")
if [ "$size" -le 0 ] || [ "$size" -gt "$max_bytes" ]; then
    die "the archive is $size bytes"
fi

# 2. Hash of the archive.
got=$(sha256 "$archive")
[ "$got" = "$want" ] || die "sha256 mismatch: expected $want, downloaded $got"

# 3. Entries of the archive, before anything is extracted.
names=$(tar -tzf "$archive") || die "not a gzip tar archive"
details=$(tar -tzvf "$archive") || die "not a gzip tar archive"
[ -n "$names" ] || die "empty archive"
[ "$(printf '%s\n' "$names" | wc -l)" -eq "$(printf '%s\n' "$details" | wc -l)" ] \
    || die "the archive has entries with a newline in their name"

while IFS= read -r line; do
    case ${line:0:1} in
        - | d) ;;
        *) die "the archive holds an entry that is not a file or a directory: $line" ;;
    esac
done <<<"$details"

while IFS= read -r name; do
    case $name in
        /* | ../* | */../* | */.. | ..) die "unsafe path in the archive: $name" ;;
    esac

    if [[ $name =~ [[:cntrl:]] ]]; then
        die "control character in a path of the archive"
    fi
done <<<"$names"

# 4. Extract into a new directory and check.
tar -xzf "$archive" -C "$stage" --no-same-owner --no-same-permissions || die "extraction failed"
[ -f "$stage/plugin.toml" ] || die "plugin.toml is not at the root of the archive"

if ! report=$("$tote" plugin check --strict --json --official "$stage"); then
    printf '%s\n' "$report" >&2
    die "plugin check --strict failed"
fi

field() {
    if command -v jq >/dev/null 2>&1; then
        printf '%s' "$report" | jq -r --arg k "$1" '.[$k] // empty | strings' | head -n 1
        return
    fi

    printf '%s' "$report" | grep -o "\"$1\": *\"[^\"]*\"" | head -n 1 | sed 's/^[^:]*: *"\(.*\)"$/\1/'
}

version=$(field version)
tree=$(field tree_sha256)
surface=$(field surface_sha256)

[[ $version =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]] || die "no version in the report"
[[ $tree =~ ^[0-9a-f]{64}$ ]] || die "no tree_sha256 in the report"
[[ $surface =~ ^[0-9a-f]{64}$ ]] || die "no surface_sha256 in the report"
[ "$(field id)" = "$id" ] || die "the plugin in the archive is not $id"

# 5. Replace the tree and the pin. The old tree is moved aside, never deleted. The new tree is
# first moved next to its final place, so that a failure cannot leave the directory missing.
trees="$bundled/plugins"
mkdir -p "$trees" || die "cannot create $trees"
mv "$stage" "$trees/.new-$id" || die "cannot move the new tree"

if [ -e "$trees/$id" ]; then
    mv "$trees/$id" "$work/previous-$id" || die "cannot move the old tree"
fi

mv "$trees/.new-$id" "$trees/$id" || die "cannot move the new tree into place"

# The block of the plugin replaces the old one, or is inserted in alphabetical order of the id; the
# comments before the first block are kept as they are.
pins="$bundled/pins.toml"
next="$work/pins.toml"

awk -v id="$id" -v version="$version" -v tree="$tree" -v surface="$surface" '
    function render(i) {
        return "[[plugin]]\nid = \"" ids[i] "\"\nversion = \"" vers[i] "\"\n" \
            "tree_sha256 = \"" trees[i] "\"\nsurface_sha256 = \"" surfs[i] "\""
    }
    /^\[\[plugin\]\][[:space:]]*$/ { n++; inblock = 1; next }
    !inblock { head = head $0 "\n"; next }
    /^id[[:space:]]*=/ { v = $0; sub(/^[^"]*"/, "", v); sub(/".*$/, "", v); ids[n] = v; next }
    /^version[[:space:]]*=/ { v = $0; sub(/^[^"]*"/, "", v); sub(/".*$/, "", v); vers[n] = v; next }
    /^tree_sha256[[:space:]]*=/ { v = $0; sub(/^[^"]*"/, "", v); sub(/".*$/, "", v); trees[n] = v; next }
    /^surface_sha256[[:space:]]*=/ { v = $0; sub(/^[^"]*"/, "", v); sub(/".*$/, "", v); surfs[n] = v; next }
    END {
        for (i = 1; i <= n; i++) {
            if (ids[i] == id) {
                vers[i] = version; trees[i] = tree; surfs[i] = surface; found = 1
            }
        }
        if (!found) {
            n++; ids[n] = id; vers[n] = version; trees[n] = tree; surfs[n] = surface
        }
        for (i = 2; i <= n; i++) {
            for (j = i; j > 1 && ids[j - 1] > ids[j]; j--) {
                t = ids[j]; ids[j] = ids[j - 1]; ids[j - 1] = t
                t = vers[j]; vers[j] = vers[j - 1]; vers[j - 1] = t
                t = trees[j]; trees[j] = trees[j - 1]; trees[j - 1] = t
                t = surfs[j]; surfs[j] = surfs[j - 1]; surfs[j - 1] = t
            }
        }
        printf "%s", head
        for (i = 1; i <= n; i++) {
            if (i > 1) print ""
            print render(i)
        }
    }
' "$pins" >"$next" || die "cannot rewrite pins.toml"

mv "$next" "$pins" || die "cannot write pins.toml"

printf '\n%s %s is vendored.\n' "$id" "$version"
printf '  tree_sha256    %s\n  surface_sha256 %s\n' "$tree" "$surface"

if [ -e "$work/previous-$id" ]; then
    printf '  previous tree  %s\n' "$work/previous-$id"
fi

printf '\n'

printf 'Next: go test ./internal/plugin/..., read the diff, then commit.\n'
