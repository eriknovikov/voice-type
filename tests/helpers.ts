import type { DotoolSink } from "../src/dotoolSink.js"

/** Interprets dotool script lines into an on-screen string for integration tests. */
export class FakeTypingTarget {
    screen = ""
    chords: string[] = []
    private _writable = true

    readonly sink: DotoolSink

    constructor() {
        const self = this
        this.sink = {
            write: (data: string) => self.applyScript(data),
            get writable() {
                return self._writable
            },
            kill: () => {
                self._writable = false
            },
        }
    }

    private applyScript(script: string) {
        const lines = script.split("\n").filter(Boolean)
        let hexMode = false
        let hexDigits = ""

        for (const line of lines) {
            if (line.startsWith("type ")) {
                this.screen += line.slice(5)
                continue
            }

            if (!line.startsWith("key ")) continue
            const key = line.slice(4).trim()

            if (hexMode) {
                if (key === "enter") {
                    const codePoint = parseInt(hexDigits, 16)
                    if (!Number.isNaN(codePoint)) {
                        this.screen += String.fromCodePoint(codePoint)
                    }
                    hexMode = false
                    hexDigits = ""
                } else if (key.length === 1 && /[0-9a-f]/i.test(key)) {
                    hexDigits += key
                }
                continue
            }

            if (key === "BackSpace") {
                this.screen = this.screen.slice(0, -1)
            } else if (key === "enter") {
                this.screen += "\n"
            } else if (key === "ctrl+shift+u") {
                hexMode = true
                hexDigits = ""
            } else {
                this.chords.push(key)
            }
        }
    }
}

export class CapturingSink implements DotoolSink {
    writes: string[] = []
    private _writable = true

    write(data: string) {
        this.writes.push(data)
    }

    get writable() {
        return this._writable
    }

    kill() {
        this._writable = false
    }
}
