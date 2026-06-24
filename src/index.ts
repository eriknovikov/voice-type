import Daemon from "./daemon.js"
import { showHelp } from "./cli.js"
import { loadConfig } from "./config.js"

process.title = "voice-type"
process.argv[0] = "voice-type"

const arg = process.argv[2]

if (arg === "help" || arg === "-h" || arg === "--help") {
    showHelp()
    process.exit(0)
}
if (arg === "update") {
    console.error("voice-type update is not yet implemented")
    process.exit(1)
}
if (arg === "shortcuts") {
    if (process.argv[3] !== "--apply") {
        showHelp()
        process.exit(0)
    }
    console.error("voice-type shortcuts --apply is not yet implemented")
    process.exit(1)
}
if (arg !== undefined) {
    showHelp()
    process.exit(0)
}

async function main() {
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
    console.error(e)
    process.exit(1)
})
