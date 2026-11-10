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

Open **Voice Type** from the launcher. Add an OpenAI, OpenRouter, or on-device
profile, choose your microphone, and set your shortcut. Cloud profiles need an
API key. On-device profiles use one of the two verified Parakeet models managed
in **Local Models**; weights are downloaded on demand and are not included in
the application installer. A local failure never uploads audio or switches to
a cloud provider.

The installer defaults to `~/.local/bin/voice-type`. It verifies release checksums
and refuses pre-v6 daemon archives. An upgrade requests a normal quit and waits
for engine ownership; unfinished dictation or pending edits prevent replacement.
Older daemon processes must be stopped manually. Start at login is enabled by
default on first launch. Existing preferences are retained.

## Build from source

Install Go 1.26+, Node 22.12+ or Node 24, C and C++ compilers, CMake 3.16+,
Python 3, `curl`, `tar`, `sha256sum`, `pkg-config`, GTK4 and WebKitGTK 6.0
development packages. On Ubuntu 24.04 these include `cmake`, `libgtk-4-dev`
and `libwebkitgtk-6.0-dev`. Vulkan acceleration is built when Vulkan development
files and `glslc` are available; set `VOICE_TYPE_VULKAN=1` to require it or
`VOICE_TYPE_VULKAN=0` to force CPU-only builds. CPU inference is always built.

```sh
make build
make check
./install.sh --local
```

The root Go module contains the application and dictation engine; `frontend/`
contains the React UI. `cmd/voice-type` is the only executable entry point.
CGO is required. The build downloads the SHA-256-pinned
`transcribe.cpp` source at revision
`2778ae4bf5ca5f394d129a39467f4ad9552dc34f` and statically links its CPU
engine. The build embeds the frontend, with no development server. The root
`VERSION` is the sole application version source.

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
- Local models: `$XDG_DATA_HOME/voice-type/models` (default
  `~/.local/share/voice-type/models`), with private partial downloads and
  verified GGUF files.

API keys are masked in routine reads. Reveal and Copy are explicit profile
editor actions. Keys stay in private files; environment API keys are not imported.
Changing profiles applies to the next recording; in-flight work retains its
original credentials. Unavailable saved microphones fall back to System default
after successful device enumeration.

The on-device catalog offers **Parakeet Unified English** (English, streaming)
and **Parakeet TDT v3** (25 European languages, batch only). Downloads are
pinned to immutable Hugging Face revisions and verified by exact size and
SHA-256 before atomic installation. Pause preserves a partial download; resume
uses a range request only when the server confirms the saved strong ETag and
exact byte range. Failed transfers can be retried or cancelled. Removing an
active model is blocked until the dictation finishes.

Unified English profiles offer **Process while recording** (default), **Live
insertion**, or **No streaming**. Live insertion sends only the engine's
append-only committed text. Voice Type never edits or deletes inserted text; if
the final transcript disagrees, it retains the recording and reports the issue.
Text is sent to whichever application is focused at insertion time. Both
catalog models support saved vocabulary as decoder bias; the UI states model
capabilities rather than silently dropping unsupported settings.

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
PKG_CONFIG_PATH="$PWD/build/transcribe/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}" \
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
