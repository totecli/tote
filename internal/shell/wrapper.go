package shell

import "strings"

// bashWrapper is the tote function for bash. Locals are declared with a value so that a shell
// running with "set -u" never trips on them, and every builtin or external command that an alias
// of the user could shadow is called through "builtin" or "command".
//
// The command line of the insert action runs through eval inside the function, so it runs in
// function scope: a plain assignment, an export or a cd reach the shell, but a declare or typeset
// at the top of the line makes a local that is lost when the function returns. For that reason
// every local is prefixed, so that none of them clashes with, or is read by, the line.
const bashWrapper = `tote() {
    local __tote_f="" __tote_d="" __tote_hdr="" __tote_act="" __tote_arg="" __tote_rc=0
    builtin type -P tote >/dev/null 2>&1 || {
        builtin printf 'tote: binary not found in PATH\n' >&2
        return 127
    }
    for __tote_d in "${XDG_RUNTIME_DIR:-}" "${TMPDIR:-}" /tmp; do
        [[ -n $__tote_d ]] || continue
        __tote_f=$(builtin umask 077; command mktemp "$__tote_d/tote.XXXXXXXX" 2>/dev/null) && break
        __tote_f=""
    done
    if [[ -z $__tote_f ]]; then
        builtin printf 'tote: no usable temporary directory, cd and insert are disabled\n' >&2
        command tote "$@"
        return
    fi
    TOTE_HANDOFF=$__tote_f TOTE_SHELL=bash command tote "$@" || __tote_rc=$?
    if [[ -f $__tote_f && ! -L $__tote_f && -O $__tote_f && -s $__tote_f ]]; then
        {
            IFS= builtin read -r __tote_hdr || :
            IFS= builtin read -r -d '' __tote_arg || :
        } <"$__tote_f"
    fi
    command rm -f -- "$__tote_f"
    [[ $__tote_hdr == "v1 "* ]] || __tote_hdr=""
    __tote_act=${__tote_hdr#v1 }
    __tote_act=${__tote_act%%,*}
    if [[ ,${__tote_hdr#v1 }, == *,reload,* ]]; then
        builtin eval "$(command tote init bash --defs)"
    fi
    case $__tote_act in
        insert)
            if [[ -n $__tote_arg && $- == *i* && -t 0 ]]; then
                if (( BASH_VERSINFO[0] >= 4 )); then
                    builtin read -e -r -i "$__tote_arg" -p "> " __tote_arg || __tote_arg=""
                    if [[ -n $__tote_arg ]]; then
                        builtin history -s "$__tote_arg"
                        set --
                        builtin eval "$__tote_arg"
                    fi
                else
                    builtin history -s "$__tote_arg"
                    printf '%s\n' "$__tote_arg"
                fi
            fi
            ;;
        cd)
            if [[ -n $__tote_arg && -d $__tote_arg ]]; then
                [[ $__tote_arg == /* ]] || __tote_arg=./$__tote_arg
                builtin cd -- "$__tote_arg" || __tote_rc=$?
            fi
            ;;
        exit)
            builtin exit "$__tote_rc"
            ;;
    esac
    return "$__tote_rc"
}
`

// zshWrapper is the tote function for zsh. It runs under "emulate -L zsh" so that the options of
// the user, such as sh_word_split or errexit, cannot change its meaning, and it reads the hand-off
// file whole because zsh cannot read up to a NUL byte.
const zshWrapper = `tote() {
    emulate -L zsh
    local f="" d="" hdr="" act="" arg="" all="" __tote_rc=0
    builtin whence -p tote >/dev/null 2>&1 || {
        builtin print -u2 'tote: binary not found in PATH'
        return 127
    }
    for d in "${XDG_RUNTIME_DIR:-}" "${TMPDIR:-}" /tmp; do
        [[ -n $d ]] || continue
        f=$(builtin umask 077; command mktemp "$d/tote.XXXXXXXX" 2>/dev/null) && break
        f=""
    done
    if [[ -z $f ]]; then
        builtin print -u2 'tote: no usable temporary directory, cd and insert are disabled'
        command tote "$@"
        return
    fi
    TOTE_HANDOFF=$f TOTE_SHELL=zsh command tote "$@" || __tote_rc=$?
    if [[ -f $f && ! -L $f && -O $f && -s $f ]]; then
        all=$(<"$f")
        hdr=${all%%$'\n'*}
        if [[ $all == *$'\n'* ]]; then
            arg=${all#*$'\n'}
        fi
    fi
    command rm -f -- "$f"
    [[ $hdr == "v1 "* ]] || hdr=""
    act=${hdr#v1 }
    act=${act%%,*}
    if [[ ,${hdr#v1 }, == *,reload,* ]]; then
        builtin eval "$(command tote init zsh --defs)"
    fi
    case $act in
        insert)
            if [[ -n $arg ]]; then
                builtin print -z -- "$arg"
            fi
            ;;
        cd)
            if [[ -n $arg && -d $arg ]]; then
                [[ $arg == /* ]] || arg=./$arg
                builtin cd -- "$arg" || __tote_rc=$?
            fi
            ;;
        exit)
            builtin exit "$__tote_rc"
            ;;
    esac
    return "$__tote_rc"
}
`

// Wrapper returns the text of the tote shell function, which the init output prints before
// anything else so that a bad record can never remove it.
//
// The function runs the tote binary with TOTE_HANDOFF set to a private temporary file and
// TOTE_SHELL set to the shell name, then carries out what the binary left in the file: insert a
// command line, change directory, exit the shell, and re-evaluate the definitions when the
// reload flag is set. The file is always removed and the exit code of the binary is the one
// returned. A relative directory is prefixed with "./" so that CDPATH is never consulted. The
// temporary directory is the first usable one of XDG_RUNTIME_DIR, TMPDIR and /tmp; when none is,
// the function says so on standard error and runs the binary without hand-off. The flags of the
// header are matched as whole words, see reloadFlag. When the binary is not in PATH it prints a
// fixed English message and returns 127, because the function must work when nothing else can
// run.
//
// Parameters:
//   - k: shell to write for
//
// Returns the function text ending with a newline, or an empty string for an unknown kind.
func Wrapper(k Kind) string {
	switch k {
	case Bash:
		return bashWrapper
	case Zsh:
		return zshWrapper
	}

	return ""
}

// bashUnload removes what the previous evaluation of the definitions created. IFS is set locally
// because the names are split on spaces, and the function removes itself afterwards.
const bashUnload = `__tote_unload() {
    local IFS=' ' n
    for n in ${__TOTE_DEFS:-}; do
        case $n in
            a:*) unalias -- "${n#a:}" 2>/dev/null || : ;;
            f:*) unset -f -- "${n#f:}" 2>/dev/null || : ;;
        esac
    done
    return 0
}
__tote_unload
unset -f __tote_unload
`

// zshUnload is the zsh form of bashUnload, which splits the variable with the "=" flag.
const zshUnload = `__tote_unload() {
    emulate -L zsh
    local IFS=$' \t\n' n
    for n in ${=__TOTE_DEFS:-}; do
        case $n in
            a:*) unalias -- "${n#a:}" 2>/dev/null || : ;;
            f:*) unset -f -- "${n#f:}" 2>/dev/null || : ;;
        esac
    done
    return 0
}
__tote_unload
unset -f __tote_unload
`

// bashComplete is the bash completion function. It asks the binary for the candidates, one per
// line, and drops the optional tab separated description.
const bashComplete = `__tote_complete() {
    local line
    COMPREPLY=()
    while IFS= read -r line; do
        COMPREPLY+=("${line%%$'\t'*}")
    done < <(command tote __complete "${COMP_WORDS[@]:1:COMP_CWORD}" 2>/dev/null)
}
`

// zshComplete is the zsh completion function. A colon in a candidate is escaped for _describe,
// the description is kept as it is.
const zshComplete = `_tote() {
    local -a items
    local line name desc
    while IFS= read -r line; do
        name=${line%%$'\t'*}
        desc=""
        if [[ $line == *$'\t'* ]]; then
            desc=${line#*$'\t'}
        fi
        items+=("${name//:/\\:}:${desc}")
    done < <(command tote __complete "${(@)words[2,CURRENT]}" 2>/dev/null)
    _describe -t tote tote items
}
`

// unloadText returns the code that removes what the previous definitions created.
//
// Parameters:
//   - k: shell to write for
func unloadText(k Kind) string {
	if k == Zsh {
		return zshUnload
	}

	return bashUnload
}

// completionText returns the completion function and its registration for some commands.
//
// Parameters:
//   - k: shell to write for
//   - names: commands to register, "tote" first
func completionText(k Kind, names []string) string {
	list := strings.Join(names, " ")

	if k == Zsh {
		return zshComplete + "if (( $+functions[compdef] )); then compdef _tote " + list + "; fi\n"
	}

	return bashComplete + "complete -o default -F __tote_complete " + list + "\n"
}
