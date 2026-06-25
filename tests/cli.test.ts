import { describe, expect, test } from "bun:test"
import { showHelp } from "../src/cli.js"

describe("showHelp", () => {
    test("prints help text containing key usage info", () => {
        let captured = ""
        const originalLog = console.log
        console.log = (...args: any[]) => {
            captured += args.join(" ") + "\n"
        }
        try {
            showHelp()
        } finally {
            console.log = originalLog
        }
        expect(captured).toContain("voice-type help")
        expect(captured).toContain("voice-type.jsonc")
        expect(captured).toContain("Configuration:")
        expect(captured).toContain("HTTP API")
    })
})
