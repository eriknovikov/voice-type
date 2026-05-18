# Voice Type

System-wide speech-to-text for Linux. Press a key, speak, and text appears wherever your cursor is.

Runs Chrome's Web Speech API quietly in the background — no local models, no paid service, no startup delay. Works in editors, terminals, browsers, and most other apps. If the transcript changes mid-sentence, Voice Type backspaces and retypes the corrected text.

Requirements: Linux with a desktop environment, a working microphone, and Chrome or Chromium installed system-wide.

---

# Installation

## Binary (recommended)

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | bash
```

### Prerequisites

| Package | Purpose |
|---|---|
| `dotool` | Types text — [install from source](https://git.sr.ht/~geb/dotool/) |
| `google-chrome` or `chromium` | Browser for Web Speech API |
| `paplay` | Audio notifications (optional) |

### Manual installation

1. Download the latest release for your architecture:
   - [Voice Type releases](https://github.com/eriknovikov/voice-type/releases)
2. Extract and move the binary:
   ```bash
   tar -xzf voice-type-linux-x64.tar.gz
   sudo mv voice-type /usr/local/bin/voice-type
   sudo chmod +x /usr/local/bin/voice-type
   ```

```bash
sudo ln -s /usr/bin/google-chrome-stable /usr/bin/google-chrome #similar for chromium
```

---

# Usage

You start the daemon once (F9). It sits idle in the background without consuming resources nor listening through the mic. When you want to dictate something, press F10, and whatever you speak gets transcribed into the currently active window in the system. Once you are done, press F10 again to stop listening. If for some reason you no longer want the daemon running idly in the background, kill it via F9.


# Options

| Flag | Description | Default |
|---|---|---|
| `--lang`, `-l` | Language for recognition, [supported languages](https://github.com/eriknovikov/voice-type/blob/main/src/constants.ts) | `en-US` |
| `--browser`, `-b` | Browser to use | `chrome` or `chromium` |
| `--browser_path`, `-p` | Path for custom installs (e.g. `google-chrome-beta`) | - |
| `--sound`, `-s` | Enable sound notifications | off |
| `--text` | Enable text notifications | off |
| `--detached`, `-d` | Run in detached mode | - |

---

# Keyboard Shortcuts (recommended)

Bind these in your desktop environment's shortcut settings. If you're in GNOME, go to Settings -> Keyboard -> View and Customize Shortcuts.


| Key | Action | Command |
|---|---|---|
| F9 | Toggle daemon | `sh -c "curl http://127.0.0.1:3232/exit 2>/dev/null \|\| START_COMMAND"` |
| F10 | Toggle dictation | `curl http://127.0.0.1:3232/toggle` |

## Binary
Replace START_COMMAND with `voice-type`

---

# Uninstalling

```bash
sudo rm /usr/local/bin/voice-type
sudo rm -rf /usr/local/share/voice-type
```

---

# Troubleshooting

**`dotool issues`** — [Check the docs](https://git.sr.ht/~geb/dotool/). After its installation, make sure that you run `sudo udevadm control --reload && sudo udevadm trigger`. Also, dotool requires that your user is in the input group. Specifically, if your user does not appear in the output of running `groups`, make sure to add it via `sudo usermod -aG input $USER`. You have to reboot for the changes to take effect.


**`voice-type: command not found`** — Add `/usr/local/bin` to your PATH in `~/.bashrc` or `~/.zshrc`:
```bash
export PATH="$PATH:/usr/local/bin"
```

**Microphone not detected or no results in dictation** — Check your system audio settings. Make sure you have configured your mic as the system's default mic. In most distros, you should use `pavucontrol`(in your package manager).


---


## Contributing

Voice Type is totally free and open source. Bug reports, fixes, documentation improvements, and feature suggestions are all welcome. The codebase entry point is `src/index.ts`. The HTTP server, CDP communication, and dotool integration are each fairly self-contained, so it's not hard to find your way around.

Open an issue first for anything non-trivial — good to align before putting work into a PR.

## Final notes
You can read [my blog about voice-type](https://dev.to/eriknovikov/how-i-built-voice-type-3i2p), or check [INTERNALS.md](./INTERNALS.md) to see in greater depth how voice-type works.