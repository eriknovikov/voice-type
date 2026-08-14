# Voice Type

System-wide speech-to-text for Linux. Press a key, speak, press again — the text
appears wherever your cursor is in the system. Super accurate and cheap.

# Straight to the point 
Nobody wants to read a __long-as-f** README__, let alone an AI-generated one. So these will be fully my words.

## Why would you care?
You can speak MUCH faster than what you can type, that's a fact. So if you 

1. Want to be more productive (e.g: waste less time typing) or;
2. Want less wrist pain

Then definitely give voice typing a try.


## Why voice-type specifically?
I genuinely don't care whether you use `voice-type`, something else, or nothing at all. Most devs are too obsessed about creating something (i.e: tech for tech's sake). Don't be like that. Always prefer solving a real problem. That's what I did. 
There are other alternatives to `voice-type`, but given my constraints, I found no effective solution. So I built it.

My constraints: 
1. Linux (Arch Linux and Fedora) on GNOME Wayland
2. Low RAM (at least, not willing to allocate more than 1GB of RAM to a local model)
3. Need for high quality transcription (a shitty model misinterprets everything, to the point that you would have been better off just typing yourself)
4. Cost effective / free (__voice-type v5 costs me pennies/mo__)

## Alternatives out there?
- There are a few, though not satisfying all the requirements above. Google / Chatgpt them if you wish.


## How Voice Type V5 works
1. Single go binary (daemon). While idle, it uses ~6 MB of RAM. On start, it uses the system's mic via [github.com/jfreymuth/pulse](https://github.com/jfreymuth/pulse). 
2. Audio is then sent to openrouter, model __parakeet-tdt-0.6b-v3__, though customizable. 
3. Provider returns result, we insert it into the system via [github.com/bendahl/uinput](https://github.com/bendahl/uinput), which is the library that dotool uses under the hood. 

It has no notifications at all (there's no need for them). You know if you are transcribing if you press the TRANSCRIPTION_TOGGLE_KEY and see the system mic icon ON. V4's approach of notifying via text and sound was redundant, and definitely not needed. Less is more.

__How to use it__: Install it, [get an API KEY on openrouter.ai](https://openrouter.ai/), configure the hotkeys for toggling the daemon (F10) and toggling the transcription (F9) depending on your system. Then:
Press F10 (daemon starts); Press F9 (transcription starts); Talk what you want; Press F9 (transcription stops and text is inserted into the system). 

```bash
curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | sh
```

## Voice Type V4 (deprecated!!)
Before V5, the overall architecture was a headless browser instance (~300 MB of RAM idle) (chrome/chromium) would capture the audio, send it in real time to Google's servers via browser's Web Speech Api (WSA), then stream the responses to the bun daemon in real time. This daemon would then make the corresponding calls to insert the text into the system by emulating a virtual keyboard on input (via dotool). This allowed for streaming the text in real time, which is a nice to have. Main issues: transcription quality and no automatic punctuation. Kinda sucks.

__How to use it__: Similar to v5. 
 ```bash
 curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/v4/install.sh | sh
 ```


## Keyboard shortcuts

Bind these in your desktop's keyboard settings (GNOME Settings → Keyboard, KDE
System Settings → Shortcuts):

| Action            | Command                                                      |
| ----------------- | ------------------------------------------------------------ |
| TRANSCRIPTION TOGGLE           | `curl -s http://localhost:3232/toggle`                       |
| DAEMON TOGGLE | `sh -c "curl -s http://localhost:3232/exit \|\| voice-type"` |


## FAQ

1. Where's the config file? `~/.config/voice-type.jsonc`
2. Uninstall v5? `curl -sSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/uninstall.sh | sh`
3. Something else? ChatGPT/Claude the issue, or [DM me on telegram](https://t.me/erik_nkv) if you want. 



