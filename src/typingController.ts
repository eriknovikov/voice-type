import type { DotoolKeyChord } from "./transcriptTransformers/types.js"
import type { DotoolSink } from "./dotoolSink.js"
import { spawnDotoolSink } from "./dotoolSink.js"
import { log } from "./utils.js"

export default class TypingController {
    private prevText: string = ""
    private dotool: DotoolSink
    public hasStopped = false

    constructor(sink?: DotoolSink) {
        this.dotool = sink ?? spawnDotoolSink()
    }

    public sendBackspaces(count: number) {
        if (count <= 0) return
        if (!this.dotool.writable) {
            log("dotool stdin not writable")
            return
        }
        const cmdString = "key BackSpace \n".repeat(count)
        this.dotool.write(cmdString)
    }

    public sendKeyChord(chord: DotoolKeyChord) {
        if (this.hasStopped) return
        if (!this.dotool.writable) {
            log("dotool stdin not writable")
            return
        }
        this.dotool.write(`key ${chord}\n`)
    }

    public typeText(text: string) {
        if (this.hasStopped) return
        if (!text) return
        if (!this.dotool.writable) {
            log("dotool stdin not writable")
            return
        }

        let script = ""
        let asciiBuffer = ""

        const flushAscii = () => {
            if (asciiBuffer.length > 0) {
                script += `type ${asciiBuffer}\n`
                asciiBuffer = ""
            }
        }

        for (const char of text) {
            const codePoint = char.codePointAt(0)
            if (!codePoint) continue

            if (codePoint === 10) {
                flushAscii()
                script += `key enter\n`
            } else if (codePoint >= 32 && codePoint <= 126) {
                asciiBuffer += char
            } else {
                flushAscii()
                const hex = codePoint.toString(16)
                script += `key ctrl+shift+u\n`
                for (const hexChar of hex) {
                    script += `key ${hexChar}\n`
                }
                script += `key enter\n`
            }
        }

        flushAscii()
        this.dotool.write(script)
    }

    public applyLiveText(currText: string) {
        if (this.hasStopped) return
        if (currText === this.prevText) return

        if (this.prevText === "") {
            this.typeText(currText)
        } else {
            const commonPrefixLen = findCommonPrefixLen(currText, this.prevText)
            const charsToDelete = this.prevText.length - commonPrefixLen
            const charsToAdd = currText.slice(commonPrefixLen)
            this.sendBackspaces(charsToDelete)
            this.typeText(charsToAdd)
        }
        this.prevText = currText
    }

    public finalizeSegment() {
        this.prevText = ""
    }

    public reset() {
        this.prevText = ""
    }

    public destroy() {
        this.dotool.kill("SIGTERM")
    }
}

function findCommonPrefixLen(currText: string, prevText: string) {
    let i = 0
    while (currText[i] === prevText[i]) i++
    return i
}
