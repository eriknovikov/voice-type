import { describe, expect, test } from "bun:test"
import { shouldAcceptSpeechEvent } from "../src/speechEventGate.js"
import { createTranscriptTransformerSession } from "../src/transcriptTransformers/index.js"
import SpeechPipeline from "../src/speechPipeline.js"
import TypingController from "../src/typingController.js"
import { FakeTypingTarget } from "./helpers.js"

describe("shouldAcceptSpeechEvent", () => {
    test("accepts while listening and not stopped", () => {
        expect(shouldAcceptSpeechEvent(true, false)).toBe(true)
    })

    test("rejects when not listening or stopped", () => {
        expect(shouldAcceptSpeechEvent(false, false)).toBe(false)
        expect(shouldAcceptSpeechEvent(false, true)).toBe(false)
        expect(shouldAcceptSpeechEvent(true, true)).toBe(false)
    })
})

describe("speech guard integration", () => {
    test("stopped typing controller blocks further pipeline events", () => {
        const target = new FakeTypingTarget()
        const transformer = createTranscriptTransformerSession("en-US", true)
        transformer.reset()
        const typing = new TypingController(target.sink)
        const pipeline = new SpeechPipeline(transformer, typing)

        if (shouldAcceptSpeechEvent(true, false)) {
            pipeline.onEvent({ kind: "text", text: "hello" })
        }
        typing.hasStopped = true
        if (shouldAcceptSpeechEvent(true, typing.hasStopped)) {
            pipeline.onEvent({ kind: "text", text: " world" })
        }
        expect(target.screen).toBe("Hello")
    })
})
