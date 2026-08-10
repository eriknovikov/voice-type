import puppeteer from "puppeteer-core"
import type { VoiceTypeConfig } from "./types.js"
import { checkBrowserPath } from "./preflight.js"

const CHROME_PATH = "/usr/bin/google-chrome"
const CHROMIUM_PATH = "/usr/bin/chromium"

const LAUNCH_ARGS = [
    "--use-fake-ui-for-media-stream",
    "--disable-background-timer-throttling",
    "--log-level=0",
    "--disable-dev-shm-usage",
    "--disable-gpu",
    "--disable-software-rasterizer",
    "--disable-background-networking",
    "--disable-default-apps",
    "--disable-extensions",
    "--disable-sync",
    "--disable-translate",
    "--metrics-recording-only",
    "--no-first-run",
    "--safebrowsing-disable-auto-update",
    "--process-per-site",
    "--disable-features=IsolateOrigins,site-per-process",
]

export type BrowserType = "chrome" | "chromium"

export async function detectDefaultBrowser(): Promise<{ path: string; type: BrowserType } | null> {
    const candidates = [
        "/usr/bin/google-chrome",
        "/usr/bin/google-chrome-stable",
        "/usr/bin/google-chrome-beta",
        "/opt/google/chrome/chrome",
        "/usr/bin/chromium",
        "/usr/bin/chromium-browser",
        "/usr/local/bin/chromium",
    ]

    for (const path of candidates) {
        if (path.startsWith("/snap/bin/") || path.startsWith("org.chromium.")) continue
        if ((await checkBrowserPath(path)).ok) {
            const type: BrowserType = path.includes("chromium") ? "chromium" : "chrome"
            return { path, type }
        }
    }
    return null
}

export async function launchBrowser(config: VoiceTypeConfig) {
    const browserPath = config.browser_path || (config.browser_type === "chrome" ? CHROME_PATH : CHROMIUM_PATH)

    return puppeteer.launch({
        executablePath: browserPath,
        // @ts-ignore
        headless: "new",
        args: LAUNCH_ARGS,
        name: "Voice-Type-browser",
    })
}
