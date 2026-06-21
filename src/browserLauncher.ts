import { access, constants as fsConstants } from "node:fs/promises"
import puppeteer from "puppeteer-core"

const CHROME_PATH = "/usr/bin/google-chrome"
const CHROMIUM_PATH = "/usr/bin/chromium"

// arguments shared by all browsers
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

export async function detectBrowser(): Promise<BrowserType | null> {
    const browsers = [
        { name: "chrome", path: CHROME_PATH },
        { name: "chromium", path: CHROMIUM_PATH },
    ]

    for (const browser of browsers) {
        const exists = await checkBrowserExists(browser.path)
        if (exists) {
            return browser.name as BrowserType
        }
    }
    return null
}

async function checkBrowserExists(browserPath: string): Promise<boolean> {
    try {
        await access(browserPath, fsConstants.R_OK)
        return true
    } catch {
        return false
    }
}

export async function launchBrowser() {
    let browserType = process.env.BROWSER_TYPE as BrowserType | undefined
    if (!browserType) {
        browserType = (await detectBrowser()) as BrowserType
    }

    const defaultBrowserPath = browserType == "chrome" ? CHROME_PATH : CHROMIUM_PATH
    const browserPath = process.env.BROWSER_PATH ? process.env.BROWSER_PATH : defaultBrowserPath

    return puppeteer.launch({
        executablePath: browserPath,
        // @ts-ignore
        headless: "new",
        args: LAUNCH_ARGS,
        name: "Voice-Type-browser",
    })
}
