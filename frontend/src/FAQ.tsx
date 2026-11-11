const openAIKeys = "https://platform.openai.com/api-keys";
const openRouterKeys = "https://openrouter.ai/keys";

export function FAQ() {
  return (
    <div className="faq">
      <p className="quiet faq-intro">Short answers to common questions.</p>
      <section className="settings-section" aria-labelledby="faq-start">
        <div className="section-heading">
          <h2 id="faq-start">Getting started</h2>
        </div>
        <div className="settings-group faq-group">
          <details className="faq-item">
            <summary>How do I dictate?</summary>
            <p>
              The shortcut is <code>Ctrl+Space</code> by default. Press it,
              speak, then press it again. The text is inserted at the cursor in
              the focused app through IBus.
            </p>
          </details>
          <details className="faq-item">
            <summary>What do I need first?</summary>
            <p>
              Add a profile in <strong>General</strong> and choose a microphone.
              Cloud profiles also need an API key.
            </p>
          </details>
          <details className="faq-item">
            <summary>Does closing the window stop Voice Type?</summary>
            <p>
              No. Closing the window keeps Voice Type running in the tray. Use{" "}
              <strong>Quit</strong> to stop the app.
            </p>
          </details>
          <details className="faq-item">
            <summary>Can Voice Type start with my session?</summary>
            <p>
              Yes. Enable <strong>Start at login</strong> in{" "}
              <strong>General</strong>. It is enabled by default on first
              launch.
            </p>
          </details>
        </div>
      </section>
      <section className="settings-section" aria-labelledby="faq-provider">
        <div className="section-heading">
          <h2 id="faq-provider">Choosing a provider</h2>
        </div>
        <div className="settings-group faq-group">
          <details className="faq-item">
            <summary>Which providers are available?</summary>
            <p>
              OpenAI and OpenRouter run in the cloud. Local runs a Parakeet
              model on this device.
            </p>
          </details>
          <details className="faq-item">
            <summary>OpenAI or OpenRouter?</summary>
            <p>
              OpenAI uses GPT Transcribe and can upload while recording.
              OpenRouter transcribes the complete recording.
            </p>
          </details>
          <details className="faq-item">
            <summary>Does Voice Type work offline?</summary>
            <p>
              Local profiles work offline. OpenAI and OpenRouter need a network
              connection.
            </p>
          </details>
          <details className="faq-item">
            <summary>Should I use a local model?</summary>
            <p>
              Local needs no API key, and audio never leaves the device. The
              tradeoff is a model download and more RAM and CPU use.
            </p>
          </details>
          <details className="faq-item">
            <summary>Does a local failure fall back to the cloud?</summary>
            <p>
              No. Local profiles never upload audio and never switch to a cloud
              provider on failure.
            </p>
          </details>
        </div>
      </section>
      <section className="settings-section" aria-labelledby="faq-keys">
        <div className="section-heading">
          <h2 id="faq-keys">API keys</h2>
        </div>
        <div className="settings-group faq-group">
          <details className="faq-item">
            <summary>How many API keys do I need?</summary>
            <p>
              One key per cloud provider. Every profile using that provider
              shares the key.
            </p>
          </details>
          <details className="faq-item">
            <summary>Where are keys stored?</summary>
            <p>
              Keys are stored in the system keyring and masked in the UI. They
              leave only by an explicit copy action.
            </p>
          </details>
          <details className="faq-item">
            <summary>Where do I get an OpenAI key?</summary>
            <p>
              Create one at{" "}
              <a
                href={openAIKeys}
                target="_blank"
                rel="noreferrer"
              >
                platform.openai.com/api-keys
              </a>
              . The account must be funded and have billing enabled.
            </p>
          </details>
          <details className="faq-item">
            <summary>Where do I get an OpenRouter key?</summary>
            <p>
              Create one at{" "}
              <a
                href={openRouterKeys}
                target="_blank"
                rel="noreferrer"
              >
                openrouter.ai/keys
              </a>
              . The account needs credits.
            </p>
          </details>
        </div>
      </section>
      <section className="settings-section" aria-labelledby="faq-local-models">
        <div className="section-heading">
          <h2 id="faq-local-models">Local models</h2>
        </div>
        <div className="settings-group faq-group">
          <details className="faq-item">
            <summary>Which local model should I choose?</summary>
            <p>
              English only: Parakeet Unified English, which streams. 25 European
              languages: Parakeet TDT v3, batch only.
            </p>
          </details>
          <details className="faq-item">
            <summary>What is Parakeet Unified English?</summary>
            <p>
              Parakeet Unified English is English only, downloads about 731 MB,
              and uses about 1.1 GiB RAM while loaded. It supports streaming
              modes.
            </p>
          </details>
          <details className="faq-item">
            <summary>What is Parakeet TDT v3?</summary>
            <p>
              Parakeet TDT v3 covers 25 European languages, downloads about 740
              MB, and uses about 1.2 GiB RAM. It is batch only.
            </p>
          </details>
          <details className="faq-item">
            <summary>How do I install a model?</summary>
            <p>
              Open <strong>Local Models</strong>, choose a model, and select{" "}
              <strong>Download</strong>. Downloads are verified before use, can
              be paused and resumed, and removal is blocked while the model is
              in use.
            </p>
          </details>
          <details className="faq-item">
            <summary>How are downloads handled?</summary>
            <p>
              Downloads are pinned to immutable revisions and verified by exact
              size and SHA-256. Paused downloads resume, and removing an active
              model is blocked until the dictation finishes.
            </p>
          </details>
          <details className="faq-item">
            <summary>Which license applies?</summary>
            <p>
              The Unified model has conflicting statements: the upstream card
              says NVIDIA Open Model License, while the GGUF card says CC BY
              4.0. TDT v3 is CC BY 4.0. Review the terms before use.
            </p>
          </details>
        </div>
      </section>
      <section className="settings-section" aria-labelledby="faq-memory">
        <div className="section-heading">
          <h2 id="faq-memory">Model memory</h2>
        </div>
        <div className="settings-group faq-group">
          <details className="faq-item">
            <summary>What does Keep loaded do?</summary>
            <p>
              It keeps the model in memory for instant reuse. The model's RAM
              stays in use while idle.
            </p>
          </details>
          <details className="faq-item">
            <summary>What does Unload after dictation do?</summary>
            <p>
              It frees the model's RAM after each dictation. The next dictation
              starts slightly later.
            </p>
          </details>
          <details className="faq-item">
            <summary>Is model memory per profile?</summary>
            <p>Yes. Each local profile chooses its own setting.</p>
          </details>
        </div>
      </section>
      <section className="settings-section" aria-labelledby="faq-languages">
        <div className="section-heading">
          <h2 id="faq-languages">Languages</h2>
        </div>
        <div className="settings-group faq-group">
          <details className="faq-item">
            <summary>Does OpenAI detect the language?</summary>
            <p>
              Yes. Recognition is automatic. Optional hints such as{" "}
              <code>en, es, de</code> narrow the expected languages.
            </p>
          </details>
          <details className="faq-item">
            <summary>How does OpenRouter handle languages?</summary>
            <p>OpenRouter transcribes the complete recording.</p>
          </details>
          <details className="faq-item">
            <summary>Which languages do local models cover?</summary>
            <p>
              Parakeet TDT v3 covers 25 European languages. Parakeet Unified
              English is English only.
            </p>
          </details>
        </div>
      </section>
      <section className="settings-section" aria-labelledby="faq-modes">
        <div className="section-heading">
          <h2 id="faq-modes">Dictation modes</h2>
        </div>
        <div className="settings-group faq-group">
          <details className="faq-item">
            <summary>What are the OpenAI modes?</summary>
            <p>
              <strong>Upload while recording</strong> uploads as you speak and
              inserts finalized text once at stop. New profiles use this mode by
              default.
            </p>
          </details>
          <details className="faq-item">
            <summary>What is Record then upload?</summary>
            <p>
              It sends the completed recording over HTTP after you stop.
              Existing profiles keep this mode.
            </p>
          </details>
          <details className="faq-item">
            <summary>What are the local modes?</summary>
            <p>
              <strong>Process while recording</strong>,{" "}
              <strong>Live insertion</strong>, or <strong>No streaming</strong>{" "}
              for Unified English. TDT v3 transcribes after recording.
            </p>
          </details>
          <details className="faq-item">
            <summary>Does live insertion edit text?</summary>
            <p>
              No. It only appends committed text and never edits inserted text.
            </p>
          </details>
        </div>
      </section>
      <section className="settings-section" aria-labelledby="faq-devices">
        <div className="section-heading">
          <h2 id="faq-devices">Microphone and shortcuts</h2>
        </div>
        <div className="settings-group faq-group">
          <details className="faq-item">
            <summary>Which microphone does Voice Type use?</summary>
            <p>
              <strong>System default</strong> or a chosen input device. Refresh
              the list after plugging in a device.
            </p>
          </details>
          <details className="faq-item">
            <summary>What happens if the saved microphone disappears?</summary>
            <p>
              Voice Type falls back to <strong>System default</strong> after
              device enumeration and shows a notice.
            </p>
          </details>
          <details className="faq-item">
            <summary>How do I set the shortcut on GNOME?</summary>
            <p>
              Voice Type registers its own shortcut. Press <code>Escape</code>{" "}
              to disable a shortcut during capture.
            </p>
          </details>
          <details className="faq-item">
            <summary>How do I change the shortcut?</summary>
            <p>
              Open <strong>Shortcuts</strong>. On GNOME, Voice Type registers it
              for you. Press <code>Escape</code> during capture to disable it.
            </p>
          </details>
          <details className="faq-item">
            <summary>What about other Wayland desktops?</summary>
            <p>
              Bind a desktop shortcut to <code>voice-type --toggle</code>.
            </p>
          </details>
          <details className="faq-item">
            <summary>What about X11?</summary>
            <p>Voice Type registers the shortcut directly.</p>
          </details>
        </div>
      </section>
      <section className="settings-section" aria-labelledby="faq-privacy">
        <div className="section-heading">
          <h2 id="faq-privacy">Privacy and data</h2>
        </div>
        <div className="settings-group faq-group">
          <details className="faq-item">
            <summary>What data leaves my device?</summary>
            <p>
              Cloud profiles upload audio to the selected provider. Local
              profiles never upload audio.
            </p>
          </details>
          <details className="faq-item">
            <summary>Do logs contain transcripts or keys?</summary>
            <p>
              No. Routine diagnostics contain neither transcripts nor
              credentials.
            </p>
          </details>
          <details className="faq-item">
            <summary>What happens if local insertion fails?</summary>
            <p>
              Voice Type keeps private WAV and transcript files for manual
              recovery. It never replays them automatically.
            </p>
          </details>
          <details className="faq-item">
            <summary>Where are my files?</summary>
            <p>
              Settings live in <code>~/.voice-type-config.jsonc</code> and logs
              in <code>~/.voice-type-logs.jsonl</code>. Models and backups live
              under <code>~/.local/share/voice-type</code>. Preferences use{" "}
              <code>~/.local/state/voice-type</code>.
            </p>
          </details>
        </div>
      </section>
      <section className="settings-section" aria-labelledby="faq-troubleshooting">
        <div className="section-heading">
          <h2 id="faq-troubleshooting">Troubleshooting</h2>
        </div>
        <div className="settings-group faq-group">
          <details className="faq-item">
            <summary>The tray icon is missing.</summary>
            <p>
              On GNOME, install the AppIndicator/KStatusNotifierItem extension.
            </p>
          </details>
          <details className="faq-item">
            <summary>A cloud profile cannot read its key.</summary>
            <p>Unlock the Secret Service keyring and retry.</p>
          </details>
          <details className="faq-item">
            <summary>Nothing was inserted.</summary>
            <p>
              Check that the target app has focus and that IBus is running.{" "}
              <strong>Logs</strong> shows the failure.
            </p>
          </details>
          <details className="faq-item">
            <summary>The microphone does not work.</summary>
            <p>
              Test it in <strong>Audio</strong> and refresh the device list.
            </p>
          </details>
          <details className="faq-item">
            <summary>Why did dictation stop at ten minutes?</summary>
            <p>Recordings are capped at ten minutes.</p>
          </details>
          <details className="faq-item">
            <summary>Where do recovery notices appear?</summary>
            <p>
              Voice Type shows a notice when settings recover from a backup.{" "}
              <strong>Logs</strong> has the details.
            </p>
          </details>
          <details className="faq-item">
            <summary>Where are the logs?</summary>
            <p>
              Open <strong>Logs</strong> in the app or read{" "}
              <code>~/.voice-type-logs.jsonl</code>.
            </p>
          </details>
        </div>
      </section>
    </div>
  );
}
