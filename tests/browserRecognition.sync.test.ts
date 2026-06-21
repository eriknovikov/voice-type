import { describe, expect, test } from "bun:test"
import { readFileSync } from "node:fs"
import { join } from "node:path"

function normalizeFnBody(source: string): string {
    return source
        .replace(/\/\/.*$/gm, "")
        .replace(/\s+/g, " ")
        .trim()
}

function extractBrowserJsMapper(): string {
    const src = readFileSync(join(import.meta.dir, "../src/browser.js"), "utf8")
    const match = src.match(/function recognitionResultsToEvents\(stream, resultIndex, results\) \{([\s\S]*?)\n    \}/)
    if (!match) throw new Error("could not extract recognitionResultsToEvents from browser.js")
    return normalizeFnBody(match[1]!)
}

function extractBrowserRecognitionMapper(): string {
    const src = readFileSync(join(import.meta.dir, "../src/browserRecognition.js"), "utf8")
    const match = src.match(/export function recognitionResultsToEvents\(stream, resultIndex, results\) \{([\s\S]*)\n\}/)
    if (!match) throw new Error("could not extract recognitionResultsToEvents from browserRecognition.js")
    return normalizeFnBody(match[1]!)
}

describe("browserRecognition sync", () => {
    test("browser.js inner mapper matches browserRecognition.js", () => {
        expect(extractBrowserJsMapper()).toBe(extractBrowserRecognitionMapper())
    })
})
