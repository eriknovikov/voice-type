import { spawn } from "child_process"
import { join } from "path"

// Get the sounds directory
function getSoundsDir(): string {
    const execPath = process.execPath
    const isDevMode = execPath.endsWith("bun") || execPath.endsWith("node")

    if (isDevMode) {
        // In development, use local assets
        return join(process.cwd(), "assets/sounds")
    }

    // In production, use system-wide assets
    return "/usr/local/share/voice-type/sounds"
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

    async notifyMicStart() {
        this.notify(SOUNDS.START)
    }

    async notifyMicStop() {
        this.notify(SOUNDS.STOP)
    }

    async notifyOffline() {
        this.notify(SOUNDS.ERROR)
    }

    async notifyError() {
        this.notify(SOUNDS.ERROR)
    }
}
