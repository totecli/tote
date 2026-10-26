#!/usr/bin/env bash
# Check the Markdown files against the conventions of the project.
#
#   scripts/docs.sh [file...]      default: README.md and every .md file below docs/
#
# A line of prose is at most 100 columns. Fenced code, tables and a line whose 100th column lies
# inside a code span (it cannot be wrapped there) are left alone. A tab or a trailing space is an
# error in prose (a fenced block may hold tab-separated output). Exit status: 0 ok, 1 an error.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
if (($# > 0)); then
    files=("$@")
else
    mapfile -t files < <(printf '%s\n' README.md; find docs -name '*.md' | sort)
fi

LC_ALL=C.UTF-8 awk -v max=100 '
function inspan(s,   i, n, c) {          # true when column max falls inside backticks
    c = 0
    for (i = 1; i <= max && i <= length(s); i++) if (substr(s, i, 1) == "`") c++
    return c % 2 == 1
}
FNR == 1 { fence = 0 }
/^[ \t]*```/ { fence = !fence; next }
{
    if ($0 ~ /\t/ && !fence) { printf "%s:%d: tab\n", FILENAME, FNR; bad++ }
    if ($0 ~ /[ \t]+$/ && !fence) { printf "%s:%d: trailing space\n", FILENAME, FNR; bad++ }
    if (fence || $0 ~ /^[ \t]*\|/) next
    if (length($0) > max && !inspan($0)) {
        printf "%s:%d: %d columns (max %d)\n", FILENAME, FNR, length($0), max
        bad++
    }
}
END {
    if (bad > 0) { printf "\n%d Markdown line(s) break the conventions.\n", bad > "/dev/stderr"; exit 1 }
    print "Markdown: OK"
}' "${files[@]}"
