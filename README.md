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

A single static Go binary that records audio and sends it to OpenRouter. New
configurations use Microsoft MAI-Transcribe-2 with Azure phrase-list vocabulary
and clean transcription; retryable provider failures fall back to OpenAI GPT
Transcribe for that dictation. MAI-Transcribe-2 is currently a public-preview
service without an SLA, which is why the fallback remains enabled.

Existing configurations keep their explicitly selected model during upgrades.
Run `voice-type models` to see the available models, vocabulary support, prices,
and fallback policy. Workspace-specific vocabulary can be selected with
`voice-type vocab set <name>`.

Install with

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v5/install.sh | bash
```

Uninstall with

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v5/uninstall.sh | bash
```

Only one version can be installed at a time: both use the `voice-type` command,
`~/.config/voice-type.jsonc`, and port 3232, so installing one replaces the other.


