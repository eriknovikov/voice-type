export function initWSA(lang) {
    console.log("browser connected. intializing WSA...")

    const SpeechRec = window.SpeechRecognition || window.webkitSpeechRecognition
    if (!SpeechRec) {
        console.error("FATAL: Web Speech API is not supported in this browser context.")
        return
    }

    const rec = new SpeechRec()
    rec.continuous = true
    rec.interimResults = true
    rec.lang = lang !== undefined ? lang : "en-US"

    rec.onstart = () => {
        console.log("Listening...")
        rec.isRunning = true
    }

    rec.onresult = (event) => {
        let interimText = ""

        for (let i = event.resultIndex; i < event.results.length; i++) {
            if (!event.results[i].isFinal) {
                interimText += event.results[i][0].transcript
            }
        }

        if (window.onSpeechUpdate) {
            window.onSpeechUpdate({ text: interimText })
        }
    }

    rec.onerror = (event) => {
        console.error("rec error: ", JSON.stringify(event))
        if (window.onOffline) window.onOffline()
    }

    rec.onend = () => {
        console.log("Stopped listening")
        rec.isRunning = false
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

export function stopListening() {
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
