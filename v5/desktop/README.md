# Voice Type desktop

A Wails v3 app with the Go dictation service embedded in the desktop process.
Linux is the supported platform for this release; the UI is portable, but audio and insertion still
use PulseAudio/PipeWire and IBus.

## Install from source

Install Go 1.26+, Node 22.12+ (or Node 24), a C compiler, pkg-config, GTK4 and
WebKitGTK 6.0 development packages. On Debian/Ubuntu these include `libgtk-4-dev`
and `libwebkitgtk-6.0-dev`; on Arch use `gtk4` and `webkitgtk-6.0`.

From `v5/`, run:

```sh
make install-desktop
```

Launch **Voice Type** from the application launcher. Add an OpenAI or
OpenRouter profile, test the microphone, and choose your shortcut.
Settings save automatically; profile edits save through their dialog. Start at
login is enabled unless you previously disabled it. GNOME stores the recording shortcut directly, without an approval dialog.

Select the profile's radio button to use it. Editing and deleting are in the
profile editor; closing with unsaved profile changes asks before discarding them,
including when closing the native window or choosing Quit from the tray.
Vocabulary terms are edited one per line. Logs can be filtered by outcome and
expanded for details.

The app installs as `~/.local/bin/voice-type-app`. Closing the settings window
keeps dictation and the tray running in that same process. Quit stops the entire
app and releases the microphone and HTTP listener. A second launch reopens settings. GNOME needs the AppIndicator/KStatusNotifierItem extension
for the tray icon; the launcher remains available without it.

Stop any standalone `voice-type` daemon before opening the desktop app; a port
conflict fails startup rather than connecting to another process. The standalone
CLI remains separately installable. Port changes save on blur and apply after quitting and reopening Voice Type.
Startup failures show a native error dialog, including background launches.

## Data and build boundaries

The existing `~/.voice-type-config.jsonc` remains the config file. Profiles contain
stable IDs, names and provider-specific settings; `active_profile` selects the
account for new dictations. You can keep several profiles for the same provider.
Switching, editing or deleting one does not change an in-flight dictation.
Deleting the selected profile leaves no account selected.

Saved legacy keys migrate automatically; the exact original is kept privately as
`config-before-profiles.jsonc`. Environment API keys are no longer used or imported.
Last-valid config and desktop-preference backups live in
`$XDG_DATA_HOME/voice-type/backups` (default `~/.local/share/voice-type/backups`).
Missing or damaged files recover automatically, with a notice. Corrupt originals
are preserved privately. Without a valid backup the app opens empty setup;
unreadable files remain untouched and show an error. Writes are atomic and synced,
files use mode 0600, and application-owned directories use 0700. Unknown config
fields are preserved.

Microphone selection persists. If a device disappears, System default becomes the
saved selection; reconnecting the previous device does not change it back. An
unavailable audio server leaves your preference unchanged.

Vocabulary selection and desktop preferences live under `$XDG_STATE_HOME/voice-type`
(default `~/.local/state/voice-type`). Logs stay at `~/.voice-type-logs.jsonl`,
with one rotated segment. No transcript history is stored.

Settings, logs and microphone tests use direct Go calls inside Wails. No desktop
management socket or daemon subprocess is created. The existing shortcut controls
remain on `127.0.0.1:3232` (or your configured port): for example,
`curl -X POST http://127.0.0.1:3232/toggle`. `/exit` quits the entire app.
Management routes and credentials are never exposed over TCP; browser requests
are rejected. Routine settings responses contain only masked keys. Explicit Reveal and Copy
actions are available in the profile editor; copying uses Go's clipboard API.

This separate Go module deliberately enables CGO for Wails. The daemon module,
its tests and its release artifacts continue to build with `CGO_ENABLED=0`.
The desktop build embeds the frontend; it does not require a development server.

```sh
make check-desktop      # from v5
./desktop/install.sh --uninstall
```

Install and uninstall leave any separately installed `voice-type` CLI untouched.
Uninstall preserves configuration, vocabulary, preferences and logs. Quit the
app first. `VOICE_TYPE_BIN_DIR`, `XDG_DATA_HOME` and `XDG_CONFIG_HOME` can redirect
installation for testing.

## Integration checks

After building, `python3 scripts/smoke.py` (from this directory) briefly launches
the actual native app with an isolated home and session bus, verifies listener
ownership, shortcut requests, shutdown and single-instance behavior, and shuts
its test processes down.

From `v5/`, `VOICE_TYPE_TEST_IBUS_DAEMON=/usr/bin/ibus-daemon go test ./...`
checks both provider request formats through real IBus insertion on a private
bus. `VOICE_TYPE_TEST_LIVE_WAV=/path/to/synthetic.wav go test -run TestLiveProviders -v`
opts into a live provider request using saved profiles; profiles without keys are never accepted. Only use a synthetic fixture for that test. The test does not print
credentials or transcripts.

To exercise native tray registration and menu actions without installing a shell
extension, run `VOICE_TYPE_DESKTOP_TEST_BINARY="$PWD/build/voice-type-app"
CGO_ENABLED=1 go test -run TestNativeTray -v` from this directory (as one command).
The test uses a private session bus and a test tray host.

Keyboard Shortcuts defaults to Ctrl+Space to toggle transcription.
The old daemon toggle and its app-owned GNOME binding are removed automatically.
Existing custom transcription shortcuts are retained;
the previous default moves to Ctrl+Space when loading older preferences.
Click a shortcut, press the keys, then click elsewhere to save. Escape disables it.

On GNOME the app manages only its own transcription shortcut entry. Other
shortcuts are preserved. Capture is inline; no GNOME Settings window or portal
dialog is opened. Other Wayland desktops currently require configuring global
shortcuts in the desktop itself. X11 uses Wails' direct shortcut registration.
