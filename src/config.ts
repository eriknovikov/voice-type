import { homedir } from "node:os"
import { access, readFile, rename, writeFile, mkdir } from "node:fs/promises"
import { detectDefaultBrowser } from "./browserLauncher.js"
import { DEFAULT_LANGUAGE, isValidLanguage } from "./language.js"
import { PORT } from "./constants.js"
import type { VoiceTypeConfig, ShortcutsConfig } from "./types.js"

export function configFilePath(): string {
    const base = process.env.XDG_CONFIG_HOME || `${homedir()}/.config`
    return `${base}/voice-type.jsonc`
}

const DEFAULT_CONFIG: VoiceTypeConfig = {
    port: PORT,
    lang: DEFAULT_LANGUAGE,
    browser_type: "chrome",
    browser_path: "",
    stream: true,
    timeout: 0,
    sound: false,
    text: false,
    punctuation: true,
    shortcuts: {
        daemon: "F10",
        toggle: "F9",
        languages: {},
    },
}

export class ConfigParseError extends Error {
    filePath: string

    constructor(message: string, filePath: string) {
        super(message)
        this.name = "ConfigParseError"
        this.filePath = filePath
    }
}

export function stripJsoncComments(text: string): string {
    let out = ""
    let inString = false
    let escapeNext = false

    for (let i = 0; i < text.length; i++) {
        const ch = text[i]

        if (escapeNext) {
            out += ch
            escapeNext = false
            continue
        }

        if (inString) {
            out += ch
            if (ch === "\\") {
                escapeNext = true
            } else if (ch === '"') {
                inString = false
            }
            continue
        }

        if (ch === '"') {
            inString = true
            out += ch
            continue
        }

        if (ch === "/" && text[i + 1] === "/") {
            while (i < text.length && text[i] !== "\n") {
                i++
            }
            if (i < text.length) {
                out += text[i]
            }
            continue
        }

        out += ch
    }

    return out
}

function isValidShortcutsConfig(raw: unknown): raw is ShortcutsConfig {
    if (typeof raw !== "object" || raw === null) return false
    const obj = raw as Record<string, unknown>
    if (typeof obj.daemon !== "string" || obj.daemon.length === 0) return false
    if (typeof obj.toggle !== "string" || obj.toggle.length === 0) return false
    if (obj.languages !== undefined) {
        if (typeof obj.languages !== "object" || obj.languages === null) return false
        for (const [key, value] of Object.entries(obj.languages as Record<string, unknown>)) {
            if (typeof key !== "string" || key.length === 0) return false
            if (typeof value !== "string" || value.length === 0) return false
        }
    }
    return true
}

function warn(field: string, reason: string): void {
    console.error(`[config] ${field}: ${reason}`)
}

export function validateConfig(raw: unknown): VoiceTypeConfig {
    if (typeof raw !== "object" || raw === null) {
        console.error("[config] config file is not a JSON object")
        return { ...DEFAULT_CONFIG }
    }

    const obj = raw as Record<string, unknown>
    const result: VoiceTypeConfig = { ...DEFAULT_CONFIG, shortcuts: { ...DEFAULT_CONFIG.shortcuts } }

    if (typeof obj.port === "number" && Number.isInteger(obj.port) && obj.port >= 1024 && obj.port <= 65535) {
        result.port = obj.port
    } else if ("port" in obj) {
        warn("port", `invalid value, using default ${DEFAULT_CONFIG.port}`)
    }

    if (typeof obj.lang === "string" && isValidLanguage(obj.lang)) {
        result.lang = obj.lang
    } else if ("lang" in obj) {
        warn("lang", `invalid language, using default ${DEFAULT_CONFIG.lang}`)
    }

    if (obj.browser_type === "chrome" || obj.browser_type === "chromium") {
        result.browser_type = obj.browser_type
    } else if ("browser_type" in obj) {
        warn("browser_type", `must be "chrome" or "chromium", using default ${DEFAULT_CONFIG.browser_type}`)
    }

    if (typeof obj.browser_path === "string") {
        result.browser_path = obj.browser_path
    } else if ("browser_path" in obj) {
        warn("browser_path", `must be a string, using default`)
    }

    if (typeof obj.stream === "boolean") {
        result.stream = obj.stream
    } else if ("stream" in obj) {
        warn("stream", `must be boolean, using default ${DEFAULT_CONFIG.stream}`)
    }

    if (typeof obj.timeout === "number" && Number.isInteger(obj.timeout) && obj.timeout >= 0) {
        result.timeout = obj.timeout
    } else if ("timeout" in obj) {
        warn("timeout", `must be a non-negative integer, using default ${DEFAULT_CONFIG.timeout}`)
    }

    if (typeof obj.sound === "boolean") {
        result.sound = obj.sound
    } else if ("sound" in obj) {
        warn("sound", `must be boolean, using default ${DEFAULT_CONFIG.sound}`)
    }

    if (typeof obj.text === "boolean") {
        result.text = obj.text
    } else if ("text" in obj) {
        warn("text", `must be boolean, using default ${DEFAULT_CONFIG.text}`)
    }

    if (typeof obj.punctuation === "boolean") {
        result.punctuation = obj.punctuation
    } else if ("punctuation" in obj) {
        warn("punctuation", `must be boolean, using default ${DEFAULT_CONFIG.punctuation}`)
    }

    if (typeof obj.shortcuts === "object" && obj.shortcuts !== null && isValidShortcutsConfig(obj.shortcuts)) {
        result.shortcuts = {
            daemon: (obj.shortcuts as ShortcutsConfig).daemon,
            toggle: (obj.shortcuts as ShortcutsConfig).toggle,
            languages: (obj.shortcuts as ShortcutsConfig).languages ?? {},
        }
    } else if ("shortcuts" in obj) {
        warn("shortcuts", `invalid shortcuts config, using defaults`)
    }

    return result
}

function hasAllFieldsInvalid(raw: unknown): boolean {
    if (typeof raw !== "object" || raw === null) return true
    const obj = raw as Record<string, unknown>
    const fields = [
        "port",
        "lang",
        "browser_type",
        "browser_path",
        "stream",
        "timeout",
        "sound",
        "text",
        "punctuation",
        "shortcuts",
    ]
    const validated = validateConfig(raw)
    for (const f of fields) {
        if (obj[f] !== undefined) {
            const vk = f as keyof VoiceTypeConfig
            if (JSON.stringify(obj[f]) === JSON.stringify(validated[vk])) {
                return false
            }
        }
    }
    return true
}

async function generateDefaultConfig(): Promise<VoiceTypeConfig> {
    const config = { ...DEFAULT_CONFIG, shortcuts: { ...DEFAULT_CONFIG.shortcuts } }

    const detected = await detectDefaultBrowser()
    if (detected) {
        config.browser_path = detected.path
        config.browser_type = detected.type
    }

    const langEnv = (process.env.LANG ?? "").split(".")[0].replace(/_/g, "-")
    if (langEnv && isValidLanguage(langEnv)) {
        config.lang = langEnv
    }

    return config
}

export async function persistConfig(cfg: VoiceTypeConfig): Promise<void> {
    const filePath = configFilePath()
    const dir = filePath.substring(0, filePath.lastIndexOf("/"))
    await mkdir(dir, { recursive: true, mode: 0o700 })
    const content = JSON.stringify(cfg, null, 4) + "\n"
    const tmp = filePath + ".tmp"
    await writeFile(tmp, content, { encoding: "utf8", mode: 0o600 })
    await rename(tmp, filePath)
}

async function backupIfMissing(filePath: string): Promise<void> {
    const bakPath = filePath + ".bak"
    try {
        await access(bakPath)
    } catch (err: any) {
        if (err.code === "ENOENT") {
            await rename(filePath, bakPath)
        }
    }
}

export async function loadConfig(): Promise<VoiceTypeConfig> {
    const filePath = configFilePath()

    let rawText: string
    try {
        rawText = await readFile(filePath, "utf8")
    } catch (err: any) {
        if (err.code === "ENOENT") {
            const config = await generateDefaultConfig()
            await persistConfig(config)
            console.error(
                "[voice-type] CLI flags moved to ~/.config/voice-type.jsonc — a default config has been written there.",
            )
            return config
        }
        console.error(`[config] could not read config file: ${(err as Error).message}`)
        return await generateDefaultConfig()
    }

    let parsed: unknown
    try {
        const stripped = stripJsoncComments(rawText)
        parsed = JSON.parse(stripped)
    } catch (err: any) {
        if (err instanceof ConfigParseError) {
            console.error(`[config] ${err.message}`)
        } else {
            console.error(`[config] could not parse config file: ${(err as Error).message}`)
        }
        await backupIfMissing(filePath)
        const defaultCfg = await generateDefaultConfig()
        await persistConfig(defaultCfg)
        return defaultCfg
    }

    const validated = validateConfig(parsed)

    if (hasAllFieldsInvalid(parsed)) {
        console.error("[config] all config fields are invalid — backing up and writing default")
        await backupIfMissing(filePath)
        const defaultCfg = await generateDefaultConfig()
        await persistConfig(defaultCfg)
        return defaultCfg
    }

    return validated
}
