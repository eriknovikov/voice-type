import { describe, expect, test } from "bun:test"
import { createEnglishTransformerSession } from "../../src/transcriptTransformers/en.js"

function session(stream = true) {
    const s = createEnglishTransformerSession(stream, () => true)
    s.reset()
    return s
}

function finalizeSegment(s: ReturnType<typeof createEnglishTransformerSession>) {
    return s.onSegmentFinalized()
}

describe("createEnglishTransformerSession", () => {
    test("comma and period", () => {
        const s = session()
        expect(s.transform("hello comma world period").text).toBe("Hello, world.")
    })

    test("question mark and exclamation", () => {
        const s = session()
        expect(s.transform("hello question mark").text).toBe("Hello?")
        expect(s.transform("wow exclamation point").text).toBe("Wow!")
        expect(s.transform("wow exclamation mark").text).toBe("Wow!")
    })

    test("semicolon variants", () => {
        const s = session()
        expect(s.transform("hello semicolon").text).toBe("Hello;")
        expect(s.transform("hello semi colon").text).toBe("Hello;")
        expect(s.transform("hello semi-colon").text).toBe("Hello;")
    })

    test("double quote with leading space after word", () => {
        const s = session()
        expect(s.transform("hello double quote world").text).toBe('Hello " world')
    })

    test("literal words commander and periodic", () => {
        const s = session()
        expect(s.transform("commander periodic").text).toBe("Commander periodic")
    })

    test("inline newline", () => {
        const s = session()
        expect(s.transform("hello new line world").text).toBe("Hello\nWorld")
        expect(s.transform("hello newline world").text).toBe("Hello\nWorld")
    })

    test("newline disabled when not streaming", () => {
        const s = session(false)
        expect(s.transform("hello new line world").text).toBe("Hello new line world")
    })

    test("standalone control enter", () => {
        const s = session()
        expect(s.transform("control enter").text).toBe("")
        expect(finalizeSegment(s)).toEqual([{ kind: "key", chord: "ctrl+enter" }])
    })

    test("mixed control enter types literally", () => {
        const s = session()
        expect(s.transform("hello control enter world").text).toBe("Hello control enter world")
        expect(finalizeSegment(s)).toEqual([])
    })

    test("cross-segment word spacing", () => {
        const s = session()
        expect(s.transform("hello").text).toBe("Hello")
        finalizeSegment(s)
        expect(s.transform("world").text).toBe(" world")
    })

    test("space after sentence-ending punctuation across segments", () => {
        const s = session()
        expect(s.transform("hello period").text).toBe("Hello.")
        finalizeSegment(s)
        expect(s.transform("world").text).toBe(" World")
    })

    test("capitalize after sentence-end pause", () => {
        const s = session()
        expect(s.transform("hello period").text).toBe("Hello.")
        finalizeSegment(s)
        expect(s.transform("and then").text).toBe(" And then")
    })

    test("punctuation-only segment attaches, next segment is spaced", () => {
        const s = session()
        expect(s.transform("hello").text).toBe("Hello")
        finalizeSegment(s)
        expect(s.transform("period").text).toBe(".")
        finalizeSegment(s)
        expect(s.transform("world").text).toBe(" World")
    })

    test("empty segment keeps boundary space", () => {
        const s = session()
        expect(s.transform("hello").text).toBe("Hello")
        finalizeSegment(s)
        finalizeSegment(s)
        expect(s.transform("world").text).toBe(" world")
    })

    test("whitespace-only segment keeps boundary space", () => {
        const s = session()
        expect(s.transform("hello").text).toBe("Hello")
        finalizeSegment(s)
        expect(s.transform(" ").text).toBe("")
        finalizeSegment(s)
        expect(s.transform("world").text).toBe(" world")
    })

    test("deferred-only segment keeps boundary space", () => {
        const s = session()
        expect(s.transform("hello").text).toBe("Hello")
        finalizeSegment(s)
        expect(s.transform("new").text).toBe("")
        finalizeSegment(s)
        expect(s.transform("stuff").text).toBe(" new stuff")
    })

    test("deferred tokens survive an empty segment", () => {
        const s = session()
        expect(s.transform("hello new").text).toBe("Hello")
        finalizeSegment(s)
        finalizeSegment(s)
        expect(s.transform("line world").text).toBe("\nWorld")
    })

    test("no boundary space after standalone control enter", () => {
        const s = session()
        expect(s.transform("hello").text).toBe("Hello")
        finalizeSegment(s)
        expect(s.transform("control enter").text).toBe("")
        expect(finalizeSegment(s)).toEqual([{ kind: "key", chord: "ctrl+enter" }])
        expect(s.transform("world").text).toBe("world")
    })

    test("deferred new line across segments", () => {
        const s = session()
        expect(s.transform("hello new").text).toBe("Hello")
        finalizeSegment(s)
        expect(s.transform("line world").text).toBe("\nWorld")
    })

    test("pending boundary space stable across interims in same segment", () => {
        const s = session()
        expect(s.transform("the text").text).toBe("The text")
        finalizeSegment(s)
        expect(s.transform("h").text).toBe(" h")
        expect(s.transform("he").text).toBe(" he")
        expect(s.transform("hello").text).toBe(" hello")
    })

    test("preserves recognizer casing of words", () => {
        const s = session()
        expect(s.transform("so I'm talking to Google").text).toBe("So I'm talking to Google")
    })

    test("preserves casing at the start of a mid-sentence segment", () => {
        const s = session()
        expect(s.transform("this is a test").text).toBe("This is a test")
        finalizeSegment(s)
        expect(s.transform("I'm not sure").text).toBe(" I'm not sure")
    })

    test("deferred tokens keep their casing", () => {
        const s = session()
        expect(s.transform("I live in New").text).toBe("I live in")
        finalizeSegment(s)
        expect(s.transform("York").text).toBe(" New York")
    })

    test("commands match case-insensitively", () => {
        const s = session()
        expect(s.transform("hello Comma world Period").text).toBe("Hello, world.")
    })

    test("reset clears state", () => {
        const s = session()
        s.transform("hello period")
        finalizeSegment(s)
        s.reset()
        expect(s.transform("world").text).toBe("World")
    })
})

describe("createEnglishTransformerSession — punctuation toggle", () => {
    test("disabled: spoken punctuation phrases type literally", () => {
        let on = false
        const s = createEnglishTransformerSession(true, () => on)
        s.reset()
        expect(s.transform("hello comma world period").text).toBe("hello comma world period")
    })

    test("disabled: no auto-capitalization", () => {
        let on = false
        const s = createEnglishTransformerSession(true, () => on)
        s.reset()
        expect(s.transform("hello world").text).toBe("hello world")
    })

    test("disabled: preserves recognizer casing", () => {
        let on = false
        const s = createEnglishTransformerSession(true, () => on)
        s.reset()
        expect(s.transform("so I'm in New York").text).toBe("so I'm in New York")
    })

    test("disabled: control enter types literally, no key chord", () => {
        let on = false
        const s = createEnglishTransformerSession(true, () => on)
        s.reset()
        expect(s.transform("control enter").text).toBe("control enter")
        expect(s.onSegmentFinalized()).toEqual([])
    })

    test("toggle mid-session: enabled then disabled", () => {
        let on = true
        const s = createEnglishTransformerSession(true, () => on)
        s.reset()
        expect(s.transform("hello comma world period").text).toBe("Hello, world.")
        on = false
        expect(s.transform("hello comma world period").text).toBe("hello comma world period")
    })

    test("toggle mid-session: disabled then enabled", () => {
        let on = false
        const s = createEnglishTransformerSession(true, () => on)
        s.reset()
        expect(s.transform("hello world").text).toBe("hello world")
        on = true
        expect(s.transform("hello comma").text).toBe("Hello,")
    })

    test("cross-segment spacing preserved across disable", () => {
        let on = true
        const s = createEnglishTransformerSession(true, () => on)
        s.reset()
        expect(s.transform("hello").text).toBe("Hello")
        s.onSegmentFinalized()
        on = false
        expect(s.transform("world").text).toBe(" world")
        s.onSegmentFinalized()
        on = true
        expect(s.transform("hello period").text).toBe(" hello.")
    })

    test("deferred tokens consumed as literal text when disabled", () => {
        let on = true
        const s = createEnglishTransformerSession(true, () => on)
        s.reset()
        expect(s.transform("hello new").text).toBe("Hello")
        s.onSegmentFinalized()
        on = false
        expect(s.transform("line world").text).toBe(" new line world")
        s.onSegmentFinalized()
        expect(s.transform("hello").text).toBe(" hello")
    })
})
