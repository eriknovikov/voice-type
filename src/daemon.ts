import { Browser, Page } from "puppeteer-core"
import { startListening, stopListening, initWSA } from "./browser.js"
import TypingController from "./typingController.js"
import { log } from "./logger.js"
import express, { type Express, type Response } from "express"
import Notifier from "./notifier.js"
import { type BrowserType, launchBrowser } from "./browserLauncher.js"
import { createServer } from "net"
import { PORT } from "./index.js"

function isPortInUse(port: number): Promise<boolean> {
    return new Promise((resolve) => {
        const server = createServer()
        server.once("error", () => {
            resolve(true)
        })
        server.once("listening", () => {
            server.once("close", () => {
                resolve(false)
            })
            server.close()
        })
        server.listen(port, "127.0.0.1")
    })
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
            await this.startTranscription(res)
        })

        this.app.get("/stop", async (req, res) => {
            await this.stopTranscription("intentional", res)
        })

        this.app.get("/toggle", async (req, res) => {
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
            log("Listener already active.")
            res.send("Listener already active")
            return
        }
        log("Starting transcription...")
        this.isWSAListening = true
        this.notifier.notifyMicStart()
        await this.page!.evaluate(startListening)
        res.send("Listening")
    }

    private async stopTranscription(reason: "intentional" | "offline", res?: Response) {
        if (this.stopCooldown) {
            log(`Stop request ignored - still in cooldown period (reason: ${reason})`)
            res?.status(429).send("Cooldown active")
            return
        }

        if (!this.isBrowserReady()) {
            log("Browser not ready - cannot stop transcription")
            res?.status(503).send("Browser not ready")
            return
        }
        if (!this.isWSAListening) {
            log("No active listener.")
            res?.send("No active listener")
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
        res?.send("Stopped")
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
        await this.page.exposeFunction("onError", this.handleError.bind(this))
        await this.page.evaluate(initWSA, this.wsaLanguage)
    }

    private handleSpeechUpdate(payload: { text: string }) {
        this.typingController.calculateAndApplyDiff(payload.text)
    }

    private async handleOffline(payload: {}) {
        log("Offline. Please connect to network")
        await this.stopTranscription("offline")
    }

    private async handleError(payload: { type: string; message: string }) {
        const { type, message } = payload

        // Map error types to user-friendly messages
        let userMessage = message
        switch (type) {
            case "not-allowed":
                userMessage = "Microphone permission denied. Please allow microphone access in your browser settings."
                break
            case "no-speech":
                userMessage = "No speech detected. Please check your microphone and try again."
                break
            case "audio-capture":
                userMessage = "No microphone found. Please connect a microphone and try again."
                break
            case "network":
                userMessage = "Network error. Please check your internet connection."
                break
            case "not-supported":
                userMessage = "Web Speech API not supported. Try using Chrome or Chromium-based browsers."
                break
            case "service-not-allowed":
                userMessage =
                    "Speech recognition service not allowed. This browser may have compatibility issues. Try Chrome or Chromium."
                break
            case "aborted":
                userMessage = "Speech recognition was aborted."
                break
            default:
                userMessage = message || `Speech recognition error: ${type}`
        }

        log(`Speech recognition error (${type}): ${userMessage}`)
        this.notifier.notifyError(userMessage)

        // Stop transcription on most errors, but not on 'no-speech' or 'aborted'
        if (type !== "no-speech" && type !== "aborted") {
            await this.stopTranscription("offline")
        }
    }

    //start spawns browser and server listener
    public async start(port: number, browserType: BrowserType, customBrowserPath?: string) {
        //silently drop start requests when server is already running
        if (await isPortInUse(PORT)) {
            await this.notifier.notifyAlreadyRunning()
            log("Daemon already running on port " + PORT)
            process.exit(0)
        }

        try {
            this.app.listen(port, "127.0.0.1", () => {
                log(`server started on port: ${port}`)
            })
            await this.initBrowser(browserType, customBrowserPath)
            this.notifier.notifyDaemonStart("F9")
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
