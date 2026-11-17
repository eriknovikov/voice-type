# Voice Type

System-wide speech to text app. Press a hotkey, speak, press it again, then what you said lands instantly where your cursor is. Free, fast, accurate, private, and cool.


## Install on Linux

```sh
curl -fsSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | sh
```


<details>
<summary>What does it do?</summary>
It installs everything for you : the distro package at `/usr/bin/voice-type`, a launcher entry, an icon and
docs, with GTK4, WebKitGTK 6.0 and IBus as dependencies. `sudo` is needed for installing the packages and dependencies, but Voice Type itself never uses sudo. You can always uninstall with the uninstall script, removing everything that Voice Type added to your system. 

</details>

## Requirements

Linux (x86_64/ARM64), GTK4, WebKitGTK 6.0, IBus, and PulseAudio or PipeWire-Pulse. The installer resolves package dependencies. Use an API key for cloud transcription or download a model for local transcription.

## macOS preview

Mac previews target macOS 13+ with separate Apple Silicon (`arm64`) and Intel
(`amd64`) DMGs. Choose a **Mac Preview** on [Releases](https://github.com/eriknovikov/voice-type/releases).
These builds are self-signed, **not Apple-notarized**, and still need installed
device testing. No Apple membership, Git or developer tools are needed to install.
Do not use the Linux install or uninstall scripts on macOS.

Testable preview: [Apple Silicon (M-series)](https://github.com/eriknovikov/voice-type/releases/download/v6.4.0-mac.preview.6/voice-type_6.4.0-mac.preview.6_darwin_arm64.dmg)
or [Intel](https://github.com/eriknovikov/voice-type/releases/download/v6.4.0-mac.preview.6/voice-type_6.4.0-mac.preview.6_darwin_amd64.dmg).
Both native builds, self-signatures and download checksums have been verified;
real microphone, permission and insertion acceptance is still pending.

Open the matching DMG, drag Voice Type to Applications and launch it there. If
macOS blocks it, use **System Settings → Privacy & Security → Open Anyway** for
this app and confirm. Do not disable Gatekeeper or install a root certificate.
Managed Macs may block this approval. Allow
Microphone and Accessibility access from General. No Screen Recording or Input
Monitoring access is needed. The default shortcut is Control-Option-Space.
After setup, ready launches stay in the menu bar; reopen the app or use its menu
to show settings. Start at login is explicit and respects the OS's effective state.

Direct insertion leaves the clipboard unchanged but is not compatible with every
app. Protected, read-only and unknown fields are refused. Paste compatibility is
opt-in, exposes dictated text to other apps/clipboard managers, and cannot be used
with live insertion. Restoration is ownership-checked, not guaranteed against
concurrent clipboard changes. Submitted events do not prove visible delivery.

For updates, download the matching newer DMG, quit and replace the app in
Applications. The bundled CLI (`"/Applications/Voice Type.app/Contents/MacOS/voice-type" update`)
checks stable releases only, not previews. Get preview updates from Releases.
It never runs the Linux installer or replaces the app automatically. Permissions
may need approval again after updates; retention has not yet been verified.

Settings, models, backups and optional file credentials are in
`~/Library/Application Support/Voice Type/`; logs are in `~/Library/Logs/Voice Type/`.
API keys default to Keychain. Development builds use separate `Voice Type Dev`
folders and Keychain entries. Recovery audio/transcript paths appear in failure
messages and are not uploaded automatically.

To remove the Mac app, disable Start at login, quit and move Voice Type.app to
Trash. Data is kept. To erase it too, first delete provider keys in API Keys (or
remove the `com.eriknovikov.voice-type` entries in Keychain Access), then delete
the two folders above and any recovery files you chose to keep. Remove permission
entries in System Settings if wanted. Deleting the app alone does not erase keys.

### Building and release gates

On a Mac, select Xcode with its command-line tools (CI uses 26.6 on Apple Silicon
and 16.4 on Intel), Go from `go.mod`, Node 24.15+,
and `cmake`, `pkgconf` and `shellcheck`. Use a case-sensitive APFS checkout, as CI
does: otherwise `VERSION` shadows C++'s `<version>` header on macOS. Run
`make check build mac-verify`.
`build/Voice Type.app` is ad-hoc signed for local testing, not distribution.
`make dev` uses a separate development identity.

`make mac-release` requires `MACOS_KEYCHAIN` containing the private signing identity
matching `packaging/macos/signing-cert.pem`. It self-signs the app and DMG, pins
the signing certificate/designated requirement, checks mounted contents and writes
the DMG/checksum to `build/macos-release/`. It does not notarize or claim Gatekeeper
acceptance. There is no unsigned/ad-hoc distribution fallback. Both native
architectures and installed device checks must pass before release readiness.

CI tests both architectures without signing credentials. Release signing runs only
on `main` or `feat/mac` behind the `macos-release` GitHub environment, using the
encrypted certificate/password secrets declared in `.github/workflows/macos.yml`.
Never paste private credentials into source or issues. The Mac Preview workflow
publishes both verified DMGs as a prerelease, never Latest, without changing VERSION.
Stable publishing still requires both Mac DMGs and all five Linux packages.
For a packaging-only retry, maintainers can set `MACOS_PREVIEW_NATIVE_RUN` to a
prior preview run. Reuse requires both native gates to have passed, unchanged app/
build sources and matching binary source stamps. Clear the variable after recovery.

## Contributing

Found a problem? Open an issue or send a PR. For bug reports, open **General → Open diagnostics → Copy to clipboard** and attach the report, or run `voice-type doctor` in a terminal.

## Disclaimer

Despite the time and effort (almost a year) I've put into Voice Type, I would genuinely recommend you used [hex from anomaly](https://github.com/anomalyco/hex). It's open source as well, though many times better. As a solo dev I can't, nor expect to compete with such a big, resourceful, and talented group of people. Still, I'd love your feedback on Voice Type!


## Uninstall on Linux

```sh
curl -fsSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/uninstall.sh | sh
```

<details>
<summary>What does it do?</summary>

Removes the package, launcher entry, icon, docs, autostart
entry and the GNOME shortcut it added. Settings, models and logs are also removed.

</details>
