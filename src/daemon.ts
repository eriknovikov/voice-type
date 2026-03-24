import puppeteer, { Browser, Page } from "puppeteer-core"
import { startListening, stopListening, initWSA } from "./browser.js"
import TypingController from "./typingController.js"
import { log } from "./logger.js"
import express, { type Express } from "express"
import Notifier from "./notifier.js"
import { sleep } from "bun"

export default class Daemon {
    private wsaLanguage: string = "en-US"
    private browser: Browser | null = null
    private page: Page | null = null
    private isWSAListening: boolean = false
    private app: Express

    private typingController: TypingController = new TypingController()
    private notifier: Notifier
    private stopCooldown: boolean = false
    private stopCooldownTimeout: NodeJS.Timeout | null = null

    constructor(textNotifsEnabled: boolean, soundsNotifsEnabled: boolean, wsaLanguage?: string) {
        this.app = express()
        this.setupRoutes()
        this.notifier = new Notifier({ textNotifsEnabled, soundsNotifsEnabled })
        if (wsaLanguage !== undefined) this.wsaLanguage = wsaLanguage
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

        this.app.get("/stop-daemon", async (req, res) => {
            await this.notifier.notifyDaemonStop()
            res.send("Stopped daemon")
            await this.destroy()
            await sleep(1000)
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

        // Set cooldown to prevent rapid successive stop requests
        this.stopCooldown = true
        if (this.stopCooldownTimeout) {
            clearTimeout(this.stopCooldownTimeout)
        }
        this.stopCooldownTimeout = setTimeout(() => {
            this.stopCooldown = false
        }, 100)

        await this.page!.evaluate(stopListening)
    }

    private isBrowserReady(): boolean {
        return this.page !== null && this.browser !== null
    }

    private async initBrowser() {
        this.browser = await puppeteer.launch({
            executablePath: "/usr/bin/google-chrome-stable",
            // @ts-ignore
            headless: "new",
            args: [
                "--use-fake-ui-for-media-stream",
                "--disable-background-timer-throttling",
                "--log-level=0",
                "--disable-dev-shm-usage",
                "--disable-gpu",
                "--disable-software-rasterizer",
                "--disable-background-networking",
                "--disable-default-apps",
                "--disable-extensions",
                "--disable-sync",
                "--disable-translate",
                "--metrics-recording-only",
                "--no-first-run",
                "--safebrowsing-disable-auto-update",
                "--disable-features=IsolateOrigins,site-per-process",
                "--process-per-site",
            ],
            name: "Voice-Type-browser",
        })

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
    public async start(port: number) {
        try {
            this.app.listen(port, "127.0.0.1", () => {
                log(`server started on port: ${port}`)
            })
            await this.initBrowser()
            this.notifier.notifyDaemonStart("F9")
        } catch (e) {
            this.notifier.notifyError("Failed to initialize Voice Type daemon.")
            log(`Startup error: ${e}`)
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
        clearTimeout(this.stopCooldownTimeout ?? undefined)

        await this.page?.close()
        await this.browser?.close()
    }
}
