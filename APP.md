# VOICE TYPE - Real-Time Speech-to-Text Daemon

## Overview

Voice Type is a system-wide speech-to-text dictation daemon for Linux that provides real-time transcription using Chrome's Web Speech API. It runs as a background service, allowing instant dictation into any focused application window with zero startup latency.

## Core Architecture

### System Components

#### 1. **CLI Interface (`src/cli.ts`)**
- **Purpose**: Command-line argument parsing and daemon control
- **Key Features**:
  - Language selection via `-l, --lang` (default: `en-US`)
  - Notification toggles: `--no-text`, `--sound`
  - Detached mode: `-d, --detached` for background operation
  - Help system with language support documentation
- **Language Support**: 40+ BCP47 language tags defined in `src/constants.ts`

#### 2. **Daemon Core (`src/daemon.ts`)**
- **Purpose**: Central orchestrator managing all system components
- **Key Responsibilities**:
  - HTTP server on port 3232 with endpoints:
    - `/start` - Begin transcription
    - `/stop` - Stop transcription
    - `/exit` - Shutdown daemon
  - Browser lifecycle management (persistent Chrome instance)
  - Speech update routing to typing controller
  - State management with cooldown protection
  - Notification coordination

#### 3. **Browser Integration (`src/browser.js`)**
- **Purpose**: Web Speech API wrapper injected into Chrome
- **Key Functions**:
  - `initWSA(lang)` - Initialize speech recognition with language
  - `startListening()` - Activate microphone
  - `stopListening()` - Deactivate microphone
- **Configuration**: Continuous mode, interim results, error handling

#### 4. **Typing Controller (`src/typingController.ts`)**
- **Purpose**: Real-time text injection with diff-based correction
- **Core Algorithm**:
  - Maintains previous text state
  - Calculates diff between speech updates
  - Applies "phantom backspace" for corrections
  - Types new text via `dotool` process
- **Diff States**:
  - `NoChange` - No action needed
  - `ChangeRes` - Partial update with backspace/type
  - `ChangeResAndClear` - Reset state (empty input)

#### 5. **Notification System**
- **Main Notifier (`src/notifier.ts`)**: Unified API for text and sound notifications
- **Text Notifier (`src/textNotifier.ts`)**: D-Bus notifications with:
  - Persistent session connection
  - Notification replacement via `replaces_id`
  - Advanced hints for real-time updates
  - Urgency levels: low, normal, critical
- **Sound Notifier (`src/soundNotifier.ts`)**: Audio feedback via `paplay`
  - Sound files: `start.oga`, `stop.oga`
  - Development/production mode detection

#### 6. **Supporting Components**
- **Logger (`src/logger.ts`)**: In-memory rotating logger with configurable buffer
- **Types (`src/types.ts`)**: Shared type definitions (DiffEnum, Urgency, CliFlags)
- **Constants (`src/constants.ts`)**: Language definitions and configuration

## Critical Design Patterns

### 1. **Zero-Latency Architecture**
- Persistent browser instance eliminates 2-3 second startup delay
- HTTP endpoints provide instant hotkey response
- Pre-initialized Web Speech API ready for immediate use

### 2. **Real-Time Text Correction**
- Diff algorithm compares current vs previous transcription
- Calculates common prefix to minimize keystrokes
- "Phantom backspace" allows AI self-correction mid-sentence

### 3. **Notification Replacement System**
- Uses D-Bus `replaces_id` to update existing notifications
- `transient` hint prevents history logging
- `x-canonical-private-synchronous` forces instant updates
- Creates real-time status indicator feel

### 4. **Modular Component Design**
- Separation of concerns: CLI, Daemon, Browser, Typing, Notifications
- Independent enable/disable of notification types
- Clean API boundaries between components

### 5. **Wayland Compatibility**
- Uses `dotool` for Wayland-compatible virtual input
- No X11 dependencies
- Works with GNOME on Wayland out of the box

## Data Flow

```
Hotkey (F9) → curl /start → Daemon HTTP Server → Browser startListening()
           ↓
Microphone → Web Speech API → Interim Results → handleSpeechUpdate()
           ↓
Text Diff Calculation → TypingController → dotool → System Input
           ↓
Notification Updates → D-Bus/paplay → User Feedback
```

## Key Technical Decisions

1. **Web Speech API over Local Models**: Leverages Chrome's optimized cloud transcription for accuracy and speed
2. **Persistent Browser**: Trade memory for zero-latency response
3. **HTTP over IPC**: Simple, debuggable, works with any hotkey system
4. **dotool for Input**: Wayland-compatible, kernel-level input simulation
5. **D-Bus Notifications**: Native system integration with real-time updates

## System Requirements

- **Linux** with desktop environment (GNOME preferred)
- **Google Chrome Stable** (`google-chrome-stable`)
- **dotool** for virtual keyboard input
- **paplay** for sound playback (PulseAudio/PipeWire)
- **D-Bus session bus** for notifications

## Configuration Points

- **Language**: 40+ supported via `-l` flag (see `src/constants.ts`)
- **Notifications**: Text (`--no-text`), Sound (`--sound`)
- **Operation Mode**: Foreground vs detached (`-d`)
- **HTTP Port**: Fixed at 3232 (localhost only)

## Technology Stack

### Core Runtime
- **Bun**: JavaScript runtime for TypeScript execution
- **Express.js**: HTTP server for hotkey endpoints (`/start`, `/stop`, `/exit`)
- **TypeScript**: Type-safe development with modern JavaScript features

### Transcription Engine
- **Google Chrome Stable**: Required for Web Speech API access
- **puppeteer-core**: Browser automation and communication bridge
- **Web Speech API**: Chrome's cloud-based speech recognition service

### System Integration
- **dotool**: Wayland-compatible virtual input via `/dev/uinput`
- **paplay**: Audio playback for sound notifications
- **D-Bus**: System notifications via `org.freedesktop.Notifications`
- **dbus-next**: Node.js D-Bus bindings for notification management

## Critical Implementation Details

### 1. **HTTP Server Design**
- **Port**: Fixed at 3232, localhost only for security
- **Endpoints**:
  - `GET /start` - Begin transcription with cooldown protection
  - `GET /stop` - Stop transcription with state cleanup
  - `GET /exit` - Graceful daemon shutdown
- **Error Handling**: HTTP status codes for operational states (503, 429, etc.)

### 2. **Browser Management**
- **Persistent Instance**: Single Chrome process for zero latency
- **Headless Mode**: `--headless=new` with optimized flags
- **Resource Isolation**: Dedicated user data directory
- **Communication**: `exposeFunction` for browser↔Node.js IPC

### 3. **Input Injection Strategy**
- **dotool Process**: Persistent child process for keyboard simulation
- **Layout Enforcement**: `DOTOOL_XKB_LAYOUT=us` for consistent key mapping
- **Unicode Support**: Hex input sequences for non-ASCII characters
- **Error Resilience**: Writable stream checks and fallback handling

### 4. **Notification Architecture**
- **Connection Pooling**: Single D-Bus session bus connection
- **Retry Logic**: Exponential backoff for connection failures
- **Health Checks**: Connection validation before notification attempts
- **Resource Cleanup**: Proper disconnection on shutdown

### 5. **State Management**
- **Cooldown Mechanism**: 100ms cooldown prevents rapid state transitions
- **Text State Tracking**: Previous text buffer for diff calculations
- **Browser Readiness**: Validation before transcription operations
- **Graceful Shutdown**: Signal handlers for SIGTERM/SIGINT

## Configuration & Deployment

### CLI Options
```
voice-type [options]
  -l, --lang <lang>       Web Speech API language (default: en-US)
  --no-text               Disable text notifications
  --sound                 Enable sound notifications
  -d, --detached          Run in background (detached mode)
  -h, --help              Show help message
```

### Hotkey Setup
```bash
# Start transcription (F9)
curl http://127.0.0.1:3232/start

# Stop transcription (F10)
curl http://127.0.0.1:3232/stop

# Shutdown daemon
curl http://127.0.0.1:3232/exit
```

### Installation Methods
1. **Flatpak**: Self-contained, includes all dependencies
2. **Binary**: System-wide installation via install script
3. **npm**: Global package installation (when published)

## Development Context

### Build System
- **Entry Point**: `src/index.ts` → compiled to `bin/voice-type`
- **TypeScript Config**: ES modules, strict type checking
- **Bundling**: `bun build --compile` for standalone binary

### Project Structure
```
voice-type/
├── src/
│   ├── index.ts          # Main entry point
│   ├── cli.ts            # Command-line interface
│   ├── daemon.ts         # Core orchestrator
│   ├── browser.js        # Web Speech API wrapper
│   ├── typingController.ts # Text injection engine
│   ├── notifier.ts       # Notification coordinator
│   ├── textNotifier.ts   # D-Bus notifications
│   ├── soundNotifier.ts  # Audio notifications
│   ├── logger.ts         # Rotating logger
│   ├── types.ts          # Type definitions
│   ├── constants.ts      # Language constants
│   └── tests/            # Manual test files
├── assets/
│   └── sounds/           # Notification sounds
├── flatpak/              # Flatpak packaging
└── package.json          # Dependencies and scripts
```

### Key Dependencies
- **Runtime**: bun, express, puppeteer-core, dbus-next
- **Development**: @types/express, bun-types, prettier, typescript
- **System**: google-chrome-stable, dotool, paplay

## Operational Characteristics

### Performance Profile
- **Startup Time**: ~2-3 seconds (browser initialization)
- **Transcription Latency**: <100ms for interim results
- **Memory Usage**: ~200MB (Chrome) + ~50MB (Node.js)
- **CPU Usage**: Minimal when idle, spikes during transcription

### Reliability Features
- **Auto-reconnection**: D-Bus and browser recovery on failure
- **Error Isolation**: Component failures don't crash entire daemon
- **Resource Cleanup**: Proper shutdown of all child processes
- **State Validation**: Prevents invalid operations (stop before start)

### Security Considerations
- **Localhost Only**: HTTP server binds to 127.0.0.1 only
- **No Authentication**: Intended for single-user desktop use
- **Microphone Access**: Requires user permission via Chrome
- **Input Simulation**: dotool requires appropriate permissions

## Integration Points

### External Systems
1. **Desktop Environments**: GNOME shortcuts, D-Bus notifications
2. **Audio Systems**: PulseAudio/PipeWire for sound playback
3. **Input Systems**: Linux kernel uinput via dotool
4. **Browser Ecosystem**: Chrome Web Speech API

### Extension Points
1. **Language Support**: Add new languages to `src/constants.ts`
2. **Notification Types**: Extend `Notifier` class with new methods
3. **Input Methods**: Alternative to dotool for different platforms
4. **Transcription Engines**: Swap Web Speech API for other services
