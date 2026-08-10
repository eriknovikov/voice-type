import { createNoopTransformerSession } from "./noop.js"
import type { TranscriptTransformerSession } from "./types.js"

// Spanish spoken-punctuation transformer — stub; not wired in createTranscriptTransformerSession().

export function createSpanishTransformerSession(): TranscriptTransformerSession {
    return createNoopTransformerSession()
}
