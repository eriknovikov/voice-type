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

    // All methods async - await text for D-Bus, await sound for safety before exit
    async notifyDaemonStart(hotkey: string) {
        await this.textNotifier.notifyDaemonStart(hotkey)
        this.soundNotifier.notifyStart()
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

    notifyOffline() {
        this.textNotifier.notifyOffline()
        this.soundNotifier.notifyOffline()
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
