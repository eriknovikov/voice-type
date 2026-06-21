// Injected into Chrome via page.evaluate — no imports. Helpers used by evaluated
// functions must be nested inside them (Puppeteer only serializes the top-level export).
// Event mapper: keep browserRecognition.js in sync (tests/browserRecognition.sync.test.ts).

export function initWSA(stream, lang) {
    function recognitionResultsToEvents(stream, resultIndex, results) {
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

    console.log("browser connected. initializing Web Speech API...")

    const SpeechRec = window.SpeechRecognition || window.webkitSpeechRecognition
    if (!SpeechRec) {
        console.error("FATAL: Web Speech API is not supported in this browser context.")
        throw new Error("FATAL: Web Speech API is not supported in this browser context.")
    }

    const rec = new SpeechRec()
    rec.continuous = true
    if (stream) {
        rec.interimResults = true
    }
    rec.lang = lang !== undefined ? lang : "en-US"

    rec.onstart = () => {
        console.log("Listening...")
        rec.isRunning = true
    }

    rec.onresult = (event) => {
        const events = recognitionResultsToEvents(stream, event.resultIndex, event.results)
        for (const speechEvent of events) {
            window.onSpeechEvent(speechEvent)
        }
    }

    rec.onerror = (event) => {
        if (event.error === "network") window.isOffline = true
        else throw new Error(`unexpected recognition error: ${event.error}`)
    }

    rec.onend = () => {
        window.onBrowserRecStop({ reason: window.isOffline ? "offline" : "silence" })
        rec.isRunning = false
        window.isOffline = undefined
    }

    window.recognition = rec
}

export function startListening() {
    if (!window.recognition) {
        const message = "rec not initialized"
        console.error(message)
        return
    }
    try {
        window.recognition.start()
    } catch (e) {
        const message = `Error starting rec: ${e.message || e}`
        console.error(message)
    }
}

export function setLangAndStart(lang) {
    if (!window.recognition) {
        console.error("rec not initialized")
        return
    }
    try {
        window.recognition.lang = lang
        window.recognition.start()
    } catch (e) {
        console.error(`Error setting lang and starting rec: ${e.message || e}`)
    }
}

export function stopRecognition() {
    if (!window.recognition) {
        const message = "rec not initialized"
        console.error(message)
        return
    }

    try {
        window.recognition.stop()
    } catch (e) {
        const message = `Error stopping rec: ${e.message || JSON.stringify(e)}`
        console.error(message)
    }
}

export function healthCheck() {
    return "ok"
}
