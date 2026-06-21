import { describe, expect, test } from "bun:test"
import { parseFlags } from "../src/cli.js"

describe("parseFlags", () => {
    test("defaults", () => {
        const flags = parseFlags([])
        expect(flags.lang).toBe("en-US")
        expect(flags.stream).toBe(true)
        expect(flags.timeout).toBe(0)
        expect(flags.textNotifs).toBe(false)
        expect(flags.soundNotifs).toBe(false)
        expect(flags.browserType).toBe("chrome")
        expect(flags.detached).toBe(false)
        expect(flags.help).toBe(false)
    })

    test("honors --no-stream, --timeout, notifications, and --lang", () => {
        const flags = parseFlags(["--no-stream", "--timeout", "30", "--sound", "--text", "--lang", "es-ES"])
        expect(flags.stream).toBe(false)
        expect(flags.timeout).toBe(30)
        expect(flags.soundNotifs).toBe(true)
        expect(flags.textNotifs).toBe(true)
        expect(flags.lang).toBe("es-ES")
    })

    test("honors --browser_type and --browser_path", () => {
        const flags = parseFlags(["--browser_type", "chromium", "-p", "/opt/chromium"])
        expect(flags.browserType).toBe("chromium")
        expect(flags.browserPath).toBe("/opt/chromium")
    })
})
