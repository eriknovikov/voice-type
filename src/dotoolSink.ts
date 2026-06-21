import { spawn, type ChildProcessWithoutNullStreams } from "child_process"
import { log } from "./utils.js"

export interface DotoolSink {
    write(data: string): void
    readonly writable: boolean
    kill(signal?: NodeJS.Signals): void
}

export function spawnDotoolSink(): DotoolSink {
    const dotool: ChildProcessWithoutNullStreams = spawn("dotool", [], {
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

    return {
        write(data: string) {
            dotool.stdin.write(data)
        },
        get writable() {
            return dotool.stdin.writable
        },
        kill(signal: NodeJS.Signals = "SIGTERM") {
            dotool.kill(signal)
        },
    }
}
