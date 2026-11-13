# Voice Type

System-wide speech to text app. Press a hotkey, speak, press it again, then what you said lands instantly where your cursor is. Free, fast, accurate, private, and cool. 

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/install.sh | sh
```


Voice Type installs system-wide only. There is no per-user installation, so
exactly one binary and one launcher can exist on a machine.

<details>
<summary>What does it do?</summary>
It installs everything for you : the distro package at `/usr/bin/voice-type`, a launcher entry, an icon and
docs, with GTK4, WebKitGTK 6.0 and IBus as dependencies. `sudo` is needed for installing the packages and dependencies, but Voice Type itself never uses sudo. You can always uninstall with the uninstall script, removing everything that Voice Type added to your system. 

</details>



## Contributing

If you encounter any problems, open up an issue or drop a PR. If so, please attach your system diagnostics (Go to App -> General -> Diagnostics, or run `voice-type doctor` in the terminal). 

## Disclaimer

Despite the time and effort (almost a year) I've put into Voice Type, I would genuinely recommend you used [hex from anomaly](https://github.com/anomalyco/hex). It's open source as well, though many times better. As a solo dev I can't, nor expect to compete with such a big, resourceful, and talented group of people. Still, I'd love your feedback on Voice Type!


## Uninstall

```sh
curl -fsSL https://raw.githubusercontent.com/eriknovikov/voice-type/main/uninstall.sh | sh
```

<details>
<summary>What does it do?</summary>

Removes the package, launcher entry, icon, docs, autostart
entry and the GNOME shortcut it added. Settings, models and logs are also removed.

</details>
