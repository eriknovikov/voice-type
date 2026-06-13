import { Browser, Page } from "puppeteer-core"
import * as browser from "./browser.js"
import TypingController from "./typingController.js"
import { isPortInUse, log } from "./utils.js"
import express, { type Express, type Request, type Response, type NextFunction } from "express"
import Notifier from "./notifier.js"
import { launchBrowser } from "./browserLauncher.js"
import { PORT } from "./index.js"
import { DEFAULT_LANGUAGE, isValidLanguage, readLanguageQuery } from "./language.js"
import { capitalizeFirst, endsSentence, transformTranscript } from "./transcriptTransformer.js"

export default class Daemon {
    private defaultLanguage: string
    private stream: boolean
    private timeout: number
    private punctuation: boolean
    // Sentence-start state for --punctuation: in stream mode a long pause
    // finalizes the WSA segment and the next transcript arrives as a fresh
    // string, so "capitalize after a period" can't be derived from the text
    // itself. Track whether the previous segment ended a sentence instead.
    private capitalizeNext: boolean = true
    private lastTransformedText: string = ""
    private browser: Browser | null = null
    private page: Page | null = null
    private isWSAListening: boolean = false
    private app: Express

    private typingController: TypingController = new TypingController()
    private notifier: Notifier
    private stopCooldown: boolean = false
    private silenceTimer: NodeJS.Timeout | null = null

    constructor(
        textNotifsEnabled: boolean,
        soundsNotifsEnabled: boolean,
        stream?: boolean,
        wsaLanguage?: string,
        timeout?: number,
        punctuation?: boolean,
    ) {
        this.app = express()
        this.setupRoutes()
        this.notifier = new Notifier({ textNotifsEnabled, soundsNotifsEnabled })
        this.defaultLanguage = wsaLanguage ?? DEFAULT_LANGUAGE
        this.stream = stream ?? false
        this.timeout = timeout ?? 0
        this.punctuation = punctuation ?? false
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
                // Language arg on an already-listening toggle is intentionally dropped:
                // a swap mid-session would require re-instantiating SpeechRecognition,
                // which is deferred. Just stop (with cooldown + typing reset).
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
            return this.defaultLanguage
        }
        if (!isValidLanguage(requested)) {
            log(`Invalid language param '${requested}'`)
            // Fire-and-forget: notify + respond without mutating state
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

        log(`Starting transcription in '${lang}'...`)
        this.capitalizeNext = true
        this.lastTransformedText = ""
        this.isWSAListening = true
        this.typingController.hasStopped = false
        this.notifier.notifyMicStart()

        if (this.stream && this.timeout > 0) {
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
            log(`Stop request ignored - still in cooldown period (reason: ${reason})`)
            res?.status(429).send("Cooldown active")
            return
        }
        if (!this.isWSAListening) {
            log("No active listener.")
            res?.send("No active listener")
            return
        }
        this.clearSilenceTimer()

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
        await this.page.evaluate(browser.initWSA, this.stream, this.defaultLanguage)
    }

    private transformIfEnabled(rawText: string): string {
        if (!this.punctuation) return rawText
        let text = transformTranscript(rawText)
        if (text.trim() === "") {
            // Segment finalized (interim transcript reset): the next segment
            // starts a new sentence only if this one ended with one.
            //
            // This is the ONLY place capitalizeNext is recomputed, and it reads
            // the LAST interim of the segment — so corrections that arrive
            // before finalization ("hello perod" -> "hello period") are
            // reflected. A correction that ships only in the segment's final
            // result is never seen: stream mode forwards interim text only, so
            // the final result reaches us as this empty update. That's fine —
            // such a correction is equally invisible to the typed text, so the
            // capitalization decision always agrees with what's on screen.
            if (this.lastTransformedText !== "") {
                this.capitalizeNext = endsSentence(this.lastTransformedText)
                this.lastTransformedText = ""
            }
            return text
        }
        // Applied on every interim update of the segment so the prefix diff
        // never flips the first letter back and forth.
        if (this.capitalizeNext) text = capitalizeFirst(text)
        this.lastTransformedText = text
        return text
    }

    private handleSpeechUpdate(payload: { text: string }) {
        this.typingController.calculateAndApplyDiff(this.transformIfEnabled(payload.text))
        if (this.stream && this.timeout > 0) {
            this.resetSilenceTimer()
        }
    }

    private resetSilenceTimer() {
        this.clearSilenceTimer()
        if (this.timeout > 0) {
            this.silenceTimer = setTimeout(() => {
                if (this.isWSAListening) {
                    this.stopTranscription("silence")
                }
            }, this.timeout * 1000)
        }
    }

    private clearSilenceTimer() {
        if (this.silenceTimer) {
            clearTimeout(this.silenceTimer)
            this.silenceTimer = null
        }
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
        this.clearSilenceTimer()
        this.notifier.destroy()
        this.typingController.destroy()

        await this.page?.close()
        await this.browser?.close()
    }
}
