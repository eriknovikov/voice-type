/** Whether inbound WSA speech events should be processed at the daemon boundary. */
export function shouldAcceptSpeechEvent(isListening: boolean, hasStopped: boolean): boolean {
    return isListening && !hasStopped
}
