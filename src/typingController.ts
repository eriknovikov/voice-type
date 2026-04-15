import { spawn, type ChildProcessWithoutNullStreams } from "child_process"
import { log } from "./logger.js"
import { DiffEnum } from "./types.js"

export default class TypingController {
    private prevText: string = ""
    private dotool: ChildProcessWithoutNullStreams
    public hasStopped = false
    constructor() {
        this.dotool = this.initDotool()
    }
    
    private initDotool() {
        const dotool = spawn("dotool", [], {
            // We force "us" layout so standard ASCII characters always map correctly.
            // All accents and special characters will be handled by our Hex Input logic.
            env: { ...process.env, DOTOOL_XKB_LAYOUT: "us" },
        })

        dotool.stderr.on("data", (data) => {
            const lines = data.toString().split("\n").filter(Boolean)
            for (const line of lines) {
                console.log(`[DOTOOL] ${line}`)
            }
        })
        dotool.on("exit", (_code, signal) => {
            log(`dotool finished with signal [${signal}]`)
        })

        return dotool
    }

    public sendBackspaces(count: number) {
        if (count <= 0) return
        if (!this.dotool.stdin.writable) {
            log("dotool stdin not writable")
            return
        }
        // Send backspace key press count times
        const cmdString = "key BackSpace \n".repeat(count)
        this.dotool.stdin.write(cmdString)
    }

    public typeText(text: string) {
        if (!text) return
        if (!this.dotool.stdin.writable) {
            log("dotool stdin not writable")
            return
        }

        let script = ""
        let asciiBuffer = ""

        // Helper to flush standard characters to the script
        const flushAscii = () => {
            if (asciiBuffer.length > 0) {
                script += `type ${asciiBuffer}\n`
                asciiBuffer = ""
            }
        }

        // Iterate over the string by Unicode code points (safely handles emojis)
        for (const char of text) {
            const codePoint = char.codePointAt(0)
            if (!codePoint) continue

            // Handle Newlines
            if (codePoint === 10) {
                flushAscii()
                script += `key enter\n`
            }
            // Handle standard printable ASCII (a-z, 0-9, basic punctuation)
            else if (codePoint >= 32 && codePoint <= 126) {
                asciiBuffer += char
            }
            // Handle Accents, Dead Keys, Emojis, and CJK (Chinese/Japanese)
            else {
                flushAscii()
                const hex = codePoint.toString(16)

                // GNOME/GTK Unicode Hex Input Sequence using chord notation
                script += `key ctrl+shift+u\n`

                // Type the hex code
                for (const hexChar of hex) {
                    script += `key ${hexChar}\n`
                }

                // Confirm the hex input
                script += `key enter\n`
            }
        }

        flushAscii()

        // Send the entire sequence to dotool instantly
        this.dotool.stdin.write(script)
    }

    public applyDiff(currText: string, diffResult: DiffEnum) {
        if (this.hasStopped) return
        switch (diffResult) {
            case DiffEnum.NoChange:
                // Do nothing
                break

            case DiffEnum.ChangeRes:
                let charsToAdd = currText
                if (this.prevText === "") {
                    this.typeText(charsToAdd)
                } else {
                    const commonPrefixLen = findCommonPrefixLen(currText, this.prevText)
                    const charsToDelete = this.prevText.length - commonPrefixLen
                    charsToAdd = charsToAdd.slice(commonPrefixLen)
                    this.sendBackspaces(charsToDelete)
                    this.typeText(charsToAdd)
                }
                this.prevText = currText

                break

            case DiffEnum.ChangeResAndClear:
                this.reset()
                break
        }
    }

    public calculateDiff(currText: string): DiffEnum {
        if (currText === this.prevText) {
            return DiffEnum.NoChange
        }

        if (currText.trim() === "") {
            return DiffEnum.ChangeResAndClear
        }

        return DiffEnum.ChangeRes
    }

    public reset() {
        this.prevText = ""
    }

    public destroy() {
        this.dotool.kill("SIGTERM")
    }

    public calculateAndApplyDiff(str: string) {
        const diffRes = this.calculateDiff(str)
        if (diffRes == DiffEnum.ChangeResAndClear) log(`[SpeechUpdate] "${this.prevText}"`)
        this.applyDiff(str, diffRes)
    }
}

function findCommonPrefixLen(currText: string, prevText: string) {
    let i = 0
    while (currText[i] === prevText[i]) i++
    return i
}
