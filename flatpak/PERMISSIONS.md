# Flatpak Permissions

## `--device=all`
**Required for:**
- Microphone access (Web Speech API)
- dotool virtual keyboard input (/dev/uinput)

## Other Permissions
| Permission | Purpose |
|------------|----------|
| `--share=network` | Web Speech API |
| `--socket=pulseaudio` | Sound notifications |
| `--talk-name=org.freedesktop.Notifications` | Desktop notifications |
| `--talk-name=org.freedesktop.Flatpak` | Host Chrome access |
| `--share=ipc` | IPC communication |
| `--socket=x11` | X11 display |
| `--socket=wayland` | Wayland display |
| `--device=dri` | GPU acceleration |
| `--filesystem=xdg-config` | User config |
| `--filesystem=xdg-cache` | Cache storage |

All permissions are essential for Voice Type to function as a system-wide speech-to-text daemon.
