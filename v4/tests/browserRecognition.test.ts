import { describe, expect, test } from "bun:test"
import { recognitionResultsToEvents } from "../src/browserRecognition.js"

function result(transcript: string, isFinal: boolean) {
    return { isFinal, 0: { transcript } }
}

describe("recognitionResultsToEvents", () => {
    test("stream interim only", () => {
        const events = recognitionResultsToEvents(true, 0, [result("hello", false)])
        expect(events).toEqual([{ kind: "text", text: "hello" }])
    })

    test("stream finalized then segment-finalized then interim", () => {
        const events = recognitionResultsToEvents(true, 0, [
            result("the text", true),
            result("hel", false),
        ])
        expect(events).toEqual([
            { kind: "text", text: "the text" },
            { kind: "segment-finalized" },
            { kind: "text", text: "hel" },
        ])
    })

    test("stream segment final without trailing interim", () => {
        const events = recognitionResultsToEvents(true, 0, [result("done", true)])
        expect(events).toEqual([{ kind: "text", text: "done" }, { kind: "segment-finalized" }])
    })

    test("non-stream emits final and segment-finalized", () => {
        const events = recognitionResultsToEvents(false, 0, [result("hello", true)])
        expect(events).toEqual([{ kind: "text", text: "hello" }, { kind: "segment-finalized" }])
    })

    test("non-stream ignores interim-only results", () => {
        const events = recognitionResultsToEvents(false, 0, [result("hello", false)])
        expect(events).toEqual([])
    })

    test("respects resultIndex", () => {
        const events = recognitionResultsToEvents(true, 1, [
            result("old", true),
            result("new", false),
        ])
        expect(events).toEqual([{ kind: "text", text: "new" }])
    })
})
