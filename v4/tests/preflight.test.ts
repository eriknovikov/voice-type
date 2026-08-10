import { describe, expect, test, afterEach, beforeEach } from "bun:test"
import { EventEmitter } from "node:events"
import { mkdtempSync, rmSync, writeFileSync, chmodSync } from "node:fs"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { createServer } from "node:net"
import { checkPortAvailable, checkBrowserPath, checkDotool, runPreflight, type SpawnFn } from "../src/preflight.js"

function fakeSpawn(opts: {
    error?: { message: string; code: string }
    exit?: { code: number | null; signal: string | null }
    stderrData?: string
    delayMs?: number
}): SpawnFn {
    return (_cmd, _args, _spawnOpts) => {
        const proc = new FakeProcess()

        if (opts.error) {
            const err = Object.assign(new Error(opts.error.message), { code: opts.error.code })
            process.nextTick(() => proc.emit("error", err))
        } else if (opts.exit) {
            process.nextTick(() => {
                proc.emit("exit", opts.exit!.code, opts.exit!.signal)
            })
        } else if (opts.stderrData) {
            process.nextTick(() => proc.stderr.emit("data", Buffer.from(opts.stderrData!)))
        }

        return proc as any
    }
}

class FakeProcess extends EventEmitter {
    stdin = new EventEmitter()
    stdout = new EventEmitter()
    stderr = new EventEmitter()
    killed = false

    kill(_signal?: string): boolean {
        this.killed = true
        return true
    }
}

describe("checkPortAvailable", () => {
    test("free port returns ok", async () => {
        const result = await checkPortAvailable(39999)
        expect(result.ok).toBe(true)
    })

    test("in-use port returns failure", async () => {
        const server = createServer()
        await new Promise<void>((resolve) => server.listen(39998, "127.0.0.1", resolve))
        try {
            const result = await checkPortAvailable(39998)
            expect(result.ok).toBe(false)
            if (!result.ok) {
                expect(result.failure.kind).toBe("port-in-use")
            }
        } finally {
            server.close()
        }
    })
})

describe("checkBrowserPath", () => {
    let tempDir: string

    function createTempFile(): string {
        tempDir = mkdtempSync(join(tmpdir(), "voice-type-pref-"))
        const filePath = join(tempDir, "mock-browser")
        writeFileSync(filePath, "#!/bin/sh\necho ok\n", { mode: 0o755 })
        return filePath
    }

    afterEach(() => {
        if (tempDir) {
            try {
                rmSync(tempDir, { recursive: true, force: true })
            } catch {}
        }
    })

    test("empty path returns failure", async () => {
        const result = await checkBrowserPath("")
        expect(result.ok).toBe(false)
        if (!result.ok) {
            expect(result.failure.kind).toBe("browser")
            expect(result.failure.message).toContain("No browser_path configured")
        }
    })

    test("existing executable returns ok", async () => {
        const path = createTempFile()
        const result = await checkBrowserPath(path)
        expect(result.ok).toBe(true)
    })

    test("non-existent path returns failure", async () => {
        const result = await checkBrowserPath("/nonexistent/browser-xyz")
        expect(result.ok).toBe(false)
        if (!result.ok) {
            expect(result.failure.kind).toBe("browser")
            expect(result.failure.message).toContain("not found or not executable")
        }
    })

    test("non-executable file returns failure", async () => {
        tempDir = mkdtempSync(join(tmpdir(), "voice-type-pref-"))
        const filePath = join(tempDir, "not-exec")
        writeFileSync(filePath, "#!/bin/sh\necho ok\n", { mode: 0o644 })
        const result = await checkBrowserPath(filePath)
        expect(result.ok).toBe(false)
        if (!result.ok) {
            expect(result.failure.kind).toBe("browser")
        }
    })
})

describe("checkDotool", () => {
    test("healthy process returns ok", async () => {
        const spawnFn: SpawnFn = (_cmd, _args, _opts) => new FakeProcess() as any
        const result = await checkDotool(spawnFn, 0)
        expect(result.ok).toBe(true)
    })

    test("healthy process kills the probe after passing", async () => {
        const proc = new FakeProcess()
        const spawnFn: SpawnFn = () => proc as any
        const result = await checkDotool(spawnFn, 0)
        expect(result.ok).toBe(true)
        expect(proc.killed).toBe(true)
    })

    test("error event returns failure", async () => {
        const result = await checkDotool(fakeSpawn({ error: { message: "ENOENT", code: "ENOENT" } }), 5000)
        expect(result.ok).toBe(false)
        if (!result.ok) {
            expect(result.failure.kind).toBe("dotool")
            expect(result.failure.message).toContain("failed to start")
        }
    })

    test("immediate exit returns failure", async () => {
        const result = await checkDotool(fakeSpawn({ exit: { code: 1, signal: null } }), 5000)
        expect(result.ok).toBe(false)
        if (!result.ok) {
            expect(result.failure.kind).toBe("dotool")
            expect(result.failure.message).toContain("exited immediately")
        }
    })

    test("permission denied on stderr returns failure", async () => {
        const result = await checkDotool(fakeSpawn({ stderrData: "/dev/uinput: Permission denied" }), 5000)
        expect(result.ok).toBe(false)
        if (!result.ok) {
            expect(result.failure.kind).toBe("dotool")
            expect(result.failure.message).toContain("Permission denied")
        }
    })

    test("command not found on stderr returns failure", async () => {
        const result = await checkDotool(fakeSpawn({ stderrData: "dotool: command not found" }), 5000)
        expect(result.ok).toBe(false)
        if (!result.ok) {
            expect(result.failure.kind).toBe("dotool")
            expect(result.failure.message).toContain("command not found")
        }
    })
})

describe("runPreflight", () => {
    let tempDir: string
    let server: ReturnType<typeof createServer>

    function createTempBrowser(): string {
        tempDir = mkdtempSync(join(tmpdir(), "voice-type-rp-"))
        const path = join(tempDir, "browser")
        writeFileSync(path, "#!/bin/sh\necho ok\n", { mode: 0o755 })
        return path
    }

    beforeEach(async () => {
        server = createServer()
        await new Promise<void>((resolve) => server.listen(39997, "127.0.0.1", resolve))
    })

    afterEach(() => {
        server.close()
        if (tempDir) {
            try {
                rmSync(tempDir, { recursive: true, force: true })
            } catch {}
        }
    })

    test("port-in-use returns failure and short-circuits", async () => {
        let spawnCalled = false
        const spawnFn: SpawnFn = () => {
            spawnCalled = true
            return new FakeProcess() as any
        }
        const result = await runPreflight({ port: 39997, browser_path: "/some/path" }, spawnFn)
        expect(result.ok).toBe(false)
        if (!result.ok) {
            expect(result.failure.kind).toBe("port-in-use")
        }
        expect(spawnCalled).toBe(false)
    })

    test("browser failure short-circuits before dotool", async () => {
        let spawnCalled = false
        const spawnFn: SpawnFn = () => {
            spawnCalled = true
            return new FakeProcess() as any
        }
        const result = await runPreflight({ port: 39999, browser_path: "" }, spawnFn)
        expect(result.ok).toBe(false)
        if (!result.ok) {
            expect(result.failure.kind).toBe("browser")
        }
        expect(spawnCalled).toBe(false)
    })

    test("all-ok returns success", async () => {
        const browserPath = createTempBrowser()
        const result = await runPreflight({ port: 39999, browser_path: browserPath }, () => new FakeProcess() as any, 0)
        expect(result.ok).toBe(true)
    })
})
