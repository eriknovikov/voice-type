export function FAQ({ platform, shortcut }: { platform?: string; shortcut?: string }) {
  const mac = platform === "darwin";
  const keys = (shortcut || (mac ? "Ctrl+Alt+Space" : "Ctrl+Space"))
    .split("+")
    .map((key) => mac ? ({ Ctrl: "Control", Alt: "Option", Super: "Command" }[key] || key) : key)
    .join("+");
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
            Press <code>{keys}</code> to record and again to stop.{" "}
            <strong>Minimize</strong> keeps Voice Type running in the
            background; <strong>Quit</strong> fully closes the app. Recordings
            are limited to ten minutes.
          </p>
          {mac && <p>After setup, ready launches stay in the menu bar. Choose Settings from its menu or reopen Voice Type to show this window.</p>}
        </details>
        <details className="faq-item">
          <summary>Local, OpenAI or OpenRouter?</summary>
          <p>
            Local works offline without an API key. Choose Parakeet Unified
            English for English and live processing, or Parakeet TDT v3 for 25
            European languages after recording. Selecting an uninstalled model
            starts its download; progress continues if you leave the editor.
            OpenAI and OpenRouter need internet access and a funded provider
            account.
          </p>
        </details>
        <details className="faq-item">
          <summary>Where do I add an API key?</summary>
          <p>
            Open <strong>API Keys</strong>. One key is shared by all profiles
            for that provider. Storage defaults to {mac ? "macOS Keychain" : "your system keyring"}; Local
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
            <strong>Audio</strong> to select and test a microphone.{" "}
            {mac ? "The default is Control-Option-Space. A shortcut conflict is shown in settings; choose another combination if needed." : <>
              GNOME and X11 shortcuts are registered automatically. On other Wayland
              desktops, bind <code>voice-type --toggle</code> in your desktop settings.
            </>}
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
          {mac && <p>Direct insertion does not use the clipboard. Opt-in paste compatibility temporarily exposes dictated text on the system clipboard. Clipboard managers and other apps may save it despite advisory privacy markers.</p>}
        </details>
        <details className="faq-item">
          <summary>Why did dictation not work?</summary>
          <p>
            “No speech detected” is a warning: check your microphone in{" "}
            <strong>Audio</strong> and try again. If text is missing, check{" "}
            {mac ? "Microphone and Accessibility access in General, then focus a supported editable text field. Protected, read-only and unknown targets are refused. Direct events may be ignored by some apps. Voice Type never retries an uncertain submission automatically." : "that the target app has focus and IBus is running."} For cloud errors,
            check <strong>API Keys</strong> and the provider’s credits.
          </p>
          <p>
            Check <strong>Logs</strong> for details, or use{" "}
            <strong>General → Open diagnostics</strong> to copy a report without
            keys, transcripts or audio.{" "}
            {!mac && "On GNOME, a missing tray icon usually needs the AppIndicator/KStatusNotifierItem extension."}
          </p>
        </details>
        {mac && <>
          <details className="faq-item">
            <summary>Which macOS permissions are needed?</summary>
            <p>In General, choose Allow for Microphone and Accessibility. Requests happen only on your action. If access is denied, open System Settings → Privacy &amp; Security and enable Voice Type there. Return to the app; quit and reopen it if macOS asks. No Screen Recording or Input Monitoring access is needed.</p>
          </details>
          <details className="faq-item">
            <summary>When should I use paste compatibility?</summary>
            <p>Only if direct insertion does not work in your target app. Choose it under General → Output method. It sends Command-V once and restores the previous clipboard only while Voice Type still owns it. Read requests do not prove insertion, and concurrent clipboard changes can prevent restoration.</p>
            <p>Paste compatibility cannot be used with live insertion. Choose direct insertion, process while recording, or insert at stop yourself. Voice Type never changes your profile or output method automatically.</p>
          </details>
          <details className="faq-item">
            <summary>How does Start at login work?</summary>
            <p>Enable Start at login in General. The switch shows the effective macOS state, not saved intent. If approval is required, allow Voice Type in System Settings → General → Login Items. Launching the app or saving other settings never overrides an OS disable. Development builds do not register at login.</p>
          </details>
          <details className="faq-item">
            <summary>Where are my data, and how do I update or remove the app?</summary>
            <p>Settings, models and optional file credentials are under <code>~/Library/Application Support/Voice Type/</code>. Logs are under <code>~/Library/Logs/Voice Type/</code>. Development builds use separate Voice Type Dev folders and Keychain entries. Recovery file locations appear in the failure message.</p>
            <p>For a published Mac release, download the DMG for Apple Silicon (arm64) or Intel (amd64), quit Voice Type and replace the app in Applications. Do not run the Linux installer on macOS.</p>
            <p>To remove it, disable Start at login, quit, then move Voice Type.app to Trash. This keeps your data. Delete the folders above only if you want to erase them; remove saved provider keys in API Keys first or delete Voice Type entries in Keychain Access. You can also remove its permission entries in System Settings.</p>
          </details>
        </>}
      </section>
    </div>
  );
}
