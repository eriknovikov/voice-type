export type DotoolKeyChord = "ctrl+enter"

export type TranscriptCommand = { kind: "key"; chord: DotoolKeyChord }

export type TransformResult = {
    text: string
    /** Key chords emitted after segment finalization (e.g. standalone "control enter"). */
    commands: TranscriptCommand[]
}

export interface TranscriptTransformerSession {
    transform(rawText: string): TransformResult
    onSegmentFinalized(): TranscriptCommand[]
    reset(): void
}
