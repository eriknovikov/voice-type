# What's Voice Type?

A system-wide Speech-to-Text for Linux. Press a key, speak, done — text lands wherever you're focused. Totally free, with unmatched speed and accuracy.

**How it works:** Chrome runs quietly in the background using its built-in Web Speech API. No local models, no paid services, no startup lag.

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

## Prerequisites

Before installing Voice Type, make sure you have:

- **Linux** - any distribution
- **A desktop environment** - GNOME preferred, but any DE works
- **Google Chrome** - required for the Web Speech API
- **A working microphone**

## Via Flatpak (Universal Compatibility)

Voice Type is available as a Flatpak for maximum compatibility across all Linux distributions and Desktop Environments. Due to the self-contained and isolated nature of flatpaks, it is the most compatible and easiest installation method, so no need to manually install system dependencies like dotool !

Publishing to flathub is on the way. for now, clone the repo and build the flatpak manually.

```bash
git clone https://E-nkv/voice-type
cd voice-type
chmod +x ./flatpak/build.sh && ./flatpak/build.sh
```

---

## Via npm

THIS DOESNT WORK FOR NOW, GOTTA PUBLISH FIRST TO NPM!

```bash
npm install --global voice-type@latest
```

## Binary Installation

The script takes care of downloading and setting up everything for you, and will let you know which system dependencies are missing, including installation instructions for these deps depending on your distro.

```bash
curl -fsSL https://github.com/E-nkv/voice-type/releases/latest/download/install.sh | bash
```

### Required System Dependencies

Whether you use the binary directly or via npm, VoiceType requires `google-chrome-stable`, `dotool`, and `paplay` installed in the system. Their installations depend on your distro, so install them manually.

**Note about dotool:** On some distributions, `dotool` is available in community or third-party repositories (COPR for Fedora, AUR for Arch, etc.). If not available in your distro's repos (like Ubuntu-Debian), you'll need to build it from source. See [dotool source](https://sr.ht/~geb/dotool/) for instructions. If this sounds like too much of a hassle, just use the flatpak.

---

## Uninstalling

### Flatpak

MISSING DOC FOR NOW

### npm

MISSING DOC FOR NOW

### Binary

```bash
sudo rm -rf /usr/local/share/voice-type /usr/local/bin/voice-type
```

## Troubleshooting

**Problem:** `dotool: command not found`

- **Solution:** Install dotool using your package manager (see above)

**Problem:** `voice-type: command not found`

- **Solution:** Make sure `/usr/local/bin` is in your PATH. Add this to your `~/.bashrc` or `~/.zshrc`:
    ```bash
    export PATH="$PATH:/usr/local/bin"
    ```

**Problem:** Microphone not detected

- **Solution:** Check your system audio settings and ensure your microphone is properly connected

**Problem:** Chrome not found

- **Solution:** Install Google Chrome (not Chromium). Voice Type requires Chrome for the Web Speech API. `google-chrome-stable`.

# Usage

## Set up Keyboard Shortcuts

Voice Type uses HTTP endpoints to control dictation. You'll need to bind these to keyboard shortcuts for easy access.

| Key | Action          | Command                            |
| --- | --------------- | ---------------------------------- |
| F9  | Start dictation | `curl http://127.0.0.1:3232/start` |
| F10 | Stop dictation  | `curl http://127.0.0.1:3232/stop`  |

If you use GNOME, go to Settings -> Keyboard -> View and Customize Shortucts -> Custom Shortcuts. Else, check manually how to set up keyboard shortcuts in your Desktop Environment.

Then, run the daemon, depending on how you installed it.

```bash
#flatpak
flatpak run org.voice_type.VoiceType

#npm
NPM_COMMAND

#binary
voice-type
```

To see all the different options, pass --help or -h to the command. You can customize the language, whether to disable sounds and text notifications for when the daemon or mic starts / stops, and whether to run wraith-type in dettached mode.

After starting the daemon, move your cursor to any textbox within your system where you want to VoiceType into. then hit F9, speak whatever you wish, and text results will be inserted in real time. to stop listening, press F10.

You might also find useful to have keyboard shortcuts for starting or stopping the daemon, but these are optional and totally up to you.

## Contributing

Voice Type is totally free and open source, and you can contribute in many ways! Feel free to report/fix a bug or issue, improve documentation, and suggest or add a new feature.
