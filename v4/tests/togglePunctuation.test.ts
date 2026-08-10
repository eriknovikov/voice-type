import { describe, expect, test, beforeEach, afterEach } from "bun:test"
import { mkdtempSync, rmSync, readFileSync } from "node:fs"
import { join } from "node:path"
import { tmpdir } from "node:os"
import type { Server } from "node:http"
import type { AddressInfo } from "node:net"
import Daemon from "../src/daemon.js"
import TypingController from "../src/typingController.js"
import type { DotoolSink } from "../src/dotoolSink.js"
import type { VoiceTypeConfig } from "../src/types.js"

const STUB_SINK: DotoolSink = {
    write() {},
    writable: false,
    kill() {},
}

const BASE_CONFIG: VoiceTypeConfig = {
    port: 0,
    lang: "en-US",
    browser_type: "chrome",
    browser_path: "",
    stream: true,
    timeout: 0,
    sound: false,
    text: false,
    punctuation: true,
}

async function startDaemon(cfg: VoiceTypeConfig): Promise<{ baseUrl: string; server: Server }> {
    const daemon = new Daemon(cfg, new TypingController(STUB_SINK))
    const server = await new Promise<Server>((resolve) => {
        const s = daemon.app.listen(0, "127.0.0.1", () => resolve(s))
    })
    const addr = server.address() as AddressInfo
    return { baseUrl: `http://127.0.0.1:${addr.port}`, server }
}

async function getJson(url: string): Promise<{ status: number; body: any }> {
    const res = await fetch(url)
    const body = await res.json()
    return { status: res.status, body }
}

describe("GET /togglePunctuation", () => {
    let dir: string

    beforeEach(() => {
        dir = mkdtempSync(join(tmpdir(), "vt-toggle-"))
        process.env.XDG_CONFIG_HOME = dir
    })

    afterEach(() => {
        rmSync(dir, { recursive: true, force: true })
        delete process.env.XDG_CONFIG_HOME
    })

    test("flip from true to false", async () => {
        const { baseUrl, server } = await startDaemon({ ...BASE_CONFIG, punctuation: true })
        try {
            const { status, body } = await getJson(`${baseUrl}/togglePunctuation`)
            expect(status).toBe(200)
            expect(body).toEqual({ punctuation: false })
        } finally {
            server.close()
        }
    })

    test("flip from false to true", async () => {
        const { baseUrl, server } = await startDaemon({ ...BASE_CONFIG, punctuation: false })
        try {
            const { status, body } = await getJson(`${baseUrl}/togglePunctuation`)
            expect(status).toBe(200)
            expect(body).toEqual({ punctuation: true })
        } finally {
            server.close()
        }
    })

    test("enabled=true sets explicitly", async () => {
        const { baseUrl, server } = await startDaemon({ ...BASE_CONFIG, punctuation: false })
        try {
            const { status, body } = await getJson(`${baseUrl}/togglePunctuation?enabled=true`)
            expect(status).toBe(200)
            expect(body).toEqual({ punctuation: true })
        } finally {
            server.close()
        }
    })

    test("enabled=false sets explicitly", async () => {
        const { baseUrl, server } = await startDaemon({ ...BASE_CONFIG, punctuation: true })
        try {
            const { status, body } = await getJson(`${baseUrl}/togglePunctuation?enabled=false`)
            expect(status).toBe(200)
            expect(body).toEqual({ punctuation: false })
        } finally {
            server.close()
        }
    })

    test("invalid enabled value returns 400", async () => {
        const { baseUrl, server } = await startDaemon({ ...BASE_CONFIG, punctuation: true })
        try {
            const { status } = await getJson(`${baseUrl}/togglePunctuation?enabled=foo`)
            expect(status).toBe(400)
        } finally {
            server.close()
        }
    })

    test("persists to config file", async () => {
        const { baseUrl, server } = await startDaemon({ ...BASE_CONFIG, punctuation: true })
        try {
            await getJson(`${baseUrl}/togglePunctuation?enabled=false`)
        } finally {
            server.close()
        }
        const configPath = join(dir, "voice-type.jsonc")
        const written = JSON.parse(readFileSync(configPath, "utf8"))
        expect(written.punctuation).toBe(false)
    })
})
