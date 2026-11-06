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
app and releases the microphone and engine lock. A second launch reopens settings.
GNOME needs the AppIndicator/KStatusNotifierItem extension for the tray icon;
the launcher remains available without it.

The desktop app opens no TCP control port or management socket. Commands reach
its existing Wails instance (session D-Bus on Linux):

| Command | App running | App stopped |
| --- | --- | --- |
| `voice-type-app` or `--show-settings` | Open settings | Start and open settings |
| `voice-type-app --background` | Leave the window unchanged | Start hidden if setup is complete |
| `voice-type-app --toggle` | Toggle recording without opening settings | Start hidden, then toggle if setup is complete |
| `voice-type-app --quit` | Quit after pending settings and dictation checks | Exit without starting the engine |

The previous `--shortcut-action transcription` command remains an alias for
`--toggle`. Commands accept no credentials or arbitrary shell arguments; invalid
or conflicting commands exit with code 2. A successful secondary command means
it was forwarded, not that transcription finished. Recording errors appear in
settings, tray status and logs. During transcription, additional recording
commands are rejected instead of queued. Startup commands wait for initialization;
missing setup or recovered settings open settings without starting a recording.
Closing settings keeps the app running; Quit uses the same save/profile safeguards
as the tray. `--remove-shortcuts` is a separate uninstall operation.

Stop any standalone `voice-type` daemon before opening the desktop app. Both
entry points hold the same nonblocking OS file lock under
`$XDG_RUNTIME_DIR/voice-type/engine.lock` (or the private state directory when
that variable is unset). The directory is private (0700) and the file is private
(0600). Only one engine may run; a conflicting launch reports an error and never
adopts or stops the other engine. Shutdown or a crash releases the lock. The
file remains in place; do not delete it to bypass a running process.
Older standalone binaries do not honor this lock: stop them before upgrading.
An unrelated application using port 3232 does not affect the desktop app.
Startup failures show a native error dialog, including background launches,
and report the error on stderr. Lock conflicts do not open another engine's logs.

## Troubleshooting

Use settings status and **Logs** to investigate desktop recording errors.
Use `voice-type-app --show-settings` to reopen a hidden window and
`voice-type-app --quit` for a normal shutdown. Port checks and
`curl http://127.0.0.1:3232/health` apply only to the standalone CLI.

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

Settings, logs, microphone tests and recording use direct Go calls inside Wails.
No daemon subprocess is created. The standalone CLI remains separately
installable and retains its loopback HTTP controls, including `/health`,
`/start`, `/stop`, `/toggle` and `/exit`. For example,
`curl -X POST http://127.0.0.1:3232/toggle` controls the standalone daemon only.
Its stored port is preserved by desktop settings edits but has no desktop UI.
Outbound provider HTTPS requests are unchanged.
Routine settings responses contain only masked keys. Explicit Reveal and Copy
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
the actual native app with an isolated home and session bus. It verifies engine
ownership without TCP, startup with an occupied CLI port, command forwarding,
cold Quit, incomplete setup, guarded shutdown and native startup errors. It shuts
its test processes down.

From `v5/`, `VOICE_TYPE_TEST_IBUS_DAEMON=/usr/bin/ibus-daemon go test ./...`
checks both provider request formats through real IBus insertion on a private
bus. `VOICE_TYPE_TEST_LIVE_WAV=/path/to/synthetic.wav go test -run TestLiveProviders -v`
opts into a live provider request using saved profiles; profiles without keys are never accepted. Only use a synthetic fixture for that test. The test does not print
credentials or transcripts.

To exercise native tray registration and menu actions without installing a shell
extension, run `VOICE_TYPE_DESKTOP_TEST_BINARY="$PWD/build/voice-type-app"
CGO_ENABLED=1 go test -run TestNativeTray -v` from this directory (as one command).
The test uses private HOME/XDG directories, a private session bus and keyfile
GSettings backend, an occupied CLI port and a test tray host. It exercises both
shortcut command forms and native menu callbacks while preserving a seeded
Gradia Print binding through startup and uninstall. No paid provider calls run.

Keyboard Shortcuts defaults to Ctrl+Space to toggle transcription.
The old daemon toggle and its app-owned GNOME binding are removed automatically.
Existing custom transcription shortcuts are retained;
the previous default moves to Ctrl+Space when loading older preferences.
Click a shortcut, press the keys, then click elsewhere to save. Escape disables it.

On GNOME the app manages only its own transcription shortcut entry. Other
shortcuts are preserved. Capture is inline; no GNOME Settings window or portal
dialog is opened. Other Wayland desktops currently require configuring global
shortcuts in the desktop itself. X11 uses Wails' direct shortcut registration.

GNOME registration uses GSettings' typed API. Normal launches and key capture
leave the shared shortcut list untouched once Voice Type is registered. Adding
or removing Voice Type preserves every other entry; shortcut conflicts fail
without changing another action. The native shortcut/tray checks use a private
keyfile backend and private D-Bus service environment, never the user's dconf.
