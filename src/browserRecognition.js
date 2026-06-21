// WSA result → speech-event mapping (testable standalone copy of the nested mapper in browser.js).
// Must stay in sync — enforced by tests/browserRecognition.sync.test.ts.

/**
 * @param {boolean} stream
 * @param {number} resultIndex
 * @param {Array<{ isFinal: boolean; 0: { transcript: string } }>} results
 */
export function recognitionResultsToEvents(stream, resultIndex, results) {
    let interimText = ""
    let finalizedText = ""
    let segmentFinalized = false

    for (let i = resultIndex; i < results.length; i++) {
        if (!results[i].isFinal) {
            interimText += results[i][0].transcript
        } else {
            finalizedText += results[i][0].transcript
            segmentFinalized = true
        }
    }

    const events = []

    if (stream) {
        if (segmentFinalized) {
            if (finalizedText) {
                events.push({ kind: "text", text: finalizedText })
            }
            events.push({ kind: "segment-finalized" })
        }
        if (interimText) {
            events.push({ kind: "text", text: interimText })
        }
    } else if (segmentFinalized) {
        if (finalizedText) {
            events.push({ kind: "text", text: finalizedText })
        }
        events.push({ kind: "segment-finalized" })
    }

    return events
}
