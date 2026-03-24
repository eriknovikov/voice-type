import { TextNotifier } from "./textNotifier"
import { SoundNotifier } from "./soundNotifier"

/**
 * Main notifier that composes TextNotifier and SoundNotifier
 * Provides a clean API for all notification types
 */
export default class Notifier {
    private textNotifier: TextNotifier
    private soundNotifier: SoundNotifier

    constructor(opts: { textNotifsEnabled?: boolean; soundsNotifsEnabled?: boolean } = {}) {
        this.textNotifier = new TextNotifier(opts.textNotifsEnabled ?? true)
        this.soundNotifier = new SoundNotifier(opts.soundsNotifsEnabled ?? true)
    }

    async notifyDaemonStart(hotkey: string) {
        await this.textNotifier.notifyDaemonStart(hotkey)
        await this.soundNotifier.notifyStart()
    }
    async notifyDaemonStop() {
        await this.textNotifier.notifyDaemonStop()
        await this.soundNotifier.notifyStop()
    }
    async notifyMicStart() {
        await this.textNotifier.notifyMicStart()
        this.soundNotifier.notifyStart()
    }

    async notifyMicStop() {
        await this.textNotifier.notifyMicStop()
        this.soundNotifier.notifyStop()
    }

    async notifyOffline() {
        await this.textNotifier.notifyOffline()
        this.soundNotifier.notifyOffline()
    }

    async notifyError(msg: string) {
        await this.textNotifier.notifyError(msg)
        this.soundNotifier.notifyError()
    }

    async notifyDaemonStarted() {
        await this.textNotifier.notifyDaemonStarted()
    }

    async notifyDaemonStopped() {
        await this.textNotifier.notifyDaemonStopped()
    }

    destroy() {
        this.textNotifier.destroy()
    }
}
