#!/bin/sh
# voice-type v5 installer.
#
# Fetches the static Go binary for this architecture from GitHub Releases,
# verifies its checksum, installs it, and writes a config. Distro-agnostic by
# construction: the binary is static with no libc dependency, so the only
# package work is the clipboard tool.
#
#   curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | sh
#
# v4 (the Chrome-based TypeScript version) is deprecated and lives in v4/.
# To install it instead: .../main/v4/install.sh
set -eu

REPO="eriknovikov/voice-type"
BINARY_NAME="voice-type"
PORT=3232

MODE="prod"          # prod | version | local
VERSION_TAG=""
PREFIX="/usr/local"
SKIP_SYSTEM=0
NEEDS_RELOGIN=0
API_KEY=""

RED=$(printf '\033[31m'); YELLOW=$(printf '\033[33m')
GREEN=$(printf '\033[32m'); RESET=$(printf '\033[0m')

log_info() { printf '%s==>%s %s\n' "$GREEN" "$RESET" "$1" >&2; }
log_warn() { printf '%s!!%s  %s\n' "$YELLOW" "$RESET" "$1" >&2; }
log_error() { printf '%sxx%s  %s\n' "$RED" "$RESET" "$1" >&2; }

usage() {
    cat >&2 << EOF
Usage: install.sh [options]

  --version vX.Y.Z   install a specific release instead of the latest
  --local            build from the working tree with the Go toolchain
  --prefix DIR       install under DIR/bin (default: /usr/local)
  --skip-system      skip uinput / clipboard / audio setup
  -h, --help         show this message
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
    case "$(uname -m)" in
        x86_64 | amd64) echo "x64" ;;
        aarch64 | arm64) echo "arm64" ;;
        *)
            log_error "Unsupported architecture: $(uname -m). voice-type ships x64 and arm64 Linux builds."
            exit 1
            ;;
    esac
}

detect_pkg_manager() {
    if command -v dnf > /dev/null 2>&1; then echo "dnf"
    elif command -v apt-get > /dev/null 2>&1; then echo "apt"
    elif command -v pacman > /dev/null 2>&1; then echo "pacman"
    elif command -v zypper > /dev/null 2>&1; then echo "zypper"
    elif command -v apk > /dev/null 2>&1; then echo "apk"
    elif command -v xbps-install > /dev/null 2>&1; then echo "xbps"
    elif command -v nix-env > /dev/null 2>&1; then echo "nix"
    else echo "none"; fi
}

install_pkg() {
    _pkg="$1"
    case "$(detect_pkg_manager)" in
        dnf) as_root dnf install -y "$_pkg" ;;
        apt) as_root apt-get install -y "$_pkg" ;;
        pacman) as_root pacman -S --noconfirm "$_pkg" ;;
        zypper) as_root zypper install -y "$_pkg" ;;
        apk) as_root apk add "$_pkg" ;;
        xbps) as_root xbps-install -y "$_pkg" ;;
        nix) nix-env -iA "nixpkgs.$_pkg" ;;
        *)
            log_warn "Unknown package manager -- install '$_pkg' manually."
            return 1
            ;;
    esac
}

user_in_input_group() {
    id -nG 2> /dev/null | tr ' ' '\n' | grep -qx input
}

# v5 sends the paste keystroke itself through /dev/uinput, which is owned by the
# 'input' group. This is a hard requirement -- there is no fallback input path.
setup_uinput() {
    if [ ! -e /dev/uinput ]; then
        log_warn "/dev/uinput does not exist. Loading the uinput module..."
        as_root modprobe uinput || log_warn "Could not load the uinput module."
        echo uinput | as_root tee /etc/modules-load.d/uinput.conf > /dev/null 2>&1 || true
    fi

    as_root udevadm control --reload || true
    as_root udevadm trigger || true

    if user_in_input_group; then
        log_info "User is already in the 'input' group."
    else
        log_warn "Adding $USER to the 'input' group."
        as_root usermod -aG input "$USER"
        NEEDS_RELOGIN=1
    fi
}

# Wayland needs wl-clipboard, X11 needs xclip. v5 picks at runtime from the
# session type, so install whichever matches this one.
setup_clipboard() {
    if [ -n "${WAYLAND_DISPLAY:-}" ] || [ "${XDG_SESSION_TYPE:-}" = "wayland" ]; then
        _clip_cmd="wl-copy"; _clip_pkg="wl-clipboard"
    else
        _clip_cmd="xclip"; _clip_pkg="xclip"
    fi

    if command -v "$_clip_cmd" > /dev/null 2>&1; then
        log_info "Clipboard tool '$_clip_cmd' found."
        return 0
    fi

    log_warn "'$_clip_cmd' not found -- voice-type cannot paste without it."
    case "$(prompt_yn "Install $_clip_pkg?" "Y")" in
        [Yy]*) install_pkg "$_clip_pkg" || log_warn "Install '$_clip_pkg' manually before first use." ;;
        *) log_warn "Skipped. Install '$_clip_pkg' manually before first use." ;;
    esac
}

check_audio() {
    if command -v pactl > /dev/null 2>&1 && pactl info > /dev/null 2>&1; then
        log_info "Audio server reachable."
    else
        log_warn "Could not reach PulseAudio/pipewire-pulse. voice-type needs one of them for capture."
    fi
}

# v4 and v5 share port 3232 and are mutually exclusive. A v4 daemon still
# holding the port would make the v5 hotkey silently no-op.
stop_running_daemon() {
    command -v curl > /dev/null 2>&1 || return 0
    curl -s -m 1 "http://localhost:$PORT/health" > /dev/null 2>&1 || return 0

    log_warn "Something is already listening on port $PORT -- probably voice-type v4."
    case "$(prompt_yn "Stop it now?" "Y")" in
        [Yy]*)
            curl -s -m 2 "http://localhost:$PORT/exit" > /dev/null 2>&1 || true
            log_info "Sent /exit to the running daemon."
            ;;
        *) log_warn "Leave it running and v5 will not be able to bind port $PORT." ;;
    esac
}

get_latest_tag() {
    curl -sSfL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name":' \
        | sed -E 's/.*"tag_name":[[:space:]]*"([^"]+)".*/\1/' \
        | head -n 1
}

# v4 and v5 releases live in the same repo. Installing a v4 tag with this script
# would drop a Bun/Chrome binary onto a v5 config, so refuse it by name.
guard_major_version() {
    _tag="$1"
    _major=$(echo "${_tag#v}" | cut -d. -f1)
    case "$_major" in
        '' | *[!0-9]*)
            log_warn "Could not read a major version from '$_tag'. Continuing."
            return 0
            ;;
    esac
    if [ "$_major" -lt 5 ]; then
        log_error "$_tag is a v4 release; this installer only handles v5 and later."
        log_error "Install v4 with: curl -sSL https://raw.githubusercontent.com/${REPO}/main/v4/install.sh | sh"
        exit 1
    fi
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
    if [ ! -f "go.mod" ] || [ ! -f "main.go" ]; then
        log_error "--local must run from the repository root (no go.mod/main.go here)."
        exit 1
    fi
    _ver="dev"
    [ -f VERSION ] && _ver=$(cat VERSION)
    log_info "Building $BINARY_NAME $_ver from source..."
    CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$_ver" -o "$TMP_DIR/$BINARY_NAME" .
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
    install_binary_file "$TMP_DIR/${BINARY_NAME}-${ARCH}/${BINARY_NAME}"
}

ask_api_key() {
    if [ -n "${OPENROUTER_API_KEY:-}" ]; then
        log_info "OPENROUTER_API_KEY is set in the environment; leaving the config key empty."
        return 0
    fi
    has_tty || return 0
    printf 'OpenRouter API key (blank to set it later): ' > /dev/tty
    read -r API_KEY < /dev/tty || API_KEY=""
}

config_is_v4() {
    sed 's|//.*||g' "$1" 2> /dev/null \
        | grep -Eq '"(browser_path|browser_type|lang|stream|punctuation)"'
}

# v5 never rewrites a config at runtime; only this installer does, and only
# after asking. v4 silently overwriting configs is the bug that started all of
# this -- do not reintroduce it here.
write_config() {
    CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}"
    CONFIG_FILE="$CONFIG_DIR/voice-type.jsonc"

    if [ -f "$CONFIG_FILE" ]; then
        if ! config_is_v4 "$CONFIG_FILE"; then
            log_info "Keeping existing config at $CONFIG_FILE"
            return 0
        fi

        log_warn "$CONFIG_FILE looks like a v4 config (Chrome/language fields)."
        if ! has_tty; then
            log_warn "No terminal to ask on -- keeping it. v5 ignores the v4 fields and uses defaults."
            return 0
        fi
        case "$(prompt_yn "Replace it with a v5 config? (a backup is written first)" "Y")" in
            [Yy]*)
                cp "$CONFIG_FILE" "$CONFIG_FILE.v4.bak"
                log_info "Backed up to $CONFIG_FILE.v4.bak"
                ;;
            *)
                log_info "Keeping it. v5 ignores the v4 fields and uses defaults."
                return 0
                ;;
        esac
    fi

    mkdir -p "$CONFIG_DIR"
    # The key lands on disk, so create the file private from the first byte
    # rather than chmod-ing after the write.
    (
        umask 077
        printf '{\n    "api_key": "%s", // or set OPENROUTER_API_KEY, which wins\n' "$API_KEY" > "$CONFIG_FILE"
        cat >> "$CONFIG_FILE" << 'EOF'
    "port": 3232, // int 1024-65535
    "model": "nvidia/parakeet-tdt-0.6b-v3", // any OpenRouter STT slug
    "max_duration": 600, // int seconds, hard cap on one dictation
    "paste_key": "ctrl+v", // terminals usually need "ctrl+shift+v"
    "paste_delay_ms": 300, // wait before restoring the previous clipboard
    "trim_silence": true // cut leading/trailing silence before upload
    // Keyboard shortcuts, set in your desktop environment:
    //   Dictate:            curl -s http://localhost:3232/toggle
    //   Start/stop daemon:  sh -c "curl -s http://localhost:3232/exit || voice-type"
}
EOF
    )
    chmod 600 "$CONFIG_FILE"
    log_info "Wrote $CONFIG_FILE"
}

print_summary() {
    printf '\n' >&2
    log_info "voice-type v5 installed."
    log_info "Config: $CONFIG_FILE"
    if [ -z "$API_KEY" ] && [ -z "${OPENROUTER_API_KEY:-}" ]; then
        log_warn "No API key yet. Set OPENROUTER_API_KEY, or add \"api_key\" to the config."
    fi
    log_info "Keyboard shortcuts, set in your desktop environment:"
    log_info "  Dictate:            curl -s http://localhost:$PORT/toggle"
    log_info "  Start/stop daemon:  sh -c \"curl -s http://localhost:$PORT/exit || voice-type\""

    if [ "$NEEDS_RELOGIN" = "1" ]; then
        printf '\n' >&2
        log_warn "Log out and back in (or run: newgrp input) before first use."
    fi
}

main() {
    ARCH=$(detect_arch)

    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

    if [ "$SKIP_SYSTEM" = "0" ]; then
        setup_uinput
        setup_clipboard
        check_audio
        stop_running_daemon
    fi

    if [ "$MODE" = "local" ]; then
        build_local
    else
        download_release
    fi

    ask_api_key
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
        --prefix)
            [ $# -ge 2 ] || { log_error "--prefix needs a directory"; exit 1; }
            PREFIX="$2"; shift 2
            ;;
        --skip-system) SKIP_SYSTEM=1; shift ;;
        -h | --help) usage; exit 0 ;;
        *) log_error "Unknown option: $1"; usage; exit 1 ;;
    esac
done

main
