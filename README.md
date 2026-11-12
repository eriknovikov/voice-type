# Voice Type

System-wide speech to text for Linux. Press Ctrl+Space, speak, press it again.
The text appears at your cursor through IBus.

Voice Type is one Go/Wails application. Closing settings leaves it running in
the tray; Quit stops the entire app. There is no separate daemon, local HTTP
control port, or management socket.

## Install

Voice Type supports Ubuntu 24.04+, Debian 13+, Fedora 40+ and Arch Linux
(x86_64). Release binaries are built on Ubuntu 24.04, so older distributions
lack the required glibc 2.38 or newer and are rejected by the package manager. Runtime
requirements are GTK4, WebKitGTK 6.0, PulseAudio or PipeWire's PulseAudio server,
IBus, and a graphical session with D-Bus. Cloud profiles also need an unlocked
Secret Service-compatible keyring. On GNOME, the
AppIndicator/KStatusNotifierItem extension makes the tray visible.

### Packages

Download the file for your system from the
[latest release](https://github.com/eriknovikov/voice-type/releases/latest), then
install it. The package manager resolves GTK4, WebKitGTK 6.0, IBus and the audio
stack from the distribution repositories.

| System | File | Install |
| --- | --- | --- |
| Ubuntu, Debian, Mint, Pop!_OS | `voice-type_VERSION_amd64.deb` | `sudo apt install ./FILE` |
| Fedora | `voice-type-VERSION-1.x86_64.rpm` | `sudo dnf install ./FILE` |
| Arch Linux | `voice-type-VERSION-1-x86_64.pkg.tar.zst` | `sudo pacman -U ./FILE` |

On ARM64, use the `arm64` Debian package or the `aarch64` RPM. Arch Linux
officially supports x86_64 only; on Arch Linux ARM use the archive installer
below. Packages install system-wide; the archive installer installs per-user.
Choose one method per machine.

### One command

```sh
curl -fsSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | sh
```

The installer detects `apt`, `dnf` or `pacman`, downloads the matching package,
verifies its checksum against the release, and installs it with `sudo`. Without
root, `sudo`, or a supported package manager it installs per-user from the
release archive (`~/.local/bin/voice-type`, the launcher entry, icon, and
documentation under `~/.local/share`).

Use `install.sh --user` to force a per-user installation, `--version v6.x.y` to
choose a release, and `--uninstall` to remove either installation type. Upgrades
reinstall the new release file; automatic updates require hosted repositories,
which are not available yet.

Open **Voice Type** from the launcher. Add an OpenAI, OpenRouter, or local
profile, choose your microphone, and set your shortcut. Cloud profiles need an
API key saved in **API Keys** first. Local profiles use one of the two
verified Parakeet models managed in **Local Models**; weights are downloaded on
demand and are not included in the application installer. A local failure never
uploads audio or switches to a cloud provider.

The installer verifies release checksums and refuses pre-v6 daemon archives. An
upgrade requests a normal quit and waits for engine ownership; unfinished
dictation or pending edits prevent replacement. Older daemon processes must be
stopped manually. Start at login is enabled by default on first launch. Existing
preferences are retained. Removing a package keeps configuration, vocabulary,
backups, logs and retained audio.

## FAQ

Short answers for a new user; the app's **FAQ** tab has the same answers.
[Settings and diagnostics](#settings-and-diagnostics) has storage and log paths.

### How do I dictate?
Add a profile, then press Ctrl+Space to start and stop; the text appears at your
cursor through IBus. Closing the window leaves Voice Type in the tray, and
**Quit** stops the app.

### Which provider should I choose?
OpenAI uses GPT Transcribe in the cloud with upload while recording. OpenRouter
transcribes the complete recording in the cloud. Local needs no key, never
uploads, and has no cloud fallback.

### Where do I get an API key?
Create one at
[platform.openai.com/api-keys](https://platform.openai.com/api-keys)
(needs a funded account), or at [openrouter.ai/keys](https://openrouter.ai/keys)
(needs credits). One key per provider, shared by its profiles, is masked in the
UI and stored in the system keyring. **API Keys → Storage** can switch to a
local base64-encoded file under `$XDG_DATA_HOME/voice-type`, and a keyring write
failure falls back there automatically.

### Which local model should I use?
**Parakeet Unified English**: English only, about 731 MB download, about 1.1 GiB
RAM, streaming. **Parakeet TDT v3**: 25 European languages, about 740 MB, about
1.2 GiB RAM, batch only. Downloads are pinned to immutable revisions and
verified by exact size and SHA-256. Unified has conflicting license statements:
CC BY 4.0 in its GGUF metadata, NVIDIA Open Model License in the upstream card.
TDT v3 is CC BY 4.0.

### Can I keep memory lower?
The per-profile model memory option keeps the model loaded for instant reuse or
unloads it after dictation to free the RAM, making the next one slightly slower.

### What about other languages?
OpenAI recognizes languages automatically and accepts optional hints such as
`en, es, de`. OpenRouter transcribes the complete recording. Locally, TDT v3
covers 25 European languages and Unified English is English only.

### What are the dictation modes?
OpenAI profiles can upload while recording, the default for newly created
profiles, and insert finalized text once at stop, or record then upload. Local
Unified profiles process while recording, use live insertion, or turn streaming
off. Live insertion appends committed text only and never edits.

### Does Voice Type upload my audio?
Only cloud profiles send audio to the selected provider; local profiles never
upload and never fall back to cloud. Logs contain neither transcripts nor keys.

### Why is the tray icon missing?
GNOME needs the AppIndicator/KStatusNotifierItem extension, and cloud profiles
need an unlocked Secret Service keyring.

### Why did nothing get inserted?
The focused app must accept IBus input; check Logs for the reason. A single
recording is capped at ten minutes.

### How do I share diagnostics?
Open **General → Diagnostics**, run the checks, then use **Copy report**. It
includes versions, paths, device names and the last failure, but no keys,
transcripts or audio. The same report is available with `voice-type doctor`.

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

Distribution packages are built with [nfpm](https://nfpm.goreleaser.com/):

```sh
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.46.3
make packages
./scripts/verify-packages.sh
```

`make packages` writes `.deb`, `.rpm` and, on x86_64, Arch Linux packages to
`build/packages`. `verify-packages.sh` installs them in clean Ubuntu, Debian and
Fedora containers (plus Arch on x86_64) and checks package contents, declared
dependencies, and application startup. It needs Docker or Podman. The declared
glibc floor is read from the built binary, so a package built on a host newer
than a target distribution is correctly rejected there; release packages are
built on Ubuntu 24.04. `PACKAGE_VERIFY_SKIP_RUN=1` skips only the startup check
and still verifies contents and dependency installation.

The root Go module contains the application and dictation engine; `frontend/`
contains the React UI. `cmd/voice-type` is the only executable entry point.
CGO is required. The build downloads the SHA-256-pinned
`transcribe.cpp` source at revision
`2778ae4bf5ca5f394d129a39467f4ad9552dc34f` and statically links its CPU
engine. Production builds embed the frontend. The root `VERSION` is the sole
application version source.

For UI and backend development, run:

```sh
make dev
```

Wails starts Vite on port 9245, rebuilds the native backend when Go, C/C++, or
shell sources change, and restarts `build/voice-type-dev`. The development app
uses a separate identity and engine lock, and does not register desktop
shortcuts or Start at login. It still uses your normal profiles, settings,
credentials, and models, so avoid editing shared state while also using the
installed app. The watcher configuration is `build/config.yml`; `wails3 dev`
can also be run directly.

## Application commands

| Command | Behavior |
| --- | --- |
| `voice-type` | Open or focus settings |
| `voice-type --background` | Start hidden when setup is complete; leave an existing window unchanged |
| `voice-type --toggle` | Toggle recording in the existing app, or start the app first |
| `voice-type --quit` | Quit through the same save and recording safeguards as the tray |
| `voice-type --version` | Print the installed version |
| `voice-type doctor` | Print audio, insertion, model and storage diagnostics |
| `voice-type update` | Install the latest release, or report that it is up to date |

Commands reach the existing Wails instance over session D-Bus. Forwarding is
one-way: successful delivery does not mean transcription has finished. Commands
are rejected during transcription rather than queued. Missing setup or recovered
settings open settings instead of starting an automatic recording.

`voice-type update` checks the latest stable release and runs the same installer
used for a fresh install: it prefers the distribution package and falls back to
the per-user archive when no package manager or `sudo` is available.
Development builds refuse to update.

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
The protocol follows the [OpenAI Realtime transcription guide](https://developers.openai.com/api/docs/guides/realtime-transcription).
An optional future live-transcription model is not implemented.

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

The local catalog offers **Parakeet Unified English** (English, buffered
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
archives, `.deb` and `.rpm` packages for both architectures, and an x86_64 Arch
Linux package. See the release workflow for dependencies and artifact
verification.

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
installed-app microphone or target-field acceptance. Real-use acceptance is
tracked separately.
