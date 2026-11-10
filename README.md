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
IBus, and a graphical session with D-Bus. Cloud profiles also need an unlocked
Secret Service-compatible keyring. On GNOME, the
AppIndicator/KStatusNotifierItem extension makes the tray visible.

Open **Voice Type** from the launcher. Add an OpenAI, OpenRouter, or on-device
profile, choose your microphone, and set your shortcut. Cloud profiles need an
API key saved in **API Keys** first. On-device profiles use one of the two
verified Parakeet models managed in **Local Models**; weights are downloaded on
demand and are not included in the application installer. A local failure never
uploads audio or switches to a cloud provider.

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

- Profiles, microphone and vocabularies: `~/.voice-type-config.jsonc`.
- OpenAI and OpenRouter API keys: the operating system credential store.
- Preferences and selected vocabulary: `$XDG_STATE_HOME/voice-type`
  (default `~/.local/state/voice-type`).
- Private recovery backups: `$XDG_DATA_HOME/voice-type/backups`
  (default `~/.local/share/voice-type/backups`).
- Diagnostics: `~/.voice-type-logs.jsonl`, with one rotated segment; also in **Logs**.
- Local models: `$XDG_DATA_HOME/voice-type/models` (default
  `~/.local/share/voice-type/models`), with private partial downloads and
  verified GGUF files.

One API key is stored per cloud provider and shared by every profile using that
provider. Keys are managed in **API Keys**, masked in routine reads, and copied
only by an explicit action. Existing profile keys move into the credential store
when the app first reads their settings. Environment API keys are not imported.
If profiles for the same provider contain different keys, make them consistent
in the configuration before retrying. If an existing Voice Type keyring entry
conflicts, remove that provider's entry with the system password manager, then
retry; the original configuration is kept. Preserved damaged files and other
older copies can still contain plaintext keys.
Changing profiles applies to the next recording; in-flight work retains its
original credentials. Unavailable saved microphones fall back to System default
after successful device enumeration.

OpenAI profiles use **GPT Transcribe** with automatic language recognition.
Optional language hints (for example `en, es, de`) narrow expected languages;
saved vocabulary provides keyword hints in both modes. **Upload while recording**
sends audio through an authenticated WebSocket as you speak, then starts
transcription only when you stop. It inserts finalized text once. **Record then
upload** sends the completed recording over HTTP. Existing profiles keep that
mode; newly created OpenAI profiles default to upload while recording. OpenRouter
continues to use complete-recording transcription.

In **Upload while recording**, the microphone starts immediately while the
WebSocket connects and configures in parallel. Green means capture is active;
you can speak without waiting for OpenAI. Audio stays in one recording buffer,
and upload begins at its first sample after acknowledgement, then catches up.
Stop ends capture immediately, including while connecting; Voice Type finishes
setup, sends the full recording, and commits once. A Stop received while the
microphone is still opening is remembered and follows this same completion
path; it does not cancel the pending connection. Amber means it is finishing.

Setup must finish within ten seconds. The initial connection backlog may occupy
that interval in the retained recording (about 320 KB of 16 kHz mono PCM16).
Newly pending audio after acknowledgement has a five-second limit; startup
audio is excluded from that limit. The recording cap remains ten minutes.
Finalization has a 120-second deadline from Stop, including any remaining setup
and upload. Queue exhaustion, network failures and provider errors stop capture,
clean up, and return to idle with a failure indication. There is no automatic
cloud reconnect or fallback. Credentials, mode, hints and vocabulary are
snapshotted at recording start; changes apply to the next recording.
The protocol follows the [OpenAI Realtime transcription guide](https://developers.openai.com/api/docs/guides/realtime-transcription);
[`deferred.md`](deferred.md) records the optional future live-transcription model.

Optional `timing` fields in completion logs record microphone startup separately
from connection setup and startup-backlog drain, followed by Stop,
capture completion, final audio upload, commit, transcript receipt, IBus
submission and cleanup. `stop_to_transcript_ms` and `stop_to_submission_ms`
measure the backend handoff, **not confirmed visible text** in the focused app.
Live local submissions that precede Stop have timestamps but no negative
Stop-to-submission duration. Routine diagnostics contain neither transcripts
nor credentials. An empty OpenAI result reports captured duration and aggregate
RMS/peak level, so missing audio can be distinguished from an empty provider
response without logging speech. Local insertion failures retain private WAV/transcript files
for manual recovery; they are never replayed automatically.

The on-device catalog offers **Parakeet Unified English** (English, buffered
streaming in the pinned engine) and **Parakeet TDT v3** (25 European languages,
batch only). Downloads are
pinned to immutable Hugging Face revisions and verified by exact size and
SHA-256 before atomic installation. Pause preserves a partial download; resume
uses a range request only when the server confirms the saved strong ETag and
exact byte range. Failed transfers can be retried or cancelled. Removing an
active model is blocked until the dictation finishes.

The pinned Unified GGUF model card has conflicting license statements: its
metadata says CC BY 4.0, while the included upstream NVIDIA model card says
NVIDIA Open Model License. Voice Type shows this conflict and links to both
the NVIDIA terms and the pinned GGUF card; review the terms before use. The
TDT v3 artifact is listed as CC BY 4.0.

The Unified GGUF card also has conflicting streaming notes: its metadata says
streaming is supported, but its prose describes an older offline-only port.
Voice Type follows the buffered streaming API documented by the pinned
`transcribe.cpp` revision and checks the loaded model's reported capability
before starting a stream.

Unified English profiles offer **Process while recording** (default), **Live
insertion**, or **No streaming**. Live insertion sends only the engine's
append-only committed text. Local processing has a bounded five-second audio
queue for transient CPU stalls; overflow fails explicitly and keeps the complete
recording. Voice Type never edits or deletes inserted text; if
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

To verify the real OS credential backend without touching production secrets,
run the isolated keyring round-trip check inside a temporary D-Bus session:

```sh
VOICE_TYPE_TEST_SYSTEM_KEYRING=1 \
PKG_CONFIG_PATH="$PWD/build/transcribe/pkgconfig" HOME="$(mktemp -d)" \
  XDG_DATA_HOME="$(mktemp -d)" XDG_RUNTIME_DIR="$(mktemp -d)" \
  dbus-run-session -- sh -c 'printf q | gnome-keyring-daemon --unlock; go test -run "^TestSystemKeyringRoundTrip$" .'
```

Use a private `HOME`, `XDG_DATA_HOME`, and `XDG_RUNTIME_DIR`; the test writes a
unique service name, verifies save/read/replace/delete, and removes its entry.

Pushes to `main` changing `VERSION` publish native Linux x64 and ARM64 app
archives. See the release workflow for dependencies and artifact verification.

## Contributing

Open an issue or send a PR. Keep the app simple; no alternative CLI runtime or
transport layer. Please, no AI slop.

Real model verification (downloads pinned weights into a reusable test cache):

```sh
PKG_CONFIG_PATH="$PWD/build/transcribe/pkgconfig" \
  VOICE_TYPE_TEST_LOCAL_INFERENCE=1 \
  VOICE_TYPE_TEST_LOCAL_AUDIO=/absolute/path/to/16khz-mono-pcm16.wav \
  VOICE_TYPE_TEST_LOCAL_MODEL_DIR="$PWD/build/acceptance/models" \
  go test -count=1 -v -run '^TestLocalNativeModelTranscribesOffline$' -timeout 25m .
```

Set `VOICE_TYPE_TEST_LOCAL_MODEL=parakeet-tdt-v3` to check the multilingual
batch model. This check requires nonempty batch output and, for Unified,
append-only commits and exact final/committed agreement. It does not establish
installed-app microphone or target-field acceptance. See `implementation-plan.md`
and `progress.md` for the real-use acceptance work.
