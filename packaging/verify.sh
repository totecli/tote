#!/usr/bin/env bash
# Verify the files of a release built by GoReleaser, the way a user's tote and install.sh will.
#
#   packaging/verify.sh [<dist-dir>]      default: dist
#
# Needs minisign 0.10 or later and the release public keys in TOTE_RELEASE_KEYS (base64 keys
# separated by spaces, current then next; at least two different keys are required, as in
# preflight.sh). Checks, and stops at the first failure:
#   - exactly one tote_<version>_checksums.txt, with its .minisig;
#   - the signature verifies, prehashed, against one of the keys;
#   - the trusted comment: project, version, tag, channel (beta when the version has a
#     pre-release part), timestamp and file name, with the tab separators;
#   - the checksums list four archives and two .deb files, none with a "~" in its name;
#   - sha256sum -c finds every listed file with its digest;
#   - the linux amd64 binary of the archive embeds every release key and,
#     on a linux amd64 host, `tote version` prints `tote <version>` as its first line (a flag
#     of the build that silently did nothing would give a signed release that refuses to update).
# The tag is taken from GITHUB_REF_NAME when it is set (CI), otherwise it is v<version>.
set -euo pipefail

dist=${1:-dist}
[[ -d $dist ]] || {
    echo "verify: $dist is not a directory" >&2
    exit 2
}
dist=$(cd "$dist" && pwd)

fail() {
    echo "verify: $*" >&2
    exit 1
}

command -v minisign > /dev/null 2>&1 || fail "minisign is not installed"
keys=${TOTE_RELEASE_KEYS:-}
[[ -n ${keys//[[:space:]]/} ]] || fail "TOTE_RELEASE_KEYS is empty"
count=$(tr -s '[:space:]' '\n' <<< "$keys" | sort -u | grep -c . || true)
((count >= 2)) || fail "TOTE_RELEASE_KEYS must hold at least two different keys, current then next"

shopt -s nullglob
found=("$dist"/tote_*_checksums.txt)
((${#found[@]} == 1)) || fail "expected one checksums file in $dist, found ${#found[@]}"
sums=${found[0]}
name=${sums##*/}
version=${name#tote_}
version=${version%_checksums.txt}
sig=$sums.minisig
[[ -f $sig ]] || fail "$name.minisig is missing"

comment=
for key in $keys; do
    if comment=$(minisign -V -H -Q -P "$key" -x "$sig" -m "$sums" 2> /dev/null); then
        break
    fi
    comment=
done
[[ -n $comment ]] || fail "no release key verifies $name.minisig"

tag=${GITHUB_REF_NAME:-v$version}
channel=stable
[[ $version == *-* ]] && channel=beta
tab=$'\t'
want="project:tote${tab}version:$version${tab}tag:$tag${tab}channel:$channel${tab}timestamp:"
rest=${comment#"$want"}
[[ $rest != "$comment" ]] || fail "unexpected trusted comment: $comment"
[[ $rest =~ ^[0-9]+${tab}file:$name$ ]] || fail "unexpected trusted comment: $comment"

archive_re="  tote_${version}_(linux|darwin)_(amd64|arm64)\.tar\.gz\$"
deb_re="  tote_${version}_linux_(amd64|arm64)\.deb\$"
archives=$(grep -c -E "$archive_re" "$sums" || true)
debs=$(grep -c -E "$deb_re" "$sums" || true)
((archives == 4)) || fail "expected 4 archives in $name, found $archives"
((debs == 2)) || fail "expected 2 .deb files in $name, found $debs"
if grep -q '~' "$sums"; then
    fail "a file name with a \"~\" in $name"
fi

(cd "$dist" && sha256sum --strict -c "$name") || fail "sha256sum -c failed"

# The build flags are not covered by the signature: look at what the binary really holds.
work=$(mktemp -d "${TMPDIR:-/tmp}/tote-verify.XXXXXX")
trap 'rm -f "$work/tote"; rmdir "$work" 2> /dev/null || true' EXIT
archive=tote_${version}_linux_amd64.tar.gz
tar -xmzf "$dist/$archive" -C "$work" tote || fail "cannot extract tote from $archive"
for key in $keys; do
    grep -aqF -- "$key" "$work/tote" || fail "the binary of $archive does not embed the release key $key"
done
ran=", not run here"
if [[ $(uname -s)-$(uname -m) == Linux-x86_64 ]]; then
    first=$(env -i HOME="$work" PATH=/usr/bin:/bin "$work/tote" version) ||
        fail "$archive: tote version failed"
    first=${first%%$'\n'*}
    [[ $first == "tote $version" ]] || fail "$archive: tote version says '$first', expected 'tote $version'"
    ran=
fi

echo "verify: $name is signed by a release key, comment ok ($channel, $tag), digests match, binary ok$ran"
