# AGENTS.md — voice-type v5

Guidance for AI agents working in this repository. The Go tree at the root **is**
voice-type v5. The deprecated v4 TypeScript version lives in `v4/` and is a
**separate project** with its own `AGENTS.md`; its rules (Prettier, Bun, dotool,
Chrome) do **not** apply here, and it is no longer developed or released — leave
it alone unless a task names it.

## What this is

A single static Go binary replacing v4's headless-Chrome pipeline. It records the
mic directly, uploads WAV to OpenRouter (NVIDIA Parakeet TDT 0.6B v3), and types
the transcript directly into the focused window. User-facing install/config/API
lives in `README.md`; keep implementation detail in the code, tests, and this
file without adding a second project plan.

The installed command is `voice-type`, at `/usr/local/bin/voice-type` — v5
deliberately takes over v4's name so existing hotkeys keep working.

## End-to-end flow

```
F9 → GET /toggle → Recorder.Start()      PulseAudio, 16kHz mono PCM16, goroutine appends []int16
                 → GET /toggle → Stop()  returns []int16
                 → worthUploading()      drop a fumbled hotkey before it costs a round trip
                 → trimSilence()         cut leading/trailing silence
                 → wavEncode()           44-byte RIFF header + samples
                 → sttTranscribe()       POST multipart to OpenRouter, retry 4x
                 → Typer.Type()          compile all text → US key events / Unicode composition
```

`/stop` runs upload and typing on the request goroutine, so it returns only once
all output events have been emitted. Capture runs on the pulse library's goroutine.

## Layout — flat, single `package main`, no `internal/`

Every `.go` file sits at the repo root. Non-Go files that matter: `VERSION`
(single source of truth for the version, read by the Makefile and release CI),
`install.sh` / `uninstall.sh` (v5), `Makefile`, and `v4/` (deprecated, untouched).

| File | Responsibility |
| --- | --- |
| `main.go` | entry, flag dispatch, `logf`, `retainWAV` |
| `config.go` | JSONC strip + validate; `configLoad`, `configAPIKey`; the tuned constants |
| `daemon.go` | HTTP routes, session state machine |
| `audio.go` | `Recorder` — PulseAudio capture |
| `wav.go` | `wavEncode` — PCM16 → WAV |
| `trim.go` | `trimSilence` — endpoint silence removal; `worthUploading` — discard gate |
| `stt.go` | `sttClient` — OpenRouter, retry, error classification |
| `output.go` | `Typer` — US key mapping, Unicode composition, timing, key release |
| `preflight.go` | ordered startup checks, returns `*resources` |

Identifiers are shared package-wide, so prefix where a bare name would collide
(`configLoad`, `sttBuildJSON`). Tests are `*_test.go` in the same package.

## Libraries

| Dependency | Use |
| --- | --- |
| `github.com/jfreymuth/pulse` v0.1.2 | PulseAudio wire protocol, pure Go |
| `github.com/bendahl/uinput` v1.7.0 | virtual keyboard via `/dev/uinput`, pure Go |

Everything else is stdlib (`net/http`, `mime/multipart`, `encoding/json`,
`encoding/binary`). **No cgo** — `CGO_ENABLED=0` everywhere. No external
runtime binaries are required; direct typing uses `/dev/uinput` through the Go
library.

## Design invariants (do not fight these)

1. **Client and keyboard are created once**, at daemon start, and reused —
   `pulse.Client` (~3 ms) and the uinput device (~201 ms). Only the record stream
   is per-session. `preflight.go` creates both and hands them over in `resources`.
2. **The daemon never writes the config file.** No `/togglePunctuation`, no
   persistence, so it cannot clobber a v4 config. The installer creates or
   explicitly replaces the file; unknown fields are ignored silently — a v4
   config must always load.
3. **`api_key` and `port` are the only config fields, and the list does not
   grow.** Everything else is a tuned constant in `config.go` (`sttModel`,
   `maxDurationSeconds`) or a constant in the file that
   owns the behaviour. An option the user must reason about is a real cost, and
   the answer to "should this be configurable?" is no. Earlier v5 installs wrote
   `model`, `max_duration`, `paste_key`, `paste_delay_ms` and `trim_silence`;
   those now fall through the ignored-fields path and must keep parsing cleanly.
4. **Both JSONC strippers, in order**: `stripTrailingCommas(stripJsoncComments(x))`.
   Stripping a trailing `// comment` leaves the preceding comma dangling. Ported
   verbatim from `src/config.ts`; keep them in sync.
5. **Multipart is the upload path** (`file` + `model`), validated against the live
   endpoint. JSON base64 (`useJSON`) also works and stays as a fallback; it
   inflates the payload 33%.
6. **Direct output is persistent and clipboard-free.** `preflight.go` creates one
   `/dev/uinput` keyboard and hands it to `Typer`; `Typer.Type` emits finalized
   text directly to the focused application and never reads or writes a
   clipboard.
7. **The output compiler targets a fixed US layout.** Printable ASCII uses the
   corresponding physical `uinput.Key*` with Shift when needed. Every other
   supported Unicode scalar uses Linux `Ctrl+Shift+U`, lowercase hexadecimal,
   and Enter; no layout or output-mode config exists.
8. **Output is compiled before emission and releases held keys on error.** Invalid
   UTF-8 and unsupported controls emit zero events. Any failed key operation makes
   a best-effort release of the primary key and all attempted modifiers; partial
   output is never retried.
9. **Uploads retry** up to `sttMaxAttempts` (4) with backoff. Together returns
   intermittent 503s under bursty load. 401/403/413 are never retried. Audio that
   still fails goes to `$TMPDIR/voice-type/` — never silently discarded.
10. **Two mutexes in `Recorder`**: `streamMu` for lifecycle, `bufMu` for the sample
    buffer the pulse goroutine writes into. One shared lock would make `Start()`
    block its own write callback.
11. **`input` group is a hard requirement.** No fallback input path. Preflight
    fails with instructions and exits 1.
12. **Port in use → exit 0**, assuming a daemon is already running. This is what
    makes the F10 `curl /exit || voice-type` toggle work.
13. **Server binds `127.0.0.1` only**, no auth. Do not expose on `0.0.0.0`.
14. **`trimSilence` only ever cuts the two ends**, never the interior, and pads
    250 ms back on. Interior pauses are prosody — Parakeet reads terminal
    punctuation off the trailing one. Each end estimates its own noise floor;
    a whole-clip estimate is pinned to zero by a single digitally silent frame
    (noise-gating mic, headset DTX) and then trims nothing. When no frame at an
    end clears the gate, that end is left alone — over-keeping costs a fraction
    of a cent, over-cutting costs the user their words.
15. **`worthUploading` only judges short captures.** Below `minCaptureSeconds`
    (0.35 s) nothing uploads; between there and `speechGateSeconds` (2 s) the clip
    must clear a whole-clip speech gate: energy contrast first, with voiced
    cadence as the fallback when speech starts before there is a quiet floor.
    Past 2 s it always uploads, unexamined. This is a discard gate, not the VAD
    auto-stop that is out of scope — session boundaries stay exactly where the
    hotkey put them. Do not raise the floor to "2–3 seconds": "yes", "no" and
    "one sec" are real dictation, and the signal gate, not the clock, is what
    catches a fumbled hotkey.

## Out of scope (deliberate — do not add without discussion)

D-Bus/sound notifications, file logging, self-updater, VAD/silence auto-stop,
client-side transcript transforms (spoken punctuation, capitalization — these come
from prosody, which is why the *interior* of a capture is never touched; see
invariant 14 for the endpoint trimming and invariant 15 for the discard gate,
both of which are in scope), language selection, streaming/interim results,
audio compression, **and any new config field** (invariant 3).

## Measured on the dev machine (Fedora aarch64, PipeWire, GNOME Wayland)

| | |
| --- | --- |
| mic cold start → first sample | 87 ms (31–37 ms warm) — under the 100 ms gate, no pre-roll ships |
| transcription round trip | 0.8–1.8 s for 7 s of audio |
| cost | $0.0015/min ($0.000176 for 7 s) |
| binary | 6.2 MB arm64 / 6.7 MB amd64, static, stripped |

## Build and test

```bash
make build    # ./voice-type
make check    # fmt + vet + test
make dist     # static linux/amd64 + linux/arm64
```

`go test ./...` is hermetic — no audio hardware, no network, no `/dev/uinput`.
Guard-clause tests cover the state machine without a mic. The live OpenRouter
round trip is opt-in:

```bash
VOICE_TYPE_LIVE=1 VOICE_TYPE_PCM=/path/to/16k-mono.pcm go test -run TestLiveTranscribe -v
```

The real keyboard smoke test is separately opt-in and types into whichever
disposable field has focus:

```bash
VOICE_TYPE_LIVE=1 go test -run TestLiveType -v
```

Generate test audio: `espeak-ng -w s.wav "..." && ffmpeg -i s.wav -ar 16000 -ac 1 -f s16le out.pcm`.
To exercise capture without speaking, set the default source to a sink monitor
(`pactl set-default-source <sink>.monitor`) and `paplay` into it — restore the
original source afterwards.

## Releasing

`VERSION` at the repo root is the only place the version number lives. The
Makefile reads it into `-X main.version`, and `.github/workflows/release.yml`
reads it to decide the tag: a push to `main` that changes `VERSION` (or repairs
the release workflow) and has no matching `vX.Y.Z` tag validates the tree,
builds both architectures, pins the tag to that exact commit, and publishes. An
unstamped `go build` reports `dev` on purpose — it must never claim a release
number.

Release assets are `voice-type-v5-linux-{x64,arm64}.tar.gz` plus
`checksums.txt`. The `-v5-` marker is load-bearing: v4 binaries already in the
wild self-update by looking for `voice-type-linux-<arch>.tar.gz` in the *latest*
release, and must fail loudly rather than silently replace themselves with a Go
binary. For the same reason `v4/install.sh` resolves the newest `v4.x.y` tag
instead of following `releases/latest`, and root `install.sh` refuses any tag
below v5.

## Code style

`gofmt` — tabs, standard Go naming. The Prettier rules in `v4/AGENTS.md` apply to
the v4 tree only. Comments explain *why* (especially the invariants above), not
what. Keep them at the density of the surrounding code.

## Gotchas

- v4 and v5 **share port 3232** and are mutually exclusive, and both install as
  `/usr/local/bin/voice-type`. Stop v4 first (`curl -s localhost:3232/exit`).
- v4's `/exit` may not actually terminate its process; check with `ss -ltnp`.
- `zsh`'s `.` searches `PATH` — source `.env` with an absolute or `./` path.
- A recording shorter than ~100 ms yields **zero samples** (cold-open latency);
  `/stop` reports "no audio captured" rather than uploading an empty WAV.
