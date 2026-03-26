# Security

## Permissions

Voice Type requires the following permissions to function:

### Microphone Access
- **Purpose**: Speech-to-text transcription using Chrome's Web Speech API
- **When Active**: Microphone is accessed ONLY when you explicitly start listening (via hotkey)
- **Data Flow**: Audio is sent to Google's Web Speech API service for transcription
- **Privacy**: No audio is stored locally or transmitted when not listening

### /dev/uinput Access (via dotool)
- **Purpose**: Virtual keyboard input to type transcribed text into any focused window
- **How It Works**: dotool creates virtual keyboard events at the kernel level
- **Scope**: Can type into any application window you have focused
- **Wayland Compatible**: Works with both X11 and Wayland display servers

### Network Access
- **Purpose**: Required for Chrome's Web Speech API (cloud-based transcription)
- **Connection**: Direct connection to Google's speech recognition servers
- **No Telemetry**: No additional data collection or analytics

### Desktop Notifications
- **Purpose**: Display transcription status (listening/stopped)
- **Optional**: Can be disabled with `--no-text` flag

### Filesystem Access
- **xdg-config**: User configuration storage
- **xdg-cache**: Cache storage for browser data

## Security Model

- **Localhost Only**: HTTP server binds to 127.0.0.1, no network exposure
- **No Authentication**: Designed for single-user desktop environments
- **User Control**: Microphone access is explicitly started/stopped by user action
- **Open Source**: All code is publicly auditable on GitHub

## What Voice Type Does NOT Do

- Does not record audio when not listening
- Does not store audio files locally
- Does not send data to third-party services (only Google's Web Speech API)
- Does not access files outside of xdg-config and xdg-cache
- Does not run with elevated privileges
- Does not collect telemetry or usage statistics
