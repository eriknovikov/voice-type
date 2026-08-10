import { WSA_LANGUAGES } from "./constants.js"

export const DEFAULT_LANGUAGE = "en-US"

export function isValidLanguage(lang: unknown): lang is string {
    return typeof lang === "string" && (Object.values(WSA_LANGUAGES) as string[]).includes(lang)
}

export function readLanguageQuery(query: Record<string, unknown>): string | undefined {
    const raw = query.language ?? query.lang
    if (typeof raw !== "string") return undefined
    const trimmed = raw.trim()
    return trimmed.length > 0 ? trimmed : undefined
}
