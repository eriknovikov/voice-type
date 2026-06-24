# Voice Type

System-wide speech-to-text for Linux. Press a key, speak, and text appears wherever your cursor is.

Runs Chrome's Web Speech API quietly in the background — no local models, no paid service, no startup delay. Works in editors, terminals, browsers, and most other apps. If the transcript changes mid-sentence, Voice Type backspaces and retypes the corrected text.

**Requirements:** Linux with a desktop environment, a working microphone, and Chrome or Chromium installed system-wide.

---

# Installation

Install the prerequisites below, then run:

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | bash
```

This downloads the latest release, verifies its checksum, and installs `voice-type` to `/usr/local/bin`.

Pin a version: `curl -sSL .../install.sh | bash -s -- --version v3.1.0`

### Prerequisites

| Package | Purpose |
| --- | --- |
| [dotool](https://git.sr.ht/~geb/dotool/) | Types text into any focused window |
| `google-chrome` or `chromium` | Hosts the Web Speech API |
| `paplay` | Sound notifications (only if using `--sound`). `canberra-gtk-play` is preferred when available. |

---

# Usage

1. **Start the daemon once** (e.g. F10) — Chrome loads in the background; the mic is not active yet.
2. **Dictate** (e.g. F9) — speak, and text appears in the focused window in real time.
3. **Stop dictation** (F9 again) — mic off; the daemon stays running for the next session.
4. **Stop the daemon** (F10 again) — shuts down Chrome and frees resources.

### Spoken punctuation

The Web Speech API doesn't insert punctuation. For **English (`en-*`)**, spoken punctuation and auto-capitalization are **on by default** and can be toggled at runtime without restarting dictation: `curl localhost:3232/togglePunctuation` flips the current state; `?enabled=true` or `?enabled=false` sets it explicitly. The setting persists to `~/.config/voice-type.jsonc`. Other languages type literally.

| You say                                     | You get                                             |
| ------------------------------------------- | --------------------------------------------------- |
| `comma`                                     | `,`                                                 |
| `period`                                    | `.`                                                 |
| `question mark`                             | `?`                                                 |
| `exclamation mark` (or `exclamation point`) | `!`                                                 |
| `semicolon`                                 | `;`                                                 |
| `double quote` (or `double quotes`)         | `"` (with a space before it when it follows a word) |
| `new line` (or `newline`)                   | Enter (inline, anywhere in the utterance)           |
| `control enter`                             | Ctrl+Enter (standalone key command; streaming only) |

Saying "hello comma world period" types `Hello, world.` — the first word of each sentence is capitalized automatically, including the first word after starting dictation and after a pause that follows a sentence end (a pause mid-sentence does not capitalize).

**Newline** (streaming mode only): say `new line` or `newline` anywhere in an utterance — e.g. "hello new line world" inserts a line break after `hello`. With `--no-stream`, these phrases type literally. Partial phrases like `new` may appear briefly before the full phrase is recognized and corrected.

**Control enter** (streaming mode only): say `control enter` as the only content in a segment to send Ctrl+Enter. It must be spoken alone — nothing else in the same segment.

All spoken punctuation phrases are case-insensitive (`ComMa` → `,`, `Double Quotes` → `"`, `New Line` → Enter).

Matching is whole-word, so "commander" and "periodic" type literally. There is no escape sequence: on English you cannot type the literal words "comma" or "period".

---

# Options

**Heads up:** CLI flags have moved to `~/.config/voice-type.jsonc`. The table below is outdated and will be replaced in a follow-up release; run `voice-type help` for the current config schema.

| Flag | Description | Default |
| --- | --- | --- |
| `--lang`, `-l` | Startup default language ([list](https://github.com/eriknovikov/voice-type/blob/main/src/constants.ts)). Override per key with `?lang=` (see [multiple languages](#dictate-in-multiple-languages)). | `en-US` |
| `--browser_type` | `chrome` or `chromium` | `chrome` |
| `--browser_path`, `-p` | Custom browser binary (e.g. `google-chrome-beta`) | — |
| `--timeout` | Auto-stop after N seconds of silence (streaming only) | `0` (off) |
| `--no-stream` | Final transcripts only (no live corrections) | off |
| `--sound`, `-s` | Sound notifications | off |
| `--text` | Desktop notifications | off |
| `--detached`, `-d` | Run daemon in background | off |

---

# Keyboard shortcuts (recommended)

Bind these in your desktop environment (GNOME: Settings → Keyboard → Custom Shortcuts).

| Key | Action                     | Command                                                               |
| --- | -------------------------- | --------------------------------------------------------------------- |
| F10 | Start / stop the daemon    | `sh -c "curl http://localhost:3232/exit 2>/dev/null \|\| voice-type"` |
| F9  | Dictate (default language) | `curl http://localhost:3232/toggle`                                   |

Press F10 once to start the daemon, then F9 to dictate. That's the whole flow.

### Dictate in multiple languages

Bind a separate key to each language by adding `?lang=` to the dictation command. Each key toggles dictation in its own language — no daemon restart, no flags to remember.

| Key | Language | Command                                          |
| --- | -------- | ------------------------------------------------ |
| F9  | English  | `curl 'http://localhost:3232/toggle?lang=en-US'` |
| F8  | Spanish  | `curl 'http://localhost:3232/toggle?lang=es-ES'` |
| F7  | French   | `curl 'http://localhost:3232/toggle?lang=fr-FR'` |

Plain `curl http://localhost:3232/toggle` (no `?lang=`) uses the daemon's startup default. Set that default with `voice-type -l es-ES`. See the [full language list](https://github.com/eriknovikov/voice-type/blob/main/src/constants.ts).

### HTTP API

All endpoints are `GET` on `http://localhost:3232`:

| Endpoint | Effect |
| --- | --- |
| `/health` | Returns `{"status":"ok"}` |
| `/toggle` | Start or stop listening |
| `/start` | Start listening |
| `/stop` | Stop listening |
| `/exit` | Shut down the daemon |
| `/togglePunctuation` | Toggle spoken punctuation (English only). Optional `?enabled=true\|false`. Returns `{"punctuation": <bool>}`. Persists to config. |

`/start` and `/toggle` accept `?lang=<bcp47>` (alias `?language=`) to set the recognition language for that request, falling back to the startup default (`--lang`) when omitted. Invalid values return `400` with an error notification and leave the current state unchanged. `/stop` and `/exit` ignore the param.

---

# Uninstalling

```bash
sudo rm /usr/local/bin/voice-type
# If an older version created this directory, you can remove it:
sudo rm -rf /usr/local/share/voice-type 2>/dev/null
```

---

# Troubleshooting

**dotool issues** — [Official docs](https://git.sr.ht/~geb/dotool/). After install: `sudo udevadm control --reload && sudo udevadm trigger`. Your user must be in the `input` group (`sudo usermod -aG input $USER`); reboot afterward.

**Chrome not found** — Voice Type expects `/usr/bin/google-chrome` or `/usr/bin/chromium`. If your distro only provides `google-chrome-stable`:

```bash
sudo ln -s /usr/bin/google-chrome-stable /usr/bin/google-chrome
```

Or use `--browser_path /usr/bin/google-chrome-stable`.

**`voice-type: command not found`** — Ensure `/usr/local/bin` is on your PATH:

```bash
export PATH="$PATH:/usr/local/bin"
```

**Microphone not detected** — Set the default input in system sound settings (`pavucontrol` on most distros).

---

## Contributing

Bug reports, fixes, docs, and feature ideas are welcome. Entry point: `src/index.ts`. For architecture and conventions when hacking on the code, see [AGENTS.md](./AGENTS.md). Open an issue first for anything non-trivial.

## Further reading

- [How I built Voice Type](https://dev.to/eriknovikov/how-i-built-voice-type-3i2p) (blog post)
- [INTERNALS.md](./INTERNALS.md) — deeper technical walkthrough
