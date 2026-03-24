import Daemon from "./daemon.js"
import * as cli from "./cli.js"

const PORT = 3232

const flags = process.argv.slice(2)
const parsedFlags = cli.parseFlags(flags)
console.log(`launching daemon with args: \n`, parsedFlags)
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

const daemon = new Daemon(parsedFlags.textNotifs, parsedFlags.soundNotifs, parsedFlags.lang)

async function destroyDaemon() {
    await daemon.destroy()
    process.exit(0)
}
process.on("SIGTERM", destroyDaemon)
process.on("SIGINT", destroyDaemon)

daemon.start(PORT).catch(console.error)
