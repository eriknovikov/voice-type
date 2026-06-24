import { Browser, Page } from "puppeteer-core"
import * as browser from "./browser.js"
import TypingController from "./typingController.js"
import { log } from "./logger.js"
import { runPreflight } from "./preflight.js"
import express, { type Express, type Request, type Response, type NextFunction } from "express"
import Notifier from "./notifier.js"
import { launchBrowser } from "./browserLauncher.js"
import { isValidLanguage, readLanguageQuery } from "./language.js"
import { createTranscriptTransformerSession } from "./transcriptTransformers/index.js"
import { createNoopTransformerSession } from "./transcriptTransformers/noop.js"
import type { TranscriptTransformerSession } from "./transcriptTransformers/types.js"
import SpeechPipeline from "./speechPipeline.js"
import { shouldAcceptSpeechEvent } from "./speechEventGate.js"
import type { SpeechEvent, VoiceTypeConfig } from "./types.js"

export default class Daemon {
    private readonly config: VoiceTypeConfig
    private transcriptTransformer: TranscriptTransformerSession = createNoopTransformerSession()
    private typingController: TypingController = new TypingController()
    private speechPipeline: SpeechPipeline = new SpeechPipeline(this.transcriptTransformer, this.typingController)
    private browser: Browser | null = null
    private page: Page | null = null
    private isWSAListening: boolean = false
    private app: Express

    private notifier: Notifier
    private stopCooldown: boolean = false
    private silenceTimer: NodeJS.Timeout | null = null

    constructor(config: VoiceTypeConfig) {
        this.config = config
        this.app = express()
        this.setupRoutes()
        this.notifier = new Notifier({ textNotifsEnabled: config.text, soundsNotifsEnabled: config.sound })
    }

    private setupRoutes() {
        this.app.get("/health", (req, res) => {
            res.json({ status: "ok" })
        })

        this.app.get("/start", this.browserHealthMiddleware.bind(this), async (req, res) => {
            const lang = this.resolveAndValidateLanguage(req, res)
            if (lang === null) return
            await this.startTranscription(lang, res)
        })

        this.app.get("/stop", this.browserHealthMiddleware.bind(this), async (req, res) => {
            await this.stopTranscription("intentional", res)
        })

        this.app.get("/toggle", this.browserHealthMiddleware.bind(this), async (req, res) => {
            if (this.isWSAListening) {
                await this.stopTranscription("intentional", res)
            } else {
                const lang = this.resolveAndValidateLanguage(req, res)
                if (lang === null) return
                await this.startTranscription(lang, res)
            }
        })

        this.app.get("/exit", async (req, res) => {
            await this.notifier.notifyDaemonStop()
            res.send("Stopped daemon")
            await this.destroy()
            process.exit(0)
        })
    }

    private resolveAndValidateLanguage(req: Request, res: Response): string | null {
        const requested = readLanguageQuery(req.query as Record<string, unknown>)
        if (requested === undefined) {
            return this.config.lang
        }
        if (!isValidLanguage(requested)) {
            log("DAEMON", `Invalid language param '${requested}'`)
            void this.notifier.notifyError(`Invalid language: ${requested}`)
            res.status(400).send(`Invalid language: ${requested}`)
            return null
        }
        return requested
    }

    private async startTranscription(lang: string, res: Response) {
        if (this.stopCooldown) {
            res.status(429).send("Cooldown active - wait before starting")
            return
        }
        if (this.isWSAListening) {
            res.send("Listener already active")
            return
        }

        log("DAEMON", `Starting transcription in '${lang}'...`)
        this.transcriptTransformer = createTranscriptTransformerSession(lang, this.config.stream)
        this.transcriptTransformer.reset()
        this.speechPipeline = new SpeechPipeline(this.transcriptTransformer, this.typingController)
        this.isWSAListening = true
        this.typingController.hasStopped = false
        this.notifier.notifyMicStart()

        if (this.config.stream && this.config.timeout > 0) {
            this.resetSilenceTimer()
        }

        await this.page!.evaluate(browser.setLangAndStart, lang)
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
            log("DAEMON", `Stop request ignored - still in cooldown period (reason: ${reason})`)
            res?.status(429).send("Cooldown active")
            return
        }
        if (!this.isWSAListening) {
            log("DAEMON", "No active listener.")
            res?.send("No active listener")
            return
        }
        this.clearSilenceTimer()

        log("DAEMON", `Stopping transcription... Reason: ${reason}`)
        this.isWSAListening = false
        this.transcriptTransformer.reset()
        this.typingController.hasStopped = true
        this.typingController.reset()

        if (reason === "intentional") {
            this.notifier.notifyMicStopIntentional()
        } else if (reason === "silence") {
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
            log("DAEMON", "Browser not ready")
            res.status(503).send("Browser not ready")
            return
        }

        try {
            await this.page!.evaluate(browser.healthCheck)
            next()
        } catch (e) {
            log("DAEMON", `Browser health check failed: ${e} - reinitializing...`)
            try {
                await this.reinitBrowser()
                next()
            } catch (e) {
                log("DAEMON", `Browser reinitialization failed: ${e}`)
                res.status(503).send("Browser reinitialization failed")
            }
        }
    }

    private async initBrowser() {
        this.browser = await launchBrowser(this.config)
        this.page = await this.browser.newPage()
        this.page.on("console", (msg) => log("BROWSER", msg.text()))

        await this.page.goto("data:text/html,<html><body><h1>Voice Type</h1></body></html>")
        await this.page.exposeFunction("onSpeechEvent", this.handleSpeechEvent.bind(this))
        await this.page.exposeFunction("onBrowserRecStop", this.handleBrowserRecStop.bind(this))
        await this.page.evaluate(browser.initWSA, this.config.stream, this.config.lang)
    }

    private handleSpeechEvent(event: SpeechEvent) {
        if (!shouldAcceptSpeechEvent(this.isWSAListening, this.typingController.hasStopped)) return

        this.speechPipeline.onEvent(event)
        if (event.kind === "text" && this.config.stream && this.config.timeout > 0) {
            this.resetSilenceTimer()
        }
    }

    private resetSilenceTimer() {
        this.clearSilenceTimer()
        if (this.config.timeout > 0) {
            this.silenceTimer = setTimeout(() => {
                if (!this.isWSAListening) return
                void this.stopTranscription("silence").catch((e) => {
                    log("DAEMON", `Silence timer stop failed: ${e}`)
                })
            }, this.config.timeout * 1000)
        }
    }

    private clearSilenceTimer() {
        if (this.silenceTimer) {
            clearTimeout(this.silenceTimer)
            this.silenceTimer = null
        }
    }
    private async handleBrowserRecStop(payload: { reason: "silence" | "offline" | undefined }) {
        if (!this.isWSAListening) return
        try {
            await this.stopTranscription(payload.reason ?? "silence")
        } catch (e) {
            log("DAEMON", `Browser rec stop handling failed: ${e}`)
        }
    }

    public async start() {
        const result = await runPreflight({
            port: this.config.port,
            browser_path: this.config.browser_path,
        })
        if (!result.ok) {
            const { kind, message } = result.failure
            if (kind === "port-in-use") {
                await this.notifier.notifyAlreadyRunning()
                log("DAEMON", message)
                process.exit(0)
            }
            await this.notifier.notifyError(message)
            log("PREFLIGHT", message)
            log("DAEMON", `startup failure: ${message}`)
            await this.destroy()
            process.exit(1)
        }

        try {
            this.app.listen(this.config.port, "127.0.0.1", () => {
                log("DAEMON", `server started on port: ${this.config.port}`)
            })
            await this.initBrowser()
            this.notifier.notifyDaemonStart()
        } catch (e) {
            this.notifier.notifyError("Failed to initialize Voice Type daemon.")
            log("DAEMON", "Failed to initialize Voice Type daemon:", e)
            await this.destroy()
            process.exit(1)
        }
    }

    public async destroy() {
        log("DAEMON", "Shutting down daemon...")
        this.clearSilenceTimer()
        this.transcriptTransformer.reset()
        this.notifier.destroy()
        this.typingController.destroy()

        await this.page?.close()
        await this.browser?.close()
    }
}
