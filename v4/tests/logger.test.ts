import { describe, expect, test, beforeEach, afterEach } from "bun:test"
import { mkdtempSync, rmSync, readFileSync, existsSync, statSync } from "node:fs"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { initLogger, log, flushLogger, destroyLogger, resetLogger } from "../src/logger.js"

describe("logger (file mode)", () => {
    let dir: string

    beforeEach(() => {
        dir = mkdtempSync(join(tmpdir(), "vt-log-"))
        resetLogger()
        initLogger({ mode: "file", dir })
    })

    afterEach(() => {
        destroyLogger()
        rmSync(dir, { recursive: true, force: true })
    })

    test("writes a formatted ISO8601 [TAG] msg line to the file", async () => {
        log("DAEMON", "hello", "world")
        await flushLogger()
        const content = readFileSync(join(dir, "voice-type.log"), "utf8")
        expect(content).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z \[DAEMON\] hello world\n$/)
    })

    test("stringifies objects and errors", async () => {
        log("DBUS", { a: 1 })
        log("DAEMON", new Error("boom"))
        await flushLogger()
        const content = readFileSync(join(dir, "voice-type.log"), "utf8")
        expect(content).toContain('[DBUS] {"a":1}')
        expect(content).toContain("[DAEMON] Error: boom")
    })

    test("multiple log calls are buffered then flushed together", async () => {
        log("DAEMON", "a")
        log("DAEMON", "b")
        log("DAEMON", "c")
        await flushLogger()
        const lines = readFileSync(join(dir, "voice-type.log"), "utf8").split("\n").filter(Boolean)
        expect(lines).toHaveLength(3)
        expect(lines[0]).toMatch(/\[DAEMON\] a$/)
        expect(lines[2]).toMatch(/\[DAEMON\] c$/)
    })

    test("rotates at 1 MB and keeps at most 5 archives", async () => {
        const big = "x".repeat(1000)
        for (let i = 0; i < 950; i++) {
            log("DAEMON", big)
        }
        await flushLogger()
        expect(existsSync(join(dir, "voice-type.log"))).toBe(true)

        for (let i = 0; i < 400; i++) {
            log("DAEMON", big)
        }
        await flushLogger()
        expect(existsSync(join(dir, "voice-type.log.1"))).toBe(true)
        expect(existsSync(join(dir, "voice-type.log.6"))).toBe(false)
        const currentSize = statSync(join(dir, "voice-type.log")).size
        expect(currentSize).toBeLessThan(1_000_000)
    })

    test("destroyLogger flushes remaining buffer synchronously", () => {
        log("DAEMON", "tail")
        destroyLogger()
        const content = readFileSync(join(dir, "voice-type.log"), "utf8")
        expect(content).toContain("] tail")
    })

    test("stdout mode creates no file", async () => {
        destroyLogger()
        resetLogger()
        initLogger({ mode: "stdout", dir })
        log("DAEMON", "to-terminal")
        await flushLogger()
        expect(existsSync(join(dir, "voice-type.log"))).toBe(false)
    })
})
