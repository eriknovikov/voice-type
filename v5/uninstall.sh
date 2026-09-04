#!/bin/sh
# voice-type v5 uninstaller.
#
#   curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v5/uninstall.sh | sh
#
# Removes the binary, the config, and any retained audio. Deliberately leaves
# your 'input' group membership alone because it is system-wide and other
# software may rely on it.
set -eu

BINARY_NAME="voice-type"
PREFIX="/usr/local"
PORT=3232

RED=$(printf '\033[31m'); YELLOW=$(printf '\033[33m')
GREEN=$(printf '\033[32m'); RESET=$(printf '\033[0m')

log_info() { printf '%s==>%s %s\n' "$GREEN" "$RESET" "$1" >&2; }
log_warn() { printf '%s!!%s  %s\n' "$YELLOW" "$RESET" "$1" >&2; }
log_error() { printf '%sxx%s  %s\n' "$RED" "$RESET" "$1" >&2; }

# /dev/tty exists but cannot be opened without a controlling terminal, so test
# the open rather than the device node's permissions.
# Probed in a subshell with `true`: a redirection failure on the special built-in
# `:` would terminate the shell outright, uncatchable by `||`.
has_tty() {
    ( true < /dev/tty ) 2> /dev/null || return 1
    ( true > /dev/tty ) 2> /dev/null
}

prompt_yn() {
    _msg="$1"; _default="$2"
    if ! has_tty; then
        echo "$_default"
        return 0
    fi
    case "$_default" in
        [Yy]*) _opts="Y/n" ;;
        *) _opts="y/N" ;;
    esac
    printf '%s (%s) ' "$_msg" "$_opts" > /dev/tty
    read -r _answer < /dev/tty || _answer=""
    [ -z "$_answer" ] && _answer="$_default"
    echo "$_answer"
}

as_root() {
    if [ "$(id -u)" = "0" ]; then
        "$@"
    else
        command -v sudo > /dev/null 2>&1 || {
            log_error "'sudo' is required to remove $PREFIX/bin/$BINARY_NAME."
            exit 1
        }
        sudo "$@"
    fi
}

stop_daemon() {
    command -v curl > /dev/null 2>&1 || return 0
    if curl -s -m 1 "http://localhost:$PORT/health" > /dev/null 2>&1; then
        log_info "Stopping the running daemon..."
        curl -s -m 2 "http://localhost:$PORT/exit" > /dev/null 2>&1 || true
    fi
}

load_config_port() {
    _binary="$PREFIX/bin/$BINARY_NAME"
    [ -x "$_binary" ] || return 0
    _version=$("$_binary" version 2> /dev/null) || return 0
    case "$_version" in
        5.*) ;;
        *) return 0 ;;
    esac
    _port=$("$_binary" config-port 2> /dev/null) || return 0
    case "$_port" in
        '' | *[!0-9]*) return 0 ;;
    esac
    if [ "$_port" -ge 1024 ] && [ "$_port" -le 65535 ]; then
        PORT="$_port"
    fi
}

remove_binary() {
    _target="$PREFIX/bin/$BINARY_NAME"
    [ -e "$_target" ] || {
        log_info "No binary at $_target."
        return 0
    }
    case "$(prompt_yn "Remove $_target?" "Y")" in
        [Yy]*)
            if [ -w "$PREFIX/bin" ]; then
                rm -f "$_target"
            else
                as_root rm -f "$_target"
            fi
            log_info "Removed $_target"
            ;;
        *) log_info "Kept $_target" ;;
    esac
}

remove_config() {
    _dir="${XDG_CONFIG_HOME:-$HOME/.config}"
    _cfg="$_dir/voice-type.jsonc"
    [ -e "$_cfg" ] || return 0
    case "$(prompt_yn "Remove the config ($_cfg)?" "N")" in
        [Yy]*)
            rm -f "$_cfg"
            log_info "Removed $_cfg"
            ;;
        *) log_info "Kept $_cfg" ;;
    esac
}

# The active vocabulary list is a single name voice-type wrote itself, so
# it goes without asking. The directory stays unless it is now empty -- a log
# may live beside it.
remove_state() {
    _dir="${XDG_STATE_HOME:-$HOME/.local/state}/voice-type"
    _state="$_dir/workspace"
    [ -e "$_state" ] || return 0
    rm -f "$_state"
    rmdir "$_dir" 2> /dev/null || true
    log_info "Removed $_state"
}

# Audio that failed to transcribe is retained here rather than discarded, so it
# may hold speech the user never got back as text. Never delete it unasked.
remove_retained_audio() {
    _dir="${TMPDIR:-/tmp}/voice-type"
    [ -d "$_dir" ] || return 0
    _count=$(find "$_dir" -name '*.wav' 2> /dev/null | wc -l | tr -d ' ')
    [ "$_count" -gt 0 ] || {
        rmdir "$_dir" 2> /dev/null || true
        return 0
    }
    log_warn "$_count retained recording(s) in $_dir (audio that failed to transcribe)."
    case "$(prompt_yn "Delete them?" "N")" in
        [Yy]*)
            rm -rf "$_dir"
            log_info "Removed $_dir"
            ;;
        *) log_info "Kept $_dir" ;;
    esac
}

usage() {
    cat >&2 << EOF
Usage: uninstall.sh [--prefix DIR]

  --prefix DIR   where the binary was installed (default: /usr/local)
EOF
}

main() {
    load_config_port
    stop_daemon
    remove_binary
    remove_config
    remove_state
    remove_retained_audio
    printf '\n' >&2
    log_info "Done. Your 'input' group membership was left in place."
}

while [ $# -gt 0 ]; do
    case "$1" in
        --prefix)
            [ $# -ge 2 ] || { log_error "--prefix needs a directory"; exit 1; }
            PREFIX="$2"; shift 2
            ;;
        -h | --help) usage; exit 0 ;;
        *) log_error "Unknown option: $1"; usage; exit 1 ;;
    esac
done

main
