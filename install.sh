#!/bin/sh
# One application installer. Installs the distribution package, which resolves
# GTK4, WebKitGTK 6.0, IBus and the audio stack from the distribution
# repositories. Voice Type installs system-wide only: there is no per-user mode,
# so exactly one launcher and one binary can exist.
set -eu
umask 022
REPO=eriknovikov/voice-type
APP_ID=io.github.eriknovikov.voice-type
# Installer tests point these at fixtures instead of the host paths.
INSTALLED_BIN=${VOICE_TYPE_INSTALLED_BIN:-/usr/bin/voice-type}
LEGACY_SYSTEM_BIN=${VOICE_TYPE_LEGACY_BIN:-/usr/local/bin/voice-type}
# Releases could also install per user. Those copies are removed with the
# package so no stale build can start instead of the installed one.
LEGACY_USER_BIN_DIR=${VOICE_TYPE_BIN_DIR:-"$HOME/.local/bin"}
LEGACY_DATA_DIR=${XDG_DATA_HOME:-"$HOME/.local/share"}
LEGACY_DESKTOP_FILE="$LEGACY_DATA_DIR/applications/$APP_ID.desktop"
LEGACY_OLD_DESKTOP_FILE="$LEGACY_DATA_DIR/applications/voice-type.desktop"
LEGACY_ICON_FILE="$LEGACY_DATA_DIR/icons/hicolor/512x512/apps/$APP_ID.png"
LEGACY_OLD_ICON_FILE="$LEGACY_DATA_DIR/icons/hicolor/512x512/apps/voice-type.png"
LEGACY_SCALABLE_ICON="$LEGACY_DATA_DIR/icons/hicolor/scalable/apps/voice-type.svg"
LEGACY_DOC_DIR="$LEGACY_DATA_DIR/doc/voice-type"
TEMP_DIR=
cleanup() { [ -z "$TEMP_DIR" ] || rm -rf "$TEMP_DIR"; }
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
fail() { printf '%s\n' "$*" >&2; exit 1; }
usage() { printf '%s\n' 'Usage: install.sh [--version v6.x.y]'; }
TAG=
while [ $# -gt 0 ]; do
    case "$1" in
        --user|--local) fail 'Voice Type installs system-wide only; per-user installation was removed.' ;;
        --version) [ $# -ge 2 ] || fail 'Missing release version.'; TAG=$2; shift 2 ;;
        --help|-h) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
done
[ "$(uname -s)" = Linux ] || fail 'Voice Type currently supports Linux.'

case "$(uname -m)" in
    x86_64) MACHINE=x86_64 ;;
    aarch64 | arm64) MACHINE=aarch64 ;;
    *) fail 'Unsupported architecture.' ;;
esac

package_kind() {
    if command -v apt-get >/dev/null 2>&1; then
        printf 'deb'
    elif command -v dnf >/dev/null 2>&1; then
        printf 'rpm'
    elif [ "$MACHINE" = x86_64 ] && command -v pacman >/dev/null 2>&1; then
        printf 'arch'
    else
        return 1
    fi
}

KIND=$(package_kind) || fail 'No supported package manager found. Voice Type installs system-wide from a deb, rpm or Arch package (apt-get, dnf or pacman on x86_64).'
[ "$(id -u)" -eq 0 ] || command -v sudo >/dev/null 2>&1 ||
    fail 'Installing system-wide requires root or sudo.'

as_root() {
    if [ "$(id -u)" -eq 0 ]; then
        "$@"
    elif command -v sudo >/dev/null 2>&1; then
        sudo "$@"
    else
        fail 'Installing system-wide requires root or sudo.'
    fi
}

resolve_tag() {
    if [ -z "$TAG" ]; then
        command -v curl >/dev/null 2>&1 || fail 'Missing required command: curl'
        RELEASE_URL=$(curl -fLsS -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")
        TAG=${RELEASE_URL##*/}
    fi
    case "$TAG" in v[6-9].*|v[1-9][0-9]*.*) ;; *) fail 'An application release (v6 or newer) is required; daemon releases are unsupported.' ;; esac
    case "$TAG" in *[!a-zA-Z0-9.-]*) fail 'Invalid release version.' ;; esac
}

# Any process running an installed copy has to be gone before the package is
# replaced: a dictation in progress would lose its recording.
voice_type_running() {
    for proc in /proc/[0-9]*/exe; do
        exe=$(readlink "$proc" 2>/dev/null || true)
        case "$exe" in
            "$INSTALLED_BIN"|"$INSTALLED_BIN (deleted)"|\
            "$LEGACY_SYSTEM_BIN"|"$LEGACY_SYSTEM_BIN (deleted)"|\
            "$LEGACY_USER_BIN_DIR/voice-type"|"$LEGACY_USER_BIN_DIR/voice-type (deleted)"|\
            "$LEGACY_USER_BIN_DIR/voice-type-app"|"$LEGACY_USER_BIN_DIR/voice-type-app (deleted)")
                return 0 ;;
        esac
    done
    return 1
}

request_quit() {
    if [ -x "$INSTALLED_BIN" ]; then
        command -v timeout >/dev/null 2>&1 || fail 'Missing required command: timeout'
        timeout 15 "$INSTALLED_BIN" --quit </dev/null || fail 'Could not request shutdown. Quit Voice Type and retry.'
    fi
    i=0
    while voice_type_running && [ "$i" -lt 50 ]; do
        sleep 0.1
        i=$((i + 1))
    done
    if voice_type_running; then
        fail 'Voice Type is still running; close it and retry.'
    fi
}

# Remove a path, escalating only when the file is not writable by this user.
remove_path() {
    path=$1
    { [ -e "$path" ] || [ -L "$path" ]; } || return 0
    rm -f "$path" 2>/dev/null && return 0
    if [ "$(id -u)" -eq 0 ] || command -v sudo >/dev/null 2>&1; then
        if as_root rm -f "$path" </dev/null; then
            return 0
        fi
    fi
    printf '%s\n' "Could not remove the older installation at $path; remove it with root." >&2
}

# A pre-6 release installed to /usr/local or per user. Either copy takes
# precedence over /usr/bin in a desktop or shell PATH, so both are removed.
remove_legacy_installs() {
    [ "$LEGACY_SYSTEM_BIN" = "$INSTALLED_BIN" ] || remove_path "$LEGACY_SYSTEM_BIN"
    for path in \
        "$LEGACY_USER_BIN_DIR/voice-type" "$LEGACY_USER_BIN_DIR/voice-type-app" \
        "$LEGACY_DESKTOP_FILE" "$LEGACY_OLD_DESKTOP_FILE" \
        "$LEGACY_ICON_FILE" "$LEGACY_OLD_ICON_FILE" "$LEGACY_SCALABLE_ICON"; do
        remove_path "$path"
    done
    [ ! -d "$LEGACY_DOC_DIR" ] || rm -rf "$LEGACY_DOC_DIR"
}

install_package() {
    if [ "$KIND" = arch ] && [ "$MACHINE" != x86_64 ]; then
        fail 'Arch Linux packages are available for x86_64 only.'
    fi
    for cmd in curl sha256sum; do command -v "$cmd" >/dev/null 2>&1 || fail "Missing required command: $cmd"; done
    TEMP_DIR=$(mktemp -d)
    resolve_tag
    BASE_URL="https://github.com/$REPO/releases/download/$TAG"
    curl -fLsS "$BASE_URL/checksums.txt" -o "$TEMP_DIR/checksums.txt"
    case "$KIND" in
        deb)
            PREFIX=voice-type_
            SUFFIX=_amd64.deb
            if [ "$MACHINE" = aarch64 ]; then SUFFIX=_arm64.deb; fi
            ;;
        rpm)
            PREFIX=voice-type-
            SUFFIX=.x86_64.rpm
            if [ "$MACHINE" = aarch64 ]; then SUFFIX=.aarch64.rpm; fi
            ;;
        arch)
            PREFIX=voice-type-
            SUFFIX=-x86_64.pkg.tar.zst
            ;;
        *) fail 'Unsupported package type.' ;;
    esac
    SELECTED=$(awk -v prefix="$PREFIX" -v suffix="$SUFFIX" '
        length($1) == 64 && $1 ~ /^[0-9a-fA-F]+$/ &&
        index($2, prefix) == 1 && index($2, "/") == 0 &&
        substr($2, length($2) - length(suffix) + 1) == suffix { print }
    ' "$TEMP_DIR/checksums.txt")
    [ -n "$SELECTED" ] || fail "The $TAG release has no $KIND package for this architecture."
    [ "$(printf '%s\n' "$SELECTED" | wc -l)" -eq 1 ] || fail 'Multiple matching packages in the release checksums.'
    EXPECTED=$(printf '%s\n' "$SELECTED" | awk '{ print $1 }')
    ASSET=$(printf '%s\n' "$SELECTED" | awk '{ print $2 }')
    curl -fLsS "$BASE_URL/$ASSET" -o "$TEMP_DIR/$ASSET"
    printf '%s  %s\n' "$EXPECTED" "$ASSET" > "$TEMP_DIR/selected-checksum"
    (cd "$TEMP_DIR" && sha256sum -c selected-checksum)
    case "$KIND" in
        deb)
            as_root apt-get update -qq || true
            as_root apt-get install -y "$TEMP_DIR/$ASSET"
            ;;
        rpm)
            as_root dnf install -y "$TEMP_DIR/$ASSET"
            ;;
        arch)
            as_root pacman -U --noconfirm "$TEMP_DIR/$ASSET"
            ;;
    esac
}

request_quit
install_package
INSTALLED=$("$INSTALLED_BIN" --version) || fail 'Voice Type was installed but cannot start. Check the package dependency error above.'
[ "v$INSTALLED" = "$TAG" ] || fail "Installed Voice Type $INSTALLED does not match release $TAG."
remove_legacy_installs
printf 'Installed Voice Type %s system-wide. Open it from the launcher.\n' "$INSTALLED"
printf '%s\n' 'Remove the package, the desktop integration and every user file with uninstall.sh.'
