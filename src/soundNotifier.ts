import { spawn } from "child_process"
import { delimiter, join, resolve } from "path"
import { existsSync } from "fs"
import { log } from "./logger.js"

const EVENT_IDS: Record<string, string> = {
    notifyStart: "dialog-error",
    notifyOffline: "dialog-error",
    notifyStop: "dialog-error",
    notifyError: "dialog-error",
}

const XDG_BASES: string[] = (() => {
    const bases: string[] = []
    const home = process.env.XDG_DATA_HOME || resolve(process.env.HOME || "/home", ".local/share")
    bases.push(join(home, "sounds"))
    const dirs = process.env.XDG_DATA_DIRS || "/usr/local/share/:/usr/share/"
    for (const d of dirs.split(":")) {
        const trimmed = d.trim()
        if (trimmed) bases.push(join(trimmed, "sounds"))
    }
    return bases
})()

type Player = "canberra" | "paplay" | null

function detectPlayer(): Player {
    if (isOnPath("canberra-gtk-play")) return "canberra"
    if (isOnPath("paplay")) return "paplay"
    return null
}

function isOnPath(cmd: string): boolean {
    const dirs = (process.env.PATH || "").split(delimiter)
    for (const dir of dirs) {
        if (existsSync(join(dir, cmd))) return true
    }
    return false
}

const PLAYER = detectPlayer()

const EXTENSIONS = ["disabled", "oga", "ogg", "wav"]

export function resolveXdgSound(name: string): string | null {
    return findSoundInBases(XDG_BASES, name, existsSync)
}

export function findSoundInBases(
    bases: string[],
    name: string,
    fileExists: (path: string) => boolean = existsSync,
): string | null {
    for (const base of bases) {
        const themed = join(base, "freedesktop", "stereo", name)
        for (const ext of EXTENSIONS) {
            const p = `${themed}.${ext}`
            if (ext === "disabled" && fileExists(p)) return null
            if (ext !== "disabled" && fileExists(p)) return p
        }
    }
    return null
}

/**
 * Handles sound notifications via canberra-gtk-play (tier 1) or paplay (tier 2)
 */
export class SoundNotifier {
    private enabled: boolean

    constructor(enabled: boolean = true) {
        this.enabled = enabled
    }

    private async notify(eventId: string): Promise<void> {
        if (!this.enabled) return Promise.resolve()
        if (PLAYER === "canberra") {
            return this.spawnNoThrow("canberra-gtk-play", [`--id=${eventId}`])
        }
        if (PLAYER === "paplay") {
            const path = resolveXdgSound(eventId)
            if (!path) return Promise.resolve()
            return this.spawnNoThrow("paplay", [path])
        }
        return Promise.resolve()
    }

    private spawnNoThrow(cmd: string, args: string[]): Promise<void> {
        return new Promise((resolve) => {
            const proc = spawn(cmd, args)

            proc.on("error", (err) => {
                log("SOUND", `${cmd} error:`, err)
                resolve()
            })

            proc.on("close", () => {
                resolve()
            })
        })
    }

    async notifyStart() {
        await this.notify(EVENT_IDS["notifyStart"])
    }

    async notifyStop() {
        await this.notify(EVENT_IDS["notifyStop"])
    }

    async notifyOffline() {
        await this.notify(EVENT_IDS["notifyOffline"])
    }

    async notifyError() {
        await this.notify(EVENT_IDS["notifyError"])
    }
}
