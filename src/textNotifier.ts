import { sessionBus, Variant, type MessageBus } from "dbus-next"
import type { Urgency } from "./types"
import { log } from "./logger"
const SYNC_ID = "voice-type-dictation"
/**
 * Handles text notifications via D-Bus (org.freedesktop.Notifications)
 * Maintains a single connection and manages notification replacement
 * Includes retry mechanism and connection health checks
 */
export class TextNotifier {
    private enabled: boolean
    private bus: MessageBus | null = null
    private notifyInterface: any = null
    private lastNotificationId: number = 0

    private initPromise: Promise<void> | null = null
    private isInitializing: boolean = false
    private maxRetries: number = 3
    private retryDelay: number = 1000 // 1 second initial delay

    constructor(enabled: boolean = true) {
        this.enabled = enabled
        if (this.enabled) {
            this.initDBus().catch((err) => log("Failed to init D-Bus TextNotifier:" + JSON.stringify(err)))
        }
    }

    /**
     * Initialize D-Bus connection with retry mechanism
     */
    private async initDBus(retryCount: number = 0): Promise<void> {
        // If already initializing, return the existing promise
        if (this.isInitializing && this.initPromise) {
            return this.initPromise
        }

        this.isInitializing = true
        this.initPromise = this._initDBusWithRetry(retryCount)

        try {
            await this.initPromise
        } finally {
            this.isInitializing = false
            this.initPromise = null
        }
    }

    /**
     * Internal method to initialize D-Bus with retry logic
     */
    private async _initDBusWithRetry(retryCount: number): Promise<void> {
        try {
            // Clean up existing connection if any
            if (this.bus) {
                try {
                    this.bus.disconnect()
                } catch (err) {
                    log("Error disconnecting existing D-Bus connection: " + JSON.stringify(err))
                }
                this.bus = null
            }

            this.bus = sessionBus()
            const obj = await this.bus.getProxyObject("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
            this.notifyInterface = obj.getInterface("org.freedesktop.Notifications")
        } catch (error) {
            log(`D-Bus initialization failed (attempt ${retryCount + 1}/${this.maxRetries}):` + JSON.stringify(error))

            if (retryCount < this.maxRetries - 1) {
                const delay = this.retryDelay * Math.pow(2, retryCount) // Exponential backoff
                await new Promise((resolve) => setTimeout(resolve, delay))
                return this._initDBusWithRetry(retryCount + 1)
            }

            throw error
        }
    }

    /**
     * Check if D-Bus connection is healthy
     */
    private isConnectionHealthy(): boolean {
        return this.bus !== null && this.notifyInterface !== null
    }

    /**
     * Ensure D-Bus connection is established, reconnecting if necessary
     */
    private async ensureConnection(): Promise<void> {
        if (!this.isConnectionHealthy()) {
            await this.initDBus()
        }
    }

    /**
     * Map string urgency to D-Bus byte levels
     * 0: low, 1: normal, 2: critical
     */
    private getUrgencyByte(urgency: Urgency): number {
        switch (urgency) {
            case "low":
                return 0
            case "critical":
                return 2
            default:
                return 1
        }
    }

    async notify(title: string, message: string, icon: string, urgency: Urgency = "normal", durationMs: number = 3000) {
        if (!this.enabled) {
            return
        }

        try {
            // Ensure connection is healthy before sending notification
            await this.ensureConnection()

            const hints = {
                transient: new Variant("b", true),
                urgency: new Variant("y", this.getUrgencyByte(urgency)),
                "x-canonical-private-synchronous": new Variant("s", SYNC_ID),
            }

            // D-Bus Notify Signature:
            // app_name, replaces_id, app_icon, summary, body, actions, hints, timeout
            this.lastNotificationId = await this.notifyInterface.Notify(
                "Voice Type",
                this.lastNotificationId, // Use the stored ID to replace the existing popup
                icon,
                title,
                message,
                [],
                hints,
                durationMs,
            )
        } catch (error) {
            log("D-Bus Notification Error:" + JSON.stringify(error))

            // If notification fails, try to reinitialize the connection
            try {
                await this.initDBus()
                // Retry the notification once after reconnection
                if (this.notifyInterface) {
                    const retryHints = {
                        transient: new Variant("b", true),
                        urgency: new Variant("y", this.getUrgencyByte(urgency)),
                        "x-canonical-private-synchronous": new Variant("s", SYNC_ID),
                    }
                    this.lastNotificationId = await this.notifyInterface.Notify(
                        "Voice Type",
                        this.lastNotificationId,
                        icon,
                        title,
                        message,
                        [],
                        retryHints,
                        durationMs,
                    )
                }
            } catch (retryError) {
                log("Failed to send notification after reconnection:" + JSON.stringify(retryError))
            }
        }
    }

    // --- Specialized Text Notifiers ---

    async notifyDaemonStart() {
        await this.notify(
            "🎤 Voice Type Daemon Active",
            "Ready to transcribe, start recording.",
            "microphone-sensitivity-high",
            "normal",
        )
    }

    async notifyDaemonStop(hotkey: string = "F10") {
        await this.notify("⏹️ Voice Type Daemon Stopped", "Daemon has been shut down.", "process-stop", "normal")
    }

    async notifyMicStart() {
        await this.notify(
            "🟢 Voice Type listening...",
            "Typing into the focused window.",
            "microphone-sensitivity-high",
            "normal",
        )
    }

    async notifyMicStopIntentional() {
        await this.notify(
            "🛑 Voice Type Stopped",
            "Microphone closed normally",
            "microphone-sensitivity-muted",
            "normal",
        )
    }
    async notifyMicStopSilence() {
        await this.notify(
            "🛑 Voice Type Stopped",
            "Microphone closed due to silence",
            "microphone-sensitivity-muted",
            "normal",
        )
    }

    async notifyOffline() {
        await this.notify("📡 Voice Type offline", "Check your internet connection", "network-error", "critical")
    }

    async notifyError(msg: string) {
        await this.notify("⚠️ Voice Type Error", msg, "dialog-error", "critical")
    }

    async notifyAlreadyRunning() {
        await this.notify(
            "⚠️ Voice Type Already Running",
            "Only one instance is allowed. Close the old one first.",
            "dialog-warning",
            "normal",
        )
    }

    destroy() {
        // Cancel any pending initialization
        this.isInitializing = false
        this.initPromise = null

        if (this.bus) {
            try {
                this.bus.disconnect()
            } catch (error) {
                log("Error disconnecting D-Bus:" + JSON.stringify(error))
            }
            this.bus = null
            this.notifyInterface = null
        }
    }
}
