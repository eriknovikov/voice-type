import { homedir } from "node:os"
import { mkdirSync, unlinkSync, existsSync, renameSync, statSync, appendFileSync } from "node:fs"
import { appendFile } from "node:fs/promises"
import { join, dirname, basename } from "node:path"

const MAX_BYTES = 1_000_000
const KEEP = 5

export interface LoggerOptions {
    mode?: "auto" | "stdout" | "file"
    dir?: string
}

interface LogSink {
    write(line: string): void
    flush(): Promise<void> | void
    destroy(): void
}

class StdoutSink implements LogSink {
    write(line: string) {
        process.stdout.write(line)
    }
    flush() {}
    destroy() {}
}

class FileSink implements LogSink {
    private readonly filePath: string
    private buffer = ""
    private tail: Promise<void> = Promise.resolve()
    private size: number

    constructor(dir: string) {
        this.filePath = join(dir, "voice-type.log")
        try {
            this.size = statSync(this.filePath).size
        } catch {
            this.size = 0
        }
    }

    write(line: string) {
        this.buffer += line
        if (line.endsWith("\n")) this.scheduleFlush()
    }

    private scheduleFlush() {
        this.tail = this.tail
            .then(() => this.flushAsync())
            .catch((e) => {
                process.stderr.write(`[logger] flush failed: ${e}\n`)
            })
    }

    private async flushAsync(): Promise<void> {
        if (this.buffer.length === 0) return
        const chunk = this.buffer
        this.buffer = ""
        const chunkBytes = Buffer.byteLength(chunk, "utf8")
        if (this.size + chunkBytes >= MAX_BYTES) {
            this.rotateSync()
            this.size = 0
        }
        await appendFile(this.filePath, chunk, "utf8")
        this.size += chunkBytes
    }

    private rotateSync(): void {
        const dir = dirname(this.filePath)
        const base = basename(this.filePath)
        try {
            const fifth = join(dir, `${base}.5`)
            if (existsSync(fifth)) unlinkSync(fifth)
            for (let i = KEEP - 1; i >= 1; i--) {
                const from = join(dir, `${base}.${i}`)
                const to = join(dir, `${base}.${i + 1}`)
                if (existsSync(from)) renameSync(from, to)
            }
            if (existsSync(this.filePath)) renameSync(this.filePath, join(dir, `${base}.1`))
        } catch (e) {
            process.stderr.write(`[logger] rotate failed: ${e}\n`)
        }
    }

    async flush() {
        await this.tail
    }

    destroy() {
        if (this.buffer.length > 0) {
            try {
                appendFileSync(this.filePath, this.buffer, "utf8")
                this.size += Buffer.byteLength(this.buffer, "utf8")
                this.buffer = ""
            } catch (e) {
                process.stderr.write(`[logger] final flush failed: ${e}\n`)
            }
        }
    }
}

let sink: LogSink | null = null
let exitHandlerRegistered = false

function defaultLogDir(): string {
    const base = process.env.XDG_DATA_HOME || `${homedir()}/.local/share`
    return join(base, "voice-type", "logs")
}

export function initLogger(opts: LoggerOptions = {}): void {
    if (sink) return
    const mode = opts.mode ?? "auto"
    const useFile = mode === "file" || (mode === "auto" && process.stdout.isTTY !== true)
    if (useFile) {
        const dir = opts.dir ?? defaultLogDir()
        mkdirSync(dir, { recursive: true, mode: 0o700 })
        sink = new FileSink(dir)
    } else {
        sink = new StdoutSink()
    }
    if (!exitHandlerRegistered) {
        process.on("exit", () => destroyLogger())
        exitHandlerRegistered = true
    }
}

export function destroyLogger(): void {
    if (!sink) return
    sink.destroy()
    sink = null
}

export async function flushLogger(): Promise<void> {
    if (!sink) return
    await sink.flush()
}

export function resetLogger(): void {
    sink = null
}

function formatPart(p: unknown): string {
    if (typeof p === "string") return p
    if (p instanceof Error) return p.stack ?? `${p.name}: ${p.message}`
    if (p === undefined) return "undefined"
    if (p === null) return "null"
    try {
        return JSON.stringify(p)
    } catch {
        return String(p)
    }
}

export function log(tag: string, ...parts: unknown[]): void {
    if (!sink) initLogger()
    const ts = new Date().toISOString()
    const msg = parts.map(formatPart).join(" ")
    sink!.write(`${ts} [${tag}] ${msg}\n`)
}
