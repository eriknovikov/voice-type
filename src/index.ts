import Daemon from "./daemon.js"
import * as cli from "./cli.js"
import { PORT } from "./constants.js"
import { log } from "./utils.js"

export { PORT } from "./constants.js"

const flags = process.argv.slice(2)
const parsedFlags = cli.parseFlags(flags)
if (process.env.VOICE_TYPE_DEBUG) {
    log("launch args:", JSON.stringify(parsedFlags))
}
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

daemon.start(PORT).catch((e) => {
    console.error(e)
    process.exit(1)
})
