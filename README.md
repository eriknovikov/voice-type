# What's Voice Type?

A system-wide Speech-to-Text for Linux. Press a key, speak, done — text lands wherever you're focused. Totally free, with unmatched speed and accuracy.

**How it works:** Chrome or Chromium runs quietly in the background using its built-in Web Speech API. No local models, no paid services, no startup lag.

# Features

- **Instant Dictation**: Press a hotkey and start speaking. Text appears in real-time in whatever window you have focused in the system.
- **Auto-Correction**: If the AI corrects a word mid-sentence, it automatically backspaces and types the corrected version.
- **System-Wide**: Works in any application - text editors, browsers, terminals, messaging apps, etc.
- **Visual & Audio Feedback**: Get instant confirmation when listening starts, stops, or if there's an error.
- **Zero Latency**: Browser runs persistently in the background - no startup delay when you press the hotkey.
- **Totally free**: Uses Chrome's built-in Web Speech API - so no paid services required.
- **No local models required**: The browser Web Speech API handles the transcription process entirely, so there's no need to install and setup models locally (like vosk or whisper).

---

# Installation

Before installing, make sure you have Linux (any distro), a desktop environment, a working mic, and either Chrome or Chromium installed. You can install voice-type via flatpak, npm, or the binary. Both via npm and the binary require you to manually install external system dependencies used by voice-type, namely `dotool` for typing into the system, and `paplay` for audio notifications. Flatpak installation is easier and more compatible across distros, and it automatically installs the mentioned system dependencies for you. Pick whichever method you prefer:

## Flatpak

```bash
git clone https://github.com/eriknovikov/voice-type.git
cd voice-type
chmod +x ./flatpak/build.sh && ./flatpak/build.sh
```

---

## Binary Installation

```bash
curl -fsSL https://github.com/eriknovikov/voice-type/releases/latest/download/install.sh | bash
```

## Via npm

```bash
npm install --global voice-type-cli@latest
```

---

## System dependencies

Only install these manually if you installed `voice-type` via npm or the binary:

- `dotool`: Used for typing into the system. On some distros, dotool is available on community repos: AUR for arch, COPR for fedora, etc. You can also install it from [dotool's source](https://git.sr.ht/~geb/dotool/). Installing from source is a bit complicated, only do this if your distro's community repo doesn't provide dotool (like ubuntu-debian).
- `paplay`: Used for audio notifications. Most distros have this pre-installed. If missing, install `pulseaudio-utils` (Ubuntu/Debian/Fedora) or `libpulse` (Arch).

# Uninstalling

## Flatpak

```bash
flatpak uninstall org.github.eriknovikov.voice-type
flatpak uninstall --unused
```

## npm

```bash
npm uninstall --global voice-type-cli
```

## Binary

```bash
sudo rm -rf /usr/local/share/voice-type /usr/local/bin/voice-type
```

# Troubleshooting

**Problem:** `dotool: command not found`

- **Solution:** Install dotool (see above)

**Problem:** `voice-type: command not found`

- **Solution:** Make sure `/usr/local/bin` is in your PATH. Add this to your `~/.bashrc` or `~/.zshrc`:
    ```bash
    export PATH="$PATH:/usr/local/bin"
    ```

**Problem:** Microphone not detected

- **Solution:** Check your system audio settings and ensure your microphone is properly connected

# Usage

## Set up Keyboard Shortcuts

Voice Type uses HTTP endpoints to control dictation. You'll need to bind these to keyboard shortcuts for easy access. We suggest F9 and F10, but you can use whatever you want.

| Key        | Action                     | Command                            |
| ---------- | -------------------------- | ---------------------------------- |
| F9         | Start dictation [required] | `curl http://127.0.0.1:3232/start` |
| F10        | Stop dictation [required]  | `curl http://127.0.0.1:3232/stop`  |
| Ctrl + F9  | Start daemon [recommended] | depends on installation. See below |
| Ctrl + F10 | Stop daemon [recommended]  | `curl http://127.0.0.1:3232/exit`  |

If you use GNOME, go to Settings -> Keyboard -> View and Customize Shortucts -> Custom Shortcuts. Else, check manually how to set up keyboard shortcuts in your Desktop Environment.

The start command to use is the same as the run command below.

```bash
# flatpak
flatpak run org.github.eriknovikov.voice-type

#npm or binary
voice-type
```

Then, start the daemon (F9 or via the terminal manually), move your cursor to any textbox within your system where you want to voice-type into. Press F9 (or your selected `START` key), speak whatever you wish, and text results will be inserted in real time. To stop listening, press F10 (or your selected `STOP` key).

## Customizable options

To see all the different options, pass `--help` or `-h` to the command. You can customize the following:

| Description | CLI Commands | Notes |
| --- | --- | --- |
| Language for speech recognition | `--language` \| `-l` | Default `en-US`. See [supported languages](https://github.com/eriknovikov/voice-type/blob/main/src/constants.ts) |
| Browser type to use | `--browser` \| `-b` | `chrome` or `chromium` |
| Custom browser path | `--browser_path` \| `-p` | For custom installations like `google-chrome-beta` or `google-chrome-dev` |
| Enable sound notifications | `--sound` \| `-s` |  |
| Disable text notifications | `--no-text` |  |
| Run in detached mode | `--detached` \| `-d` |  |

## Contributing

Voice Type is totally free and open source, and you can contribute in many ways! Feel free to report/fix a bug or issue, improve documentation, and suggest or add a new feature.
