# Voice Type

System-wide Speech-to-Text for Linux. Press a key, speak, done — text lands wherever you're focused. Free, fast, private.

**How it works:** Chrome runs quietly in the background using its built-in Web Speech API. No local models, no paid services, no startup lag.

---

## Install

### Flatpak (Recommended)

Requires `flatpak-builder` on your system. The build script installs the required SDKs automatically.

```bash
git clone https://github.com/E-nkv/voice-type.git
cd voice-type
chmod +x ./flatpak/build.sh && ./flatpak/build.sh
```

### Binary

Requires: **Google Chrome**, **dotool**, **libnotify**, **pulseaudio-utils**

```bash
curl -fsSL https://github.com/E-nkv/voice-type/releases/latest/download/install.sh | bash
```

---

## Usage

Start the daemon once — it runs in the background and logs to the console.

```bash
flatpak run org.voice_type.VoiceType &  # Flatpak
voice-type &                             # Binary
```

Click into any text field, then use F9/F10 (or your bound shortcuts) to start and stop dictation.

```bash
curl http://127.0.0.1:3232/start  # start listening
curl http://127.0.0.1:3232/stop   # stop listening
```

Force kill if needed:

```bash
sh -c "flatpak kill org.voice_type.VoiceType 2>/dev/null; rm -rf /tmp/voice-type-browser"  # Flatpak
sh -c "pkill -f voice-type 2>/dev/null; rm -rf /tmp/voice-type-browser"                     # Binary
```

---

## Keyboard Shortcuts

Bind in your system settings: **F7** launch/restart, **F8** force close, **F9** start dictation, **F10** stop.

```bash
# F7 — launch or restart clean (Flatpak)
sh -c "flatpak kill org.voice_type.VoiceType 2>/dev/null; rm -rf /tmp/voice-type-browser; flatpak run org.voice_type.VoiceType"

# F7 — launch or restart clean (Binary)
sh -c "pkill -f voice-type 2>/dev/null; rm -rf /tmp/voice-type-browser; sleep 1; voice-type"

# F8 — force close (Flatpak)
sh -c "flatpak kill org.voice_type.VoiceType 2>/dev/null; rm -rf /tmp/voice-type-browser"

# F8 — force close (Binary)
sh -c "pkill -f voice-type 2>/dev/null; rm -rf /tmp/voice-type-browser"

curl http://127.0.0.1:3232/start  # F9 — start dictation
curl http://127.0.0.1:3232/stop   # F10 — stop dictation
```

---

## Uninstall

### Flatpak

```bash
flatpak uninstall --user org.voice_type.VoiceType
rm -rf ~/.var/app/org.voice_type.VoiceType .flatpak-builder flatpak/build flatpak/repo

# Optional — remove SDKs pulled in by Voice Type (flatpak will clean up dependencies)
flatpak uninstall --user org.electronjs.Electron2.BaseApp//24.08 org.freedesktop.Sdk.Extension.golang//24.08
```

### Binary

```bash
sudo rm /usr/local/bin/voice-type
sudo rm -rf /usr/local/share/voice-type
```

---

## Development

```bash
bun install && bun run ./src/index.ts
```

Contributions welcome — bugs, docs, features, PRs, all good.