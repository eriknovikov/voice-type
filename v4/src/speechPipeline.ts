import type { TranscriptTransformerSession } from "./transcriptTransformers/types.js"
import type TypingController from "./typingController.js"
import type { SpeechEvent } from "./types.js"

export default class SpeechPipeline {
    constructor(
        private transformer: TranscriptTransformerSession,
        private typing: TypingController,
    ) {}

    onEvent(event: SpeechEvent): void {
        if (event.kind === "text") {
            const { text } = this.transformer.transform(event.text)
            this.typing.applyLiveText(text)
        } else {
            const commands = this.transformer.onSegmentFinalized()
            this.typing.finalizeSegment()
            for (const cmd of commands) {
                if (cmd.kind === "key") this.typing.sendKeyChord(cmd.chord)
            }
        }
    }
}
