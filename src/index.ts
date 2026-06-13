import Daemon from "./daemon.js"
import * as cli from "./cli.js"

export const PORT = 3232

const flags = process.argv.slice(2)
const parsedFlags = cli.parseFlags(flags)
console.log("launch args:", JSON.stringify(parsedFlags))
if (parsedFlags.help) {
    cli.showHelp()
    process.exit(0)
}

if (parsedFlags.detached) {
    cli.respawnDetached(process.argv)
    process.exit(0)
}

process.title = "voice-type"
process.argv[0] = "voice-type"

const daemon = new Daemon(
    parsedFlags.textNotifs,
    parsedFlags.soundNotifs,
    parsedFlags.stream,
    // The CLI --lang flag is the startup default; per-request language is
    // resolved inside Daemon.setupRoutes() from the ?language= / ?lang=
    // query param of /start and /toggle.
    parsedFlags.lang,
    parsedFlags.timeout,
    parsedFlags.punctuation,
)

async function destroyDaemon() {
    await daemon.destroy()
    process.exit(0)
}
process.on("SIGTERM", destroyDaemon)
process.on("SIGINT", destroyDaemon)

if (parsedFlags.browserPath) {
    process.env.BROWSER_PATH = parsedFlags.browserPath
}
process.env.BROWSER_TYPE = parsedFlags.browserType

daemon.start(PORT).catch(console.error)
