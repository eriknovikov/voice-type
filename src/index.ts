import Daemon from "./daemon.js"
import { showHelp } from "./cli.js"
import { loadConfig } from "./config.js"
import { initLogger, log } from "./logger.js"

process.title = "voice-type"
process.argv[0] = "voice-type"

const arg = process.argv[2]

if (arg === "help" || arg === "-h" || arg === "--help") {
    showHelp()
    process.exit(0)
}

async function main() {
    initLogger()

    if (arg === "update") {
        const { runUpdate } = await import("./updater.js")
        await runUpdate()
        return
    }

    if (arg !== undefined) {
        showHelp()
        process.exit(0)
    }

    const config = await loadConfig()
    const daemon = new Daemon(config)

    const destroyDaemon = async () => {
        await daemon.destroy()
        process.exit(0)
    }
    process.on("SIGTERM", destroyDaemon)
    process.on("SIGINT", destroyDaemon)

    await daemon.start()
}

main().catch((e) => {
    log("SYSTEM", "fatal:", e)
    process.exit(1)
})
