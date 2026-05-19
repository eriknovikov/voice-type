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
Hotkey → curl :3232/toggle → Daemon → browser.startListening()
              ↓
Microphone → Web Speech API → onSpeechUpdate({ text })
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
5. **No automated tests** — Manual scripts live in `src/tests/*.manual.ts`. Run with `bun run src/tests/<file>`. Do not add Jest/Vitest unless explicitly requested.
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
│   ├── typingController.ts
│   ├── notifier.ts, textNotifier.ts, soundNotifier.ts
│   ├── logger.ts, utils.ts, types.ts, constants.ts
│   └── tests/*.manual.ts
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
| `GET /start` | Start listening |
| `GET /stop` | Stop listening (intentional) |
| `GET /toggle` | Toggle listen state |
| `GET /exit` | Shutdown daemon |

**Responses:** `503` browser not ready · `429` stop cooldown (100ms) · `200` with short text body on success

Recommended hotkeys (see README): F9 toggles daemon (`/exit` or start `voice-type`), F10 toggles dictation (`/toggle`).

---

## CLI flags

Parsed in [`src/cli.ts`](src/cli.ts) via `node:util.parseArgs` (strict).

| Flag | Default | Notes |
|---|---|---|
| `-l, --lang` | `en-US` | Must be in `WSA_LANGUAGES` ([`constants.ts`](src/constants.ts)) |
| `--browser_type` | `chrome` | `chrome` or `chromium` → sets `BROWSER_TYPE` env |
| `-p, --browser_path` | — | Custom executable → `BROWSER_PATH` env |
| `--timeout` | `0` | Seconds of silence before auto-stop; **only when streaming** (`timeout > 0` resets on each speech update) |
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
- `initBrowser()` → launch, new page, `exposeFunction` for speech callbacks, `browser.initWSA(stream, lang)`
- `startTranscription` / `stopTranscription(reason)` — reasons: `intentional`, `silence`, `offline`
- `handleBrowserRecStop` — auto-stops if WSA ends while still listening
- `silenceTimer` — daemon-side timeout when `stream && timeout > 0`; reset on each `handleSpeechUpdate`
- `isPortInUse()` — prevents duplicate daemon instances
- State: `isWSAListening`, `stopCooldown`, `typingController.hasStopped`

### Browser/WSA: [`src/browser.js`](src/browser.js)

- `initWSA(stream, lang)` — `continuous: true`; `interimResults` only if `stream`
- `onresult` → `onSpeechUpdate({ text })` with interim or accumulated final text
- `onend` → `onBrowserRecStop({ reason: offline \| silence })`
- `onerror` — `network` sets offline flag; other errors throw
- Exported: `startListening`, `stopRecognition`, `healthCheck`

### TypingController: [`src/typingController.ts`](src/typingController.ts)

- Persistent `dotool` child; `hasStopped` blocks `applyDiff` after manual stop
- **DiffEnum:** `NoChange`, `ChangeRes` (backspace + type), `ChangeResAndClear` (empty transcript → reset)
- Unicode via GNOME hex entry; ASCII buffered as `type ...`

### Browser launcher: [`src/browserLauncher.ts`](src/browserLauncher.ts)

- `detectBrowser()`, `launchBrowser()` — headless `"new"`, shared `LAUNCH_ARGS` (media fake UI, disable throttling, etc.)
- `BrowserType = "chrome" | "chromium"`

### Notifications

- [`notifier.ts`](src/notifier.ts) — composes text + sound; all methods async
- [`textNotifier.ts`](src/textNotifier.ts) — `dbus-next`, replace via `lastNotificationId`, retry with backoff
- [`soundNotifier.ts`](src/soundNotifier.ts) — `paplay`; prod path `/usr/local/share/voice-type/sounds`

### Logger: [`src/logger.ts`](src/logger.ts)

- Rotating in-memory buffer (10MB default); `[DAEMON]` prefix on console

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
    detached: boolean
    help: boolean
}
```

`WSA_LANGUAGES` — 45 BCP47 tags in [`src/constants.ts`](src/constants.ts).

---

## Extension points

| Goal | Where to change |
|---|---|
| New language | Add to `WSA_LANGUAGES` in `constants.ts` |
| New notification | Method on `Notifier` + wire in `daemon.ts` |
| Non-dotool input | Replace `TypingController` (high effort) |
| Alternate STT | Replace `browser.js` + launch config (high effort) |

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
