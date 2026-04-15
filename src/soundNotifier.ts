import { spawn } from "child_process"
import { dirname, join } from "path"
import { fileURLToPath } from "url"
// Get the sounds directory
function getSoundsDir(): string {
    // Flatpak detection
    if (process.env.FLATPAK_ID === "org.github.eriknovikov.VoiceType") {
        return "/app/share/sounds/voice-type"
    }

    // if not ran via bun nor node, then its the binary
    const isCompiledBinary = !process.execPath.endsWith("bun") && !process.execPath.endsWith("node")
    if (isCompiledBinary) {
        return "/usr/local/share/voice-type/sounds"
    }

    // 3. NPM Global Install & Local Dev Mode
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

    private async notify(audioPath: string): Promise<void> {
        if (!this.enabled || !audioPath) return Promise.resolve()

        return new Promise((resolve) => {
            const proc = spawn("paplay", [audioPath])

            proc.on("error", (err) => {
                console.error("[SoundNotifier] paplay error:", err)
                resolve()
            })

            proc.on("close", () => {
                resolve()
            })
        })
    }

    async notifyStart() {
        await this.notify(SOUNDS.START)
    }

    async notifyStop() {
        await this.notify(SOUNDS.STOP)
    }

    async notifyOffline() {
        await this.notify(SOUNDS.ERROR)
    }

    async notifyError() {
        await this.notify(SOUNDS.ERROR)
    }
}
