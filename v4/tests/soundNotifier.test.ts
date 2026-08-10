import { describe, expect, test } from "bun:test"
import { findSoundInBases } from "../src/soundNotifier.js"

describe("findSoundInBases", () => {
    test("returns the first existing .oga file across bases", () => {
        const exists = (p: string) => p.includes("/second/sounds/") && p.endsWith("service-login.oga")

        const bases = ["/first/sounds", "/second/sounds"]
        const result = findSoundInBases(bases, "service-login", exists)

        expect(result).toBe("/second/sounds/freedesktop/stereo/service-login.oga")
    })

    test("respects XDG_DATA_HOME priority — first base wins", () => {
        const exists = (p: string) => p.includes("/home-user/") && p.endsWith("service-login.oga")

        const bases = ["/home-user/.local/share/sounds", "/usr/share/sounds"]
        const result = findSoundInBases(bases, "service-login", exists)

        expect(result).toBe("/home-user/.local/share/sounds/freedesktop/stereo/service-login.oga")
    })

    test("returns null when no file exists", () => {
        const exists = () => false

        const bases = ["/tmp/nonexistent"]
        const result = findSoundInBases(bases, "dialog-error", exists)

        expect(result).toBeNull()
    })

    test("honors .disabled pseudo-format — returns null if .disabled exists", () => {
        const exists = (p: string) => p.endsWith(".disabled")

        const bases = ["/usr/share/sounds"]
        const result = findSoundInBases(bases, "service-logout", exists)

        expect(result).toBeNull()
    })

    test("falls through .oga to .ogg to .wav across extensions", () => {
        const exists = (p: string) => p.endsWith(".ogg")

        const bases = ["/test/sounds"]
        const result = findSoundInBases(bases, "service-login", exists)

        expect(result).toBe("/test/sounds/freedesktop/stereo/service-login.ogg")
    })

    test(".disabled terminates lookup even when .oga also exists", () => {
        const exists = (p: string) => p.endsWith(".disabled") || p.endsWith(".oga")

        const bases = ["/test/sounds"]
        const result = findSoundInBases(bases, "service-login", exists)

        expect(result).toBeNull()
    })
})
