export function FAQ() {
  return (
    <div className="faq">
      <p className="quiet faq-intro">The essentials, in one place.</p>
      <section
        className="settings-group faq-group"
        aria-label="Common questions"
      >
        <details className="faq-item">
          <summary>How do I start dictating?</summary>
          <p>
            Add a profile in <strong>General</strong>, then focus a text field.
            Press <code>Ctrl+Space</code> to record and again to stop.{" "}
            <strong>Minimize</strong> keeps Voice Type running in the
            background; <strong>Quit</strong> fully closes the app. Recordings
            are limited to ten minutes.
          </p>
        </details>
        <details className="faq-item">
          <summary>Local, OpenAI or OpenRouter?</summary>
          <p>
            Local works offline without an API key. Parakeet Unified English is
            English only and the only model that streams. Parakeet Ultra and
            Parakeet TDT v3 cover 25 European languages, and Qwen3 ASR covers 30
            languages including Chinese, Japanese, Korean and Arabic while
            detecting the language automatically; batch models insert text after
            recording. Selecting an uninstalled model starts its download;
            progress continues if you leave the editor. OpenAI and OpenRouter
            need internet access and a funded provider account.
          </p>
        </details>
        <details className="faq-item">
          <summary>Where do I add an API key?</summary>
          <p>
            Open <strong>API Keys</strong>. One key is shared by all profiles
            for that provider. Storage defaults to your system keyring; Local
            file storage is also available. Create a key at{" "}
            <a
              href="https://platform.openai.com/api-keys"
              target="_blank"
              rel="noreferrer"
            >
              platform.openai.com/api-keys
            </a>{" "}
            or{" "}
            <a
              href="https://openrouter.ai/keys"
              target="_blank"
              rel="noreferrer"
            >
              openrouter.ai/keys
            </a>
            .
          </p>
        </details>
        <details className="faq-item">
          <summary>What do mode and model memory change?</summary>
          <p>
            Processing or uploading while recording reduces the wait after you
            stop. Local live insertion appends text as you speak; other modes
            insert at stop. Keep loaded makes local models faster to reuse;
            unload after dictation frees their RAM. These settings are per
            profile.
          </p>
        </details>
        <details className="faq-item">
          <summary>How do I change the shortcut or microphone?</summary>
          <p>
            Use <strong>Shortcuts</strong> to record a new shortcut, and{" "}
            <strong>Audio</strong> to select and test a microphone. GNOME and
            X11 shortcuts are registered automatically. On other Wayland
            desktops, bind <code>voice-type --toggle</code> in your desktop
            settings.
          </p>
        </details>
        <details className="faq-item">
          <summary>How does vocabulary work?</summary>
          <p>
            Add names and technical terms in <strong>Vocabulary</strong>, one
            per line. General terms are always included. Create a separate
            vocabulary for a project or topic and select <strong>Use</strong> to
            add its terms. Changes save automatically.
          </p>
        </details>
        <details className="faq-item">
          <summary>What stays on my device?</summary>
          <p>
            Local profiles never upload audio and never fall back to the cloud.
            Cloud profiles send audio to your selected provider. Routine logs
            contain no transcripts, audio or API keys. If local insertion fails,
            private audio and transcript files are kept for manual recovery.
          </p>
        </details>
        <details className="faq-item">
          <summary>Why did dictation not work?</summary>
          <p>
            “No speech detected” is a warning: check your microphone in{" "}
            <strong>Audio</strong> and try again. If text is missing, check that
            the target app has focus and IBus is running. For cloud errors,
            check <strong>API Keys</strong> and the provider’s credits.
          </p>
          <p>
            Check <strong>Logs</strong> for details, or use{" "}
            <strong>General → Open diagnostics</strong> to copy a report without
            keys, transcripts or audio. On GNOME, a missing tray icon usually
            needs the AppIndicator/KStatusNotifierItem extension.
          </p>
        </details>
      </section>
    </div>
  );
}
