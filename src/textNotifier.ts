import { sessionBus, Variant, type MessageBus } from "dbus-next"
import type { Urgency } from "./types"
import { log } from "./logger"

/**
 * Handles text notifications via D-Bus (org.freedesktop.Notifications)
 * Maintains a single connection and manages notification replacement
 */
export class TextNotifier {
    private enabled: boolean
    private bus: MessageBus | null = null
    private notifyInterface: any = null
    private lastNotificationId: number = 0
    private readonly SYNC_ID = "voice-type-dictation"

    constructor(enabled: boolean = true) {
        this.enabled = enabled
        if (this.enabled) {
            this.initDBus().catch((err) => log("Failed to init D-Bus TextNotifier:" + JSON.stringify(err)))
        }
    }

    private async initDBus() {
        this.bus = sessionBus()
        const obj = await this.bus.getProxyObject("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
        this.notifyInterface = obj.getInterface("org.freedesktop.Notifications")
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
        if (!this.enabled) return

        try {
            // Lazy-init if not ready
            if (!this.notifyInterface) await this.initDBus()

            const hints = {
                transient: new Variant("b", true),
                urgency: new Variant("y", this.getUrgencyByte(urgency)),
                "x-canonical-private-synchronous": new Variant("s", this.SYNC_ID),
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
        }
    }

    // --- Specialized Text Notifiers ---

    async notifyDaemonStarted(hotkey: string = "F9") {
        await this.notify(
            "🎤 Voice Type Daemon Active",
            "Ready to transcribe, start recording.",
            "microphone-sensitivity-high",
            "normal",
        )
    }

    async notifyDaemonStopped(hotkey: string = "F10") {
        await this.notify("⏹️ Voice Type Daemon Stopped", "Daemon has been shut down.", "process-stop", "normal")
    }

    async notifyMicStart() {
        await this.notify(
            "🟢 Voice Type Listening...",
            "Typing into the focused window.",
            "microphone-sensitivity-high",
            "normal",
        )
    }

    async notifyDaemonStop() {
        await this.notify(
            "Voice Type daemon stopped",
            "All resources have been released",
            "microphone-sensitivity-high",
            "normal",
        )
    }

    async notifyMicStop() {
        await this.notify(
            "🛑 Voice Type Stopped",
            "Microphone closed. Text finalized.",
            "microphone-sensitivity-muted",
            "normal",
        )
    }

    async notifyOffline() {
        await this.notify(
            "📡 Connection Error",
            "WSA requires internet to transcribe speech.",
            "network-error",
            "critical",
        )
    }

    async notifyError(msg: string) {
        await this.notify("⚠️ Voice Type Error", msg, "dialog-error", "critical")
    }

    destroy() {
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
