#!/usr/bin/env bash
# Move the data of mn (https://github.com/20syldev/mn), the Bash tool that tote replaces, to tote.
#
#   scripts/import.sh             dry run: shows what would be imported, writes nothing
#   scripts/import.sh --apply     imports it
#   scripts/import.sh --help      the full reference
#
# It is not part of the tote binary. It reads the files of mn, never writes below the mn
# directory, and appends the records to the TOML files of tote with the block layout that tote
# itself writes (see internal/store). Exit status: 0 ok or nothing to do, 1 some entries were
# skipped, 2 usage or fatal error.

if [ -z "${BASH_VERSINFO:-}" ] || [ "${BASH_VERSINFO[0]}" -lt 4 ]; then
    echo "import.sh: bash 4 or later is required" >&2
    exit 2
fi

set -o pipefail
umask 077
export LC_ALL=C

readonly US=$'\037' RSEP=$'\036' MAXLINE=65536
# Same list as reservedNames in internal/shell/init.go.
readonly RESERVED_LIST=(tote source eval command builtin alias unalias unset local read history
    umask exit return emulate : . set printf complete compdef _describe _tote __tote_unload
    __tote_complete if 'then' else elif 'fi' case 'esac' for select while until 'do' 'done' in
    function time coproc repeat foreach end always nocorrect)
# Valid UTF-8 as a regular expression over bytes (C locale), and the characters a shell name
# must not hold (shellForbidden in internal/store/validate.go).
readonly U8RE=$'^([\x01-\x7f]|[\xc2-\xdf][\x80-\xbf]|\xe0[\xa0-\xbf][\x80-\xbf]|'\
$'[\xe1-\xec\xee\xef][\x80-\xbf][\x80-\xbf]|\xed[\x80-\x9f][\x80-\xbf]|'\
$'\xf0[\x90-\xbf][\x80-\xbf][\x80-\xbf]|[\xf1-\xf3][\x80-\xbf][\x80-\xbf][\x80-\xbf]|'\
$'\xf4[\x80-\x8f][\x80-\xbf][\x80-\xbf])*$'
readonly SHELL_BAD='[]/$`="'"'"'|&;()<>!{}*?[#~^\\]'

declare -A TBL=([co]=connection [alias]=alias [repo]=repo [func]=func)
declare -A FILES=([co]=connections [alias]=aliases [repo]=repos [func]=funcs)
declare -A KEYS=([co]="name via target user aliases ip desc needs_review" [alias]="name cmd desc"
    [repo]="name path desc" [func]="name desc kind script managed")
declare -A RESERVED EX NSP WORDS NEW NEWFP NEWBLK CNT FOUND PEND
for w in "${RESERVED_LIST[@]}"; do RESERVED[$w]=1; done

APPLY=0 VERBOSE=0 FROM="" TO="" MNHOME="" SYSBIN="/usr/local/bin"
ROWS=() NOTES=() WRITTEN=() CO=() F=() DROPPED=0 SKIPPED=0 ERR="" LINE="" CUR="" FP="" BLK=""

say() { printf '%s\n' "$*"; }
err() { printf 'import.sh: %s\n' "$*" >&2; }
die() { err "$*"; exit 2; }
fail() { ERR=$1; return 1; }
note() { NOTES+=("${1//[[:cntrl:]]/?}"); }
drop() { note "$1"; DROPPED=$((DROPPED + 1)); }

usage() {
    cat <<'EOF'
Usage: import.sh [--apply] [--from DIR] [--to DIR] [--home DIR] [--system-bin DIR] [--verbose]

Moves the data of mn (https://github.com/20syldev/mn) to tote. Without --apply it is a dry run:
it checks every value against what tote accepts, prints what it would import, writes nothing.

Options:
  --apply          write the records
  --from DIR       mn directory (default: .config/mn in the home of the account)
  --to DIR         tote configuration directory (default: where tote looks, in this order:
                   TOTE_CONFIG_DIR, XDG_CONFIG_HOME/tote, $HOME/.config/tote)
  --home DIR       home of the mn user: default --from, scripts of functions in DIR/.local/bin
                   (default: the account database, not $HOME, so a rehearsal with a temporary
                   HOME still reads the real mn data)
  --system-bin DIR second place for function scripts (default /usr/local/bin)
  --verbose        also list the entries that are already in tote
  -h, --help       this text

Read, under DIR/data, one record per line, fields split on ":::" (leftmost first):
  connexions.dat   name, via, target, user, aliases, ip, desc  -> connections.toml (exactly 7
                   fields: the older layouts are skipped; a via "ssh" is stored empty; an empty
                   target becomes the name; user@host is split)
  aliases.dat      name, command, desc                         -> aliases.toml
  repos.dat        name, path, desc                            -> repos.toml (the path is kept
                   verbatim, "~" included; a relative path is skipped)
  functions.dat    name, desc, optional "shell"                -> funcs.toml (no code in the data:
                   the script is HOME/.local/bin/NAME, else SYSBIN/NAME, only referenced and never
                   managed; "shell" gives kind source, else exec; no script, skipped)
Comments (#), blank lines, CRLF and a BOM are accepted; a missing or empty file is not an error.
Not imported, on purpose: .lang, .editor, .repos, .cdn, data/ghrel.list, plugins/, docs/, the
files mn generates (~/.bash_aliases, ~/.bash_functions) and every backup.

Rules:
  - Every name, via, target, user, alias, ip, description, path and command is checked with the
    rules of tote; a record that fails is skipped with its reason. A description that is too long
    or holds control characters is cut or cleaned (noted). An invalid ip or alias is left out of
    its connection (noted, the connection is kept).
    A line longer than 65536 bytes is skipped.
  - Aliases, repos and funcs share one namespace in tote, with the reserved shell words: the
    first claimed wins, in the order aliases, repos, funcs (as in mn, an alias shadows a script).
    Connection names and aliases are compared ignoring case; names are claimed before aliases.
  - A tote record is never changed. The same record already there is "same"; a record of the same
    name with other content is a "conflict", kept as it is, and counts as skipped.
  - Idempotent: a second --apply imports nothing and changes no file.

--apply refuses to run as root, takes the tote lock files (flock), copies each file it changes to
FILE.bak-YYYYmmdd-HHMMSS (nothing is deleted), writes through a temporary file in the same
directory and a rename (new files are 0600, an existing file keeps its mode, a symbolic link is
followed) and reads the files back to check them. Nothing below the mn directory is ever written.

Exit status: 0 ok or nothing to do, 1 some entries were skipped, 2 usage or fatal error.
EOF
}

abspath() { case $1 in /*) REPLY=$1 ;; *) REPLY=$PWD/$1 ;; esac; REPLY=${REPLY%/}; }

passwd_home() {
    local h
    h=$(getent passwd "$(id -u)" 2>/dev/null | cut -d: -f6)
    if [[ -z $h ]] && command -v dscl >/dev/null 2>&1; then
        h=$(dscl . -read "/Users/$(id -un)" NFSHomeDirectory 2>/dev/null |
            sed -n 's/^NFSHomeDirectory: //p')
    fi
    [[ $h == /* ]] || h=${HOME:-}
    printf '%s' "$h"
}

# default_to: where tote looks for config.toml (internal/config/paths.go): override, XDG, home.
default_to() {
    if [[ -n ${TOTE_CONFIG_DIR:-} ]]; then
        [[ $TOTE_CONFIG_DIR == /* ]] || die "TOTE_CONFIG_DIR is not an absolute path"
        REPLY=${TOTE_CONFIG_DIR%/}
    elif [[ ${XDG_CONFIG_HOME:-} == /* ]]; then
        REPLY=${XDG_CONFIG_HOME%/}/tote
    else
        [[ ${HOME:-} == /* ]] || die "HOME is not an absolute path, pass --to"
        REPLY=${HOME%/}/.config/tote
    fi
}

# resolve FILE: the real path behind a symbolic link (tote replaces the target, not the link).
resolve() {
    [[ -L $1 ]] || { printf '%s' "$1"; return 0; }
    [[ -e $1 ]] || { err "$1 is a dangling symbolic link"; return 1; }
    readlink -f -- "$1" 2>/dev/null || realpath -- "$1" 2>/dev/null ||
        { err "cannot resolve $1"; return 1; }
}

utf8_ok() { [[ $1 != *[$'\x80'-$'\xff']* || $1 =~ $U8RE ]]; }

# strip_hidden: removes C1 controls, invisible format characters (category Cf, except the
# zero-width joiner and non-joiner) and line separators.
strip_hidden() {
    REPLY=${1//$'\xc2'[$'\x80'-$'\x9f\xad']/}
    REPLY=${REPLY//$'\xd8'[$'\x80'-$'\x85\x9c']/}
    REPLY=${REPLY//$'\xdb\x9d'/}
    REPLY=${REPLY//$'\xdc\x8f'/}
    REPLY=${REPLY//$'\xe0\xa2'[$'\x90\x91']/}
    REPLY=${REPLY//$'\xe0\xa3\xa2'/}
    REPLY=${REPLY//$'\xe1\xa0\x8e'/}
    REPLY=${REPLY//$'\xe2\x80'[$'\x8b\x8e\x8f\xa8'-$'\xae']/}
    REPLY=${REPLY//$'\xe2\x81'[$'\xa0'-$'\xaf']/}
    REPLY=${REPLY//$'\xef\xbb\xbf'/}
    REPLY=${REPLY//$'\xef\xbf'[$'\xb9'-$'\xbb']/}
    REPLY=${REPLY//$'\xf0\x91\x82\xbd'/}
    REPLY=${REPLY//$'\xf0\x91\x83\x8d'/}
    REPLY=${REPLY//$'\xf0\x93\x90'[$'\xb0'-$'\xbf']/}
    REPLY=${REPLY//$'\xf0\x9b\xb2'[$'\xa0'-$'\xa3']/}
    REPLY=${REPLY//$'\xf0\x9d\x85'[$'\xb3'-$'\xba']/}
    REPLY=${REPLY//$'\xf3\xa0'[$'\x80\x81'][$'\x80'-$'\xbf']/}
}

# odd_space VALUE: true when it holds a space other than the ASCII one, a private use character
# or a joiner: tote rejects them in names (they are not printable for unicode.IsPrint).
odd_space() {
    [[ $1 == *$'\xc2\xa0'* || $1 == *$'\xe1\x9a\x80'* || $1 == *$'\xe3\x80\x80'* ||
        $1 == *$'\xe2\x81\x9f'* || $1 == *$'\xe2\x80'[$'\x80'-$'\x8d\xaf']* ||
        $1 == *$'\xee'[$'\x80'-$'\xbf'][$'\x80'-$'\xbf']* ||
        $1 == *$'\xef'[$'\x80'-$'\xa3'][$'\x80'-$'\xbf']* ||
        $1 == *$'\xf3'[$'\xb0'-$'\xbf']* || $1 == *$'\xf4'* ]]
}

# plain_word LABEL VALUE: UTF-8 without space, control or invisible character.
plain_word() {
    utf8_ok "$2" || fail "$1 is not valid UTF-8" || return 1
    strip_hidden "$2"
    [[ $2 != *[[:space:][:cntrl:]]* && $REPLY == "$2" ]] ||
        fail "$1 contains whitespace or control characters" || return 1
    ! odd_space "$2" || fail "$1 contains whitespace or control characters"
}

v_name() {
    [[ -n $2 ]] || fail "empty name" || return 1
    (( ${#2} <= 64 )) || fail "name longer than 64 bytes" || return 1
    case $1 in
        co) [[ $2 =~ ^[A-Za-z0-9_][A-Za-z0-9_.-]*$ ]] ||
            fail "name must use ASCII letters, digits, _ . - and not start with . or -" ;;
        func) [[ $2 =~ ^[A-Za-z_][A-Za-z0-9_-]*$ ]] ||
            fail "name must use ASCII letters, digits, _ - and not start with a digit" ;;
        *) plain_word name "$2" || return 1
           [[ $2 != -* ]] || fail "name must not start with -" || return 1
           [[ ! $2 =~ $SHELL_BAD ]] || fail "name holds a character the shell cannot take" ;;
    esac
}

# v_line LABEL VALUE: a command, a path or a script: not blank, UTF-8, on one line.
v_line() {
    [[ -n ${2//[[:space:]]/} ]] || fail "$1 is empty" || return 1
    utf8_ok "$2" || fail "$1 is not valid UTF-8" || return 1
    [[ $2 != *[$'\r\n']* ]] || fail "$1 holds a line break"
}

v_via() {
    local v=$1 rest w
    [[ -z $v ]] && return 0
    plain_word via "${v// /}" || return 1
    [[ $v != " "* && $v != *" " ]] || fail "via starts or ends with a space" || return 1
    [[ ${v%% *} =~ ^[A-Za-z0-9_][A-Za-z0-9_./-]*$ ]] ||
        fail "first word of the via is not a command" || return 1
    rest=$v
    for w in 'user@' user host ip name; do rest=${rest//\{$w\}/}; done
    [[ $rest != *[\{\}]* ]] || fail "via has an unknown placeholder or a stray brace"
}

v_target() {
    [[ -n $1 ]] || fail "target is empty" || return 1
    plain_word target "$1" || return 1
    [[ $1 != -* && $1 != :* && $1 != *: ]] || fail "target starts with - or starts or ends with :"
}

v_user() {
    [[ -z $1 ]] || { (( ${#1} <= 32 )) && [[ $1 =~ ^[A-Za-z0-9_][A-Za-z0-9._-]*$ ]]; } ||
        fail "user must use ASCII letters, digits, _ . - and be at most 32 bytes"
}

valid_v4() {
    local o
    [[ $1 =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$ ]] || return 1
    for o in "${BASH_REMATCH[@]:1}"; do
        [[ $o == 0 || $o != 0* ]] && (( 10#$o <= 255 )) || return 1
    done
}

# valid_ip: an IPv4 or IPv6 literal without brackets or zone (netip.ParseAddr).
valid_ip() {
    local ip=$1 h='[0-9A-Fa-f]{1,4}' a b t n=0 m=0
    valid_v4 "$ip" && return 0
    [[ $ip == *:* && $ip =~ ^[0-9A-Fa-f:.]+$ ]] || return 1
    if [[ $ip =~ ^(.*:)([0-9.]+)$ && ${BASH_REMATCH[2]} == *.* ]]; then
        a=${BASH_REMATCH[1]} t=${BASH_REMATCH[2]}   # valid_v4 overwrites BASH_REMATCH
        valid_v4 "$t" || return 1
        ip=${a}0:0
    fi
    [[ $ip == *::* ]] || { [[ $ip =~ ^($h:){7}$h$ ]]; return; }
    a=${ip%%::*} b=${ip#*::}
    [[ $b != *::* && $a =~ ^($h(:$h)*)?$ && $b =~ ^($h(:$h)*)?$ ]] || return 1
    [[ -n $a ]] && { t=${a//[^:]/}; n=$(( ${#t} + 1 )); }
    [[ -n $b ]] && { t=${b//[^:]/}; m=$(( ${#t} + 1 )); }
    (( n + m <= 7 ))
}

# clean_desc TEXT NAME: control and invisible characters go, 200 characters at most. Result in
# REPLY.
clean_desc() {
    local s=${1//[[:cntrl:]]/ } i=0 c=0 t
    strip_hidden "$s"; s=$REPLY
    s=${s#"${s%%[![:space:]]*}"}
    s=${s%"${s##*[![:space:]]}"}
    t=${s//[$'\x80'-$'\xbf']/}
    if (( ${#t} > 200 )); then
        while (( i < ${#s} )); do
            [[ ${s:i:1} == [$'\x80'-$'\xbf'] ]] || { (( c++ == 200 )) && break; }
            i=$((i + 1))
        done
        s=${s:0:i}
    fi
    t=${1#"${1%%[![:space:]]*}"}; t=${t%"${t##*[![:space:]]}"}
    [[ $s == "$t" ]] || note "$CUR $2: $LINE: description cleaned or cut"
    REPLY=$s
}

# claim_shell KIND NAME: aliases, repos and funcs share one namespace, with the reserved words.
claim_shell() {
    [[ -z ${RESERVED[$2]:-} ]] || fail "reserved name" || return 1
    if [[ $1 == alias && $2 == mn ]]; then fail "reserved name"; return 1; fi
    [[ -z ${NSP[$2]:-} ]] || fail "name already used by ${NSP[$2]}"
}

# toml_q VALUE: a TOML basic string, escaped like the encoder of tote does. Result in REPLY.
toml_q() {
    local s=${1//\\/\\\\} out="" c i
    s=${s//\"/\\\"}
    if [[ $s == *[[:cntrl:]]* ]]; then
        for ((i = 0; i < ${#s}; i++)); do
            c=${s:i:1}
            case $c in
                $'\t') out+='\t' ;;
                $'\b') out+='\b' ;;
                $'\f') out+='\f' ;;
                [[:cntrl:]]) printf -v c '\\u%04x' "'$c"; out+=$c ;;
                *) out+=$c ;;
            esac
        done
        s=$out
    fi
    REPLY="\"$s\""
}

# fld KEY TOKEN adds a field to the record being built (fingerprint and block text); fld_s does it
# for a string and leaves an empty one out, as the omitempty tags of internal/store do.
fld() { FP+="${FP:+$RSEP}$1=$2"; BLK+="$1 = $2"$'\n'; }
fld_s() { [[ -n $2 ]] || return 0; toml_q "$2"; fld "$1" "$REPLY"; }

# The reader of the existing tote files: one "R name fingerprint" line per [[table]] block, or
# "E line message" for what it does not understand. It handles the layout tote writes and the
# usual hand-written variants (literal strings, comments, spacing, one-line arrays).
read -r -d '' AWK_READER <<'EOF'
function scan(s, i,   q, c, n) {
    q = substr(s, i, 1); n = length(s)
    for (i++; i <= n; i++) {
        c = substr(s, i, 1)
        if (q == "\"" && c == "\\") { i++; continue }
        if (c == q) return i
    }
    return 0
}
function str(t,   b) {
    if (substr(t, 1, 1) != sq) return t
    b = substr(t, 2, length(t) - 2)
    return (b ~ /["\\]/) ? t : "\"" b "\""
}
function value(s,   c, j) {
    sub(/^[ \t]+/, "", s); c = substr(s, 1, 1)
    if (c == "\"" || c == sq) {
        if (substr(s, 1, 3) == c c c) return "!multi-line string"
        if (!(j = scan(s, 1))) return "!unterminated string"
        return str(substr(s, 1, j))
    }
    if (c == "[") return match(s, /^\[.*\]/) ? substr(s, 1, RLENGTH) : "!multi-line array"
    if (!match(s, /^[^ \t#]+/)) return "!missing value"
    return substr(s, 1, RLENGTH)
}
function flush(   i, k, fp, nm) {
    if (!inb) return
    if (!("name" in V)) { print "E" US FNR US "record without a name"; return }
    nm = V["name"]
    nm = (nm ~ /^"[^\\]*"$/) ? substr(nm, 2, length(nm) - 2) : nm
    for (i = 1; i <= n; i++) {
        k = K[i]
        if ((k in V) && V[k] != "\"\"" && !(k == "needs_review" && V[k] == "false"))
            fp = fp (fp == "" ? "" : RSEP) k "=" V[k]
    }
    print "R" US nm US fp
}
BEGIN { sq = sprintf("%c", 39); bom = sprintf("%c%c%c", 239, 187, 191); n = split(keys, K, " ") }
FNR == 1 && index($0, bom) == 1 { $0 = substr($0, 4) }
{ sub(/\r$/, "") }
/^[ \t]*(#.*)?$/ { next }
/^[ \t]*\[/ {
    h = $0; sub(/#.*$/, "", h); gsub(/[ \t]/, "", h)
    if (h == "[[" table "]]") { flush(); inb = 1; split("", V) }
    else print "E" US FNR US "unsupported table header"
    next
}
{
    if (!match($0, /^[ \t]*("[^"]*"|[A-Za-z0-9_-]+)[ \t]*=/)) {
        print "E" US FNR US "unsupported line"; next
    }
    key = substr($0, 1, RLENGTH); sub(/[ \t]*=$/, "", key); sub(/^[ \t]*/, "", key)
    gsub(/"/, "", key)
    val = value(substr($0, RLENGTH + 1))
    if (substr(val, 1, 1) == "!") { print "E" US FNR US substr(val, 2); next }
    if (inb) V[key] = val
    else if (key == "schema" && val != "1")
        print "E" US FNR US "schema " val " is not supported (this script writes schema 1)"
    else if (key != "schema") print "E" US FNR US "unsupported top-level key"
}
END { flush() }
EOF

# load_tote KIND: fills EX, NSP and WORDS with the records already in the tote file of a kind.
load_tote() {
    local tag a b w file=$TO/${FILES[$1]}.toml
    local -a arr
    [[ -e $file ]] || return 0
    [[ -f $file && -r $file ]] || die "$file is not a readable file"
    while IFS=$US read -r tag a b; do
        case $tag in
            E) die "$file:$a: $b; fix the file, or add the records by hand" ;;
            R) [[ -z ${EX[$1:$a]+x} ]] || continue
               EX[$1:$a]=$b
               if [[ $1 != co ]]; then NSP[$a]="$1 $a"; continue; fi
               WORDS[${a,,}]=$a
               if [[ $b =~ (^|$RSEP)aliases=\[([^]]*)\] ]]; then
                   b=${BASH_REMATCH[2]//[\"\',]/ }
                   read -r -a arr <<<"$b"
                   for w in "${arr[@]}"; do WORDS[${w,,}]=$a; done
               fi ;;
        esac
    done < <(awk -v table="${TBL[$1]}" -v keys="${KEYS[$1]}" -v US="$US" -v RSEP="$RSEP" \
        "$AWK_READER" "$file")
}

row() {
    local n=${2//[[:cntrl:]]/?} d=${4//[[:cntrl:]]/?}
    ROWS+=("$1$US$n$US$3$US$d")
    CNT[$1.$3]=$(( ${CNT[$1.$3]:-0} + 1 ))
}

# ok KIND NAME CHECK...: runs a check; on failure the record is skipped with the reason.
ok() { local k=$1 n=$2; shift 2; "$@" || { row "$k" "$n" skip "$LINE: $ERR"; return 1; }; }

# differs OLD NEW sets REPLY to the names of the fields whose text differs between two
# fingerprints, sorted and separated by commas, so that a conflict says what to look at.
differs() {
    local -A a=() b=() d=()
    local -a pa pb
    local p k
    IFS=$RSEP read -r -a pa <<<"$1"
    IFS=$RSEP read -r -a pb <<<"$2"
    for p in "${pa[@]}"; do a[${p%%=*}]=${p#*=}; done
    for p in "${pb[@]}"; do b[${p%%=*}]=${p#*=}; done
    for k in "${!a[@]}" "${!b[@]}"; do
        [[ -n ${a[$k]+x} && -n ${b[$k]+x} && ${a[$k]} == "${b[$k]}" ]] || d[$k]=1
    done
    REPLY=$(printf '%s\n' "${!d[@]}" | sort | paste -sd, - | sed 's/,/, /g')
}

# decide KIND NAME DETAIL: the record is in FP and BLK; it is the same, a conflict, skipped or new.
decide() {
    local k=$1 key="$1:$2" why="already in tote"
    if [[ -n ${EX[$key]+x} ]]; then
        [[ -z ${NEW[$key]+x} ]] || why="duplicate in mn, identical"
        if [[ ${EX[$key]} == "$FP" ]]; then row "$k" "$2" same "$why"
        elif [[ -n ${NEW[$key]+x} ]]; then
            row "$k" "$2" skip "$LINE: duplicate name in mn with other content, first one kept"
        else
            differs "${EX[$key]}" "$FP"
            row "$k" "$2" conflict "exists in tote with other content (${REPLY:-?}), kept"
        fi
        return
    fi
    if [[ $k != co ]]; then
        ok "$k" "$2" claim_shell "$k" "$2" || return
        NSP[$2]="$k $2"
    fi
    EX[$key]=$FP NEW[$key]=1 NEWFP[$key]=$FP
    NEWBLK[$k]+="${NEWBLK[$k]:+$'\n'}[[${TBL[$k]}]]"$'\n'"$BLK"
    row "$k" "$2" import "$3"
}

# read_dat KIND FILE MIN MAX HANDLER: calls HANDLER, with the fields in F, for every record line.
read_dat() {
    local line rest n ln=0 f=$2 want=$3
    [[ $3 == "$4" ]] || want="$3 to $4"
    FOUND[$1]=0 CUR=$1
    [[ -e $f ]] || return 0
    [[ -f $f && -r $f ]] || die "$f is not a readable file"
    while IFS= read -r line || [[ -n $line ]]; do
        ln=$((ln + 1)); LINE="line $ln"
        line=${line%$'\r'}
        (( ln == 1 )) && line=${line#$'\xef\xbb\xbf'}
        [[ -z ${line//[[:space:]]/} || $line == \#* ]] && continue
        FOUND[$1]=$(( ${FOUND[$1]} + 1 ))
        if (( ${#line} > MAXLINE )); then
            row "$1" "${line%%:::*}" skip "$LINE: line longer than $MAXLINE bytes"
            continue
        fi
        F=() rest=$line
        while [[ $rest == *:::* ]]; do F+=("${rest%%:::*}"); rest=${rest#*:::}; done
        F+=("$rest"); n=${#F[@]}
        if (( n < $3 || n > $4 )); then
            row "$1" "${F[0]}" skip "$LINE: expected $want fields, found $n"
        elif ! utf8_ok "$line"; then row "$1" "${F[0]}" skip "$LINE: not valid UTF-8"
        else "$5"; fi
    done < "$f"
}

h_alias() {
    local name=${F[0]} cmd=${F[1]} desc
    ok alias "$name" v_name alias "$name" && ok alias "$name" v_line command "$cmd" || return
    clean_desc "${F[2]:-}" "$name"; desc=$REPLY
    FP="" BLK=""
    fld_s name "$name"; fld_s cmd "$cmd"; fld_s desc "$desc"
    decide alias "$name" "$cmd"
}

h_repo() {
    local name=${F[0]} path=${F[1]} desc
    ok repo "$name" v_name repo "$name" && ok repo "$name" v_line path "$path" || return
    [[ $path == \~ || $path == \~/* || $path == /* ]] ||
        { row repo "$name" skip "$LINE: path is relative, tote needs / or ~/"; return; }
    clean_desc "${F[2]:-}" "$name"; desc=$REPLY
    FP="" BLK=""
    fld_s name "$name"; fld_s path "$path"; fld_s desc "$desc"
    decide repo "$name" "$path"
}

h_func() {
    local name=${F[0]} kind=exec script desc
    ok func "$name" v_name func "$name" || return
    [[ -n ${EX[func:$name]+x} ]] || ok func "$name" claim_shell func "$name" || return
    [[ ${F[2]:-} == shell ]] && kind=source
    if [[ -f $MNHOME/.local/bin/$name ]]; then script=\~/.local/bin/$name
    elif [[ -f $SYSBIN/$name ]]; then script="$SYSBIN/$name"
    else row func "$name" skip "$LINE: no script in ~/.local/bin or $SYSBIN"; return; fi
    ok func "$name" v_line script "$script" || return
    clean_desc "${F[1]}" "$name"; desc=$REPLY
    FP="" BLK=""
    fld_s name "$name"; fld_s desc "$desc"; fld kind "\"$kind\""; fld_s script "$script"
    fld managed false
    decide func "$name" "$kind $script"
}

# Connections go in two passes: the names are claimed first (an exact name outranks an alias),
# then each alias is kept only when no other connection already holds that word.
h_co_check() {
    local name=${F[0]} via=${F[1]} target=${F[2]} user=${F[3]} ip=${F[5]} word
    ok co "$name" v_name co "$name" || return
    [[ $via == ssh ]] && via=""
    ok co "$name" v_via "$via" || return
    [[ -n $target ]] || target=$name
    if [[ $target == *@* ]]; then [[ -n $user ]] || user=${target%@*}; target=${target##*@}; fi
    ok co "$name" v_target "$target" && ok co "$name" v_user "$user" || return
    if [[ -z ${EX[co:$name]+x} && -z ${PEND[$name]+x} ]]; then
        word=${WORDS[${name,,}]:-}
        [[ -z $word ]] ||
            { row co "$name" skip "$LINE: name already used by connection $word"; return; }
        WORDS[${name,,}]=$name
    fi
    PEND[$name]=1
    if [[ -n $ip ]] && ! valid_ip "$ip"; then
        drop "co $name: $LINE: ip \"$ip\" is not an IP address, left out"; ip=""
    fi
    clean_desc "${F[6]}" "$name"
    CO+=("$name$US$via$US$target$US$user$US${F[4]}$US$ip$US$REPLY$US$LINE")
}

h_co_build() {
    local name via target user aliases ip desc a owner list="" w
    local -a arr
    IFS=$US read -r name via target user aliases ip desc LINE <<<"$1"
    IFS=, read -r -a arr <<<"${aliases// /}"
    for a in "${arr[@]}"; do
        a=${a,,}
        [[ -z $a || $a == "${name,,}" || " $list " == *" $a "* ]] && continue
        v_name co "$a" || { drop "co $name: $LINE: alias \"$a\": $ERR, left out"; continue; }
        owner=${WORDS[$a]:-}
        if [[ -n $owner && $owner != "$name" ]]; then
            drop "co $name: $LINE: alias \"$a\" is already used by connection $owner, left out"
            continue
        fi
        WORDS[$a]=$name; list+=" $a"
    done
    FP="" BLK="" w=""
    fld_s name "$name"; fld_s via "$via"; fld_s target "$target"; fld_s user "$user"
    for a in $list; do toml_q "$a"; w+="${w:+, }$REPLY"; done
    [[ -z $w ]] || fld aliases "[$w]"
    fld_s ip "$ip"; fld_s desc "$desc"
    decide co "$name" "${user:+$user@}$target${via:+ via $via}"
}

# plan: reads tote, then mn, and decides every record. It writes nothing.
plan() {
    local k rec
    EX=() NSP=() WORDS=() NEW=() NEWFP=() NEWBLK=() CNT=() FOUND=() PEND=()
    ROWS=() NOTES=() CO=() DROPPED=0
    for k in co alias repo func; do load_tote "$k"; done
    read_dat alias "$FROM/data/aliases.dat" 2 3 h_alias
    read_dat repo "$FROM/data/repos.dat" 2 3 h_repo
    read_dat func "$FROM/data/functions.dat" 2 3 h_func
    read_dat co "$FROM/data/connexions.dat" 7 7 h_co_check
    for rec in "${CO[@]}"; do h_co_build "$rec"; done
}

file_mode() { stat -c %a "$1" 2>/dev/null || stat -f %Lp "$1" 2>/dev/null; }

# write_kind KIND: appends the new blocks of a kind, with a backup and an atomic rename.
write_kind() {
    local file=$TO/${FILES[$1]}.toml blocks=${NEWBLK[$1]:-} target cur="" out last tmp bak mode n=0
    local eol=$'\n'
    [[ -n $blocks ]] || return 0
    target=$(resolve "$file") || exit 2
    if [[ -e $target ]]; then
        [[ -f $target && -w $target ]] || die "$file is not a writable file"
        IFS= read -r -d '' cur <"$target"
        if (( $(grep -c $'\r$' "$target") * 2 > $(grep -c '' "$target") )); then
            eol=$'\r\n' blocks=${blocks//$'\n'/$'\r\n'}
        fi
    fi
    out=$cur
    if [[ -n $out ]]; then
        [[ $out == *$'\n' ]] || out+=$eol
        last=${out%$'\n'}; last=${last##*$'\n'}
        [[ -z ${last//[[:space:]]/} ]] || out+=$eol
    fi
    cur=${cur#$'\xef\xbb\xbf'}   # a BOM hides the first line from the check below
    if [[ -z $out ]] || ! grep -Eq '^[[:space:]]*(schema[[:space:]]*=|\[)' <<<"$cur"; then
        out+="schema = 1$eol$eol"
    fi
    out+=$blocks
    if [[ -e $target ]]; then
        bak=$file.bak-$(date +%Y%m%d-%H%M%S)
        while [[ -e $bak ]]; do n=$((n + 1)); bak=$file.bak-$(date +%Y%m%d-%H%M%S)-$n; done
        if ! cp -- "$target" "$bak" || ! chmod 600 "$bak"; then die "cannot back up $file"; fi
        WRITTEN+=("$file (backup $bak)")
    else
        WRITTEN+=("$file (new file)")
    fi
    tmp=$(mktemp "${target%/*}/.${target##*/}.tmp-XXXXXX") || die "cannot create a temporary file"
    mode=$(file_mode "$target")
    if ! printf '%s' "$out" >"$tmp" || { [[ -n $mode ]] && ! chmod "$mode" "$tmp"; } ||
        ! mv -f -- "$tmp" "$target"; then
        rm -f -- "$tmp"; die "cannot write $file"
    fi
}

# verify: reads the files back and checks that every new record is there as planned.
verify() {
    local key k bad=0
    local -A want=()
    for key in "${!NEWFP[@]}"; do want[$key]=${NEWFP[$key]}; done
    EX=() NSP=() WORDS=()
    for k in co alias repo func; do load_tote "$k"; done
    for key in "${!want[@]}"; do [[ ${EX[$key]:-} == "${want[$key]}" ]] || bad=$((bad + 1)); done
    (( bad == 0 )) || die "$bad written records do not read back as planned; see the backups"
}

lock_all() {
    local k f fd
    command -v flock >/dev/null 2>&1 ||
        { err "warning: flock not found, the files are not locked"; return 0; }
    for f in "${FILES[@]}"; do
        k=$(resolve "$TO/$f.toml") || exit 2
        exec {fd}>>"$k.lock" || die "cannot open the lock file of $f.toml"
        flock -w 10 "$fd" || die "$f.toml is locked by another process"
    done
}

report() {
    local r k kind name status detail imp=0 s=0 n f ign=()
    say "tote import from mn: $( ((APPLY)) && echo apply || echo dry run)"
    say "  from  $FROM"
    say "  to    $TO"
    say ""
    for k in co alias repo func; do
        printf '  %-12s read %-4s import %-4s same %-4s conflict %-4s skipped %s\n' "${FILES[$k]}" \
            "${FOUND[$k]:-0}" "${CNT[$k.import]:-0}" "${CNT[$k.same]:-0}" "${CNT[$k.conflict]:-0}" \
            "${CNT[$k.skip]:-0}"
        imp=$((imp + ${CNT[$k.import]:-0})); s=$((s + ${CNT[$k.skip]:-0} + ${CNT[$k.conflict]:-0}))
    done
    SKIPPED=$((s + DROPPED))
    say ""
    printf '  %-6s %-24s %-9s %s\n' KIND NAME STATUS DETAIL
    for k in co alias repo func; do
        for r in "${ROWS[@]}"; do
            IFS=$US read -r kind name status detail <<<"$r"
            [[ $kind == "$k" ]] || continue
            [[ $status == same && $VERBOSE == 0 ]] && continue
            [[ $status == import && $APPLY == 1 ]] && status=imported
            printf '  %-6s %-24s %-9s %s\n' "$kind" "$name" "$status" "$detail"
        done
    done
    (( ${#NOTES[@]} )) && { say ""; say "Notes:"; for n in "${NOTES[@]}"; do say "  $n"; done; }
    say ""
    for f in .lang .editor .repos .cdn data/ghrel.list plugins docs; do
        [[ -e $FROM/$f ]] && ign+=("$f")
    done
    (( ${#ign[@]} )) && say "Not imported, on purpose: ${ign[*]}"
    for n in "${WRITTEN[@]}"; do say "wrote $n"; done
    if (( imp && APPLY )); then say "Imported $imp entries."
    elif (( imp )); then say "Dry run: nothing written. Use --apply to import $imp entries."
    else say "Nothing to import."; fi
    (( SKIPPED )) && say "Skipped: $SKIPPED (see the table and the notes above)."
}

main() {
    local o v k f rf rt
    while (( $# )); do
        o=$1 v=""
        case $o in
            --*=*) v=${o#*=} o=${o%%=*}; shift ;;
            --from|--to|--home|--system-bin)
                (( $# > 1 )) || die "$o needs a value"; v=$2; shift 2 ;;
            *) shift ;;
        esac
        case $o in
            --apply) APPLY=1 ;;
            --verbose) VERBOSE=1 ;;
            -h|--help) usage; exit 0 ;;
            --from) FROM=$v ;;
            --to) TO=$v ;;
            --home) MNHOME=$v ;;
            --system-bin) SYSBIN=$v ;;
            *) printf 'import.sh: unknown option %s\n\n' "$o" >&2; usage >&2; exit 2 ;;
        esac
    done
    (( EUID != 0 )) || die "refusing to run as root: run it as the user who owns the data"

    abspath "$SYSBIN"; SYSBIN=$REPLY
    [[ -n $MNHOME ]] || MNHOME=$(passwd_home)
    [[ $MNHOME == /* ]] || die "cannot find the home directory, pass --home"
    MNHOME=${MNHOME%/}
    abspath "${FROM:-$MNHOME/.config/mn}"; FROM=$REPLY
    if [[ -z $TO ]]; then default_to; TO=$REPLY; fi
    abspath "$TO"; TO=$REPLY
    [[ -d $FROM/data ]] || die "$FROM/data is not a directory: this is not an mn directory"
    [[ ! -e $TO || -d $TO ]] || die "$TO is not a directory"
    rf=$(cd -P -- "$FROM" && pwd) || die "cannot enter $FROM"
    rt=$TO; [[ -d $TO ]] && rt=$(cd -P -- "$TO" && pwd)
    [[ $rt/ != "$rf"/* && $rf/ != "$rt"/* ]] || die "--from and --to must not contain each other"

    plan
    if (( APPLY )) && [[ -n ${NEWBLK[*]:-} ]]; then
        mkdir -p -- "$TO" || die "cannot create $TO"
        lock_all
        plan
        for k in co alias repo func; do   # refuse before writing anything
            [[ -z ${NEWBLK[$k]:-} ]] && continue
            f=$(resolve "$TO/${FILES[$k]}.toml") || exit 2
            [[ -w ${f%/*} && ( ! -e $f || ( -f $f && -w $f ) ) ]] || die "$f is not writable"
        done
        for k in co alias repo func; do write_kind "$k"; done
        verify
    fi
    report
    (( SKIPPED == 0 ))
}

main "$@"
