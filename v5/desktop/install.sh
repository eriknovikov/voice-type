#!/bin/sh
set -eu
# Build first with `make install-desktop` from v5. Installation never writes
# credentials or replaces an existing config. Desktop setup owns those steps.
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
BIN_DIR=${VOICE_TYPE_BIN_DIR:-"$HOME/.local/bin"}
DATA_DIR=${XDG_DATA_HOME:-"$HOME/.local/share"}
CONFIG_DIR=${XDG_CONFIG_HOME:-"$HOME/.config"}
DESKTOP_FILE="$DATA_DIR/applications/io.github.eriknovikov.voice-type.desktop"
ICON_FILE="$DATA_DIR/icons/hicolor/512x512/apps/voice-type.png"
case "${1:-}" in
 --uninstall)
  if command -v gsettings >/dev/null 2>&1; then
   if SHORTCUT_PATHS=$(gsettings get org.gnome.settings-daemon.plugins.media-keys custom-keybindings 2>/dev/null); then
    SHORTCUT_PATHS=$(printf '%s' "$SHORTCUT_PATHS" | sed -E "s@'/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/voice-type-(transcription|daemon)/',? ?@@g; s@, ?\]@]@")
    gsettings set org.gnome.settings-daemon.plugins.media-keys custom-keybindings "$SHORTCUT_PATHS"
    for ACTION in transcription daemon; do
     gsettings reset-recursively "org.gnome.settings-daemon.plugins.media-keys.custom-keybinding:/org/gnome/settings-daemon/plugins/media-keys/custom-keybindings/voice-type-$ACTION/"
    done
   fi
  fi
  rm -f "$BIN_DIR/voice-type-app" "$BIN_DIR/voice-type" "$DESKTOP_FILE" "$ICON_FILE" "$DATA_DIR/icons/hicolor/scalable/apps/voice-type.svg" "$CONFIG_DIR/autostart/voice-type.desktop"
  printf '%s\n' 'Voice Type binaries and desktop integration removed. Settings and logs were kept.'
  exit 0 ;;
 '') ;;
 *) printf '%s\n' 'Usage: install.sh [--uninstall]' >&2; exit 2 ;;
esac
for binary in voice-type voice-type-app; do
 [ -x "$SCRIPT_DIR/build/$binary" ] || { printf 'Missing %s. Run make install-desktop from v5.\n' "$binary" >&2; exit 1; }
done
mkdir -p "$BIN_DIR" "$(dirname "$DESKTOP_FILE")" "$(dirname "$ICON_FILE")"
for binary in voice-type voice-type-app; do
 install -m 755 "$SCRIPT_DIR/build/$binary" "$BIN_DIR/.$binary.new"
 mv -f "$BIN_DIR/.$binary.new" "$BIN_DIR/$binary"
done
install -m 644 "$SCRIPT_DIR/icon.png" "$ICON_FILE"
rm -f "$DATA_DIR/icons/hicolor/scalable/apps/voice-type.svg"
if command -v gtk-update-icon-cache >/dev/null 2>&1; then gtk-update-icon-cache -f -t "$DATA_DIR/icons/hicolor" >/dev/null 2>&1 || true; fi
# Desktop Entry escaping is distinct from shell quoting.
ESCAPED_BIN=$(printf '%s' "$BIN_DIR/voice-type-app" | sed 's/\\/\\\\/g; s/"/\\"/g; s/`/\\`/g; s/\$/\\$/g; s/%/%%/g')
cat > "$DESKTOP_FILE" <<ENTRY
[Desktop Entry]
Type=Application
Name=Voice Type
Comment=System-wide voice dictation
Exec="$ESCAPED_BIN"
Icon=voice-type
Terminal=false
Categories=Utility;Audio;
StartupNotify=true
ENTRY
chmod 644 "$DESKTOP_FILE"
if command -v update-desktop-database >/dev/null 2>&1; then update-desktop-database "$DATA_DIR/applications"; fi
printf 'Installed Voice Type. Open it from your application launcher or run:\n  %s/voice-type-app\n' "$BIN_DIR"
