import { createServer } from "http"

export async function isPortInUse(port: number): Promise<boolean> {
    return new Promise((resolve) => {
        const server = createServer()
        server.once("error", () => {
            resolve(true)
        })
        server.once("listening", () => {
            server.once("close", () => {
                resolve(false)
            })
            server.close()
        })
        server.listen(port, "127.0.0.1")
    })
}
