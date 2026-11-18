<p align="center">
  <img src="icon.png" alt="Voice Type logo" width="128" height="128">
</p>

# Voice Type

System-wide speech to text app. Press a hotkey, speak, press it again, then what you said lands instantly where your cursor is. Free, fast, accurate, private, and cool.

## Why use it?

There are other apps for dictation, like [hex from anomaly](https://github.com/anomalyco/hex) . If you care mostly about a nice UX, and offline models, then I definitely suggest you use hex instead. I personally use Voice Type because of memory constraints (I work with 8GB of RAM laptops, so having 1GB for a local model isn't viable), and the Voice Type's integration with openai's gpt-transcribe model is incredible accurate and fast. You'd need an openai (or openrouter) api key for this, which costs cents per month even with heavy usage, but is a tradeoff I'm perfectly OK with. 
Either way, I suggest you try a few of these dictation apps, and keep the one you like the most.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | sh
```


<details>
<summary>What does it do?</summary>
It installs everything for you : the distro package at `/usr/bin/voice-type`, a launcher entry, an icon and
docs, with GTK4, WebKitGTK 6.0 and IBus as dependencies. `sudo` is needed for installing the packages and dependencies, but Voice Type itself never uses sudo. You can always uninstall with the uninstall script, removing everything that Voice Type added to your system. 

</details>

## Requirements

Linux (x86_64/ARM64), GTK4, WebKitGTK 6.0, IBus, and PulseAudio or PipeWire-Pulse. The installer resolves package dependencies, plus an API key for cloud transcription or download a model for local transcription.

## Contributing

Found a problem? Open an issue or send a PR. For bug reports, open **General → Open diagnostics → Copy to clipboard** and attach the report, or run `voice-type doctor` in a terminal.


## Uninstall

```sh
curl -fsSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/uninstall.sh | sh
```

<details>
<summary>What does it do?</summary>

Removes the package, launcher entry, icon, docs, autostart
entry and the GNOME shortcut it added. Settings, models and logs are also removed.

</details>
