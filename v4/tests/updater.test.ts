import { describe, expect, test } from "bun:test"
import { compareVersions } from "../src/updater.js"

describe("compareVersions", () => {
    test("equal versions", () => expect(compareVersions("3.0.0", "3.0.0")).toBe(0))
    test("newer patch", () => expect(compareVersions("3.0.1", "3.0.0")).toBe(1))
    test("newer minor", () => expect(compareVersions("3.1.0", "3.0.0")).toBe(1))
    test("newer major", () => expect(compareVersions("4.0.0", "3.0.0")).toBe(1))
    test("older", () => expect(compareVersions("2.0.0", "3.0.0")).toBe(-1))
    test("different lengths", () => expect(compareVersions("3.0", "3.0.0")).toBe(0))
})
