#!/usr/bin/env bash
# Check the commit messages of a range against the conventions of the project.
#
#   scripts/commits.sh [<range>]      default: every commit reachable from HEAD
#   scripts/commits.sh origin/master..HEAD
#
# A commit is a single subject line, `SEVERITY: scope: Description`:
#   - severities MAJOR | MEDIUM | MINOR carry one scope: cmd ui config store shell plugin i18n update scripts
#   - BUILD and DOC carry no scope
#   - no body, no trailer, no trailing period, a capital letter after the colon
#   - never the word "refactor", and never a trace of an AI assistant (author, committer
#     or message): no Co-Authored-By, no "Generated with", no name of an AI assistant.
# Merge commits are skipped. A squash-merge suffix ` (#123)` is tolerated.
set -euo pipefail

range="${1:-HEAD}"
re_scoped='^(MAJOR|MEDIUM|MINOR): (cmd|ui|config|store|shell|plugin|i18n|update|scripts): [A-Z][^ ].*'
re_plain='^(BUILD|DOC): [A-Z][^ ].*'
# The names are written in pieces so that this file does not hold them.
re_ai='co-authored-by|generated with|clau''de|anthro''pic'
failures=0

fail() { printf '%s  %s\n    -> %s\n' "${1:0:7}" "$2" "$3" >&2; failures=$((failures + 1)); }

while read -r sha; do
    subject=$(git log -1 --format=%s "$sha")
    body=$(git log -1 --format=%b "$sha" | sed '/^[[:space:]]*$/d')
    identity=$(git log -1 --format='%an <%ae>%n%cn <%ce>' "$sha")
    message=$(git log -1 --format=%B "$sha")
    clean=${subject% \(#[0-9]*\)}

    if grep -qiE "$re_ai" <<< "$identity"$'\n'"$message"; then
        fail "$sha" "$subject" "mentions an AI assistant (author, committer, trailer or text)"
        continue
    fi
    if [[ -n "$body" ]]; then
        fail "$sha" "$subject" "a commit message is a single subject line, no body or trailer"
        continue
    fi
    if ! [[ "$clean" =~ $re_scoped || "$clean" =~ $re_plain ]]; then
        fail "$sha" "$subject" "expected 'SEVERITY: scope: Description' (BUILD and DOC have no scope)"
        continue
    fi
    if [[ "$clean" == *. ]]; then
        fail "$sha" "$subject" "no trailing period"
        continue
    fi
    if grep -qi 'refactor' <<< "$subject"; then
        fail "$sha" "$subject" "do not use the word 'refactor'"
        continue
    fi
done < <(git rev-list --no-merges "$range")

if (( failures > 0 )); then
    printf '\n%d commit(s) break the conventions (see the Conventions section of the README).\n' "$failures" >&2
    exit 1
fi
echo "Commit messages: OK"
