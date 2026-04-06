# Voice Type

System-wide speech-to-text for Linux. Press a key, speak, and text appears wherever the cursor is.

It runs Chrome's built-in Web Speech API quietly in the background. No local models, no paid service, no startup delay. It works in editors, terminals, browsers, and most other apps. If the transcript changes mid-sentence, Voice Type backspaces and retypes the corrected text.

**Requirements:** Linux with a desktop environment, a working microphone, and Chrome or Chromium installed system-wide (required to expose the Web Speech API).

---

# Installation

## Flatpak

Installs system dependencies automatically. Requires `flatpak-builder`.

```bash
git clone https://github.com/eriknovikov/voice-type.git
cd voice-type
chmod +x ./flatpak/build.sh && ./flatpak/build.sh
```

## Binary

```bash
curl -fsSL https://github.com/eriknovikov/voice-type/releases/latest/download/install.sh | bash
```

## npm

```bash
npm install --global voice-type-cli@latest
```

### Sound Assets (npm only)

When installing via npm, sound files are not included. If you want audio notifications, download them manually:

```bash
sudo mkdir -p /usr/local/share/voice-type/sounds
curl -L -o /usr/local/share/voice-type/sounds/start.oga \
  https://github.com/eriknovikov/voice-type/raw/main/assets/sounds/start.oga
curl -L -o /usr/local/share/voice-type/sounds/stop.oga \
  https://github.com/eriknovikov/voice-type/raw/main/assets/sounds/stop.oga
```

This places sounds in the same location the binary installer uses, so the `--sound` flag will work.

### System dependencies (binary and npm only)

| Package | Purpose | Install |
|---|---|---|
| `dotool` | Types text into the system | AUR (Arch), COPR (Fedora), or [from source](https://git.sr.ht/~geb/dotool/) |
| `paplay` | Audio notifications | Usually pre-installed. If missing: `pulseaudio-utils` (Ubuntu/Debian/Fedora) or `libpulse` (Arch) |

---

# Start

> [!IMPORTANT]
> Ensure these symlinks exist:
> - `google-chrome` → your Chrome variant (e.g., `sudo ln -s /usr/bin/google-chrome-stable /usr/bin/google-chrome`)
> - `chromium` → your Chromium variant (e.g., `sudo ln -s /usr/bin/chromium-stable /usr/bin/chromium`)

| Installation | Command |
|---|---|
| Flatpak | `flatpak run org.github.eriknovikov.VoiceType` |
| Binary | `voice-type` |
| npm | find the path with `which node; which voice-type`, then run: `/path/to/node /path/to/voice-type` |

# Usage

| Action | Command |
|---|---|
| Start listening | `curl http://127.0.0.1:3232/start` |
| Stop listening | `curl http://127.0.0.1:3232/stop` |
| Toggle listening | `curl http://127.0.0.1:3232/toggle` |
| Stop daemon | `curl http://127.0.0.1:3232/exit` |

---

# Options

| Flag | Description | Default |
|---|---|---|
| `--lang`, `-l` | Language for recognition, [supported languages](https://github.com/eriknovikov/voice-type/blob/main/src/constants.ts) | `en-US` |
| `--browser`, `-b` | Browser to use | `chrome` or `chromium` |
| `--browser_path`, `-p` | Path for custom installs (e.g. `google-chrome-beta`) | - |
| `--sound`, `-s` | Enable sound notifications | off |
| `--no-text` | Disable text notifications | on |
| `--detached`, `-d` | Run in detached mode | - |

---

# Keyboard Shortcuts (optional)

Bind these in your desktop environment's shortcut settings.

> [!TIP]
> To pass flags, wrap commands with `sh -c "command --flag"`. Example: `sh -c "voice-type --lang es"`

## Flatpak

| Key | Action | Command |
|---|---|---|
| F9 | Toggle daemon | `sh -c "flatpak ps \| grep -q VoiceType && curl -sf http://127.0.0.1:3232/exit \|\| flatpak run org.github.eriknovikov.VoiceType"` |
| F10 | Toggle dictation | `curl http://127.0.0.1:3232/toggle` |

## Binary and npm

| Key | Action | Command |
|---|---|---|
| F8 | Start daemon | `voice-type` |
| F9 | Toggle dictation | `curl http://127.0.0.1:3232/toggle` |
| F10 | Stop daemon | `curl http://127.0.0.1:3232/exit` |

> [!TIP]
> For npm, the F8 command needs the full paths. Run `which node; which voice-type` in your terminal first, then set F8 to:
> ```
> /path/to/node /path/to/voice-type
> ```

---

# Uninstalling

| Installation | Command |
|---|---|
| Flatpak | `flatpak uninstall org.github.eriknovikov.voice-type && flatpak uninstall --unused` |
| npm | `npm uninstall --global voice-type-cli` |
| Binary | `sudo rm -rf /usr/local/share/voice-type /usr/local/bin/voice-type` |

---

# Troubleshooting

**`dotool: command not found`** — Install dotool (see above).

**`voice-type: command not found`** — Add `/usr/local/bin` to your PATH in `~/.bashrc` or `~/.zshrc`:
```bash
export PATH="$PATH:/usr/local/bin"
```

**Microphone not detected** — Check system audio settings.

---

Voice Type is free and open source. See [INTERNALS.md](./INTERNALS.md) for how it works and how to contribute.
