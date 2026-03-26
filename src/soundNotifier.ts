import { spawn } from "child_process"
import { dirname, join } from "path"
import { fileURLToPath } from "url"
// Get the sounds directory
function getSoundsDir(): string {
    const execPath = process.execPath

    // Flatpak detection
    if (process.env.FLATPAK_ID === "io.github.eriknovikov.VoiceType") {
        return "/app/share/sounds/voice-type"
    }

    // Development mode detection
    const isCompiledBinary = !process.execPath.endsWith("bun") && !process.execPath.endsWith("node")
    if (isCompiledBinary) {
        return "/usr/local/share/voice-type/sounds"
    }

    // 3. NPM Global Install & Local Dev Mode
    // This works natively in both Node.js (NPM users) and Bun (You developing locally)
    const currentFilePath = fileURLToPath(import.meta.url)
    const currentDir = dirname(currentFilePath)

    return join(currentDir, "../assets/sounds")
}

const SOUNDS = {
    START: join(getSoundsDir(), "start.oga"),
    STOP: join(getSoundsDir(), "stop.oga"),
    ERROR: join(getSoundsDir(), "stop.oga"),
}

/**
 * Handles sound notifications via paplay
 */
export class SoundNotifier {
    private enabled: boolean

    constructor(enabled: boolean = true) {
        this.enabled = enabled
    }

    private notify(audioPath: string) {
        if (!this.enabled || !audioPath) return

        const proc = spawn("paplay", [audioPath])

        proc.on("error", (err) => {
            console.error("[SoundNotifier] paplay error:", err)
        })
    }

    async notifyStart() {
        this.notify(SOUNDS.START)
    }

    async notifyStop() {
        this.notify(SOUNDS.STOP)
    }

    async notifyOffline() {
        this.notify(SOUNDS.ERROR)
    }

    async notifyError() {
        this.notify(SOUNDS.ERROR)
    }
}
