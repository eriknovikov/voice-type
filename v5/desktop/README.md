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

Launch **Voice Type** from the application launcher. Set your OpenAI or
OpenRouter key, test the microphone, and save your shortcut and startup
preferences. GNOME stores the recording shortcut directly, without an approval dialog.

The app installs as `~/.local/bin/voice-type-app`. Closing the settings window
keeps dictation and the tray running in that same process. Quit stops the entire
app and releases the microphone and HTTP listener. A second launch reopens settings. GNOME needs the AppIndicator/KStatusNotifierItem extension
for the tray icon; the launcher remains available without it.

Stop any standalone `voice-type` daemon before opening the desktop app; a port
conflict fails startup rather than connecting to another process. The standalone
CLI remains separately installable. Port changes require saving, quitting and
reopening Voice Type.

## Data and build boundaries

The existing `~/.voice-type-config.jsonc` remains the config file. The first
save keeps an exact private `.before-desktop` backup and converts comments to
formatted JSON, preserving unknown fields. An unreadable configuration can be
reset from the app after saving a private recovery backup beside the original. Separate provider keys are stored
with mode 0600. `OPENAI_API_KEY` / `OPENROUTER_API_KEY` override saved keys.

Vocabulary selection and desktop preferences live under `$XDG_STATE_HOME/voice-type`
(default `~/.local/state/voice-type`). Logs stay at `~/.voice-type-logs.jsonl`,
with one rotated segment. No transcript history is stored.

Settings, logs and microphone tests use direct Go calls inside Wails. No desktop
management socket or daemon subprocess is created. The existing shortcut controls
remain on `127.0.0.1:3232` (or your configured port): for example,
`curl -X POST http://127.0.0.1:3232/toggle`. `/exit` quits the entire app.
Management routes and credentials are never exposed over TCP; browser requests
are rejected. Credentials are write-only from the frontend's perspective.

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
opts into a live provider request using configured keys; providers without keys
are skipped. Only use a synthetic fixture for that test. The test does not print
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
