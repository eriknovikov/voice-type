import { Browser, Page } from "puppeteer-core"
import * as browser from "./browser.js"
import TypingController from "./typingController.js"
import { isPortInUse, log } from "./utils.js"
import express, { type Express, type Request, type Response, type NextFunction } from "express"
import Notifier from "./notifier.js"
import { launchBrowser } from "./browserLauncher.js"
import { PORT } from "./index.js"

export default class Daemon {
    private wsaLanguage: string
    private stream: boolean
    private browser: Browser | null = null
    private page: Page | null = null
    private isWSAListening: boolean = false
    private app: Express

    private typingController: TypingController = new TypingController()
    private notifier: Notifier
    private stopCooldown: boolean = false

    constructor(textNotifsEnabled: boolean, soundsNotifsEnabled: boolean, stream?: boolean, wsaLanguage?: string) {
        this.app = express()
        this.setupRoutes()
        this.notifier = new Notifier({ textNotifsEnabled, soundsNotifsEnabled })
        this.wsaLanguage = wsaLanguage ?? "en-US"
        this.stream = stream ?? false
    }

    private setupRoutes() {
        this.app.get("/health", (req, res) => {
            res.json({ status: "ok" })
        })

        this.app.get("/start", this.browserHealthMiddleware.bind(this), async (req, res) => {
            await this.startTranscription(res)
        })

        this.app.get("/stop", this.browserHealthMiddleware.bind(this), async (req, res) => {
            await this.stopTranscription("intentional", res)
        })

        this.app.get("/toggle", this.browserHealthMiddleware.bind(this), async (req, res) => {
            if (this.isWSAListening) {
                await this.stopTranscription("intentional", res)
            } else {
                await this.startTranscription(res)
            }
        })

        this.app.get("/exit", async (req, res) => {
            await this.notifier.notifyDaemonStop()
            res.send("Stopped daemon")
            await this.destroy()
            process.exit(0)
        })
    }

    private async startTranscription(res: Response) {
        if (this.stopCooldown) {
            res.status(429).send("Cooldown active - wait before starting")
            return
        }
        if (this.isWSAListening) {
            res.send("Listener already active")
            return
        }

        log("Starting transcription...")
        this.isWSAListening = true
        this.typingController.hasStopped = false
        this.notifier.notifyMicStart()

        await this.page!.evaluate(browser.startListening)
        res.send("Listening")
    }

    private async reinitBrowser() {
        if (this.browser) {
            try {
                await this.browser.close()
            } catch {}
        }
        this.browser = null
        this.page = null
        this.isWSAListening = false
        await this.initBrowser()
    }

    private async stopTranscription(reason: "intentional" | "offline" | "silence", res?: Response) {
        if (this.stopCooldown) {
            log(`Stop request ignored - still in cooldown period (reason: ${reason})`)
            res?.status(429).send("Cooldown active")
            return
        }
        if (!this.isWSAListening) {
            log("No active listener.")
            res?.send("No active listener")
            return
        }
        log(`Stopping transcription... Reason: ${reason}`)
        this.isWSAListening = false
        this.typingController.hasStopped = true
        this.typingController.reset()

        // Trigger corresponding notification
        if (reason === "intentional") {
            this.notifier.notifyMicStopIntentional()
        } else if (reason == "silence") {
            this.notifier.notifyMicStopSilence()
        } else if (reason === "offline") {
            this.notifier.notifyOffline()
        }

        this.stopCooldown = true
        setTimeout(() => {
            this.stopCooldown = false
        }, 100)

        await this.page!.evaluate(browser.stopRecognition)
        res?.send("Stopped")
    }

    private isBrowserReady(): boolean {
        return this.page !== null && this.browser !== null
    }

    private async browserHealthMiddleware(req: Request, res: Response, next: NextFunction) {
        if (!this.isBrowserReady()) {
            log("Browser not ready - initializing...")
            throw new Error("Unexpected path. browser should be ready already")
        }

        try {
            await this.page!.evaluate(browser.healthCheck)
            next()
        } catch (e) {
            log(`Browser health check failed: ${e} - reinitializing...`)
            try {
                await this.reinitBrowser()
                next()
            } catch (e) {
                res.status(503).send("Browser reinitialization failed")
            }
        }
    }

    private async initBrowser() {
        this.browser = await launchBrowser()
        this.page = await this.browser.newPage()
        this.page.on("console", (msg) => console.log("[BROWSER]", msg.text()))

        await this.page.goto("data:text/html,<html><body><h1>Voice Type</h1></body></html>")
        await this.page.exposeFunction("onSpeechUpdate", this.handleSpeechUpdate.bind(this))
        await this.page.exposeFunction("onBrowserRecStop", this.handleBrowserRecStop.bind(this))
        await this.page.evaluate(browser.initWSA, this.stream, this.wsaLanguage)
    }

    private handleSpeechUpdate(payload: { text: string }) {
        this.typingController.calculateAndApplyDiff(payload.text)
    }
    private async handleBrowserRecStop(payload: { reason: "silence" | "offline" | undefined }) {
        if (this.isWSAListening) await this.stopTranscription(payload.reason ?? "intentional")
    }

    //start spawns browser and server listener
    public async start(port: number) {
        if (await isPortInUse(PORT)) {
            await this.notifier.notifyAlreadyRunning()
            log("Daemon already running on port " + PORT)
            process.exit(0)
        }

        try {
            this.app.listen(port, "127.0.0.1", () => {
                log(`server started on port: ${port}`)
            })
            await this.initBrowser()
            this.notifier.notifyDaemonStart()
        } catch (e) {
            this.notifier.notifyError("Failed to initialize Voice Type daemon.")
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
