import { describe, expect, test } from "bun:test"
import { createTranscriptTransformerSession } from "../../src/transcriptTransformers/index.js"
import { createEnglishTransformerSession } from "../../src/transcriptTransformers/en.js"
import { createNoopTransformerSession } from "../../src/transcriptTransformers/noop.js"

describe("createTranscriptTransformerSession", () => {
    test("en-US uses English session", () => {
        const s = createTranscriptTransformerSession("en-US", true, () => true)
        expect(s.transform("hello comma").text).toBe("Hello,")
    })

    test("en-GB uses English session", () => {
        const s = createTranscriptTransformerSession("en-GB", true, () => true)
        expect(s.transform("hello period").text).toBe("Hello.")
    })

    test("non-English uses noop", () => {
        const s = createTranscriptTransformerSession("es-ES", true, () => true)
        expect(s.transform("hola comma")).toEqual({ text: "hola comma", commands: [] })
    })
})

describe("factory routing", () => {
    test("English factory differs from noop", () => {
        const en = createEnglishTransformerSession(true, () => true)
        const noop = createNoopTransformerSession()
        en.reset()
        expect(en.transform("comma").text).not.toBe(noop.transform("comma").text)
    })
})
