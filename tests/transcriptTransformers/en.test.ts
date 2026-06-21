import { describe, expect, test } from "bun:test"
import { createEnglishTransformerSession } from "../../src/transcriptTransformers/en.js"

function session(stream = true) {
    const s = createEnglishTransformerSession(stream)
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
        expect(s.transform('hello double quote world').text).toBe('Hello " world')
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

    test("no space after sentence-ending punctuation", () => {
        const s = session()
        expect(s.transform("hello period").text).toBe("Hello.")
        finalizeSegment(s)
        expect(s.transform("world").text).toBe("World")
    })

    test("capitalize after sentence-end pause", () => {
        const s = session()
        expect(s.transform("hello period").text).toBe("Hello.")
        finalizeSegment(s)
        expect(s.transform("and then").text).toBe("And then")
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

    test("reset clears state", () => {
        const s = session()
        s.transform("hello period")
        finalizeSegment(s)
        s.reset()
        expect(s.transform("world").text).toBe("World")
    })
})
