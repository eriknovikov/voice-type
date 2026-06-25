import { describe, expect, test, beforeAll, afterAll, beforeEach, afterEach } from "bun:test"
import { mkdtempSync, rmSync, writeFileSync, readFileSync, accessSync, renameSync } from "node:fs"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { stripJsoncComments, validateConfig, persistConfig, loadConfig, configFilePath } from "../src/config.js"
import { PORT } from "../src/constants.js"

describe("stripJsoncComments", () => {
    test("strips // comment and newline correctly", () => {
        const result = stripJsoncComments("// c\n{}")
        expect(JSON.parse(result.trim())).toEqual({})
    })

    test("strips inline comment after value", () => {
        const result = stripJsoncComments('{"a": 1 // c\n}')
        const trimmed = result.trim()
        expect(JSON.parse(trimmed)).toEqual({ a: 1 })
    })

    test("does not strip // inside a string (url)", () => {
        const result = stripJsoncComments('"http://x" // c')
        expect(result).toContain("http://x")
        expect(JSON.parse(result.trim())).toBe("http://x")
    })

    test("does not strip comment chars inside a string", () => {
        const result = stripJsoncComments('"a//b"')
        expect(result).toBe('"a//b"')
    })

    test("handles escaped quote inside string", () => {
        const result = stripJsoncComments('{"a":"x\\"//y"}')
        expect(result.trim()).toBe('{"a":"x\\"//y"}')
    })
})

describe("validateConfig", () => {
    test("returns full DEFAULT_CONFIG for empty object", () => {
        const cfg = validateConfig({})
        expect(cfg.port).toBe(PORT)
        expect(cfg.lang).toBe("en-US")
        expect(cfg.browser_type).toBe("chrome")
        expect(cfg.browser_path).toBe("")
        expect(cfg.stream).toBe(true)
        expect(cfg.timeout).toBe(0)
        expect(cfg.sound).toBe(false)
        expect(cfg.text).toBe(false)
        expect(cfg.punctuation).toBe(true)
    })

    test("returns default config for non-object input", () => {
        const cfg = validateConfig("not an object")
        expect(cfg.port).toBe(PORT)
        expect(cfg.lang).toBe("en-US")
    })

    test("fail-soft: invalid fields fall back to defaults", () => {
        const cfg = validateConfig({
            port: "x",
            lang: "zz-ZZ",
            timeout: -1,
            stream: "yes",
            browser_type: "firefox",
            punctuation: 1,
        })
        expect(cfg.port).toBe(PORT)
        expect(cfg.lang).toBe("en-US")
        expect(cfg.timeout).toBe(0)
        expect(cfg.stream).toBe(true)
        expect(cfg.browser_type).toBe("chrome")
        expect(cfg.punctuation).toBe(true)
    })

    test("valid config passes through unchanged", () => {
        const input = {
            port: 4040,
            lang: "es-ES",
            browser_type: "chromium",
            browser_path: "/usr/bin/chromium",
            stream: false,
            timeout: 30,
            sound: true,
            text: true,
            punctuation: false,
        }
        const cfg = validateConfig(input)
        expect(cfg.port).toBe(4040)
        expect(cfg.lang).toBe("es-ES")
        expect(cfg.browser_type).toBe("chromium")
        expect(cfg.browser_path).toBe("/usr/bin/chromium")
        expect(cfg.stream).toBe(false)
        expect(cfg.timeout).toBe(30)
        expect(cfg.sound).toBe(true)
        expect(cfg.text).toBe(true)
        expect(cfg.punctuation).toBe(false)
    })
})

describe("loadConfig", () => {
    let tempDir: string
    let originalHome: string | undefined
    let originalLang: string | undefined

    beforeAll(() => {
        originalHome = process.env.XDG_CONFIG_HOME
        originalLang = process.env.LANG
    })

    beforeEach(() => {
        tempDir = mkdtempSync(join(tmpdir(), "voice-type-test-config-"))
        process.env.XDG_CONFIG_HOME = tempDir
        process.env.LANG = "en_US.UTF-8"
    })

    afterEach(() => {
        process.env.XDG_CONFIG_HOME = originalHome
        process.env.LANG = originalLang
        try {
            rmSync(tempDir, { recursive: true, force: true })
        } catch {}
    })

    test("missing file: writes default config and returns it", async () => {
        const cfg = await loadConfig()
        expect(cfg.port).toBe(PORT)
        const fp = configFilePath()
        const content = readFileSync(fp, "utf8")
        const parsed = JSON.parse(content)
        expect(parsed.port).toBe(PORT)
        expect(parsed.lang).toBe("en-US")
    })

    test("parses JSONC with comments", async () => {
        const fp = configFilePath()
        const dir = fp.substring(0, fp.lastIndexOf("/"))
        const { mkdirSync } = require("node:fs")
        mkdirSync(dir, { recursive: true })
        writeFileSync(fp, '// comment line\n{"port": 4040}\n// trailing comment\n', "utf8")

        const cfg = await loadConfig()
        expect(cfg.port).toBe(4040)
    })

    test("bad JSON: backs up to .bak and writes default", async () => {
        const fp = configFilePath()
        const dir = fp.substring(0, fp.lastIndexOf("/"))
        const { mkdirSync } = require("node:fs")
        mkdirSync(dir, { recursive: true })
        writeFileSync(fp, "{{{not json", "utf8")

        const cfg = await loadConfig()
        expect(cfg.port).toBe(PORT)

        const bakPath = fp + ".bak"
        expect(readFileSync(bakPath, "utf8")).toBe("{{{not json")

        const newContent = readFileSync(fp, "utf8")
        const parsed = JSON.parse(newContent)
        expect(parsed.port).toBe(PORT)
    })

    test("lang heuristic: es_ES.UTF-8 → lang es-ES", async () => {
        process.env.LANG = "es_ES.UTF-8"
        const cfg = await loadConfig()
        expect(cfg.lang).toBe("es-ES")
    })

    test("lang heuristic: unknown lang → default en-US", async () => {
        process.env.LANG = "xx_XX.UTF-8"
        const cfg = await loadConfig()
        expect(cfg.lang).toBe("en-US")
    })

    test("persistConfig: atomic write, no .tmp left behind", async () => {
        const fp = configFilePath()
        const config = validateConfig({ port: 5555, lang: "fr-FR" })
        await persistConfig(config)

        const tmpPath = fp + ".tmp"
        let tmpExists = false
        try {
            accessSync(tmpPath)
            tmpExists = true
        } catch {}

        expect(tmpExists).toBe(false)

        const content = readFileSync(fp, "utf8")
        const parsed = JSON.parse(content)
        expect(parsed.port).toBe(5555)
        expect(parsed.lang).toBe("fr-FR")
        expect(content.endsWith("\n")).toBe(true)
    })
})
