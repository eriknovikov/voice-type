import { describe, expect, test } from "bun:test"
import { DEFAULT_LANGUAGE, isValidLanguage, readLanguageQuery } from "../src/language.js"

describe("isValidLanguage", () => {
    test("accepts WSA languages", () => {
        expect(isValidLanguage("en-US")).toBe(true)
        expect(isValidLanguage("es-ES")).toBe(true)
    })

    test("rejects invalid values", () => {
        expect(isValidLanguage("not-a-lang")).toBe(false)
        expect(isValidLanguage(42)).toBe(false)
        expect(isValidLanguage(undefined)).toBe(false)
    })
})

describe("readLanguageQuery", () => {
    test("reads language param", () => {
        expect(readLanguageQuery({ language: "fr-FR" })).toBe("fr-FR")
    })

    test("reads lang alias", () => {
        expect(readLanguageQuery({ lang: "de-DE" })).toBe("de-DE")
    })

    test("trims whitespace", () => {
        expect(readLanguageQuery({ language: "  es-ES  " })).toBe("es-ES")
    })

    test("empty string is absent", () => {
        expect(readLanguageQuery({ language: "   " })).toBeUndefined()
    })

    test("non-string is absent", () => {
        expect(readLanguageQuery({ language: 1 })).toBeUndefined()
    })
})

describe("DEFAULT_LANGUAGE", () => {
    test("is en-US", () => {
        expect(DEFAULT_LANGUAGE).toBe("en-US")
        expect(isValidLanguage(DEFAULT_LANGUAGE)).toBe(true)
    })
})
