import { createEnglishTransformerSession } from "./en.js"
import { createNoopTransformerSession } from "./noop.js"
import type { TranscriptTransformerSession } from "./types.js"

export function createTranscriptTransformerSession(
    language: string,
    streamEnabled: boolean,
): TranscriptTransformerSession {
    if (language.startsWith("en-")) return createEnglishTransformerSession(streamEnabled)
    // es-*, fr-*, etc. use noop until a language-specific transformer is wired here.
    return createNoopTransformerSession()
}

export type { TranscriptTransformerSession } from "./types.js"
