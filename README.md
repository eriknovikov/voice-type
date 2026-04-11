# Voice Type

System-wide speech-to-text for Linux. Press a key, speak, and text appears wherever your cursor is.

Runs Chrome's Web Speech API quietly in the background — no local models, no paid service, no startup delay. Works in editors, terminals, browsers, and most other apps. If the transcript changes mid-sentence, Voice Type backspaces and retypes the corrected text.

Requirements: Linux with a desktop environment, a working microphone, and Chrome or Chromium installed system-wide.

---

# Installation

## Flatpak

Installs system dependencies automatically. Requires `flatpak-builder`.

```bash
git clone https://github.com/eriknovikov/voice-type.git
cd voice-type
chmod +x ./flatpak/build.sh && ./flatpak/build.sh
```


## npm

```bash
npm install --global voice-type-cli@latest
```

### System dependencies (npm only)

| Package | Purpose | Install |
|---|---|---|
| `dotool` | Types text into the system. | [See dotool docs](https://git.sr.ht/~geb/dotool/) 
| `paplay` | Audio notifications | Usually pre-installed. If missing: `pulseaudio-utils` (Ubuntu/Debian/Fedora) or `libpulse` (Arch) |

### Sound Assets (optional)

To use sound notifications (`--sound` flag) when transcription starts or stops: 

```bash
sudo mkdir -p /usr/local/share/voice-type/sounds
curl -L -o /usr/local/share/voice-type/sounds/start.oga https://github.com/eriknovikov/voice-type/raw/main/assets/sounds/start.oga
curl -L -o /usr/local/share/voice-type/sounds/stop.oga https://github.com/eriknovikov/voice-type/raw/main/assets/sounds/stop.oga
```


### Browser symlink (required)

Voice Type expects browser binaries named `google-chrome` and `chromium` respectively. If your browser uses a different name (e.g. `google-chrome-stable`), add a symlink:

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
| `--no-text` | Disable text notifications | on |
| `--detached`, `-d` | Run in detached mode | - |

---

# Keyboard Shortcuts (recommended)

Bind these in your desktop environment's shortcut settings. If you're in GNOME, go to Settings -> Keyboard -> View and Customize Shortcuts.


| Key | Action | Command |
|---|---|---|
| F9 | Toggle daemon | `sh -c "curl http://127.0.0.1:3232/exit 2>/dev/null \|\| START_COMMAND"` |
| F10 | Toggle dictation | `curl http://127.0.0.1:3232/toggle` |

## npm
Replace START_COMMAND with the output of `echo "$(which node) $(which voice-type)"`. For example, in my case, START_COMMAND would be `/home/nkv/.nvm/versions/node/v25.9.0/bin/node /home/nkv/.nvm/versions/node/v25.9.0/bin/voice-type` 

## Flatpak
Replace START_COMMAND with `flatpak run org.github.eriknovikov.VoiceType`

---

# Uninstalling

| Installation | Command |
|---|---|
| Flatpak | `flatpak uninstall org.github.eriknovikov.VoiceType && flatpak uninstall --unused` |
| npm | `npm uninstall --global voice-type-cli` |

---

# Troubleshooting

**`dotool issues`** — [Check the docs](https://git.sr.ht/~geb/dotool/). After its installation, make sure that you run `sudo udevadm control --reload && sudo udevadm trigger`. Also, dotool requires that your user is in the input group. Specifically, if your user does not appear in the output of running `groups`, make sure to add it via `sudo usermod -aG input $USER`. You have to reboot for the changes to take effect.


**`voice-type: command not found`** — Add `/usr/local/bin` to your PATH in `~/.bashrc` or `~/.zshrc`:
```bash
export PATH="$PATH:/usr/local/bin"
```

**npm installation works properly via cli but not via shortcut**: GNOME (or Desktop Environments in general) don't know about your PATH defined in .bashrc or .zshrc; to fix it, make sure you are sending the full paths in your system of both `node` and `voice-type`. So, the command should be the result of running this `$(which node) $(which voice-type)`, and CANNOT be just `voice-type`.

**Microphone not detected or no results in dictation** — Check your system audio settings. Make sure you have configured your mic as the system's default mic. In most distros, you should use `pavucontrol`(in your package manager).


---


## Contributing

Voice Type is totally free and open source. Bug reports, fixes, documentation improvements, and feature suggestions are all welcome. The codebase entry point is `src/index.ts`. The HTTP server, CDP communication, and dotool integration are each fairly self-contained, so it's not hard to find your way around.

Open an issue first for anything non-trivial — good to align before putting work into a PR.

## Final notes
You can read [my blog about voice-type](https://dev.to/eriknovikov/how-i-built-voice-type-3i2p), or check [INTERNALS.md](./INTERNALS.md) to see in greater depth how voice-type works.