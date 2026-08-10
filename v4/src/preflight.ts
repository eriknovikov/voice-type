import { access, constants as fsConstants } from "node:fs/promises"
import { spawn, type ChildProcess, type SpawnOptions } from "child_process"
import { isPortInUse } from "./utils.js"

export type PreflightFailure =
    | { kind: "port-in-use"; message: string }
    | { kind: "dotool"; message: string }
    | { kind: "browser"; message: string }

export type PreflightResult = { ok: true } | { ok: false; failure: PreflightFailure }

export type SpawnFn = (cmd: string, args: string[], opts?: SpawnOptions) => ChildProcess

const defaultSpawn: SpawnFn = (cmd, args, opts) => {
    if (opts) {
        return spawn(cmd, args, opts)
    }
    return spawn(cmd, args)
}

export async function checkPortAvailable(port: number): Promise<PreflightResult> {
    if (await isPortInUse(port)) {
        return {
            ok: false,
            failure: { kind: "port-in-use", message: `Port ${port} is already in use — daemon already running?` },
        }
    }
    return { ok: true }
}

export async function checkDotool(spawnFn: SpawnFn = defaultSpawn, probeMs: number = 500): Promise<PreflightResult> {
    return new Promise((resolve) => {
        let settled = false
        let stderrBuf = ""
        let proc: ChildProcess

        const fail = (message: string) => {
            if (settled) return
            settled = true
            try {
                proc.kill("SIGTERM")
            } catch {}
            resolve({ ok: false, failure: { kind: "dotool", message } })
        }
        const pass = () => {
            if (settled) return
            settled = true
            try {
                proc.kill("SIGTERM")
            } catch {}
            resolve({ ok: true })
        }

        try {
            proc = spawnFn("dotool", [], { env: { ...process.env, DOTOOL_XKB_LAYOUT: "us" } })
        } catch (e) {
            resolve({
                ok: false,
                failure: { kind: "dotool", message: `dotool not spawnable: ${(e as Error).message}` },
            })
            return
        }

        proc.on("error", (err: NodeJS.ErrnoException) => fail(`dotool failed to start: ${err.message}`))
        proc.on("exit", (code, signal) =>
            fail(`dotool exited immediately (code=${code ?? "null"}, signal=${signal ?? "none"})`),
        )

        if (proc.stderr) {
            proc.stderr.on("data", (data: Buffer) => {
                stderrBuf += data.toString()
                if (/uinput|Permission denied|command not found|No such file/i.test(stderrBuf)) {
                    fail(`dotool error: ${stderrBuf.trim()}`)
                }
            })
        }

        setTimeout(pass, probeMs)
    })
}

export async function checkBrowserPath(path: string): Promise<PreflightResult> {
    if (!path) {
        return {
            ok: false,
            failure: {
                kind: "browser",
                message: "No browser_path configured. Set browser_path in ~/.config/voice-type.jsonc.",
            },
        }
    }
    try {
        await access(path, fsConstants.R_OK | fsConstants.X_OK)
        return { ok: true }
    } catch {
        return {
            ok: false,
            failure: { kind: "browser", message: `Browser binary not found or not executable: ${path}` },
        }
    }
}

export async function runPreflight(
    config: { port: number; browser_path: string },
    spawnFn: SpawnFn = defaultSpawn,
    probeMs: number = 500,
): Promise<PreflightResult> {
    const portCheck = await checkPortAvailable(config.port)
    if (!portCheck.ok) return portCheck

    const browserCheck = await checkBrowserPath(config.browser_path)
    if (!browserCheck.ok) return browserCheck

    const dotoolCheck = await checkDotool(spawnFn, probeMs)
    if (!dotoolCheck.ok) return dotoolCheck

    return { ok: true }
}
