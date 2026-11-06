#!/bin/sh
# One application installer. Configuration and credentials belong to the UI.
set -eu
umask 022
REPO=eriknovikov/voice-type
BIN_DIR=${VOICE_TYPE_BIN_DIR:-"$HOME/.local/bin"}
DATA_DIR=${XDG_DATA_HOME:-"$HOME/.local/share"}
CONFIG_DIR=${XDG_CONFIG_HOME:-"$HOME/.config"}
RUNTIME_DIR=${XDG_RUNTIME_DIR:+"$XDG_RUNTIME_DIR/voice-type"}
RUNTIME_DIR=${RUNTIME_DIR:-"${XDG_STATE_HOME:-$HOME/.local/state}/voice-type"}
APP_ID=io.github.eriknovikov.voice-type
DESKTOP_FILE="$DATA_DIR/applications/$APP_ID.desktop"
ICON_FILE="$DATA_DIR/icons/hicolor/512x512/apps/$APP_ID.png"
TEMP_DIR=
STAGED_BIN=
cleanup() {
    [ -z "$TEMP_DIR" ] || rm -rf "$TEMP_DIR"
    [ -z "$STAGED_BIN" ] || rm -f "$STAGED_BIN"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
fail() { printf '%s\n' "$*" >&2; exit 1; }
usage() { printf '%s\n' 'Usage: install.sh [--local | --version v6.x.y | --uninstall]'; }
MODE=download
TAG=
case "${1:-}" in
    '') ;;
    --local) MODE=local; shift ;;
    --version) [ $# -ge 2 ] || fail 'Missing release version.'; TAG=$2; shift 2 ;;
    --uninstall) MODE=uninstall; shift ;;
    --help|-h) usage; exit 0 ;;
    *) usage >&2; exit 2 ;;
esac
[ $# -eq 0 ] || { usage >&2; exit 2; }
[ "$(uname -s)" = Linux ] || fail 'Voice Type currently supports Linux.'
for cmd in flock timeout; do command -v "$cmd" >/dev/null || fail "Missing required command: $cmd"; done

if [ "$MODE" != uninstall ]; then
    if [ "$MODE" = local ]; then
        SOURCE_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
        [ -f "$SOURCE_DIR/go.mod" ] || fail '--local requires a source checkout.'
        make -C "$SOURCE_DIR" build
        PAYLOAD=$SOURCE_DIR/build
        SOURCE_ICON=$SOURCE_DIR/icon.png
    else
        for cmd in curl tar sha256sum; do command -v "$cmd" >/dev/null || fail "Missing required command: $cmd"; done
        TEMP_DIR=$(mktemp -d)
        if [ -z "$TAG" ]; then
            RELEASE_URL=$(curl -fLsS -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")
            TAG=${RELEASE_URL##*/}
        fi
        case "$TAG" in v[6-9].*|v[1-9][0-9]*.*) ;; *) fail 'An application release (v6 or newer) is required; daemon releases are unsupported.' ;; esac
        case "$TAG" in *[!a-zA-Z0-9.-]*) fail 'Invalid release version.' ;; esac
        case "$(uname -m)" in x86_64) ARCH=x64 ;; aarch64|arm64) ARCH=arm64 ;; *) fail 'Unsupported architecture.' ;; esac
        ARCHIVE="voice-type-linux-$ARCH.tar.gz"
        BASE_URL="https://github.com/$REPO/releases/download/$TAG"
        curl -fLsS "$BASE_URL/$ARCHIVE" -o "$TEMP_DIR/$ARCHIVE"
        curl -fLsS "$BASE_URL/checksums.txt" -o "$TEMP_DIR/checksums.txt"
        # Select exactly this artifact; never follow paths from a checksum file.
        EXPECTED=$(awk -v name="$ARCHIVE" '$2 == name { print $1 }' "$TEMP_DIR/checksums.txt")
        [ "${#EXPECTED}" -eq 64 ] || fail 'Missing or invalid application checksum.'
        case "$EXPECTED" in *[!0-9a-fA-F]*) fail 'Invalid checksum.' ;; esac
        printf '%s  %s\n' "$EXPECTED" "$ARCHIVE" > "$TEMP_DIR/selected-checksum"
        (cd "$TEMP_DIR" && sha256sum -c selected-checksum)
        tar -tzf "$TEMP_DIR/$ARCHIVE" | sort > "$TEMP_DIR/entries"
        printf '%s\n' LICENSE icon.png voice-type | sort > "$TEMP_DIR/expected-entries"
        cmp -s "$TEMP_DIR/entries" "$TEMP_DIR/expected-entries" || fail 'Unexpected application archive layout.'
        mkdir "$TEMP_DIR/payload"
        tar -xzf "$TEMP_DIR/$ARCHIVE" --no-same-owner -C "$TEMP_DIR/payload"
        PAYLOAD=$TEMP_DIR/payload
        SOURCE_ICON=$PAYLOAD/icon.png
    fi
    for asset in "$PAYLOAD/voice-type" "$SOURCE_ICON"; do
        [ -f "$asset" ] && [ ! -L "$asset" ] || fail 'Missing application binary or icon.'
    done
    APP_VERSION=$("$PAYLOAD/voice-type" --version) || fail 'Cannot run application. Install GTK4 and WebKitGTK 6.0 runtime libraries.'
    case "$APP_VERSION" in [6-9].*|[1-9][0-9]*.*) ;; *) fail 'Refusing to install a daemon-only binary.' ;; esac
    [ -z "$TAG" ] || [ "v$APP_VERSION" = "$TAG" ] || fail 'Downloaded application version does not match the release.'
    CONTROL_BIN=$PAYLOAD/voice-type
else
    CONTROL_BIN=$BIN_DIR/voice-type
    [ -x "$CONTROL_BIN" ] || CONTROL_BIN=$BIN_DIR/voice-type-app
    [ -x "$CONTROL_BIN" ] || fail 'Application binary not found; integration was left untouched.'
fi

# Request the normal guarded shutdown. A refused/unfinished save or recording
# must prevent replacement; forwarding alone does not acknowledge completion.
timeout 15 "$CONTROL_BIN" --quit || fail 'Could not request shutdown. Quit Voice Type and retry.'
[ ! -L "$RUNTIME_DIR" ] || fail 'Unsafe runtime directory.'
(umask 077; mkdir -p "$RUNTIME_DIR")
[ "$(stat -c %u "$RUNTIME_DIR")" = "$(id -u)" ] && [ "$(stat -c %a "$RUNTIME_DIR")" = 700 ] || fail 'Runtime directory must be private and owned by you.'
[ ! -L "$RUNTIME_DIR/engine.lock" ] || fail 'Unsafe engine lock.'
(umask 077; touch "$RUNTIME_DIR/engine.lock")
[ -f "$RUNTIME_DIR/engine.lock" ] && [ "$(stat -c %u "$RUNTIME_DIR/engine.lock")" = "$(id -u)" ] && [ "$(stat -c %a "$RUNTIME_DIR/engine.lock")" = 600 ] || fail 'Engine lock must be a private regular file owned by you.'
exec 9>> "$RUNTIME_DIR/engine.lock"
flock -w 15 9 || fail 'Voice Type is still running. Finish dictation or save/discard pending edits, then retry.'
# Old installations predating the engine lock must also be closed manually.
for proc in /proc/[0-9]*/exe; do
    exe=$(readlink "$proc" 2>/dev/null || true)
    case "$exe" in "$BIN_DIR/voice-type"|"$BIN_DIR/voice-type-app"|"$BIN_DIR/voice-type (deleted)"|"$BIN_DIR/voice-type-app (deleted)") fail 'An older Voice Type process is still running; close it before retrying.' ;; esac
 done

if [ "$MODE" = uninstall ]; then
    "$CONTROL_BIN" --remove-shortcuts
    rm -f "$BIN_DIR/voice-type" "$BIN_DIR/voice-type-app" "$DESKTOP_FILE" "$ICON_FILE" "$CONFIG_DIR/autostart/voice-type.desktop"
    rm -f "$DATA_DIR/icons/hicolor/scalable/apps/voice-type.svg" "$DATA_DIR/icons/hicolor/512x512/apps/voice-type.png"
    printf '%s\n' 'Voice Type removed. Configuration, vocabulary, backups, logs and retained audio were kept.'
else
    mkdir -p "$BIN_DIR" "$(dirname "$DESKTOP_FILE")" "$(dirname "$ICON_FILE")"
    STAGED_BIN=$(mktemp "$BIN_DIR/.voice-type.XXXXXX")
    install -m 755 "$PAYLOAD/voice-type" "$STAGED_BIN"
    install -m 644 "$SOURCE_ICON" "$ICON_FILE"
    # Desktop Entry escaping, not shell escaping. Reject line-breaking paths.
    case "$BIN_DIR" in *'
'*|*"$(printf '\r')"*) fail 'Invalid application installation path.' ;; esac
    # Literal dollar signs belong to Desktop Entry escaping.
    # shellcheck disable=SC2016
    ESCAPED_BIN=$(printf '%s' "$BIN_DIR/voice-type" | sed 's/\\/\\\\/g; s/"/\\"/g; s/`/\\`/g; s/\$/\\$/g; s/%/%%/g')
    write_entry() {
        entry_path=$1
        entry_argument=$2
        entry_temp=$(mktemp "$(dirname "$entry_path")/.voice-type-entry.XXXXXX")
        cat > "$entry_temp" <<ENTRY
[Desktop Entry]
Type=Application
Name=Voice Type
Comment=Voice dictation
Exec="$ESCAPED_BIN"$entry_argument
Icon=$APP_ID
Terminal=false
Categories=AudioVideo;Audio;
StartupNotify=true
ENTRY
        chmod 644 "$entry_temp"
        mv -f "$entry_temp" "$entry_path"
    }
    write_entry "$DESKTOP_FILE" ''
    if [ -f "$CONFIG_DIR/autostart/voice-type.desktop" ]; then
        write_entry "$CONFIG_DIR/autostart/voice-type.desktop" ' --background'
    fi
    mv -f "$STAGED_BIN" "$BIN_DIR/voice-type"
    STAGED_BIN=
    rm -f "$BIN_DIR/voice-type-app" "$DATA_DIR/icons/hicolor/scalable/apps/voice-type.svg" "$DATA_DIR/icons/hicolor/512x512/apps/voice-type.png"
    printf 'Installed Voice Type %s. Open it from the launcher or run %s/voice-type\n' "$APP_VERSION" "$BIN_DIR"
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then gtk-update-icon-cache -f -t "$DATA_DIR/icons/hicolor" >/dev/null 2>&1 || true; fi
if command -v update-desktop-database >/dev/null 2>&1; then update-desktop-database "$DATA_DIR/applications" >/dev/null 2>&1 || true; fi
