#!/bin/sh
# voice-type v4 installer (DEPRECATED).
#
# v4 is the Chrome + Web Speech API version, superseded by the v5 Go daemon in
# the repository root. It still installs and runs; it is no longer developed.
#
#   curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v4/install.sh | sh
set -e

REPO="eriknovikov/voice-type"
BINARY_NAME="voice-type"
V4_FALLBACK_TAG="v4.2.2"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() {
    printf "${GREEN}[INFO]${NC} %s\n" "$1" >&2
}

log_warn() {
    printf "${YELLOW}[WARN]${NC} %s\n" "$1" >&2
}

log_error() {
    printf "${RED}[ERROR]${NC} %s\n" "$1" >&2
}

log_link() {
    _label="$1"
    _path="$2"
    printf "${GREEN}[INFO]${NC} %s " "$_label" >&2
    printf "\033]8;;file://%s\007%s\033]8;;\007\n" "$_path" "$_path" >&2
}

user_in_input_group() {
    id -nG 2>/dev/null | tr ' ' '\n' | grep -qx "input"
}

prompt_yn() {
    _msg="$1"
    _default="$2"
    case "$_default" in
        [Yy]*) _opts="Y/n"; _bracket="y" ;;
        [Nn]*) _opts="y/N"; _bracket="n" ;;
        *) log_error "prompt_yn: default must be Y or N"; exit 1 ;;
    esac
    printf "%s (%s) [%s] " "$_msg" "$_opts" "$_bracket" >&2
    read -r _answer < /dev/tty || _answer=""
    if [ -z "$_answer" ]; then
        echo "$_default"
    else
        echo "$_answer"
    fi
}

detect_arch() {
    ARCH="$(uname -m)"
    case "$ARCH" in
        x86_64|amd64) echo "x64" ;;
        arm64|aarch64) echo "arm64" ;;
        *) log_error "Unsupported architecture: $ARCH"; exit 1 ;;
    esac
}

detect_pkg_manager() {
    if command -v apt-get >/dev/null 2>&1; then echo "apt"; return; fi
    if command -v dnf >/dev/null 2>&1; then echo "dnf"; return; fi
    if command -v pacman >/dev/null 2>&1; then echo "pacman"; return; fi
    if command -v apk >/dev/null 2>&1; then echo "apk"; return; fi
    if command -v xbps-install >/dev/null 2>&1; then echo "xbps"; return; fi
    if command -v nix-env >/dev/null 2>&1; then echo "nix"; return; fi
    echo "none"
}

browser_probe() {
    for p in \
        "/usr/bin/google-chrome" \
        "/usr/bin/google-chrome-stable" \
        "/usr/bin/google-chrome-beta" \
        "/opt/google/chrome/chrome" \
        "/usr/bin/chromium" \
        "/usr/bin/chromium-browser" \
        "/usr/local/bin/chromium"
    do
        case "$p" in
            /snap/bin/*) continue ;;
            *org.chromium.*) continue ;;
        esac
        if [ -x "$p" ]; then
            echo "$p"
            return 0
        fi
    done
    return 1
}

detect_browser() {
    BROWSER_PATH=$(browser_probe)
    if [ -n "$BROWSER_PATH" ]; then
        case "$BROWSER_PATH" in
            *chromium*) BROWSER_TYPE="chromium" ;;
            *) BROWSER_TYPE="chrome" ;;
        esac
    else
        log_warn "No Chrome/Chromium found. Set browser_path in config later."
        BROWSER_PATH=""
        BROWSER_TYPE="chrome"
    fi
}

ask_notifications() {
    ANSWER=$(prompt_yn "Enable text notifications?" "N")
    case "$ANSWER" in
        [Yy]*) TEXT_ENABLED="true" ;;
        *) TEXT_ENABLED="false" ;;
    esac

    ANSWER=$(prompt_yn "Enable sound notifications?" "N")
    case "$ANSWER" in
        [Yy]*)
            SOUND_ENABLED="true"
            if ! command -v canberra-gtk-play >/dev/null 2>&1 && ! command -v paplay >/dev/null 2>&1; then
                PM=$(detect_pkg_manager)
                case "$PM" in
                    apt) sudo apt-get install -y pulseaudio-utils libcanberra-gtk3-module || true ;;
                    dnf) sudo dnf install -y pulseaudio-utils libcanberra-gtk3 || true ;;
                    pacman) sudo pacman -S --noconfirm libpulse libcanberra || true ;;
                    apk) sudo apk add pulseaudio-utils libcanberra || true ;;
                    xbps) sudo xbps-install -y pulseaudio-utils libcanberra || true ;;
                    *) log_warn "Install pulseaudio-utils + libcanberra manually." ;;
                esac
            fi
            ;;
        *) SOUND_ENABLED="false" ;;
    esac
}

install_dotool() {
    HAS_DOTOOL=0
    if command -v dotool >/dev/null 2>&1; then
        HAS_DOTOOL=1
    fi
    : "${DOTOOL_NEEDS_RELOGIN:=0}"

    while true; do
        if [ "$HAS_DOTOOL" = "1" ]; then
            ANSWER=$(prompt_yn "Reinstall dotool from source? (already on PATH)" "N")
        else
            ANSWER=$(prompt_yn "Install dotool from source? (REQUIRED - not found on PATH)" "Y")
        fi
        case "$ANSWER" in
            [Yy]*) break ;;
            [Nn]*)
                if [ "$HAS_DOTOOL" = "1" ]; then
                    return 0
                else
                    log_error "dotool is required for typing. You cannot skip this step."
                    continue
                fi
                ;;
        esac
    done

    PM=$(detect_pkg_manager)

    DEPS=""
    INSTALL_CMD=""
    UNINSTALL_CMD=""
    case "$PM" in
        apt)
            DEPS="golang-go libxkbcommon-dev scdoc build-essential"
            INSTALL_CMD="sudo apt-get install -y"
            UNINSTALL_CMD="sudo apt-get remove -y"
            ;;
        dnf)
            DEPS="golang libxkbcommon-devel scdoc gcc make"
            INSTALL_CMD="sudo dnf install -y"
            UNINSTALL_CMD="sudo dnf remove -y"
            ;;
        pacman)
            DEPS="go libxkbcommon scdoc base-devel"
            INSTALL_CMD="sudo pacman -S --noconfirm"
            UNINSTALL_CMD="sudo pacman -Rs --noconfirm"
            ;;
        apk)
            DEPS="go libxkbcommon-dev scdoc build-base"
            INSTALL_CMD="sudo apk add"
            UNINSTALL_CMD="sudo apk del"
            ;;
        xbps)
            DEPS="go libxkbcommon-devel scdoc base-devel"
            INSTALL_CMD="sudo xbps-install -y"
            UNINSTALL_CMD="sudo xbps-remove -y"
            ;;
        *)
            log_error "Unsupported package manager. Install dotool manually."
            return 1
            ;;
    esac

    NEEDED=""
    for dep in $DEPS; do
        if ! dpkg -s "$dep" >/dev/null 2>&1 \
            && ! rpm -q "$dep" >/dev/null 2>&1 \
            && ! pacman -Qi "$dep" >/dev/null 2>&1 \
            && ! apk info "$dep" >/dev/null 2>&1 \
            && ! xbps-query "$dep" >/dev/null 2>&1; then
            NEEDED="$NEEDED $dep"
        fi
    done

    NEEDED=$(echo "$NEEDED" | sed 's/^ *//;s/ *$//')

    if [ -n "$NEEDED" ]; then
        log_info "Installing build deps:$NEEDED"
        $INSTALL_CMD $NEEDED || {
            log_error "Failed to install build dependencies"
            return 1
        }
    fi

    TMP_DOTOOL=$(mktemp -d)
    trap 'rm -rf "$TMP_DOTOOL"' EXIT

    log_info "Downloading and building dotool 1.6..."
    curl -sSL "https://git.sr.ht/~geb/dotool/archive/1.6.tar.gz" | tar -xz -C "$TMP_DOTOOL" || {
        log_error "Failed to download dotool source"
        rm -rf "$TMP_DOTOOL"
        return 1
    }

    (cd "$TMP_DOTOOL/dotool-1.6" && ./build.sh && sudo ./build.sh install) || {
        log_error "dotool build failed"
        rm -rf "$TMP_DOTOOL"
        return 1
    }

    sudo udevadm control --reload
    sudo udevadm trigger

    if user_in_input_group; then
        DOTOOL_NEEDS_RELOGIN=0
    else
        log_warn "Adding $USER to 'input' group. You MUST re-login or run: newgrp input"
        sudo usermod -aG input "$USER"
        DOTOOL_NEEDS_RELOGIN=1
    fi

    if [ -n "$NEEDED" ]; then
        ANSWER=$(prompt_yn "Remove build dependencies? (dotool is already installed)" "Y")
        case "$ANSWER" in
            [Yy]*) $UNINSTALL_CMD $NEEDED 2>/dev/null || true ;;
        esac
    fi

    rm -rf "$TMP_DOTOOL"
    trap - EXIT
}

write_config() {
    CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}"
    CONFIG_FILE="$CONFIG_DIR/voice-type.jsonc"

    LANG_HEURISTIC="en-US"
    if [ -n "$LANG" ]; then
        RAW_LANG=$(echo "$LANG" | cut -d. -f1 | tr '_' '-')
        case "$RAW_LANG" in
            en-US|en-GB|en-AU|en-CA|en-IN|es-ES|es-MX|es-AR|es-CO|ru-RU|\
            zh-CN|zh-TW|zh-HK|ja-JP|ko-KR|fr-FR|fr-CA|de-DE|de-AT|de-CH|\
            pt-BR|pt-PT|it-IT|nl-NL|pl-PL|tr-TR|ar-SA|hi-IN|sv-SE|no-NO|\
            da-DK|fi-FI|el-GR|he-IL|th-TH|vi-VN|id-ID|uk-UA|cs-CZ|ro-RO|hu-HU)
                LANG_HEURISTIC="$RAW_LANG"
                ;;
        esac
    fi

    mkdir -p "$CONFIG_DIR"

    cat > "$CONFIG_FILE" << EOF
{
    "port": 3232, // int 1024-65535, default 3232
    "lang": "${LANG_HEURISTIC}", // BCP47 tag, default en-US; see src/constants.ts for allowed values
    "browser_type": "${BROWSER_TYPE:-chrome}", // "chrome" | "chromium", default "chrome"
    "browser_path": "${BROWSER_PATH}", // absolute path to Chrome/Chromium binary, default auto-detected
    "stream": true, // bool (true | false), default true — live interim transcripts
    "timeout": 0, // int seconds of silence before auto-stop (streaming only), default 0 (off)
    "sound": ${SOUND_ENABLED:-false}, // bool (true | false), default false
    "text": ${TEXT_ENABLED:-false}, // bool (true | false), default false
    "punctuation": true // bool (true | false), default true — spoken punctuation + capitalization for en-*
    // Set up keyboard shortcuts in your DE settings using these commands:
    //   Start/stop daemon:  sh -c "curl -s http://localhost:3232/exit || voice-type"
    //   Dictate:            curl -s http://localhost:3232/toggle?lang=en-US
    //   Dictate (Spanish):  curl -s http://localhost:3232/toggle?lang=es-ES
}
EOF
}

print_summary() {
    CONFIG_PATH="${XDG_CONFIG_HOME:-$HOME/.config}/voice-type.jsonc"
    PORT="${VT_PORT:-3232}"
    log_info "Voice Type installed."
    log_link "Config:" "$CONFIG_PATH"
    log_info "Set up keyboard shortcuts in your DE settings:"
    log_info "  Start/stop daemon:  sh -c \"curl -s http://localhost:${PORT}/exit || voice-type\""
    log_info "  Dictate (English):  curl -s http://localhost:${PORT}/toggle?lang=en-US"
    log_info "  Dictate (Spanish):  curl -s http://localhost:${PORT}/toggle?lang=es-ES"
    if [ "${DOTOOL_NEEDS_RELOGIN:-0}" = "1" ]; then
        log_warn "Log out and back in (or run: newgrp input) for input group to take effect."
    fi
}

# v5 ships from this same repository, so /releases/latest now points at a Go
# binary. v4 must resolve the newest v4.x.y tag explicitly -- otherwise this
# installer would download v5 and then configure it as if it were v4.
get_latest_version() {
    _tags=$(curl -sS "https://api.github.com/repos/${REPO}/releases?per_page=100" | \
        grep '"tag_name":' | \
        sed -E 's/.*"tag_name":[[:space:]]*"([^"]+)".*/\1/' | \
        grep -E '^v4\.[0-9]+\.[0-9]+$' || true)

    if [ -z "$_tags" ]; then
        log_warn "Could not list v4 releases; falling back to ${V4_FALLBACK_TAG}."
        echo "$V4_FALLBACK_TAG"
        return 0
    fi

    echo "$_tags" | sort -t. -k1,1 -k2,2n -k3,3n | tail -n 1
}

install_binary_file() {
    BINARY_PATH="$1"
    TARGET="/usr/local/bin/${BINARY_NAME}"
    if [ -w /usr/local/bin ]; then
        install -m 755 "$BINARY_PATH" "$TARGET"
    else
        sudo install -m 755 "$BINARY_PATH" "$TARGET"
    fi
}

install_binary() {
    if [ "$MODE" = "local" ]; then
        if ! command -v bun >/dev/null 2>&1; then
            log_error "bun not found. Install bun first."
            exit 1
        fi
        log_info "Building binary..."
        mkdir -p build
        bun build src/index.ts --compile --outfile build/voice-type
        mkdir -p "releases/voice-type-${ARCH}"
        cp build/voice-type "releases/voice-type-${ARCH}/"
        tar -czf "releases/voice-type-linux-${ARCH}.tar.gz" -C releases "voice-type-${ARCH}/"
        sha256sum "releases/voice-type-linux-${ARCH}.tar.gz"
        install_binary_file "build/voice-type"
        return 0
    fi

    if [ "$MODE" = "version" ]; then
        TAG="$VERSION"
    else
        log_info "Fetching latest version..."
        TAG=$(get_latest_version)
        if [ -z "$TAG" ]; then
            log_error "Could not determine latest version"
            exit 1
        fi
    fi

    BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
    FILENAME="voice-type-linux-${ARCH}.tar.gz"
    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "$TMP_DIR"' EXIT

    log_info "Downloading ${TAG}..."
    curl -sSfL "${BASE_URL}/${FILENAME}" -o "${TMP_DIR}/${FILENAME}"
    curl -sSfL "${BASE_URL}/checksums.txt" -o "${TMP_DIR}/checksums.txt"

    cd "$TMP_DIR"
    EXPECTED_CHECKSUM=$(grep "${FILENAME}" checksums.txt | awk '{print $1}')
    if [ -z "$EXPECTED_CHECKSUM" ]; then
        log_error "Could not find checksum for ${FILENAME}"
        exit 1
    fi

    if command -v sha256sum > /dev/null 2>&1; then
        ACTUAL_CHECKSUM=$(sha256sum "${FILENAME}" | awk '{print $1}')
    elif command -v shasum > /dev/null 2>&1; then
        ACTUAL_CHECKSUM=$(shasum -a 256 "${FILENAME}" | awk '{print $1}')
    else
        log_error "Neither sha256sum nor shasum found"
        exit 1
    fi

    if [ "$EXPECTED_CHECKSUM" != "$ACTUAL_CHECKSUM" ]; then
        log_error "Checksum mismatch!"
        exit 1
    fi

    tar -xzf "${FILENAME}" -C "$TMP_DIR"
    install_binary_file "${TMP_DIR}/voice-type-${ARCH}/voice-type"

    case ":$PATH:" in
        *":/usr/local/bin:"*) ;;
        *) log_warn "/usr/local/bin is not in your PATH. Add it to use voice-type." ;;
    esac

    rm -rf "$TMP_DIR"
    trap - EXIT
}

main() {
    ARCH=$(detect_arch)
    detect_browser
    ask_notifications
    install_dotool
    install_binary
    write_config
    print_summary
}

MODE="prod"
VERSION=""
while [ $# -gt 0 ]; do
    case "$1" in
        --local) MODE="local"; shift ;;
        --version) VERSION="$2"; MODE="version"; shift 2 ;;
        -*) shift ;;
        *) shift ;;
    esac
done

main "$@"
