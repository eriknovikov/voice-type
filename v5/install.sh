#!/bin/sh
# voice-type v5 installer.
#
# Fetches the static Go binary for this architecture from GitHub Releases,
# verifies its checksum, installs it, and writes a config. Runtime and package
# setup belong elsewhere.
#
#   curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v5/install.sh | sh
#
# Installs one voice-type: the binary, config, and port are shared across
# majors, so this replaces whatever is already installed.
set -eu

REPO="eriknovikov/voice-type"
BINARY_NAME="voice-type"
PORT=3232

MODE="prod"          # prod | version | local
VERSION_TAG=""
PREFIX="/usr/local"
API_KEY=""
CONFIG_CREATED=0
CONFIG_TMP=""
TMP_DIR=""

RED=$(printf '\033[31m'); YELLOW=$(printf '\033[33m')
GREEN=$(printf '\033[32m'); RESET=$(printf '\033[0m')

log_info() { printf '%s==>%s %s\n' "$GREEN" "$RESET" "$1" >&2; }
log_warn() { printf '%s!!%s  %s\n' "$YELLOW" "$RESET" "$1" >&2; }
log_error() { printf '%sxx%s  %s\n' "$RED" "$RESET" "$1" >&2; }

cleanup() {
    [ -z "$CONFIG_TMP" ] || rm -f "$CONFIG_TMP"
    [ -z "$TMP_DIR" ] || rm -rf "$TMP_DIR"
}

# Wraps a path in an OSC 8 hyperlink so the config is click-to-open in terminals
# that support them (GNOME Terminal, Kitty, WezTerm, iTerm2, Konsole). Emitted
# only when stderr is a terminal, so redirected output keeps plain paths instead
# of escape sequences. Spaces are the one character that must be encoded for the
# file:// URL to survive.
link_path() {
    if [ ! -t 2 ]; then
        printf '%s' "$1"
        return 0
    fi
    printf '\033]8;;file://%s\007%s\033]8;;\007' "$(printf '%s' "$1" | sed 's/ /%20/g')" "$1"
}

usage() {
    cat >&2 << EOF
Usage: install.sh [--version vX.Y.Z | --local]

  --version vX.Y.Z   install a specific v5 release
  --local            build from the working tree with the Go toolchain
EOF
}

# Piped through curl, stdin is the script itself -- every prompt must come from
# the terminal, and there may not be one. Testing permissions on the device node
# is not enough: /dev/tty exists but cannot be opened when the process has no
# controlling terminal (systemd, cron, setsid), and an unguarded prompt then dies
# under `set -e` before the config is written.
# The probe runs in a subshell with `true`, not `:`. A redirection failure on a
# special built-in like `:` terminates the shell outright -- neither `2>/dev/null`
# nor `||` can catch it -- which silently killed the installer before it wrote
# the config.
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

need_cmd() {
    command -v "$1" > /dev/null 2>&1 || {
        log_error "'$1' is required but not installed."
        exit 1
    }
}

# Root's own commands run directly; everyone else goes through sudo.
as_root() {
    if [ "$(id -u)" = "0" ]; then
        "$@"
    else
        need_cmd sudo
        sudo "$@"
    fi
}

detect_arch() {
    if [ "$(uname -s)" != "Linux" ]; then
        log_error "Unsupported operating system: $(uname -s). voice-type v5 requires Linux."
        exit 1
    fi
    case "$(uname -m)" in
        x86_64 | amd64) echo "x64" ;;
        aarch64 | arm64) echo "arm64" ;;
        *)
            log_error "Unsupported architecture: $(uname -m). voice-type ships x64 and arm64 Linux builds."
            exit 1
            ;;
    esac
}

get_latest_tag() {
    curl -sSfL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name":' \
        | sed -E 's/.*"tag_name":[[:space:]]*"([^"]+)".*/\1/' \
        | head -n 1
}

# This repository publishes several majors from one release stream, and their
# assets are not interchangeable. Accept v5 tags only; anything older ships a
# different runtime that this script would misconfigure.
guard_major_version() {
    _tag="$1"
    if ! printf '%s\n' "$_tag" \
        | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$'; then
        log_error "Invalid release tag: $_tag"
        exit 1
    fi
    _major=$(echo "${_tag#v}" | cut -d. -f1)
    [ "$_major" -lt 5 ] || return 0

    if [ "$MODE" = "version" ]; then
        log_error "$_tag is not a v5 release; this installer only handles v5 and later."
        exit 1
    fi

    log_error "The latest published release is $_tag -- no v5 release exists yet."
    if [ -f go.mod ] && [ -f main.go ]; then
        log_error "You are in a source tree; build and install it with:  v5/install.sh --local"
    else
        log_error "This installer only installs v5; retry after its first release is published."
    fi
    exit 1
}

install_binary_file() {
    _src="$1"
    _target_dir="$PREFIX/bin"

    if [ ! -d "$_target_dir" ]; then
        mkdir -p "$_target_dir" 2> /dev/null || as_root mkdir -p "$_target_dir"
    fi

    log_info "Installing to $_target_dir/$BINARY_NAME"
    if [ -w "$_target_dir" ]; then
        install -m 755 "$_src" "$_target_dir/$BINARY_NAME"
    else
        as_root install -m 755 "$_src" "$_target_dir/$BINARY_NAME"
    fi

    case ":$PATH:" in
        *":$_target_dir:"*) ;;
        *) log_warn "$_target_dir is not in your PATH. Add it to run '$BINARY_NAME'." ;;
    esac
}

build_local() {
    need_cmd go
    # The v5 tree lives in v5/, so --local works from anywhere in the checkout.
    _dir=$(dirname "$0")
    if [ -f "$_dir/go.mod" ]; then
        cd "$_dir"
    fi
    if [ ! -f "go.mod" ] || [ ! -f "main.go" ]; then
        log_error "--local must run from the v5 source tree (no go.mod/main.go here)."
        exit 1
    fi
    _ver="dev"
    [ -f VERSION ] && _ver=$(cat VERSION)
    log_info "Building $BINARY_NAME $_ver from source..."
    CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$_ver" -o "$TMP_DIR/$BINARY_NAME" ./cmd/voice-type
    install_binary_file "$TMP_DIR/$BINARY_NAME"
}

download_release() {
    need_cmd curl
    need_cmd tar

    if [ "$MODE" = "version" ]; then
        TAG="$VERSION_TAG"
    else
        log_info "Resolving the latest release..."
        TAG=$(get_latest_tag)
        [ -n "$TAG" ] || {
            log_error "Could not determine the latest release."
            exit 1
        }
    fi
    guard_major_version "$TAG"

    _file="${BINARY_NAME}-v5-linux-${ARCH}.tar.gz"
    _base="https://github.com/${REPO}/releases/download/${TAG}"

    log_info "Downloading $TAG ($ARCH)..."
    curl -sSfL "${_base}/${_file}" -o "$TMP_DIR/$_file"
    curl -sSfL "${_base}/checksums.txt" -o "$TMP_DIR/checksums.txt"

    _expected=$(grep " ${_file}\$" "$TMP_DIR/checksums.txt" | awk '{print $1}' | head -n 1)
    [ -n "$_expected" ] || {
        log_error "No checksum for $_file in checksums.txt."
        exit 1
    }

    if command -v sha256sum > /dev/null 2>&1; then
        _actual=$(sha256sum "$TMP_DIR/$_file" | awk '{print $1}')
    elif command -v shasum > /dev/null 2>&1; then
        _actual=$(shasum -a 256 "$TMP_DIR/$_file" | awk '{print $1}')
    else
        log_error "Neither sha256sum nor shasum found -- cannot verify the download."
        exit 1
    fi

    [ "$_expected" = "$_actual" ] || {
        log_error "Checksum mismatch for $_file. Refusing to install."
        exit 1
    }
    log_info "Checksum verified."

    tar -xzf "$TMP_DIR/$_file" -C "$TMP_DIR"

    _binary="$TMP_DIR/${BINARY_NAME}-${ARCH}/${BINARY_NAME}"
    _reported_version=$("$_binary" version)
    if [ "$_reported_version" != "${TAG#v}" ]; then
        log_error "Downloaded binary reports version $_reported_version, expected ${TAG#v}."
        exit 1
    fi
    install_binary_file "$_binary"
}

# Called when creating or replacing a config. An existing file is never edited to
# add a key.
#
# The key is a credential, so it is read with terminal echo off and echoed back
# as asterisks: it must not survive in scrollback, in a screen share, or in the
# terminal's own buffer. `stty -g` captures the exact prior state to restore --
# and the INT/TERM trap is mandatory, because a Ctrl-C between disabling echo and
# restoring it would hand the user back a shell that types invisibly.
ask_api_key() {
    # A key in the environment wins over the file, so there is nothing to ask.
    [ -z "${OPENROUTER_API_KEY:-}" ] || return 0
    has_tty || return 0

    # Echo goes off *before* the prompt is printed. Anything arriving between the
    # two -- unexpected input, a fast typist, an automated answer -- would otherwise be
    # echoed by the line discipline in that window.
    _stty_saved=$(stty -g < /dev/tty 2> /dev/null) || _stty_saved=""
    if [ -n "$_stty_saved" ]; then
        trap 'stty "$_stty_saved" < /dev/tty 2> /dev/null; exit 130' INT TERM
        stty -echo < /dev/tty 2> /dev/null || _stty_saved=""
    fi

    printf 'OpenRouter API key (hidden, blank to set it later): ' > /dev/tty
    read -r API_KEY < /dev/tty || API_KEY=""

    if [ -n "$_stty_saved" ]; then
        stty "$_stty_saved" < /dev/tty 2> /dev/null || true
        trap 'exit 130' INT TERM
        # Echo off swallowed the user's newline too, so this line both masks the
        # key and terminates the prompt. With no stty the terminal echoed the key
        # itself and there is nothing to mask.
        printf '%s\n' "$(printf '%s' "$API_KEY" | sed 's/./*/g')" > /dev/tty
    fi
}

# Writes a config, offering to skip when one already exists. The safe default is
# to keep it; declining the prompt explicitly replaces it without a backup.
write_config() {
    CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}"
    CONFIG_FILE="$CONFIG_DIR/voice-type.jsonc"
    _config_action="Wrote"

    if [ -f "$CONFIG_FILE" ]; then
        case "$(prompt_yn "Skip creating the existing config at $(link_path "$CONFIG_FILE")?" "Y")" in
            [Yy]*)
                chmod 600 "$CONFIG_FILE" || {
                    log_error "Could not restrict $CONFIG_FILE to mode 600."
                    exit 1
                }
                log_info "Keeping existing config at $(link_path "$CONFIG_FILE")"
                return 0
                ;;
            *) _config_action="Replaced" ;;
        esac
    fi

    ask_api_key
    mkdir -p "$CONFIG_DIR"
    CONFIG_TMP=$(mktemp "$CONFIG_DIR/.voice-type.jsonc.XXXXXX")
    # The key lands on disk, so write a private same-directory temporary file
    # and atomically replace the config only after the complete write succeeds.
    (
        umask 077
        _json_key=$(printf '%s' "$API_KEY" | sed 's/\\/\\\\/g; s/"/\\"/g')
        printf '{\n    "api_key": "%s", // or set OPENROUTER_API_KEY, which wins\n' "$_json_key" > "$CONFIG_TMP"
        cat >> "$CONFIG_TMP" << 'EOF'
    "port": 3232, // int 1024-65535
    // These models read "vocabulary": gpt-4o-transcribe (~$0.22/hr, the most
    // accurate), whisper-large-v3 (~$0.03/hr, ~1s slower), gpt-transcribe,
    // whisper-1, whisper-large-v3-turbo, gpt-4o-mini-transcribe. These ignore
    // it, for less: parakeet-v3 (v5.1's model), whisper-large-v3-turbo-groq.
    "model": "gpt-4o-transcribe",
    // Names and jargon, sent with the audio so they are spelled right as they
    // are transcribed. Every term is billed on every dictation.
    "vocabulary": [] // e.g. ["Numbero", "kubectl", "Erik Novikov"]
    //
    // Keyboard shortcuts, set in your desktop environment:
    //   Dictate:            curl -s http://localhost:3232/toggle
    //   Start/stop daemon:  sh -c "curl -s http://localhost:3232/exit || voice-type"
}
EOF
    )
    chmod 600 "$CONFIG_TMP"
    mv -f "$CONFIG_TMP" "$CONFIG_FILE"
    CONFIG_TMP=""
    CONFIG_CREATED=1
    log_info "$_config_action $(link_path "$CONFIG_FILE")"
}

# Read from the file rather than from what this run happened to type: on a
# reinstall the key is already in the config and warning about it is a lie.
config_has_key() {
    [ -f "$CONFIG_FILE" ] || return 1
    sed 's|//.*||g' "$CONFIG_FILE" 2> /dev/null \
        | grep -Eq '"api_key"[[:space:]]*:[[:space:]]*"[^"]+"'
}

print_summary() {
    if [ "$CONFIG_CREATED" = "1" ]; then
        log_info "Keyboard shortcuts, to set in your desktop environment:"
        log_info "  Dictate:            curl -s http://localhost:$PORT/toggle"
        log_info "  Start/stop daemon:  sh -c \"curl -s http://localhost:$PORT/exit || voice-type\""
    fi

    # Both of these leave voice-type unable to work, so they stay.
    if [ -z "${OPENROUTER_API_KEY:-}" ] && ! config_has_key; then
        log_warn "No API key yet. Set OPENROUTER_API_KEY, or add \"api_key\" to $(link_path "$CONFIG_FILE")"
    fi
}

main() {
    ARCH=$(detect_arch)

    TMP_DIR=$(mktemp -d)
    trap cleanup EXIT
    trap 'exit 130' INT TERM

    if [ "$MODE" = "local" ]; then
        build_local
    else
        download_release
    fi

    write_config
    print_summary
}

while [ $# -gt 0 ]; do
    case "$1" in
        --version)
            [ $# -ge 2 ] || { log_error "--version needs a tag, e.g. --version v5.0.0"; exit 1; }
            VERSION_TAG="$2"; MODE="version"; shift 2
            ;;
        --local) MODE="local"; shift ;;
        *) log_error "Unknown option: $1"; usage; exit 1 ;;
    esac
done

main
