# AGENTS.md — voice-type v5

Guidance for AI agents working in this repository. The Go tree at the root **is**
voice-type v5. The deprecated v4 TypeScript version lives in `v4/` and is a
**separate project** with its own `AGENTS.md`; its rules (Prettier, Bun, dotool,
Chrome) do **not** apply here, and it is no longer developed or released — leave
it alone unless a task names it.

## What this is

A single static Go binary replacing v4's headless-Chrome pipeline. It records the
mic directly, uploads WAV to OpenRouter (NVIDIA Parakeet TDT 0.6B v3), and pastes
the transcript into the focused window. Design rationale and non-goals live in
`PLAN_V5.md` (untracked, local only); user-facing install/config/API lives in
`README.md`. Do not duplicate either here.

The installed command is `voice-type`, at `/usr/local/bin/voice-type` — v5
deliberately takes over v4's name so existing hotkeys keep working.

## End-to-end flow

```
F9 → GET /toggle → Recorder.Start()      PulseAudio, 16kHz mono PCM16, goroutine appends []int16
                 → GET /toggle → Stop()  returns []int16
                 → trimSilence()         cut leading/trailing silence (config: trim_silence)
                 → wavEncode()           44-byte RIFF header + samples
                 → sttTranscribe()       POST multipart to OpenRouter, retry 4x
                 → Typer.Paste()         save clipboard → wl-copy --sensitive → Ctrl+V → restore
```

`/stop` runs upload and paste on the request goroutine, so it returns only once
text has landed. Capture runs on the pulse library's goroutine.

## Layout — flat, single `package main`, no `internal/`

Every `.go` file sits at the repo root. Non-Go files that matter: `VERSION`
(single source of truth for the version, read by the Makefile and release CI),
`install.sh` / `uninstall.sh` (v5), `Makefile`, and `v4/` (deprecated, untouched).

| File | Responsibility |
| --- | --- |
| `main.go` | entry, flag dispatch, `logf`, `retainWAV` |
| `config.go` | JSONC strip + validate; `configLoad`, `configAPIKey` |
| `daemon.go` | HTTP routes, session state machine |
| `audio.go` | `Recorder` — PulseAudio capture |
| `wav.go` | `wavEncode` — PCM16 → WAV |
| `trim.go` | `trimSilence` — endpoint silence removal |
| `stt.go` | `sttClient` — OpenRouter, retry, error classification |
| `output.go` | `Typer` — key chord parsing, clipboard, paste |
| `preflight.go` | ordered startup checks, returns `*resources` |

Identifiers are shared package-wide, so prefix where a bare name would collide
(`configLoad`, `sttBuildJSON`). Tests are `*_test.go` in the same package.

## Libraries

| Dependency | Use |
| --- | --- |
| `github.com/jfreymuth/pulse` v0.1.2 | PulseAudio wire protocol, pure Go |
| `github.com/bendahl/uinput` v1.7.0 | virtual keyboard via `/dev/uinput`, pure Go |

Everything else is stdlib (`net/http`, `mime/multipart`, `encoding/json`,
`encoding/binary`). **No cgo** — `CGO_ENABLED=0` everywhere. External binaries:
`wl-copy`/`wl-paste` (Wayland) or `xclip` (X11), invoked via `os/exec`.

## Design invariants (do not fight these)

1. **Client and keyboard are created once**, at daemon start, and reused —
   `pulse.Client` (~3 ms) and the uinput device (~201 ms). Only the record stream
   is per-session. `preflight.go` creates both and hands them over in `resources`.
2. **v5 never writes the config file.** No `/togglePunctuation`, no persistence,
   so it cannot clobber a v4 config. Unknown fields are ignored silently — a v4
   config must always load.
3. **Both JSONC strippers, in order**: `stripTrailingCommas(stripJsoncComments(x))`.
   Stripping a trailing `// comment` leaves the preceding comma dangling. Ported
   verbatim from `src/config.ts`; keep them in sync.
4. **Multipart is the upload path** (`file` + `model`), validated against the live
   endpoint. JSON base64 (`useJSON`) also works and stays as a fallback; it
   inflates the payload 33%.
5. **Never use `wl-copy --paste-once`** — it breaks pasting into XWayland windows
   (Electron, JetBrains, Steam).
6. **`wl-copy` forks and must stay alive** to serve the selection. Wait on the
   parent only; never kill the survivor. `cmd.Cancel` sends SIGTERM to the parent.
7. **Every clipboard subprocess is bounded** by `clipboardTimeout` (3 s). The
   Wayland clipboard is client-owned, so `wl-paste` blocks forever if the owner is
   wedged — observed in testing; unbounded it freezes `/stop`.
8. **Uploads retry** up to `sttMaxAttempts` (4) with backoff. Together returns
   intermittent 503s under bursty load. 401/403/413 are never retried. Audio that
   still fails goes to `$TMPDIR/voice-type/` — never silently discarded.
9. **Two mutexes in `Recorder`**: `streamMu` for lifecycle, `bufMu` for the sample
   buffer the pulse goroutine writes into. One shared lock would make `Start()`
   block its own write callback.
10. **`input` group is a hard requirement.** No fallback input path. Preflight
    fails with instructions and exits 1.
11. **Port in use → exit 0**, assuming a daemon is already running. This is what
    makes the F10 `curl /exit || voice-type` toggle work.
12. **Never log clipboard contents** — it is the user's data and may hold secrets.
13. **Server binds `127.0.0.1` only**, no auth. Do not expose on `0.0.0.0`.
14. **`trimSilence` only ever cuts the two ends**, never the interior, and pads
    250 ms back on. Interior pauses are prosody — Parakeet reads terminal
    punctuation off the trailing one. Each end estimates its own noise floor;
    a whole-clip estimate is pinned to zero by a single digitally silent frame
    (noise-gating mic, headset DTX) and then trims nothing. When no frame at an
    end clears the gate, that end is left alone — over-keeping costs a fraction
    of a cent, over-cutting costs the user their words.

## Out of scope (deliberate — do not add without discussion)

D-Bus/sound notifications, file logging, self-updater, VAD/silence auto-stop,
client-side transcript transforms (spoken punctuation, capitalization — these come
from prosody, which is why the *interior* of a capture is never touched; see
invariant 14 for the endpoint trimming that is in scope), language selection,
streaming/interim results, audio compression.

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

Generate test audio: `espeak-ng -w s.wav "..." && ffmpeg -i s.wav -ar 16000 -ac 1 -f s16le out.pcm`.
To exercise capture without speaking, set the default source to a sink monitor
(`pactl set-default-source <sink>.monitor`) and `paplay` into it — restore the
original source afterwards.

## Releasing

`VERSION` at the repo root is the only place the version number lives. The
Makefile reads it into `-X main.version`, and `.github/workflows/release.yml`
reads it to decide the tag: a push to `main` whose `VERSION` has no matching
`vX.Y.Z` tag builds both architectures, tags, and publishes. An unstamped
`go build` reports `dev` on purpose — it must never claim a release number.

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
