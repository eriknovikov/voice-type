import type { SpeechEvent } from "../src/types.js"

export type StreamingSegment = {
    interims: string[]
    final: string
}

/** Events for one WSA segment in streaming mode (interims, then final text, then finalize). */
export function streamingSegmentEvents(segment: StreamingSegment): SpeechEvent[] {
    const events: SpeechEvent[] = segment.interims.map((text) => ({ kind: "text", text }))
    events.push({ kind: "text", text: segment.final })
    events.push({ kind: "segment-finalized" })
    return events
}

/** Events for non-stream mode: one final chunk per segment. */
export function nonStreamSegmentEvents(final: string): SpeechEvent[] {
    return [{ kind: "text", text: final }, { kind: "segment-finalized" }]
}

export function normCase(text: string): string {
    return text.toLowerCase()
}
