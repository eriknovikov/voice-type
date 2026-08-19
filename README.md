# Voice Type

System-wide speech-to-text for Linux. Press a key, speak, press again — the text
appears wherever your cursor is in the system. Super accurate and cheap.

# Why I built it

I needed high quality transcription, near real time, cheap or free, while using minimal system resources, that works mainly in Arch Linux and Fedora, both x64 and ARM64. Most solutions don't meet these requirements. 

# V4

Uses a hidden chromium instance to take advantage of Google's Speech to Text infrastructure. Install with 

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v4/install.sh | bash
```

Uninstall with

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v4/uninstall.sh | bash
```

# V5
Single Go Binary that records audio and sends to some OpenRouter voice model (parakeet v3 from Nvidia). Install with

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v5/install.sh | bash
```

Uninstall with

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v5/uninstall.sh | bash
```

Only one version can be installed at a time: both use the `voice-type` command,
`~/.config/voice-type.jsonc`, and port 3232, so installing one replaces the other.



