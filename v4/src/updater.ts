import { createWriteStream, mkdtempSync } from "node:fs"
import { rename, chmod, access } from "node:fs/promises"
import { execSync } from "node:child_process"
import { promisify } from "node:util"
import { pipeline } from "node:stream"
import { join } from "node:path"
import { tmpdir } from "node:os"
import { CURRENT_VERSION, REPO } from "./constants.js"

const streamPipeline = promisify(pipeline)

const GITHUB_API = `https://api.github.com/repos/${REPO}/releases/latest`

interface ReleaseInfo {
    tag: string
    version: string
    assets: { name: string; browser_download_url: string }[]
}

function detectArch(): "x64" | "arm64" {
    const arch = process.arch
    if (arch === "x64") return "x64"
    if (arch === "arm64") return "arm64"
    throw new Error(`Unsupported architecture: ${arch}`)
}

function parseTag(tag: string): string {
    return tag.startsWith("v") ? tag.slice(1) : tag
}

export function compareVersions(a: string, b: string): number {
    const ap = a.split(".").map(Number)
    const bp = b.split(".").map(Number)
    for (let i = 0; i < Math.max(ap.length, bp.length); i++) {
        const av = ap[i] ?? 0
        const bv = bp[i] ?? 0
        if (av > bv) return 1
        if (av < bv) return -1
    }
    return 0
}

async function fetchRelease(): Promise<ReleaseInfo> {
    const res = await fetch(GITHUB_API)
    if (!res.ok) throw new Error(`GitHub API returned ${res.status}`)
    const data = (await res.json()) as any
    const tag = data.tag_name as string
    const assets = (data.assets ?? []).map((a: any) => ({
        name: a.name,
        browser_download_url: a.browser_download_url,
    }))
    return { tag, version: parseTag(tag), assets }
}

async function downloadFile(url: string, dest: string): Promise<void> {
    const res = await fetch(url)
    if (!res.ok || !res.body) throw new Error(`Download failed: ${res.status} ${url}`)
    await streamPipeline(res.body as any, createWriteStream(dest))
}

function sha256sum(filePath: string): string {
    const out = execSync(`sha256sum "${filePath}"`, { encoding: "utf8" })
    return out.split(" ")[0].trim()
}

async function verifyChecksum(assetPath: string, checksumPath: string, expectedName: string): Promise<void> {
    const expected = execSync(`grep "${expectedName}" "${checksumPath}" | awk '{print $1}'`, {
        encoding: "utf8",
    }).trim()
    if (!expected) throw new Error(`No checksum found for ${expectedName}`)
    const actual = sha256sum(assetPath)
    if (expected !== actual) throw new Error(`Checksum mismatch: expected ${expected}, got ${actual}`)
}

export async function runUpdate(): Promise<void> {
    const current = CURRENT_VERSION
    console.log(`Current version: ${current}`)

    const release = await fetchRelease()
    console.log(`Latest version:  ${release.version} (${release.tag})`)

    if (compareVersions(release.version, current) <= 0) {
        console.log("Already up to date.")
        return
    }

    const arch = detectArch()
    const tarballName = `voice-type-linux-${arch}.tar.gz`
    const asset = release.assets.find((a) => a.name === tarballName)
    if (!asset) throw new Error(`Release asset not found: ${tarballName}`)

    const checksumsAsset = release.assets.find((a) => a.name === "checksums.txt")
    if (!checksumsAsset) throw new Error("checksums.txt not found in release assets")

    const tmpDir = mkdtempSync(join(tmpdir(), "vt-update-"))
    const tgzPath = join(tmpDir, tarballName)
    const checksumPath = join(tmpDir, "checksums.txt")

    try {
        console.log(`Downloading ${tarballName}...`)
        await downloadFile(asset.browser_download_url, tgzPath)
        await downloadFile(checksumsAsset.browser_download_url, checksumPath)

        console.log("Verifying checksum...")
        await verifyChecksum(tgzPath, checksumPath, tarballName)

        console.log("Extracting...")
        execSync(`tar -xzf "${tgzPath}" -C "${tmpDir}"`, { encoding: "utf8" })

        const extractedBinary = join(tmpDir, `voice-type-${arch}`, "voice-type")

        const targetPath = process.execPath
        const targetDir = join(targetPath, "..")
        const tempBinary = join(targetDir, ".voice-type.new")

        execSync(`cp "${extractedBinary}" "${tempBinary}"`, { encoding: "utf8" })
        await chmod(tempBinary, 0o755)

        try {
            await access(targetPath)
            await rename(tempBinary, targetPath)
        } catch (err: any) {
            if (err.code === "EACCES" || err.code === "EPERM") {
                console.log("Target is not writable. Attempting with sudo...")
                execSync(`sudo mv "${tempBinary}" "${targetPath}"`, { stdio: "inherit" })
            } else {
                throw err
            }
        }
    } finally {
        execSync(`rm -rf "${tmpDir}"`, { encoding: "utf8", stdio: "ignore" })
    }

    console.log(`Updated to ${release.tag}. Restart the daemon (Super+Shift+V cycle) to apply.`)
}
