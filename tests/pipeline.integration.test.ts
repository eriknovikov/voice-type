import { describe, expect, test } from "bun:test"
import { createTranscriptTransformerSession } from "../src/transcriptTransformers/index.js"
import SpeechPipeline from "../src/speechPipeline.js"
import TypingController from "../src/typingController.js"
import type { SpeechEvent } from "../src/types.js"
import { FakeTypingTarget } from "./helpers.js"
import { normCase, nonStreamSegmentEvents, streamingSegmentEvents } from "./streamingHelpers.js"

function runPipeline(stream: boolean, lang: string, segments: SpeechEvent[][]) {
    const target = new FakeTypingTarget()
    const transformer = createTranscriptTransformerSession(lang, stream)
    transformer.reset()
    const typing = new TypingController(target.sink)
    const pipeline = new SpeechPipeline(transformer, typing)

    const screens: string[] = []
    for (const segment of segments) {
        for (const event of segment) {
            pipeline.onEvent(event)
            screens.push(target.screen)
        }
    }
    return { screen: target.screen, chords: target.chords, screens }
}

describe("SpeechPipeline integration", () => {
    test("the text + pause + hello with multi-interim (regression)", () => {
        const { screen, screens } = runPipeline(true, "en-US", [
            streamingSegmentEvents({ interims: ["the", "the text"], final: "the text" }),
            streamingSegmentEvents({ interims: ["h", "he", "hel", "hello"], final: "hello" }),
        ])
        expect(normCase(screen)).toBe("the text hello")
        for (const snap of screens) {
            if (snap.includes("text") && snap.includes("hello")) {
                expect(snap).not.toMatch(/texthello/)
            }
        }
    })

    test("hello + pause + world", () => {
        const { screen } = runPipeline(true, "en-US", [
            streamingSegmentEvents({ interims: ["hel", "hello"], final: "hello" }),
            streamingSegmentEvents({ interims: ["w", "world"], final: "world" }),
        ])
        expect(normCase(screen)).toBe("hello world")
    })

    test("hello period + world no extra space", () => {
        const { screen } = runPipeline(true, "en-US", [
            streamingSegmentEvents({ interims: ["hello period"], final: "hello period" }),
            streamingSegmentEvents({ interims: ["world"], final: "world" }),
        ])
        expect(normCase(screen)).toBe("hello.world")
    })

    test("hello period + and then capitalizes", () => {
        const { screen } = runPipeline(true, "en-US", [
            streamingSegmentEvents({ interims: ["hello period"], final: "hello period" }),
            streamingSegmentEvents({ interims: ["and then"], final: "and then" }),
        ])
        expect(normCase(screen)).toBe("hello.and then")
    })

    test("deferred new line across segments", () => {
        const { screen } = runPipeline(true, "en-US", [
            streamingSegmentEvents({ interims: ["hello new"], final: "hello new" }),
            streamingSegmentEvents({ interims: ["line world"], final: "line world" }),
        ])
        expect(normCase(screen)).toBe("hello\nworld")
    })

    test("single segment punctuation", () => {
        const { screen } = runPipeline(true, "en-US", [
            streamingSegmentEvents({
                interims: ["hello comma world period"],
                final: "hello comma world period",
            }),
        ])
        expect(screen).toBe("Hello, world.")
    })

    test("standalone control enter", () => {
        const { screen, chords } = runPipeline(true, "en-US", [
            streamingSegmentEvents({ interims: ["control enter"], final: "control enter" }),
        ])
        expect(screen).toBe("")
        expect(chords).toEqual(["ctrl+enter"])
    })

    test("mixed control enter", () => {
        const { screen, chords } = runPipeline(true, "en-US", [
            streamingSegmentEvents({
                interims: ["hello control enter world"],
                final: "hello control enter world",
            }),
        ])
        expect(screen).toBe("Hello control enter world")
        expect(chords).toEqual([])
    })

    test("non-stream capitalization across segments", () => {
        const { screen } = runPipeline(false, "en-US", [
            nonStreamSegmentEvents("hello period"),
            nonStreamSegmentEvents("and then"),
        ])
        expect(normCase(screen)).toBe("hello.and then")
    })

    test("non-English noop passthrough", () => {
        const { screen } = runPipeline(true, "es-ES", [
            streamingSegmentEvents({ interims: ["hola comma"], final: "hola comma" }),
        ])
        expect(screen).toBe("hola comma")
    })
})
