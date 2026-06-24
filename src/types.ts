import type { BrowserType } from "./browserLauncher.js"
import type { WSA_LANGUAGES } from "./constants.js"

export type Urgency = "low" | "normal" | "critical"

export type WSALanguage = (typeof WSA_LANGUAGES)[keyof typeof WSA_LANGUAGES]

export type SpeechEvent = { kind: "text"; text: string } | { kind: "segment-finalized" }

export interface ShortcutsConfig {
    daemon: string
    toggle: string
    languages?: Record<string, string>
}

export interface VoiceTypeConfig {
    port: number
    lang: string
    browser_type: BrowserType
    browser_path: string
    stream: boolean
    timeout: number
    sound: boolean
    text: boolean
    punctuation: boolean
    shortcuts: ShortcutsConfig
}
