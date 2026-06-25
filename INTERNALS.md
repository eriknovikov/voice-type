# Internals

```
hotkey → curl → HTTP server → Chrome (Web Speech API) → text → dotool → focused window
```

## Config

`~/.config/voice-type.jsonc` is the single source of truth. On first run: probe for Chrome/Chromium, read `$LANG` for default language, write default config. No CLI flags — only `help` and `update` subcommands.

### Startup preflight

1. Port not in use
2. Browser path exists and is executable
3. dotool spawns and uinput works

Failure → notification + exit 1. Port in use → exit 0 (already running).

### Logging

TTY → stdout. Background → `~/.local/share/voice-type/logs/voice-type.log` (1 MB rotation, 5 files). Format: `ISO8601 [TAG] message`.

## Web Speech API

Chrome exposes `SpeechRecognition` natively — no extension, no external service. Voice Type keeps a headless Chrome open for the daemon lifetime with a local page running `SpeechRecognition`. Audio streams to Google's servers, transcripts return in real time.

Returns interim (may change) and final (committed) results. Voice Type types interim results live and retypes corrections via prefix-diff.

## Daemon

HTTP server on `127.0.0.1:3232` + headless Chrome via CDP. Chrome stays up for the daemon lifetime — no per-dictation startup lag.

| Endpoint | Effect |
| --- | --- |
| `/health` | `{"status":"ok"}` |
| `/toggle` | Start/stop listening |
| `/start` | Start listening |
| `/stop` | Stop listening |
| `/exit` | Shut down daemon |
| `/togglePunctuation` | Toggle spoken punctuation (`?enabled=true\|false`) |

`/start` and `/toggle` accept `?lang=<bcp47>`.

## Speech pipeline

WSA events → `SpeechPipeline`:

1. **Transform** — per-language session. English applies spoken punctuation, capitalization, `new line`, `control enter`. Others pass through.
2. **Type** — `TypingController` prefix-diffs into dotool keystrokes.
3. **Finalize** — on segment end, transformer may emit key chords; controller resets baseline.

Late events after stop are dropped at the daemon boundary. The WSA→event mapper lives in `browser.js` (nested in `page.evaluate`) with a testable copy in `browserRecognition.js`.

## dotool

Types via `/dev/uinput` — works in any app without clipboard or app-specific integration.

Sound notifications: 2-tier resolver (canberra-gtk-play → paplay → silent). Searches freedesktop sound theme names in XDG data dirs.
