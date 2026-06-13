import { spawn } from "child_process"
import { parseArgs } from "node:util"
import { WSA_LANGUAGES } from "./constants.js"
import { isValidLanguage } from "./language.js"
import type { CliFlags, WSALanguage } from "./types.js"
import type { BrowserType } from "./browserLauncher.js"

const options = {
    sound: {
        type: "boolean",
        default: false,
        short: "s",
    },
    "no-stream": {
        default: false,
        type: "boolean",
    },
    text: {
        type: "boolean",
        default: false,
        short: "t",
    },
    lang: {
        type: "string",
        default: "en-US",
        short: "l",
    },
    browser_type: {
        type: "string",
        default: "chrome",
    },
    browser_path: {
        type: "string",
        short: "p",
    },
    timeout: {
        type: "string",
        default: "0",
    },
    punctuation: {
        type: "boolean",
        default: false,
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
  -l, --lang <lang>           Default language for dictation (e.g., en-US, es-ES). Used when /start
                              or /toggle is called without a ?language= param. Default: en-US
  --browser_type <browser>    Browser type: chrome or chromium. Default: chrome
  -p, --browser_path <path>   Path to browser executable
  --timeout <sec>             Auto-stop after N seconds of silence (streaming only). Default: 0
  --punctuation               Convert spoken punctuation ("comma", "period", "question mark",
                              "exclamation mark", "semicolon", "colon") and auto-capitalize
                              sentence starts (default: false)
  --no-stream                 Use final transcripts only (no interim corrections)
  -t, --text                  Enable text notifications (default: false)
  -s, --sound                 Enable sound notifications (default: false)
  -d, --detached              Run the daemon in the background (detached mode)
  -h, --help                  Show this help message

Per-request language:
  The active language can be overridden at call time via ?language=<bcp47> on
  /start and /toggle. ?lang= is also accepted. /stop and /exit ignore the param.

Supported Languages (most common):
  English: en-US
  Spanish: es-ES
  Russian: ru-RU
  Chinese: zh-CN
  French: fr-FR

To see the exhaustive list of languages, visit:
  https://github.com/eriknovikov/voice-type/tree/master/src/constants.ts
`

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

    if (values.browser_type != "chrome" && values.browser_type != "chromium") {
        console.error(`Error: Invalid browser_type '${values.browser_type}'`)
        console.error(`Supported Browsers: "chrome", "chromium"`)
        process.exit(1)
    }
    const timeout = parseInt(values.timeout, 10)
    if (isNaN(timeout) || timeout < 0) {
        console.error(`Error: Invalid timeout '${values.timeout}'. Must be a non-negative number.`)
        process.exit(1)
    }

    return {
        lang: lang as WSALanguage,
        browserType: values.browser_type as BrowserType,
        browserPath: values.browser_path,
        timeout,
        punctuation: values.punctuation,
        textNotifs: values["text"],
        stream: !values["no-stream"],
        soundNotifs: values["sound"],
        detached: values.detached,
        help: values.help,
    }
}

export function showHelp() {
    console.log(HELP_TEXT)
}
