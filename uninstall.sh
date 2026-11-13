#!/bin/sh
# One command removes Voice Type: the distribution package, the per-user
# installation, the desktop integration (launcher, icon, docs, autostart entry
# and the GNOME shortcut), the keyring entries it owns and, unless --keep-data
# is given, every file the application wrote (settings, vocabulary, backups,
# logs, credential fallback, local models and retained audio).
#
# Safe to pipe into a shell, which is how the README documents it:
#
#   curl -fsSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/uninstall.sh | sh
#
# Usage: uninstall.sh [--keep-data]
set -eu
umask 022

BIN_DIR=${VOICE_TYPE_BIN_DIR:-"$HOME/.local/bin"}
DATA_DIR=${XDG_DATA_HOME:-"$HOME/.local/share"}
CONFIG_DIR=${XDG_CONFIG_HOME:-"$HOME/.config"}
STATE_DIR=${XDG_STATE_HOME:-"$HOME/.local/state"}
RUNTIME_DIR=${XDG_RUNTIME_DIR:+"$XDG_RUNTIME_DIR/voice-type"}
RUNTIME_DIR=${RUNTIME_DIR:-"$STATE_DIR/voice-type"}
APP_ID=io.github.eriknovikov.voice-type
DESKTOP_FILE="$DATA_DIR/applications/$APP_ID.desktop"
LEGACY_DESKTOP_FILE="$DATA_DIR/applications/voice-type.desktop"
ICON_FILE="$DATA_DIR/icons/hicolor/512x512/apps/$APP_ID.png"
LEGACY_ICON_FILE="$DATA_DIR/icons/hicolor/512x512/apps/voice-type.png"
SCALABLE_ICON="$DATA_DIR/icons/hicolor/scalable/apps/voice-type.svg"
DOC_DIR="$DATA_DIR/doc/voice-type"
AUTOSTART_FILE="$CONFIG_DIR/autostart/voice-type.desktop"
APP_DATA_DIR="$DATA_DIR/voice-type"
APP_CONFIG_DIR="$CONFIG_DIR/voice-type"
CONFIG_FILE="$HOME/.voice-type-config.jsonc"
LOG_FILE="$HOME/.voice-type-logs.jsonl"
DIAGNOSTICS_DIR="${TMPDIR:-/tmp}/voice-type"
# Installer tests point this at a fixture instead of the host's /usr/bin.
INSTALLED_BIN=${VOICE_TYPE_INSTALLED_BIN:-/usr/bin/voice-type}
CREDENTIAL_SERVICE=com.eriknovikov.voice-type
KEEP_DATA=0
REMOVED=0

# Every external command below reads /dev/null: the script itself arrives on
# stdin when the README's `curl ... | sh` form is used, and a child that reads
# stdin would consume the rest of this script.
fail() { printf '%s\n' "$*" >&2; exit 1; }
usage() { printf '%s\n' 'Usage: uninstall.sh [--keep-data]'; }
note() { printf '%s\n' "$*" >&2; }

while [ $# -gt 0 ]; do
    case "$1" in
        --keep-data) KEEP_DATA=1; shift ;;
        --help|-h) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
done
[ "$(uname -s)" = Linux ] || fail 'Voice Type currently supports Linux.'

package_kind() {
    if command -v dpkg-query >/dev/null 2>&1 && dpkg-query -W -f='${Status}' voice-type 2>/dev/null </dev/null | grep -qx 'install ok installed'; then
        printf 'deb'
    elif command -v rpm >/dev/null 2>&1 && rpm -q voice-type </dev/null >/dev/null 2>&1; then
        printf 'rpm'
    elif command -v pacman >/dev/null 2>&1 && pacman -Q voice-type </dev/null >/dev/null 2>&1; then
        printf 'arch'
    else
        return 1
    fi
}

can_root() {
    [ "$(id -u)" -eq 0 ] || command -v sudo >/dev/null 2>&1
}

as_root() {
    if [ "$(id -u)" -eq 0 ]; then
        "$@" </dev/null
    else
        sudo "$@" </dev/null
    fi
}

# A rejected shutdown must stop the uninstall: removing the files underneath a
# recording would lose the take.
request_quit() {
    [ -x "$1" ] || return 0
    command -v timeout >/dev/null 2>&1 || fail 'Missing required command: timeout'
    timeout 15 "$1" --quit </dev/null || fail 'Could not request shutdown. Quit Voice Type and retry.'
}

# An older release without --remove-shortcuts, or a missing GSettings, must not
# block the rest of the removal; the desktop entries go away either way.
remove_shortcuts() {
    [ -x "$1" ] || return 0
    timeout 15 "$1" --remove-shortcuts </dev/null >/dev/null 2>&1 || note 'Could not remove the GNOME shortcut; the desktop integration is removed anyway.'
}

# The application stores API keys in the system keyring. Remove the entries it
# owns; a missing or locked keyring is not a failure.
forget_credentials() {
    [ "${VOICE_TYPE_NO_KEYRING:-0}" = 1 ] && return 0
    command -v secret-tool >/dev/null 2>&1 || return 0
    owners=$(secret-tool search --all service "$CREDENTIAL_SERVICE" </dev/null 2>/dev/null | awk -F' = ' '/^username = /{ print $2 }') || return 0
    [ -n "$owners" ] || return 0
    printf '%s\n' "$owners" | while IFS= read -r owner; do
        [ -n "$owner" ] || continue
        secret-tool clear service "$CREDENTIAL_SERVICE" username "$owner" </dev/null >/dev/null 2>&1 || true
    done
    REMOVED=1
}

# Remove a directory only when it is a real directory: never follow a symlink out
# of the user's home.
remove_directory() {
    [ -e "$1" ] || return 0
    if [ -L "$1" ]; then fail "Refusing to remove symlinked directory: $1"; fi
    [ -d "$1" ] || return 0
    rm -rf "$1"
    REMOVED=1
}

remove_files() {
    for path in "$@"; do
        { [ -e "$path" ] || [ -L "$path" ]; } || continue
        rm -f "$path"
        REMOVED=1
    done
}

# 1. Quit the application and drop the desktop integration state it owns. This
#    has to happen while the binary still exists: the package may be next.
BINARY=
for candidate in "$BIN_DIR/voice-type" "$INSTALLED_BIN"; do
    if [ -x "$candidate" ]; then
        BINARY=$candidate
        break
    fi
done
if [ -n "$BINARY" ]; then
    request_quit "$BINARY"
    remove_shortcuts "$BINARY"
fi
for proc in /proc/[0-9]*/exe; do
    exe=$(readlink "$proc" 2>/dev/null || true)
    case "$exe" in
        "$BIN_DIR/voice-type"|"$BIN_DIR/voice-type-app"|"$BIN_DIR/voice-type (deleted)"|"$BIN_DIR/voice-type-app (deleted)")
            fail 'Voice Type is still running; close it and retry.' ;;
    esac
done
forget_credentials

# 2. Remove the distribution package when one is installed.
PACKAGE_REMOVED=0
if PACKAGE=$(package_kind); then
    if ! can_root; then
        note 'A system package is installed but removing it needs root or sudo; it was left in place.'
    else
        case "$PACKAGE" in
            deb) as_root apt-get remove -y voice-type ;;
            rpm)
                if command -v dnf >/dev/null 2>&1; then as_root dnf remove -y voice-type; else as_root rpm -e voice-type; fi ;;
            arch) as_root pacman -R --noconfirm voice-type ;;
        esac
        PACKAGE_REMOVED=1
        REMOVED=1
    fi
fi

# 3. Remove the per-user installation and the desktop integration. The legacy
#    names predate the current application ID.
remove_files "$BIN_DIR/voice-type" "$BIN_DIR/voice-type-app" \
    "$DESKTOP_FILE" "$LEGACY_DESKTOP_FILE" "$ICON_FILE" "$LEGACY_ICON_FILE" \
    "$SCALABLE_ICON" "$AUTOSTART_FILE"
remove_directory "$DOC_DIR"

# 4. Remove every file the application wrote. `--keep-data` stops here.
if [ "$KEEP_DATA" = 0 ]; then
    remove_files "$CONFIG_FILE" "$CONFIG_FILE".* "$LOG_FILE" "$LOG_FILE".*
    remove_directory "$APP_DATA_DIR"
    remove_directory "$APP_CONFIG_DIR"
    remove_directory "$RUNTIME_DIR"
    remove_directory "$STATE_DIR/voice-type"
    remove_directory "$DIAGNOSTICS_DIR"
fi

# 5. Refresh the desktop caches that still reference the removed entries.
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -f -t "$DATA_DIR/icons/hicolor" </dev/null >/dev/null 2>&1 || true
fi
if command -v update-desktop-database >/dev/null 2>&1; then
    update-desktop-database "$DATA_DIR/applications" </dev/null >/dev/null 2>&1 || true
fi

if [ "$REMOVED" = 0 ]; then
    printf '%s\n' 'Voice Type is not installed; nothing to remove.'
elif [ -n "$PACKAGE_REMOVED" ] && [ "$PACKAGE_REMOVED" = 1 ]; then
    printf '%s\n' 'Removed the Voice Type package, its desktop integration and its user files.'
elif [ "$KEEP_DATA" = 1 ]; then
    printf '%s\n' 'Removed Voice Type. Settings, vocabulary, backups, logs, local models, credentials and retained audio were kept.'
else
    printf '%s\n' 'Removed Voice Type and all of its settings, vocabulary, backups, logs, local models, credentials, retained audio and runtime files.'
fi
