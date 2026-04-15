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

    async notifyDaemonStart() {
        await this.textNotifier.notifyDaemonStart()
        await this.soundNotifier.notifyStart()
    }

    async notifyDaemonStop() {
        await this.textNotifier.notifyDaemonStop()
        await this.soundNotifier.notifyStop()
    }

    async notifyMicStart() {
        await this.textNotifier.notifyMicStart()
        await this.soundNotifier.notifyStart()
    }

    async notifyMicStopIntentional() {
        await this.textNotifier.notifyMicStopIntentional()
        await this.soundNotifier.notifyStop()
    }
    async notifyMicStopSilence() {
        await this.textNotifier.notifyMicStopSilence()
        await this.soundNotifier.notifyStop()
    }

    async notifyOffline() {
        await this.textNotifier.notifyOffline()
        await this.soundNotifier.notifyOffline()
    }

    async notifyError(msg: string) {
        await this.textNotifier.notifyError(msg)
        await this.soundNotifier.notifyError()
    }

    async notifyAlreadyRunning() {
        await this.textNotifier.notifyAlreadyRunning()
        await this.soundNotifier.notifyError()
    }

    destroy() {
        this.textNotifier.destroy()
    }
}
