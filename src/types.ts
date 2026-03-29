import type { BrowserType } from "./browserLauncher"
import type { WSA_LANGUAGES } from "./constants"

export enum DiffEnum {
    NoChange = "NO_CHANGE",
    ChangeRes = "CHANGE_RES",
    ChangeResAndClear = "CHANGE_RES_AND_CLEAR",
}

export type Urgency = "low" | "normal" | "critical"

export type WSALanguage = (typeof WSA_LANGUAGES)[keyof typeof WSA_LANGUAGES]

export interface CliFlags {
    lang: WSALanguage
    textNotifs: boolean
    soundNotifs: boolean
    browser: BrowserType
    browserPath?: string
    detached: boolean
    help: boolean
}
