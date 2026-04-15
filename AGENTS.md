# AGENTS.md

This file provides guidance to agents when working with code in this repository.

## Project Overview

Voice Type is a system-wide speech-to-text daemon for Linux. It uses Chrome's Web Speech API (running headless in the background) to transcribe speech into text, which is then typed into the active window via `dotool`. No local models, no paid services.

**Key Flow:**
1. Daemon starts HTTP server on port 3232 and launches headless Chrome
2. Chrome initializes Web Speech API (WSA) with exposed functions for speech events
3. User triggers `/toggle` endpoint to start/stop listening
4. Speech results flow: Chrome WSA → `onSpeechUpdate({ text })` → TypingController → dotool
5. TypingController calculates diff between previous and current text, then backspaces and retypes corrections

---

## Build Commands (Bun required)
- `bun run dev` - Run with watch mode (`bun --watch src/index.ts`)
- `bun run build` - Build for distribution (outputs to `dist/`)
- `bun run start` - Run directly (`bun run src/index.ts`)
- `bun run compile-binary` - Compile to native binary (outputs to `build/voice-type`)

---

## Architecture & Components

### Entry Point: [`src/index.ts`](src/index.ts)
- Exports `PORT = 3232`
- Parses CLI flags via `cli.parseFlags()`, handles `--detached` mode via `cli.respawnDetached()`
- Creates Daemon instance and starts it
- Handles SIGTERM/SIGINT for clean shutdown via `destroyDaemon()`

### Daemon: [`src/daemon.ts`](src/daemon.ts)
- HTTP Express server with routes: `/start`, `/stop`, `/toggle`, `/exit`
- Manages browser lifecycle via Puppeteer-core
- Coordinates TypingController and Notifier
- Uses 100ms `stopCooldown` after stop to prevent rapid start/stop cycles
- Exposes functions to browser via `page.exposeFunction()`:
  - `onSpeechUpdate({ text })` - handles interim transcription results
  - `onBrowserRecStop({ reason })` - called when speech recognition stops (reason: "silence" | "offline" | undefined)
- Private state:
  - `isWSAListening` - whether speech recognition is active
  - `stopCooldown` - prevents rapid start/stop
  - `typingController.hasStopped` - flag checked when applying diffs
- Route responses:
  - Success: `res.send()` with message
  - Cooldown active: `res.status(429).send("Cooldown active")`
  - Browser not ready: `res.status(503).send("Wait for browser")`
- Public methods:
  - `start(port, browserType, customBrowserPath?)` - initializes server and browser
  - `destroy()` - cleanup resources on shutdown
- Private methods:
  - `initBrowser()` - launches browser, sets up page, exposes functions, calls `browser.initWSA()`
  - `startTranscription()` / `stopTranscription(reason)` - control speech recognition
  - `handleSpeechUpdate(payload)` - forwards to typing controller
  - `handleBrowserRecStop(payload)` - auto-stops if was listening
  - `isBrowserReady()` - checks `page` and `browser` are non-null
- Uses `isPortInUse()` helper to prevent multiple daemon instances

### Browser/WSA: [`src/browser.js`](src/browser.js) (`.js` extension required)
- Runs in Chrome context, uses `window.SpeechRecognition` or `window.webkitSpeechRecognition`
- `initWSA(lang)` - initializes continuous recognition with interim results
- Stores reference as `window.recognition` and `window.isOffline` flag
- Recognition config: `continuous: true`, `interimResults: true`
- Only sends interim (in-progress) transcripts via `window.onSpeechUpdate({ text: interimText })`
- Final transcripts are accumulated in `finalText` and logged to console (not typed)
- `rec.onstart` sets `rec.isRunning = true`
- `rec.onerror` sets `window.isOffline = true` for network errors, throws for other errors
- `rec.onend` calls `window.onBrowserRecStop({ reason: window.isOffline ? "offline" : "silence" })`

### TypingController: [`src/typingController.ts`](src/typingController.ts)
- Spawns `dotool` process with `DOTOOL_XKB_LAYOUT=us` env var
- Public properties:
  - `hasStopped` - when true, `applyDiff()` does nothing (prevents typing after manual stop)
- **Diff Algorithm** via `calculateDiff(currText)`: `DiffEnum`
  - `NoChange` - texts match
  - `ChangeResAndClear` - current text is empty (after silence)
  - `ChangeRes` - texts differ but current is non-empty
- `calculateAndApplyDiff(str)` - combines diff calculation and application
- `applyDiff(currText, diffResult)` - handles typing/backspacing
  - `NoChange`: do nothing
  - `ChangeRes`: backspace deleted chars, type new chars (or type all if prevText is empty)
  - `ChangeResAndClear`: call `reset()`
- `reset()` - clears `prevText`
- `sendBackspaces(count)` - sends `key BackSpace \n` repeated count times to dotool
- `typeText(text)` - handles character typing with buffering
- `destroy()` - kills dotool process with SIGTERM
- **Character Handling**:
  - ASCII (32-126): buffered in `asciiBuffer`, sent as `type <string>`
  - Newlines (`\n`): `key enter`
  - Unicode/CJK/Emojis: GNOME hex input (`key ctrl+shift+u` + hex chars + `key enter`)
- Helper: `findCommonPrefixLen(currText, prevText)` - finds common prefix length

### Browser Launcher: [`src/browserLauncher.ts`](src/browserLauncher.ts)
- Exports `BrowserType = "chrome" | "chromium"`
- `detectBrowser()` - checks for Chrome at `/usr/bin/google-chrome` or Chromium at `/usr/bin/chromium`
- `launchBrowser(browserType, browserPath?)` - launches browser via puppeteer-core
- Chrome/Chromium paths: `/usr/bin/google-chrome`, `/usr/bin/chromium`
- Launch args (shared):
  - `--use-fake-ui-for-media-stream`
  - `--disable-background-timer-throttling`
  - `--log-level=0`
  - `--disable-dev-shm-usage`
  - `--disable-gpu`
  - `--disable-software-rasterizer`
  - `--disable-background-networking`
  - `--disable-default-apps`
  - `--disable-extensions`
  - `--disable-sync`
  - `--disable-translate`
  - `--metrics-recording-only`
  - `--no-first-run`
  - `--safebrowsing-disable-auto-update`
  - `--process-per-site`
  - `--disable-features=IsolateOrigins,site-per-process`
- Uses `headless: "new"` mode and `name: "Voice-Type-browser"`

### CLI Parsing: [`src/cli.ts`](src/cli.ts)
- Uses `node:util.parseArgs` with strict mode
- Options:
  - `-s, --sound` - enable sound notifications
  - `--no-text` - disable text notifications
  - `-l, --lang <lang>` - WSA language (default: "en-US")
  - `-b, --browser <browser>` - "chrome" or "chromium" (default: "chrome")
  - `-p, --browser_path <path>` - custom browser executable path
  - `-d, --detached` - run in background
  - `-h, --help` - show help
- `parseFlags(args)` - validates language against `WSA_LANGUAGES`, validates browser type
- `showHelp()` - displays usage information
- `respawnDetached(args)` - spawns detached child process, unrefs it so parent can exit immediately
- `pruneFlags(flags)` - removes `--detached` and `-d` from args when respawning

### Notifications: [`src/notifier.ts`](src/notifier.ts)
- Composes `TextNotifier` and `SoundNotifier`
- `destroy()` - cleans up text notifier
- Async notification methods (all `await` both text and sound):
  - `notifyDaemonStart()` - daemon started
  - `notifyDaemonStop()` - daemon shutting down
  - `notifyMicStart()` - started listening
  - `notifyMicStopIntentional()` - stopped intentionally
  - `notifyMicStopSilence()` - stopped due to silence timeout
  - `notifyOffline()` - network disconnected
  - `notifyError(msg)` - error occurred
  - `notifyAlreadyRunning()` - prevented duplicate daemon

### TextNotifier: [`src/textNotifier.ts`](src/textNotifier.ts)
- Uses `dbus-next` to send desktop notifications via `org.freedesktop.Notifications`
- Maintains D-Bus connection with retry mechanism (max 3 retries, exponential backoff)
- Connection health check via `isConnectionHealthy()`
- `ensureConnection()` - reconnects if needed before sending
- Notification replacement: uses `lastNotificationId` to replace existing popup (uses `x-canonical-private-synchronous` hint)
- Transient notifications with urgency levels (low=0, normal=1, critical=2)
- `destroy()` - disconnects D-Bus

### SoundNotifier: [`src/soundNotifier.ts`](src/soundNotifier.ts)
- Uses `paplay` to play audio feedback
- Sound files determined by installation method:
  - Flatpak: `/app/share/sounds/voice-type`
  - Compiled binary: `/usr/local/share/voice-type/sounds`
  - Dev/NPM: `<cwd>/assets/sounds`
- Sounds: `start.oga`, `stop.oga`, `error.oga` (ERROR and OFFLINE both use `stop.oga`)
- `notify()` is async - returns `Promise<void>` that resolves when paplay process closes
- Handles paplay errors gracefully

### Logger: [`src/logger.ts`](src/logger.ts)
- `Logger` class with buffer rotation (default max size: 10MB)
- `log(message)` - adds to buffer, console.logs, evicts old messages when full
- `log()` helper exports singleton, prepends `"[DAEMON] "` to messages

---

## Types: [`src/types.ts`](src/types.ts)

```typescript
enum DiffEnum { NoChange, ChangeRes, ChangeResAndClear }
type WSALanguage = (typeof WSA_LANGUAGES)[keyof typeof WSA_LANGUAGES]
type Urgency = "low" | "normal" | "critical"

interface CliFlags {
    lang: WSALanguage
    textNotifs: boolean
    soundNotifs: boolean
    browser: BrowserType
    browserPath?: string
    detached: boolean
    help: boolean
}
```

`BrowserType` is exported from `browserLauncher.ts` as `"chrome" | "chromium"`

### Constants: [`src/constants.ts`](src/constants.ts)
- `WSA_LANGUAGES` - 45 BCP47 language tags (en-US, en-GB, es-ES, ru-RU, zh-CN, etc.)

---

## Code Style (Prettier)
- Tab width: 4 spaces, Print width: 120, No semicolons, No prose wrap
- **Import extensions**: Always use `.js` extension in imports due to `verbatimModuleSyntax`
- **Type imports**: Use `import type { X }` syntax for type-only imports
- Classes export as `export default class ClassName`
- TypeScript strict mode enabled

---

## Testing
- No automated test framework - manual tests in [`src/tests/*.manual.ts`](src/tests/)
- Run manually with `bun run src/tests/<filename>`

---

## Linux-Specific Dependencies (external, not in package.json)
- `dotool` - types text into system (install from source: https://git.sr.ht/~geb/dotool/)
- `dbus-next` - desktop notifications via libdbus
- `x11` - window management (for detecting active window)
- `paplay` - audio playback (pulseaudio-utils package)

### dotool Setup
After installation, run: `sudo udevadm control --reload && sudo udevadm trigger`
User must be in `input` group: `sudo usermod -aG input $USER` (requires reboot)

---

## CLI Usage Examples

```bash
voice-type                    # Start daemon (F9 to toggle, F10 to dictate)
voice-type -l es-ES          # Spanish dictation
voice-type -s                # Enable sound notifications
voice-type --no-text         # Disable text notifications
voice-type -d                # Run detached (background)
voice-type -p /path/to/chrome # Custom browser path
```

### HTTP API (port 3232)
- `GET /start` - Start listening (returns "Listening" or error)
- `GET /stop` - Stop listening (returns "Stopped" or error)
- `GET /toggle` - Toggle listening state
- `GET /exit` - Stop daemon completely

### Error Responses
- `503 Service Unavailable` - Browser not ready
- `429 Too Many Requests` - Cooldown active (100ms after stop)

---

## Package.json Details

```json
{
  "name": "voice-type-cli",
  "version": "1.2.2",
  "type": "module",
  "bin": { "voice-type": "bin/voice-type" },
  "files": ["bin", "dist", "assets"],
  "dependencies": {
    "dbus-next": "^0.10.2",
    "express": "^5.2.1",
    "puppeteer-core": "^24.39.1",
    "x11": "^2.3.0"
  }
}