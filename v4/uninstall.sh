#!/bin/sh
set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

log_info() {
    printf "${GREEN}[INFO]${NC} %s\n" "$1" >&2
}

log_error() {
    printf "${RED}[ERROR]${NC} %s\n" "$1" >&2
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

detect_pkg_manager() {
    if command -v apt-get >/dev/null 2>&1; then echo "apt"; return; fi
    if command -v dnf >/dev/null 2>&1; then echo "dnf"; return; fi
    if command -v pacman >/dev/null 2>&1; then echo "pacman"; return; fi
    if command -v apk >/dev/null 2>&1; then echo "apk"; return; fi
    if command -v xbps-install >/dev/null 2>&1; then echo "xbps"; return; fi
    if command -v nix-env >/dev/null 2>&1; then echo "nix"; return; fi
    echo "none"
}

DETECTED_PM=$(detect_pkg_manager)

config_sound_was_enabled() {
    CONFIG_FILE="${XDG_CONFIG_HOME:-$HOME/.config}/voice-type.jsonc"
    if [ -f "$CONFIG_FILE" ]; then
        stripped=$(sed 's|//.*||g' "$CONFIG_FILE")
        echo "$stripped" | grep -Eq '"sound"[[:space:]]*:[[:space:]]*true'
        return $?
    fi
    return 1
}

remove_voice_type_binary() {
    TARGET="/usr/local/bin/voice-type"
    [ -e "$TARGET" ] || return 0
    ANSWER=$(prompt_yn "Remove voice-type binary ($TARGET)?" "Y")
    case "$ANSWER" in
        [Yy]*)
            if [ -w /usr/local/bin ]; then rm -f "$TARGET"; else sudo rm -f "$TARGET"; fi
            ;;
    esac
}

remove_config_and_logs() {
    CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}"
    CONFIG_FILE="$CONFIG_DIR/voice-type.jsonc"
    CONFIG_BAK="$CONFIG_FILE.bak"
    LOG_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/voice-type"

    if [ -f "$CONFIG_FILE" ]; then
        ANSWER=$(prompt_yn "Remove config ($CONFIG_FILE)?" "Y")
        case "$ANSWER" in [Yy]*) rm -f "$CONFIG_FILE" ;; esac
    fi
    if [ -f "$CONFIG_BAK" ]; then
        ANSWER=$(prompt_yn "Remove config backup ($CONFIG_BAK)?" "Y")
        case "$ANSWER" in [Yy]*) rm -f "$CONFIG_BAK" ;; esac
    fi
    if [ -d "$LOG_DIR" ]; then
        ANSWER=$(prompt_yn "Remove log directory ($LOG_DIR)?" "Y")
        case "$ANSWER" in [Yy]*) rm -rf "$LOG_DIR" ;; esac
    fi
}

remove_dotool() {
    _present=0
    for f in /usr/local/bin/dotool /usr/local/bin/dotoolc /usr/local/bin/dotoold \
             /etc/udev/rules.d/80-dotool.rules; do
        [ -e "$f" ] && _present=1 && break
    done
    [ "$_present" = "1" ] || return 0
    ANSWER=$(prompt_yn "Remove dotool (binaries + man page + udev rule)?" "Y")
    case "$ANSWER" in
        [Yy]*)
            for f in /usr/local/bin/dotool /usr/local/bin/dotoolc /usr/local/bin/dotoold \
                     /etc/udev/rules.d/80-dotool.rules /usr/share/man/man1/dotool.1 \
                     /usr/share/man/man1/dotool.1.gz; do
                if [ -e "$f" ]; then
                    if [ -w "$(dirname "$f")" ]; then rm -f "$f"; else sudo rm -f "$f"; fi
                fi
            done
            if command -v udevadm >/dev/null 2>&1; then
                sudo udevadm control --reload 2>/dev/null || true
                sudo udevadm trigger 2>/dev/null || true
            fi
            ;;
    esac
}

remove_sound_deps() {
    if ! config_sound_was_enabled; then return 0; fi
    DEPS=""
    case "$DETECTED_PM" in
        apt) DEPS="pulseaudio-utils libcanberra-gtk3-module" ;;
        dnf) DEPS="pulseaudio-utils libcanberra-gtk3" ;;
        pacman) DEPS="libpulse libcanberra" ;;
        apk) DEPS="pulseaudio-utils libcanberra" ;;
        xbps) DEPS="pulseaudio-utils libcanberra" ;;
        *) return 0 ;;
    esac
    ANY_PRESENT=0
    for dep in $DEPS; do
        if dpkg -s "$dep" >/dev/null 2>&1 || rpm -q "$dep" >/dev/null 2>&1 || \
           pacman -Qi "$dep" >/dev/null 2>&1 || apk info "$dep" >/dev/null 2>&1 || \
           xbps-query "$dep" >/dev/null 2>&1; then
            ANY_PRESENT=1
            break
        fi
    done
    if [ "$ANY_PRESENT" = "0" ]; then return 0; fi
    ANSWER=$(prompt_yn "Remove sound notification deps ($DEPS)?" "Y")
    case "$ANSWER" in
        [Yy]*)
            case "$DETECTED_PM" in
                apt) sudo apt-get remove -y $DEPS 2>/dev/null || true ;;
                dnf) sudo dnf remove -y $DEPS 2>/dev/null || true ;;
                pacman) sudo pacman -Rs --noconfirm $DEPS 2>/dev/null || true ;;
                apk) sudo apk del $DEPS 2>/dev/null || true ;;
                xbps) sudo xbps-remove -y $DEPS 2>/dev/null || true ;;
            esac
            ;;
    esac
}

main() {
    if [ ! -t 1 ] && [ ! -e /dev/tty ]; then
        log_error "Run from a terminal: bash uninstall.sh"
        exit 1
    fi
    remove_voice_type_binary
    remove_config_and_logs
    remove_dotool
    remove_sound_deps
    log_info "Voice Type uninstalled."
}

main "$@"
