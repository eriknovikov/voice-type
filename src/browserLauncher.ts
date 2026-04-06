import puppeteer from "puppeteer-core"
import { spawn } from "child_process"
import { log } from "./logger"

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
    return new Promise((resolve) => {
        const which = spawn("which", [browserPath])
        let output = ""

        which.stdout.on("data", (data) => {
            output += data.toString()
        })

        which.on("close", (code) => {
            resolve(code === 0 && output.trim().length > 0)
        })

        which.on("error", () => {
            resolve(false)
        })
    })
}

export async function launchBrowser(browserType: BrowserType, browserPath?: string) {
    let defaultBrowserPath: string

    switch (browserType) {
        case "chrome":
            defaultBrowserPath = CHROME_PATH
            break
        case "chromium":
            defaultBrowserPath = CHROMIUM_PATH
            break
    }
    if (!browserPath) browserPath = defaultBrowserPath

    log(`launching browser at ${browserPath}`)
    return puppeteer.launch({
        executablePath: browserPath,
        // @ts-ignore
        headless: "new",
        args: LAUNCH_ARGS,
        name: "Voice-Type-browser",
    })
}
