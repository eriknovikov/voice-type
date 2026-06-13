# AGENTS.md

Guidance for AI agents and contributors working in this repository.

## Documentation map

| File | Audience | Contents |
|---|---|---|
| [README.md](README.md) | End users | Install, usage, hotkeys, troubleshooting |
| [INTERNALS.md](INTERNALS.md) | Curious users / deep dive | How Voice Type works under the hood |
| **AGENTS.md** (this file) | Agents & contributors | Architecture, conventions, APIs, agent rules |

---

## Project overview

Voice Type is a system-wide speech-to-text daemon for Linux. It runs Chrome or Chromium headlessly, uses the Web Speech API for cloud transcription, and types results into the focused window via `dotool`. No local models, no paid API keys.

**Runtime flow:**

1. Daemon binds HTTP on `127.0.0.1:3232` and launches a persistent headless browser
2. `browser.js` initializes WSA; Node exposes `onSpeechUpdate` / `onBrowserRecStop` via Puppeteer
3. Hotkey hits `/toggle` (or `/start` / `/stop`) to begin or end listening
4. Transcripts → `TypingController` diff → `dotool` keystrokes
5. Optional D-Bus / `paplay` notifications

**Data flow:**

```
Hotkey → curl :3232/toggle[?language=xx] → Daemon
              ↓
        resolveAndValidateLanguage (?language= → CLI --lang → en-US)
              ↓
        browser.setLangAndStart(lang)  (mutates rec.lang then start())
              ↓
Microphone → Web Speech API → onSpeechUpdate({ text })
              ↓
transformIfEnabled (spoken punctuation + capitalization, --punctuation only)
              ↓
TypingController (diff) → dotool → focused application
              ↓
Notifier → D-Bus / paplay (if enabled)
```

---

## Design principles (do not fight these)

1. **Persistent browser** — Chrome stays up for the daemon lifetime (~2–3s init once; near-zero hotkey latency after). Do not tear down the browser per dictation session.
2. **HTTP hotkeys** — Localhost Express endpoints, not D-Bus or custom IPC. Keeps integration with any DE shortcut manager.
3. **Interim streaming by default** — `stream: true` (default) sends in-progress transcripts; diff logic backspaces and retypes corrections. `--no-stream` waits for final WSA results only.
4. **dotool for input** — Wayland-friendly `/dev/uinput` simulation. Requires `input` group + udev rules. Layout forced to US via `DOTOOL_XKB_LAYOUT=us`.
5. **Web Speech API only** — Transcription lives in Chrome; swapping engines means replacing `browser.js` + browser launch, not a small patch.

**Key technical choices (from project history):**

- WSA over local STT models (accuracy, zero model management)
- `puppeteer-core` + system Chrome (no bundled Chromium download)
- D-Bus notification replacement (`replaces_id`, transient hints) for live status without spam
- 100ms stop cooldown to prevent rapid start/stop races

---

## Rules for agents

1. **Minimize scope** — Smallest correct change. No drive-by refactors or unrelated README/architecture edits.
2. **Preserve file roles** — `src/browser.js` must stay `.js` (injected into Chrome). Imports use `.js` extensions (`verbatimModuleSyntax`).
3. **Match existing style** — Prettier: 4-space tabs, 120 print width, no semicolons. `export default class` pattern. `import type` for type-only imports.
4. **User docs vs agent docs** — User-facing install/usage → `README.md`. Architecture and contributor detail → this file or `INTERNALS.md`. Do not duplicate long architecture sections in the README.
5. **No automated tests** — There is currently no test suite (former `src/tests/*.manual.ts` scripts were removed). Verify changes by running the daemon. Do not add Jest/Vitest unless explicitly requested.
6. **Linux-only** — Do not introduce macOS/Windows code paths. External deps: `dotool`, Chrome/Chromium, `paplay`, D-Bus session bus.
7. **Binary releases** — Production installs use `install.sh` + GitHub releases (`bun build --compile`). Keep `package.json` scripts in sync when changing build steps.
8. **Update this file** — If you change HTTP routes, CLI flags, streaming behavior, or core data flow, update AGENTS.md in the same PR.
9. **Security model** — HTTP server is localhost-only, no auth (single-user desktop). Do not expose the daemon on `0.0.0.0` without an explicit security review.

---

## Build & development

**Requires Bun.**

| Command | Purpose |
|---|---|
| `bun run dev` | Watch mode (`bun --watch src/index.ts`) |
| `bun run start` | Run daemon directly |
| `bun run build` | Bundle to `dist/` (Node target) |
| `bun build src/index.ts --compile --outfile build/voice-type` | Standalone binary (same as CI / `install.sh --local`) |

**Project layout:**

```
voice-type/
├── src/
│   ├── index.ts           # Entry: CLI parse, signal handlers, daemon start
│   ├── cli.ts             # parseArgs, help, detached respawn
│   ├── daemon.ts          # HTTP server, browser lifecycle, transcription control
│   ├── browser.js         # WSA wrapper (Chrome context) — must remain .js
│   ├── browserLauncher.ts # puppeteer-core launch + detectBrowser()
│   ├── language.ts        # Shared isValidLanguage, readLanguageQuery, DEFAULT_LANGUAGE
│   ├── transcriptTransformer.ts # Spoken punctuation + capitalization (--punctuation)
│   ├── typingController.ts
│   ├── notifier.ts, textNotifier.ts, soundNotifier.ts
│   └── utils.ts, types.ts, constants.ts
├── assets/sounds/         # start.oga, stop.oga (dev); /usr/local/share/... in prod
├── install.sh             # Release installer
└── INTERNALS.md           # User-facing deep dive
```

---

## Release process (binary-only)

Triggered by git tags matching `v*` (see `.github/workflows/release.yml`).

- Matrix: linux x64 + arm64
- `bun build --compile` → `voice-type-linux-{arch}.tar.gz` + `checksums.txt`
- `install.sh`: detect arch, download release, verify SHA256, install to `/usr/local/bin`, fetch sounds to `/usr/local/share/voice-type/sounds`
- Flags: `--version <tag>`, `--local` (build + install from source tree)

---

## HTTP API (port 3232, localhost)

| Route | Action |
|---|---|
| `GET /health` | JSON `{ status: "ok" }` |
| `GET /start` | Start listening. Accepts `?language=<bcp47>` (alias `?lang=`) to override the default for this request. |
| `GET /stop` | Stop listening (intentional). Param ignored. |
| `GET /toggle` | Toggle listen state. Accepts `?language=<bcp47>` (alias `?lang=`). When already listening, the param is dropped and the listener is stopped. |
| `GET /exit` | Shutdown daemon. Param ignored. |

**Language resolution order** (per request, on `/start` and `/toggle` only):

1. `?language=` (or `?lang=`) — trimmed; empty string is treated as absent
2. CLI `--lang` startup default
3. Hard-coded `DEFAULT_LANGUAGE` (`en-US`) from [`src/language.ts`](src/language.ts)

**Responses:** `503` browser not ready · `429` stop cooldown (100ms) · `400` invalid `?language=` value (with error notification) · `200` with short text body on success

**Validation:** `?language=` is checked against [`WSA_LANGUAGES`](src/constants.ts) in [`isValidLanguage`](src/language.ts). On failure, the route responds `400` and calls `notifier.notifyError(...)` (D-Bus + paplay if enabled) without mutating any state. `/stop` and `/exit` never validate the param.

Recommended hotkeys (see README): F9 toggles daemon (`/exit` or start `voice-type`), F10 toggles dictation (`/toggle`). Bind F11/F12 etc. to `?language=es-ES` / `?language=fr-FR` to switch languages per hotkey.

---

## CLI flags

Parsed in [`src/cli.ts`](src/cli.ts) via `node:util.parseArgs` (strict).

| Flag | Default | Notes |
|---|---|---|
| `-l, --lang` | `en-US` | Startup default language. Must be in `WSA_LANGUAGES` ([`constants.ts`](src/constants.ts)). Override per request via `?language=` on `/start` and `/toggle` (see HTTP API). |
| `--browser_type` | `chrome` | `chrome` or `chromium` → sets `BROWSER_TYPE` env |
| `-p, --browser_path` | — | Custom executable → `BROWSER_PATH` env |
| `--timeout` | `0` | Seconds of silence before auto-stop; **only when streaming** (`timeout > 0` resets on each speech update) |
| `--punctuation` | off | Spoken punctuation ("comma", "period", "question mark", "exclamation mark", "semicolon", "colon") → symbols; auto-capitalize sentence starts. Applied to the transcript **before** diffing (see `transcriptTransformer.ts`) |
| `--no-stream` | off | Final transcripts only (no interim diffs) |
| `--text` | off | D-Bus desktop notifications |
| `-s, --sound` | off | `paplay` feedback |
| `-d, --detached` | off | Respawn self detached; parent exits |
| `-h, --help` | — | Print help |

Browser paths checked by launcher: `/usr/bin/google-chrome`, `/usr/bin/chromium`.

---

## Architecture & components

### Entry: [`src/index.ts`](src/index.ts)

- `PORT = 3232`
- `parseFlags()` → `Daemon` constructor → `daemon.start(PORT)`
- SIGTERM/SIGINT → `destroy()`

### Daemon: [`src/daemon.ts`](src/daemon.ts)

- Express routes (see HTTP API); `browserHealthMiddleware` before transcribe routes
- `initBrowser()` → launch, new page, `exposeFunction` for speech callbacks, `browser.initWSA(stream, defaultLanguage)`
- `resolveAndValidateLanguage(req, res)` — reads `?language=` / `?lang=`, validates against `WSA_LANGUAGES`, returns the resolved string or `null` after sending `400` + `notifyError`. Used by `/start` and `/toggle`.
- `startTranscription(lang, res)` — passes `lang` to `browser.setLangAndStart` so the in-page `rec.lang` is mutated and recognition begins in the requested language
- `stopTranscription(reason)` — reasons: `intentional`, `silence`, `offline`
- `handleBrowserRecStop` — auto-stops if WSA ends while still listening
- `silenceTimer` — daemon-side timeout when `stream && timeout > 0`; reset on each `handleSpeechUpdate`
- `transformIfEnabled(rawText)` — with `--punctuation`, runs `transformTranscript` on the full transcript before diffing and manages sentence-start state: `capitalizeNext` is set on `startTranscription` and recomputed only at segment finalization (empty interim update) from `lastTransformedText`, so a gap after a sentence end capitalizes the next segment while a mid-sentence gap does not
- `isPortInUse()` — prevents duplicate daemon instances
- State: `isWSAListening`, `stopCooldown`, `typingController.hasStopped`, `defaultLanguage` (startup default only), `capitalizeNext` + `lastTransformedText` (`--punctuation` only)

### Browser/WSA: [`src/browser.js`](src/browser.js)

- `initWSA(stream, lang)` — `continuous: true`; `interimResults` only if `stream`
- `onresult` → `onSpeechUpdate({ text })` with interim or accumulated final text
- `onend` → `onBrowserRecStop({ reason: offline \| silence })`
- `onerror` — `network` sets offline flag; other errors throw
- Exported: `startListening`, `stopRecognition`, `setLangAndStart(lang)`, `healthCheck`

### TypingController: [`src/typingController.ts`](src/typingController.ts)

- Persistent `dotool` child; `hasStopped` blocks `applyDiff` after manual stop
- **DiffEnum:** `NoChange`, `ChangeRes` (backspace + type), `ChangeResAndClear` (empty transcript → reset)
- Unicode via GNOME hex entry; ASCII buffered as `type ...`

### Transcript transformer: [`src/transcriptTransformer.ts`](src/transcriptTransformer.ts)

- Pure functions, no state: `transformTranscript` (spoken-word punctuation rules + in-string capitalization after `.?!`), `capitalizeFirst`, `endsSentence`
- Word-boundary, case-insensitive rules — "commander"/"periodic" stay literal; no escape mechanism for literal "comma" etc. (off = literal typing)
- Always applied to the **whole** transcript per update so `prevText`/`currText` diff in the same transformed space; segment-boundary capitalization state lives in `Daemon`, not here

### Browser launcher: [`src/browserLauncher.ts`](src/browserLauncher.ts)

- `detectBrowser()`, `launchBrowser()` — headless `"new"`, shared `LAUNCH_ARGS` (media fake UI, disable throttling, etc.)
- `BrowserType = "chrome" | "chromium"`

### Notifications

- [`notifier.ts`](src/notifier.ts) — composes text + sound; all methods async
- [`textNotifier.ts`](src/textNotifier.ts) — `dbus-next`, replace via `lastNotificationId`, retry with backoff
- [`soundNotifier.ts`](src/soundNotifier.ts) — `paplay`; prod path `/usr/local/share/voice-type/sounds`

### Logging

- No dedicated logger module — `log()` in [`src/utils.ts`](src/utils.ts) prefixes console output with `[DAEMON]`; browser console is piped with `[BROWSER]`, dotool stderr with `[DOTOOL]`

---

## Types ([`src/types.ts`](src/types.ts))

```typescript
enum DiffEnum { NoChange, ChangeRes, ChangeResAndClear }

interface CliFlags {
    lang: WSALanguage
    textNotifs: boolean
    soundNotifs: boolean
    stream: boolean
    browserType: BrowserType
    browserPath?: string
    timeout: number
    punctuation: boolean
    detached: boolean
    help: boolean
}
```

`WSA_LANGUAGES` — 41 BCP47 tags in [`src/constants.ts`](src/constants.ts).

Per-request language is shared between CLI and HTTP via [`src/language.ts`](src/language.ts):

```typescript
const DEFAULT_LANGUAGE = "en-US"
function isValidLanguage(lang: unknown): lang is string
function readLanguageQuery(query: Record<string, unknown>): string | undefined
```

---

## Extension points

| Goal | Where to change |
|---|---|
| New language | Add to `WSA_LANGUAGES` in `constants.ts` |
| New notification | Method on `Notifier` + wire in `daemon.ts` |
| Non-dotool input | Replace `TypingController` (high effort) |
| Alternate STT | Replace `browser.js` + launch config (high effort) |
| Per-request language validation/resolution | [`src/language.ts`](src/language.ts) |
| New spoken-punctuation rule | `PUNCTUATION_RULES` in [`src/transcriptTransformer.ts`](src/transcriptTransformer.ts) |

---

## Operational notes

- **Startup:** ~2–3s browser init; then idle until `/toggle`
- **Memory:** ~200MB Chrome + ~50MB Bun (rough)
- **Transcription latency:** interim results typically &lt;100ms after WSA fires
- **Reliability:** browser reinit on health check failure; D-Bus reconnect with backoff
- **Permissions:** dotool needs `input` group; mic permission via Chrome fake-UI flag + system default input

---

## Linux dependencies (system, not npm)

| Tool | Role |
|---|---|
| `dotool` | Virtual keyboard — [source](https://git.sr.ht/~geb/dotool/) |
| `google-chrome` or `chromium` | WSA host |
| `paplay` | Sound notifications |
| D-Bus session | Text notifications |

**dotool setup:** `sudo udevadm control --reload && sudo udevadm trigger`; `sudo usermod -aG input $USER` (reboot).

---

## Code style (Prettier)

4-space indent, 120 columns, no semicolons, no prose wrap. Strict TypeScript. Classes: `export default class Name`.

---

## package.json (reference)

```json
{
  "name": "voice-type-cli",
  "type": "module",
  "dependencies": {
    "dbus-next": "^0.10.2",
    "express": "^5.2.1",
    "puppeteer-core": "^24.39.1",
    "x11": "^2.3.0"
  }
}
```

`x11` is listed for optional/window utilities; core dictation path is HTTP + dotool + Chrome.
