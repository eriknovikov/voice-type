# Voice Type

System-wide speech-to-text for Linux. Press a key, speak, text appears wherever your cursor is.

Runs Chrome's Web Speech API in the background — no local models, no API keys, no startup delay. Works in editors, terminals, browsers. If a transcript corrects mid-sentence, Voice Type backspaces and retypes it.

**Requirements:** Linux desktop, microphone, Chrome or Chromium.

---

## Install

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | bash
```

The installer handles binary, dotool (built from source), browser detection, and config. Pin a version with `-- --version v3.1.0`.

## Keyboard shortcuts

Set these up in your desktop environment's keyboard settings (GNOME Settings → Keyboard, KDE System Settings → Shortcuts, etc.):

| Action            | Command                                                      |
| ----------------- | ------------------------------------------------------------ |
| Start/stop daemon | `sh -c "curl -s http://localhost:3232/exit \|\| voice-type"` |
| Dictate (English) | `curl -s http://localhost:3232/toggle?lang=en-US`            |
| Dictate (Spanish) | `curl -s http://localhost:3232/toggle?lang=es-ES`            |

Bind each command to whatever key you want. The installer prints these commands at the end.

## Usage

1. **Start daemon** — press your daemon shortcut (Chrome loads, mic off)
2. **Dictate** — press your toggle shortcut (speak, text appears live)
3. **Stop dictation** — press toggle again
4. **Stop daemon** — press daemon shortcut again

### Spoken punctuation (English)

On by default. Say `comma`, `period`, `question mark`, `new line`, etc. — they type as symbols/keys. Capitalization is automatic at sentence starts.

Toggle at runtime: `curl localhost:3232/togglePunctuation`

### Multiple languages

Bind a separate key to each language command above. Each toggles dictation in its own language — no restart needed.

[Full language list](https://github.com/eriknovikov/voice-type/blob/main/src/constants.ts)

## Config

`~/.config/voice-type.jsonc` — JSON with `//` comments. Written on first run.

```jsonc
{
    "port": 3232,
    "lang": "en-US",
    "browser_path": "/usr/bin/google-chrome",
    "stream": true,
    "sound": false,
    "text": false,
    "punctuation": true,
}
```

Restart the daemon after editing.

## Update

```bash
voice-type update
```

## Troubleshooting

**dotool** — Must be in `input` group: `sudo usermod -aG input $USER && newgrp input`

**Browser not found** — Set `browser_path` in config to your Chrome/Chromium path.

**Logs** — `~/.local/share/voice-type/logs/voice-type.log` (background) or stdout (terminal).

**`command not found`** — `export PATH="$PATH:/usr/local/bin"`

---

## Uninstall

```bash
./uninstall.sh
```

Or manually:

```bash
sudo rm /usr/local/bin/voice-type
rm -rf ~/.config/voice-type.jsonc ~/.config/voice-type.jsonc.bak ~/.local/share/voice-type
```

---

Want to know more about voice-type?

- [How I built Voice Type](https://dev.to/eriknovikov/how-i-built-voice-type-3i2p)
- [INTERNALS.md](./INTERNALS.md)
