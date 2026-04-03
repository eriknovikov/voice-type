import puppeteer, { Browser, Page } from "puppeteer-core"
import { startListening, stopListening, initWSA } from "./browser.js"
import TypingController from "./typingController.js"
import { log } from "./logger.js"
import express, { type Express } from "express"
import Notifier from "./notifier.js"
import { type BrowserType, launchBrowser } from "./browserLauncher.js"
import { createServer } from "net"

const DAEMON_PORT = 3232

function isPortInUse(port: number): Promise<boolean> {
    return new Promise((resolve) => {
        const server = createServer()
        server.once("error", () => {
            resolve(true)
        })
        server.once("listening", () => {
            server.close()
            resolve(false)
        })
        server.listen(port, "127.0.0.1")
    })
}

export async function acquireLock(): Promise<void> {
    if (await isPortInUse(DAEMON_PORT)) {
        log("Daemon already running on port " + DAEMON_PORT)
        process.exit(0)
    }
}

export default class Daemon {
    private wsaLanguage: string
    private browser: Browser | null = null
    private page: Page | null = null
    private isWSAListening: boolean = false
    private app: Express

    private typingController: TypingController = new TypingController()
    private notifier: Notifier
    private stopCooldown: boolean = false

    constructor(textNotifsEnabled: boolean, soundsNotifsEnabled: boolean, wsaLanguage?: string) {
        this.app = express()
        this.setupRoutes()
        this.notifier = new Notifier({ textNotifsEnabled, soundsNotifsEnabled })
        this.wsaLanguage = wsaLanguage || "en-US"
    }

    private setupRoutes() {
        this.app.get("/start", async (req, res) => {
            if (!this.isBrowserReady()) {
                log("Browser not ready - cannot start transcription")
                this.notifier.notifyError("Browser not ready yet.")
                res.status(503).send("Wait for browser")
                return
            }
            if (this.stopCooldown) {
                log("Start request ignored - still in cooldown period after stop")
                res.status(429).send("Cooldown active - wait before starting")
                return
            }
            if (this.isWSAListening) {
                log("Already listening.")
                return
            }
            log("Starting transcription...")
            this.isWSAListening = true
            this.notifier.notifyMicStart()
            await this.page!.evaluate(startListening)
            res.send("Listening")
        })

        this.app.get("/stop", async (req, res) => {
            await this.stopTranscription("intentional")
            res.send("Stopped")
        })

        this.app.get("/exit", async (req, res) => {
            await this.notifier.notifyDaemonStop()
            res.send("Stopped daemon")
            await this.destroy()
            process.exit(0)
        })
    }

    private async stopTranscription(reason: "intentional" | "offline") {
        if (this.stopCooldown) {
            log(`Stop request ignored - still in cooldown period (reason: ${reason})`)
            return
        }

        if (!this.isBrowserReady()) {
            log("Browser not ready - cannot stop transcription")
            return
        }
        if (!this.isWSAListening) {
            log(`Cannot call stop before start (reason: ${reason})`)
            return
        }
        log(`Stopping transcription... Reason: ${reason}`)
        this.isWSAListening = false
        this.typingController.reset()

        // Trigger corresponding notification
        if (reason === "intentional") {
            this.notifier.notifyMicStop()
        } else if (reason === "offline") {
            this.notifier.notifyOffline()
        }

        this.stopCooldown = true
        setTimeout(() => {
            this.stopCooldown = false
        }, 100)

        await this.page!.evaluate(stopListening)
    }

    private isBrowserReady(): boolean {
        return this.page !== null && this.browser !== null
    }

    private async initBrowser(browserType: BrowserType, customBrowserPath?: string) {
        this.browser = await launchBrowser(browserType, customBrowserPath)
        this.page = await this.browser.newPage()
        this.page.on("console", (msg) => console.log("[BROWSER]", msg.text()))

        await this.page.goto("data:text/html,<html><body><h1>Voice Type</h1></body></html>")
        await this.page.exposeFunction("onSpeechUpdate", this.handleSpeechUpdate.bind(this))
        await this.page.exposeFunction("onOffline", this.handleOffline.bind(this))
        await this.page.evaluate(initWSA, this.wsaLanguage)
    }

    private handleSpeechUpdate(payload: { text: string }) {
        this.typingController.calculateAndApplyDiff(payload.text)
    }

    private async handleOffline(payload: {}) {
        log("Offline. Please connect to network")
        await this.stopTranscription("offline")
    }

    //start spawns browser and server listener
    public async start(port: number, browserType: BrowserType, customBrowserPath?: string) {
        // Check for existing instance (silently takes over if another daemon is running)
        await acquireLock()

        try {
            this.app.listen(port, "127.0.0.1", () => {
                log(`server started on port: ${port}`)
            })
            await this.initBrowser(browserType, customBrowserPath)
            await this.notifier.notifyDaemonStart("F9")
        } catch (e) {
            await this.notifier.notifyError("Failed to initialize Voice Type daemon.")
            console.error(e)

            process.exit(0)
        }
    }

    /**
     * Cleanup resources when shutting down the daemon
     */
    public async destroy() {
        console.log("\n[DAEMON] Shutting down daemon...")
        this.notifier.destroy()
        this.typingController.destroy()

        await this.page?.close()
        await this.browser?.close()
    }
}
