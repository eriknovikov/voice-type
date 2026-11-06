# Voice Type

System-wide speech to text for Linux. Press Ctrl+Space, speak, press it again.
The text appears at your cursor through IBus.

Voice Type is one Go/Wails application. Closing settings leaves it running in
the tray; Quit stops the entire app. There is no separate daemon, local HTTP
control port, or management socket.

## Install

Download a Linux x64 or ARM64 application release:

```sh
curl -fsSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | sh
```

The installer requires `curl`, `tar`, `sha256sum`, `flock` and `timeout`. Runtime
requirements are GTK4, WebKitGTK 6.0, PulseAudio or PipeWire's PulseAudio server,
IBus, and a graphical session with D-Bus. On GNOME, the
AppIndicator/KStatusNotifierItem extension makes the tray visible.

Open **Voice Type** from the launcher. Add an OpenAI or OpenRouter profile,
choose your microphone, and set your shortcut. Internet access and an API key
are required. Local models are not included.

The installer defaults to `~/.local/bin/voice-type`. It verifies release checksums
and refuses pre-v6 daemon archives. An upgrade requests a normal quit and waits
for engine ownership; unfinished dictation or pending edits prevent replacement.
Older daemon processes must be stopped manually. Start at login is enabled by
default on first launch. Existing preferences are retained.

## Build from source

Install Go 1.26+, Node 22.12+ or Node 24, a C compiler, `pkg-config`, GTK4 and
WebKitGTK 6.0 development packages. On Ubuntu 24.04 these include
`libgtk-4-dev` and `libwebkitgtk-6.0-dev`.

```sh
make build
make check
./install.sh --local
```

The root Go module contains the application and dictation engine; `frontend/`
contains the React UI. `cmd/voice-type` is the only executable entry point.
CGO is required. The build embeds the frontend, with no development server.
The root `VERSION` is the sole application version source.

## Application commands

| Command | Behavior |
| --- | --- |
| `voice-type` | Open or focus settings |
| `voice-type --background` | Start hidden when setup is complete; leave an existing window unchanged |
| `voice-type --toggle` | Toggle recording in the existing app, or start the app first |
| `voice-type --quit` | Quit through the same save and recording safeguards as the tray |
| `voice-type --version` | Print the installed version |

Commands reach the existing Wails instance over session D-Bus. Forwarding is
one-way: successful delivery does not mean transcription has finished. Commands
are rejected during transcription rather than queued. Missing setup or recovered
settings open settings instead of starting an automatic recording.

GNOME uses Voice Type's own custom shortcut entry and preserves all other
shortcuts. Escape disables a shortcut during capture. Other Wayland desktops
require configuring a global shortcut to `voice-type --toggle` in desktop
settings. X11 uses direct Wails shortcut registration.

## Settings and diagnostics

- Profiles, credentials, microphone and vocabularies: `~/.voice-type-config.jsonc`.
- Preferences and selected vocabulary: `$XDG_STATE_HOME/voice-type`
  (default `~/.local/state/voice-type`).
- Private recovery backups: `$XDG_DATA_HOME/voice-type/backups`
  (default `~/.local/share/voice-type/backups`).
- Diagnostics: `~/.voice-type-logs.jsonl`, with one rotated segment; also in **Logs**.

API keys are masked in routine reads. Reveal and Copy are explicit profile
editor actions. Keys stay in private files; environment API keys are not imported.
Changing profiles applies to the next recording; in-flight work retains its
original credentials. Unavailable saved microphones fall back to System default
after successful device enumeration.

Missing or corrupt settings recover from a last-valid backup, with a visible
notice. Damaged originals are preserved. Unreadable files remain untouched and
show an error. Unknown configuration fields, including obsolete `port` values,
are preserved but have no runtime effect.

The engine holds a private OS file lock under `$XDG_RUNTIME_DIR/voice-type`
(or the state directory when unset). Shutdown or a crash releases ownership;
never delete the lock file to bypass another instance. Startup failures show a
native dialog and stderr diagnostics.

To uninstall from a checkout, run `./uninstall.sh`. The installer also accepts
`--uninstall`. Only application files and owned desktop integration are removed;
configuration, vocabulary, backups, logs and retained audio are kept.

## Integration checks

```sh
VOICE_TYPE_TEST_IBUS_DAEMON=/usr/bin/ibus-daemon go test -race ./...
make native-check
```

The first command verifies mocked provider requests through real isolated IBus.
Native checks require a graphical session and use private HOME/XDG directories,
a private D-Bus and keyfile GSettings. They exercise forwarding, tray actions,
recovery and shutdown without changing your desktop settings or using paid APIs.
Set `VOICE_TYPE_TEST_REAL_AUDIO=1` to opt into a short local microphone test.

Pushes to `main` changing `VERSION` publish native Linux x64 and ARM64 app
archives. See the release workflow for dependencies and artifact verification.

## Contributing

Open an issue or send a PR. Keep the app simple; no alternative CLI runtime or
transport layer. Please, no AI slop.
