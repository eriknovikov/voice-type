# How Voice Type Works

This document covers the internals — how the pieces fit together from hotkey press to text appearing on screen.

---

## Overview

Voice Type is a small Node.js daemon that bridges three things: Chrome's Web Speech API for transcription, an HTTP control server for hotkey commands, and `dotool` for injecting the resulting text system-wide.

```
hotkey → curl → HTTP server (Node.js) → Chrome (Web Speech API) → text → dotool → focused window
```

---

## Web Speech API

Chrome and Chromium expose the [Web Speech API](https://developer.mozilla.org/en-US/docs/Web/API/Web_Speech_API) natively — no extension, no external service. Voice Type launches a headless Chrome instance on startup and keeps a local webpage open inside it. That page runs the `SpeechRecognition` interface, which streams audio from the microphone directly to Google's speech servers and returns transcripts in real time.

This is why Chrome or Chromium must be installed system-wide: the API is part of the browser's internals and isn't available in sandboxed or Snap-packaged versions.

The Web Speech API returns two kinds of results: interim (in-progress, may change) and final (committed). Voice Type handles both — interim results appear as you speak, and if a word gets corrected mid-sentence, it automatically backspaces and retypes the corrected version.

---

## The Daemon

When you run `voice-type`, it starts an HTTP server on `127.0.0.1:3232` and launches Chrome in the background. Chrome stays open for the lifetime of the daemon — this is what eliminates startup lag when you press the hotkey. The browser is already running; it just needs to be told to start listening.

The daemon communicates with the Chrome page via [Chrome DevTools Protocol (CDP)](https://chromedevtools.github.io/devtools-protocol/), using it to evaluate JavaScript in the page context — starting and stopping the `SpeechRecognition` instance, and receiving transcript results as structured speech events.

---

## Speech events and transcript pipeline

WSA `onresult` callbacks are mapped to **speech events** before they leave the browser:

| Event | Meaning |
|---|---|
| `{ kind: "text", text }` | Interim or final transcript for the current segment |
| `{ kind: "segment-finalized" }` | WSA finalized a pause-delimited segment |

The daemon forwards events through `SpeechPipeline`:

1. **Transform** — per-language session (`transcriptTransformers/`). English (`en-*`) applies spoken punctuation, capitalization, and streaming-only commands (`new line`, `control enter`). Other languages pass through literally.
2. **Type** — `TypingController` prefix-diffs live text into `dotool` keystrokes.
3. **Finalize** — on `segment-finalized`, the transformer may emit key chords (e.g. Ctrl+Enter); the typing controller resets its diff baseline for the next segment.

Late events after stop are dropped at the daemon boundary (`shouldAcceptSpeechEvent`).

The mapper logic lives nested inside `browser.js` (Puppeteer constraint) and as a testable copy in `browserRecognition.js`.

---

## Usage

The daemon exposes these endpoints on localhost:

| Action | Command |
|---|---|
| Health check | `curl http://127.0.0.1:3232/health` |
| Start listening | `curl http://127.0.0.1:3232/start` |
| Stop listening | `curl http://127.0.0.1:3232/stop` |
| Toggle listening | `curl http://127.0.0.1:3232/toggle` |
| Stop daemon | `curl http://127.0.0.1:3232/exit` |

These are plain HTTP — no auth, localhost only.

---

## Text Injection via dotool

Once a transcript comes back from the Web Speech API, Voice Type passes it to `dotool`, which replays it as keyboard input at the OS level. Because `dotool` operates via `/dev/uinput`, it works in any application — terminals, browsers, native apps — without needing clipboard access or application-specific integrations.

Sound notifications play through a 2-tier resolver that searches for freedesktop sound theme names (`service-login`, `service-logout`, `dialog-error`) in the user's XDG data directories (`$XDG_DATA_HOME/sounds/` then `$XDG_DATA_DIRS/sounds/`). If `canberra-gtk-play` is on PATH it handles full theme lookup (including user overrides and theme inheritance); otherwise `paplay` plays the first found `.oga`/`.ogg`/`.wav` file. Neither available → silent no-op.

---

## Flatpak Specifics

The Flatpak build bundles `dotool` and `paplay` so users don't need to install them manually. Chrome is still expected to be present on the host system — the Flatpak uses `flatpak-spawn --host` to launch it outside the sandbox, which is what gives it access to the microphone and the Web Speech API.

The toggle-daemon shortcut works by checking `flatpak ps` for a running `VoiceType` instance. If found, it sends `/exit` to shut it down; if not, it starts a new one. This avoids spawning duplicate daemons.

---

