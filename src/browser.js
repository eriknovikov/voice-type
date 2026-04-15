export function initWSA(lang) {
    console.log("browser connected. intializing WSA...")

    const SpeechRec = window.SpeechRecognition || window.webkitSpeechRecognition
    if (!SpeechRec) {
        console.error("FATAL: Web Speech API is not supported in this browser context.")
        throw new Error("FATAL: Web Speech API is not supported in this browser context.")
    }

    const rec = new SpeechRec()
    rec.continuous = true
    rec.interimResults = true
    rec.lang = lang !== undefined ? lang : "en-US"

    rec.onstart = () => {
        console.log("Listening...")
        rec.isRunning = true
    }

    let finalText = ""
    rec.onresult = (event) => {
        let interimText = ""
        for (let i = event.resultIndex; i < event.results.length; i++) {
            if (!event.results[i].isFinal) {
                interimText += event.results[i][0].transcript
            } else {
                finalText += event.results[i][0].transcript
            }
        }

        console.log("FINAL TEXT: ", finalText)
        window.onSpeechUpdate({ text: interimText })
    }

    //onerror happens always before onend.
    rec.onerror = (event) => {
        if (event.error === "network") window.isOffline = true
        else throw new Error(`unexpected recognition error: ${event.error}`)
    }

    rec.onend = () => {
        // if recognition was stopped normally, onBrowserRecStop is a no-op since the daemon.isWSAListening is already FALSE
        window.onBrowserRecStop({ reason: window.isOffline ? "offline" : "silence" })
        //clear state
        finalText = ""
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
