# Voice Type

System-wide speech-to-text for Linux. Press a key, speak — text appears wherever you're focused.

Uses Chrome's built-in Web Speech API running silently in the background. No local models, no paid services, no startup delay. It works in any application — editors, terminals, browsers, wherever your cursor is — and if the transcription corrects a word mid-sentence, it automatically backspaces and retypes it. Audio and visual feedback confirm when it's listening or stops.

**Requirements:** Linux with a desktop environment, a working microphone, and Chrome or Chromium installed system-wide (required to expose the Web Speech API).

---

# Installation

## Flatpak

Installs system dependencies automatically.

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

### System dependencies (binary and npm only)

| Package | Purpose | Install |
|---|---|---|
| `dotool` | Types text into the system | AUR (Arch), COPR (Fedora), or [from source](https://git.sr.ht/~geb/dotool/) |
| `paplay` | Audio notifications | Usually pre-installed. If missing: `pulseaudio-utils` (Ubuntu/Debian/Fedora) or `libpulse` (Arch) |

---

# Running

To start the daemon manually from a terminal, or to test your setup before binding hotkeys:

| Installation | Command |
|---|---|
| Flatpak | `flatpak run org.github.eriknovikov.VoiceType` |
| Binary | `voice-type` |
| npm | run `which node; which voice-type` in your terminal and use the full paths: `/path/to/node /path/to/voice-type` |

---

# Keyboard Shortcuts

Bind these in your desktop environment's shortcut settings (GNOME: Settings → Keyboard → Custom Shortcuts).

## Flatpak

| Key | Action | Command |
|---|---|---|
| F9 | Toggle daemon | `sh -c "flatpak ps \| grep -q VoiceType && curl -sf http://127.0.0.1:3232/exit \|\| flatpak run org.github.eriknovikov.VoiceType"` |
| F10 | Toggle dictation | `curl http://127.0.0.1:3232/toggle` |

## Binary, npm

| Key | Action | Command |
|---|---|---|
| F8 | Start daemon | `sh -c "voice-type"` |
| F9 | Start dictation | `sh -c "curl http://127.0.0.1:3232/start"` |
| F10 | Stop dictation | `sh -c "curl http://127.0.0.1:3232/stop"` |

For npm, the F8 command needs the full paths. Run `which node; which voice-type` in your terminal first, then set F8 to:
```
sh -c "/path/to/node /path/to/voice-type"
```

## Endpoints

| Endpoint | Action |
|---|---|
| `/start` | Start listening |
| `/stop` | Stop listening |
| `/toggle` | Toggle listening on/off |
| `/exit` | Stop the daemon |

---

# Options

| Flag | Description | Default |
|---|---|---|
| `--language`, `-l` | Language for recognition, [supported languages](https://github.com/eriknovikov/voice-type/blob/main/src/constants.ts) | `en-US` |
| `--browser`, `-b` | Browser to use | `chrome` or `chromium` |
| `--browser_path`, `-p` | Path for custom installs (e.g. `google-chrome-beta`) | - |
| `--sound`, `-s` | Enable sound notifications | off |
| `--no-text` | Disable text notifications | on |
| `--detached`, `-d` | Run in detached mode | - |

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