import { describe, expect, test } from "bun:test"
import { createNoopTransformerSession } from "../../src/transcriptTransformers/noop.js"

describe("createNoopTransformerSession", () => {
    test("passthrough text", () => {
        const s = createNoopTransformerSession()
        expect(s.transform("hello world")).toEqual({ text: "hello world", commands: [] })
    })

    test("no commands on finalize", () => {
        const s = createNoopTransformerSession()
        s.transform("hello")
        expect(s.onSegmentFinalized()).toEqual([])
    })
})
