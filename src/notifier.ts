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

    notifyDaemonStart(hotkey: string) {
        this.textNotifier.notifyDaemonStarted(hotkey)
        this.soundNotifier.notifyStart()
    }
    notifyDaemonStop() {
        this.textNotifier.notifyDaemonStop()
        this.soundNotifier.notifyStop()
    }
    notifyMicStart() {
        this.textNotifier.notifyMicStart()
        this.soundNotifier.notifyStart()
    }

    notifyMicStop() {
        this.textNotifier.notifyMicStop()
        this.soundNotifier.notifyStop()
    }

    notifyOffline() {
        this.textNotifier.notifyOffline()
        this.soundNotifier.notifyOffline()
    }

    notifyError(msg: string) {
        this.textNotifier.notifyError(msg)
        this.soundNotifier.notifyError()
    }

    async notifyAlreadyRunning() {
        await this.textNotifier.notifyAlreadyRunning()
        await this.soundNotifier.notifyError()
    }

    notifyDaemonStarted() {
        this.textNotifier.notifyDaemonStarted()
    }

    notifyDaemonStopped() {
        this.textNotifier.notifyDaemonStopped()
    }

    destroy() {
        this.textNotifier.destroy()
    }
}
