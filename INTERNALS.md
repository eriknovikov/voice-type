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

The daemon communicates with the Chrome page via [Chrome DevTools Protocol (CDP)](https://chromedevtools.github.io/devtools-protocol/), using it to evaluate JavaScript in the page context — starting and stopping the `SpeechRecognition` instance, and reading back transcript results.

---

## HTTP Control Server

The daemon exposes four endpoints on localhost:

| Endpoint | What it does |
|---|---|
| `POST /start` | Tells the Chrome page to start `SpeechRecognition` |
| `POST /stop` | Stops recognition and flushes any remaining transcript |
| `POST /toggle` | Starts if idle, stops if listening |
| `POST /exit` | Stops recognition, closes Chrome, shuts down the daemon |

These are plain HTTP — no auth, localhost only. `curl` calls them from your keyboard shortcut.

---

## Text Injection via dotool

Once a transcript comes back from the Web Speech API, Voice Type passes it to `dotool`, which replays it as keyboard input at the OS level. Because `dotool` operates via `/dev/uinput`, it works in any application — terminals, browsers, native apps — without needing clipboard access or application-specific integrations.

`paplay` is used alongside this to play short audio cues (start, stop, error) from system sound files.

---

## Flatpak Specifics

The Flatpak build bundles `dotool` and `paplay` so users don't need to install them manually. Chrome is still expected to be present on the host system — the Flatpak uses `flatpak-spawn --host` to launch it outside the sandbox, which is what gives it access to the microphone and the Web Speech API.

The toggle-daemon shortcut works by checking `flatpak ps` for a running `VoiceType` instance. If found, it sends `/exit` to shut it down; if not, it starts a new one. This avoids spawning duplicate daemons.

---

## Contributing

Bug reports, fixes, documentation improvements, and feature suggestions are all welcome. The codebase entry point is `src/index.ts`. The HTTP server, CDP communication, and dotool integration are each fairly self-contained, so it's not hard to find your way around.

Open an issue first for anything non-trivial — good to align before putting work into a PR.