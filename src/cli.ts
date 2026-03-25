import { spawn } from "child_process"
import { parseArgs } from "node:util"
import { WSA_LANGUAGES } from "./constants.js"
import type { CliFlags } from "./types.js"

const options = {
    sound: {
        type: "boolean",
        default: false,
        short: "s",
    },
    "no-text": {
        type: "boolean",
        default: false,
    },
    lang: {
        type: "string",
        default: "en-US",
        short: "l",
    },
    detached: {
        type: "boolean",
        short: "d",
        default: false,
    },
    help: {
        type: "boolean",
        short: "h",
        default: false,
    },
} as const

const HELP_TEXT = `
VOICE TYPE - Real-Time Dictation Daemon

Usage: voice-type [options]

Options:
  -l, --lang <lang>       Set Web Speech API language (e.g., en-US, es-ES). Default: en-US
  --no-text                Disable text notifications (default: false)
  --sound                  Enable sound notifications (default: false)
  -d, --detached           Run the daemon in the background (detached mode)
  -h, --help               Show this help message

Supported Languages (most common):
  English: en-US
  Spanish: es-ES
  Russian: ru-RU
  Chinese: zh-CN
  French: fr-FR

To see the exhaustive list of languages, visit:
  https://github.com/eriknovikov/voice-type/tree/master/src/constants.ts
`

function isValidLanguage(lang: string): boolean {
    return Object.values(WSA_LANGUAGES).includes(lang as any)
}

function pruneFlags(flags: string[]) {
    return flags.filter((flag) => flag !== "--detached" && flag !== "-d")
}

export function respawnDetached(args: string[]) {
    // Spawn the exact same binary, but detached from the current terminal

    const bin = args[0]
    const prunedFlags = pruneFlags(args.slice(2))
    const child = spawn(bin, [args[1], ...prunedFlags], {
        detached: true,
        stdio: "ignore", // Disconnect standard I/O so the terminal can be closed
    })

    // Unreference the child so the parent process can exit immediately
    child.unref()

    console.log(`Voice Type daemon started in detached mode with PID: ${child.pid}`)
    console.log(`you can stop it by running:`)
    console.log(`curl http://localhost:3232/exit`)
}
export function parseFlags(args: string[]): CliFlags {
    const { values } = parseArgs({
        args,
        options,
        strict: true,
        allowPositionals: false,
    })

    const lang = values.lang
    if (!isValidLanguage(lang)) {
        console.error(`Error: Invalid language '${lang}'`)
        console.error(`Supported languages: ${Object.values(WSA_LANGUAGES).join(", ")}`)
        process.exit(1)
    }

    return {
        lang,
        textNotifs: !values["no-text"],
        soundNotifs: values["sound"],
        detached: values.detached,
        help: values.help,
    }
}

export function showHelp() {
    console.log(HELP_TEXT)
}
