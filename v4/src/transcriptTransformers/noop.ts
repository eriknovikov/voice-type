import type { TranscriptTransformerSession } from "./types.js"

export function createNoopTransformerSession(): TranscriptTransformerSession {
    return {
        transform(rawText: string) {
            return { text: rawText, commands: [] }
        },
        onSegmentFinalized() {
            return []
        },
        reset() {},
    }
}
