#!/usr/bin/env bash
# Refuse a real release build that lacks its signing setup, before anything is built.
#
#   packaging/preflight.sh snapshot|release
#
# snapshot  a dry run (goreleaser release --snapshot): nothing to check, nothing is uploaded.
# release   checks the environment of a real release and lists every problem at once:
#           - TOTE_RELEASE_KEYS: at least two different well formed minisign public keys, the
#             current one and the next one, no placeholder (see "Rotating the key" in
#             docs/RELEASING.md: a build that knows only one key cannot be moved to another);
#           - MINISIGN_KEY_FILE: the readable secret key file, and minisign installed;
#           - MINISIGN_PASSWORD: the password of that key;
#           - a dry signature with that key and password, verified against the keys of
#             TOTE_RELEASE_KEYS: the secret key must be one of them;
#           - README.md: no placeholder left (the release keys and the install command are
#             written into it before the tag, see docs/RELEASING.md); set
#             TOTE_ALLOW_PLACEHOLDER_README=1 for a throw-away test run only;
#           - GITHUB_TOKEN: the token that creates the draft release;
#           - TOTE_MAINTAINER: the Maintainer field of the .deb, as `Name <address>`.
# Run by GoReleaser as a before hook (see .goreleaser.yaml).
set -euo pipefail

mode=${1:-}
case $mode in
    snapshot)
        echo "preflight: snapshot, signing and upload are not required"
        exit 0
        ;;
    release) ;;
    *)
        echo "usage: preflight.sh snapshot|release" >&2
        exit 2
        ;;
esac

problems=0
fail() {
    echo "preflight: $*" >&2
    problems=$((problems + 1))
}

keys=${TOTE_RELEASE_KEYS:-}
if [[ -z ${keys//[[:space:]]/} ]]; then
    fail "TOTE_RELEASE_KEYS is empty: a build without release keys cannot update itself"
else
    good=
    for key in $keys; do
        if [[ $key == *PLACEHOLDER* ]]; then
            fail "TOTE_RELEASE_KEYS holds a placeholder: $key"
        elif ! [[ $key =~ ^RW[A-Za-z0-9+/]{54}$ ]]; then
            fail "TOTE_RELEASE_KEYS holds something that is not a minisign public key: $key"
        else
            good+="$key"$'\n'
        fi
    done
    if (($(printf '%s' "$good" | sort -u | grep -c .) < 2)); then
        fail "TOTE_RELEASE_KEYS must hold at least two different keys, current then next: a build with one key cannot follow a key rotation"
    fi
fi

if ! command -v minisign > /dev/null 2>&1; then
    fail "minisign is not installed"
fi
if [[ -z ${MINISIGN_KEY_FILE:-} ]]; then
    fail "MINISIGN_KEY_FILE is not set"
elif [[ ! -f $MINISIGN_KEY_FILE || ! -r $MINISIGN_KEY_FILE ]]; then
    fail "MINISIGN_KEY_FILE is not a readable file: $MINISIGN_KEY_FILE"
elif [[ ! -s $MINISIGN_KEY_FILE ]]; then
    fail "MINISIGN_KEY_FILE is empty: $MINISIGN_KEY_FILE"
else
    IFS= read -r first < "$MINISIGN_KEY_FILE" || true
    if [[ $first != "untrusted comment:"* ]]; then
        fail "MINISIGN_KEY_FILE is not a minisign secret key file: $MINISIGN_KEY_FILE"
    fi
fi
if [[ -z ${MINISIGN_PASSWORD:-} ]]; then
    fail "MINISIGN_PASSWORD is not set"
fi

# Dry signature: proves that the password opens the key and that the key is one of the keys
# built into the binary. Without that, a wrong secret is only found after the draft release
# and its assets exist. The password goes through a pipe, as GoReleaser passes it.
if ((problems == 0)); then
    work=$(mktemp -d "${TMPDIR:-/tmp}/tote-preflight.XXXXXX")
    trap 'rm -f "$work/file" "$work/file.minisig"; rmdir "$work"' EXIT
    echo tote > "$work/file"
    if ! printf '%s\n' "$MINISIGN_PASSWORD" |
        minisign -S -s "$MINISIGN_KEY_FILE" -m "$work/file" -x "$work/file.minisig" > /dev/null 2>&1; then
        fail "MINISIGN_KEY_FILE cannot sign with MINISIGN_PASSWORD: wrong password, or not a minisign secret key"
    else
        matched=
        for key in $keys; do
            if minisign -V -q -P "$key" -x "$work/file.minisig" -m "$work/file" > /dev/null 2>&1; then
                matched=1
                break
            fi
        done
        if [[ -z $matched ]]; then
            fail "MINISIGN_KEY_FILE is not the secret key of any key of TOTE_RELEASE_KEYS: wrong secret, or the key is not listed"
        fi
    fi
fi

# The archives ship the README as it is at the tag: refuse a release that still holds the
# placeholders of the release keys.
readme=$(dirname -- "${BASH_SOURCE[0]}")/../README.md
if [[ ${TOTE_ALLOW_PLACEHOLDER_README:-} != 1 ]] && grep -q PLACEHOLDER "$readme" 2> /dev/null; then
    fail "README.md still holds a PLACEHOLDER: write the public keys and the install command into it, commit, and tag again"
fi

if [[ -z ${GITHUB_TOKEN:-} ]]; then
    fail "GITHUB_TOKEN is not set"
fi

re_maintainer='^[^<>]+ <[^<>@ ]+@[^<>@ ]+>$'
if ! [[ ${TOTE_MAINTAINER:-} =~ $re_maintainer ]]; then
    fail "TOTE_MAINTAINER must be 'Name <address>', the Maintainer field of the .deb"
fi

if ((problems > 0)); then
    echo "preflight: $problems problem(s): no release was built" >&2
    exit 1
fi
echo "preflight: release environment is complete"
