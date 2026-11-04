# Voice Type desktop

A Wails v3 companion for the static Voice Type daemon. Linux is the supported
platform for this release; the UI is portable, but audio and insertion still
use PulseAudio/PipeWire and IBus.

## Install from source

Install Go 1.26+, Node 22.12+ (or Node 24), a C compiler, pkg-config, GTK4 and
WebKitGTK 6.0 development packages. On Debian/Ubuntu these include `libgtk-4-dev`
and `libwebkitgtk-6.0-dev`; on Arch use `gtk4` and `webkitgtk-6.0`.

From `v5/`, run:

```sh
make install-desktop
```

Launch **Voice Type** from the application launcher. Set your OpenAI or
OpenRouter key, test the microphone, and save your shortcut and startup
preferences. Wayland shows a desktop-owned shortcut approval dialog.

The app installs alongside `voice-type` in `~/.local/bin`. The desktop window
closes to the background; Quit Voice Type stops both processes. A second launch
reopens the window. GNOME needs the AppIndicator/KStatusNotifierItem extension
for the tray icon; the launcher remains available without it.

If an older daemon is already running, stop it before opening the new app.
Port changes require saving settings and restarting the daemon from Settings.

## Data and build boundaries

The existing `~/.voice-type-config.jsonc` remains the config file. The first
save keeps an exact private `.before-desktop` backup and converts comments to
formatted JSON, preserving unknown fields. Separate provider keys are stored
with mode 0600. `OPENAI_API_KEY` / `OPENROUTER_API_KEY` override saved keys.

Workspace and desktop preferences live under `$XDG_STATE_HOME/voice-type`
(default `~/.local/state/voice-type`). Logs stay at `~/.voice-type-logs.jsonl`,
with one rotated segment. No transcript history is stored.

Desktop management uses a mode-0600 Unix socket inside a private directory.
The existing localhost recording controls remain compatible. Credentials are
write-only from the frontend's perspective.

This separate Go module deliberately enables CGO for Wails. The daemon module,
its tests and its release artifacts continue to build with `CGO_ENABLED=0`.
The desktop build embeds the frontend; it does not require a development server.

```sh
make check-desktop      # from v5
./desktop/install.sh --uninstall
```

Uninstall preserves configuration, vocabulary, preferences and logs. Quit the
app first. `VOICE_TYPE_BIN_DIR`, `XDG_DATA_HOME` and `XDG_CONFIG_HOME` can redirect
installation for testing.
