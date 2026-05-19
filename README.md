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

This downloads the latest release, verifies its checksum, and installs `voice-type` to `/usr/local/bin` plus notification sounds to `/usr/local/share/voice-type/sounds`.

Pin a version: `curl -sSL .../install.sh | bash -s -- --version v3.1.0`

### Prerequisites

| Package | Purpose |
|---|---|
| [dotool](https://git.sr.ht/~geb/dotool/) | Types text into any focused window |
| `google-chrome` or `chromium` | Hosts the Web Speech API |
| `paplay` | Sound notifications (only if using `--sound`) |

---

# Usage

1. **Start the daemon once** (e.g. F9) — Chrome loads in the background; the mic is not active yet.
2. **Dictate** (e.g. F10) — speech is transcribed into the focused window in real time.
3. **Stop dictation** (F10 again) — mic off; daemon keeps running for the next session.
4. **Stop the daemon** (F9 again) — shuts down Chrome and frees resources.

---

# Options

| Flag | Description | Default |
|---|---|---|
| `--lang`, `-l` | Recognition language ([list](https://github.com/eriknovikov/voice-type/blob/main/src/constants.ts)) | `en-US` |
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

| Key | Action | Command |
|---|---|---|
| F9 | Toggle daemon | `sh -c "curl http://127.0.0.1:3232/exit 2>/dev/null \|\| voice-type"` |
| F10 | Toggle dictation | `curl http://127.0.0.1:3232/toggle` |

Add flags to the start side of F9 if needed, e.g. `voice-type -l es-ES -s`.

### HTTP API

All endpoints are `GET` on `http://127.0.0.1:3232`:

| Endpoint | Effect |
|---|---|
| `/toggle` | Start or stop listening |
| `/start` | Start listening |
| `/stop` | Stop listening |
| `/exit` | Shut down the daemon |

---

# Uninstalling

```bash
sudo rm /usr/local/bin/voice-type
sudo rm -rf /usr/local/share/voice-type
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
