import { isOperationAllowed } from "./policy.js"
import type { DotoolKeyChord, TranscriptCommand, TranscriptTransformerSession, TransformResult } from "./types.js"

type PunctuationRole = "trailing" | "openingQuote"

type TextCommandDef = {
    kind: "text"
    id: string
    words: string[]
    render: { kind: "punctuation"; char: string; role: PunctuationRole } | { kind: "newline" }
}
type KeyCommandDef = { kind: "key"; id: string; words: string[]; chord: DotoolKeyChord }
type CommandDef = TextCommandDef | KeyCommandDef

type RenderItem =
    | { kind: "word"; text: string }
    | { kind: "punctuation"; char: string; role: PunctuationRole }
    | { kind: "newline" }
    | { kind: "key"; chord: DotoolKeyChord }

// Shared command registry: inline text ops and standalone key chords.
const COMMAND_DEFS: CommandDef[] = [
    { kind: "text", id: "comma", words: ["comma"], render: { kind: "punctuation", char: ",", role: "trailing" } },
    { kind: "text", id: "period", words: ["period"], render: { kind: "punctuation", char: ".", role: "trailing" } },
    {
        kind: "text",
        id: "question mark",
        words: ["question", "mark"],
        render: { kind: "punctuation", char: "?", role: "trailing" },
    },
    {
        kind: "text",
        id: "exclamation mark",
        words: ["exclamation", "mark"],
        render: { kind: "punctuation", char: "!", role: "trailing" },
    },
    {
        kind: "text",
        id: "exclamation mark",
        words: ["exclamation", "point"],
        render: { kind: "punctuation", char: "!", role: "trailing" },
    },
    { kind: "text", id: "semicolon", words: ["semicolon"], render: { kind: "punctuation", char: ";", role: "trailing" } },
    { kind: "text", id: "semicolon", words: ["semi", "colon"], render: { kind: "punctuation", char: ";", role: "trailing" } },
    { kind: "text", id: "semicolon", words: ["semi-colon"], render: { kind: "punctuation", char: ";", role: "trailing" } },
    { kind: "text", id: "new line", words: ["new", "line"], render: { kind: "newline" } },
    { kind: "text", id: "newline", words: ["newline"], render: { kind: "newline" } },
    {
        kind: "text",
        id: "double quote",
        words: ["double", "quote"],
        render: { kind: "punctuation", char: '"', role: "openingQuote" },
    },
    {
        kind: "text",
        id: "double quote",
        words: ["double", "quotes"],
        render: { kind: "punctuation", char: '"', role: "openingQuote" },
    },
    { kind: "key", id: "control enter", words: ["control", "enter"], chord: "ctrl+enter" },
]

const SENTENCE_CAPITALIZE = /([.?!]\s+|\n\s*)(\p{Ll})/gu
const FIRST_LETTER = /\p{L}/u
const ENDS_SENTENCE = /(?:[.?!]|\n)\s*$/

type TextBoundary = "none" | "word" | "afterPunctuation" | "afterNewline" | "afterOpeningQuote"

function tokenize(text: string): string[] {
    return text
        .trim()
        .split(/\s+/)
        .filter(Boolean)
        .map((w) => w.toLowerCase())
}

function allowedCommands(streamEnabled: boolean): CommandDef[] {
    return COMMAND_DEFS.filter((cmd) => {
        if (!isOperationAllowed(cmd.id)) return false
        if (!streamEnabled && (cmd.id === "new line" || cmd.id === "newline" || cmd.kind === "key")) return false
        return true
    })
}

type CommandContext = {
    allCommands: CommandDef[]
    textCommands: CommandDef[]
    streamEnabled: boolean
}

function buildCommandContext(streamEnabled: boolean): CommandContext {
    const allCommands = allowedCommands(streamEnabled)
    return {
        allCommands,
        textCommands: allCommands.filter((cmd) => cmd.kind !== "key"),
        streamEnabled,
    }
}

function findStandaloneKeyCommand(tokens: string[], ctx: CommandContext): KeyCommandDef | null {
    if (tokens.length === 0) return null
    for (const cmd of ctx.allCommands) {
        if (cmd.kind !== "key") continue
        if (wordsMatch(tokens, 0, cmd.words) && cmd.words.length === tokens.length) {
            return cmd
        }
    }
    return null
}

function wordsMatch(tokens: string[], start: number, words: string[]): boolean {
    if (start + words.length > tokens.length) return false
    return words.every((word, i) => tokens[start + i] === word)
}

function isCommandPrefix(tokens: string[], ctx: CommandContext): boolean {
    if (tokens.length === 0) return false
    return ctx.textCommands.some((cmd) => {
        if (tokens.length >= cmd.words.length) return false
        return wordsMatch(tokens, 0, cmd.words.slice(0, tokens.length))
    })
}

function findLongestCommand(
    tokens: string[],
    start: number,
    ctx: CommandContext,
): { cmd: CommandDef; len: number } | null {
    let best: { cmd: CommandDef; len: number } | null = null
    for (const cmd of ctx.textCommands) {
        if (!wordsMatch(tokens, start, cmd.words)) continue
        if (!best || cmd.words.length > best.len) {
            best = { cmd, len: cmd.words.length }
        }
    }
    return best
}

function commandToRenderItem(cmd: CommandDef): RenderItem {
    if (cmd.kind === "key") return { kind: "key", chord: cmd.chord }
    if (cmd.render.kind === "newline") return { kind: "newline" }
    return { kind: "punctuation", char: cmd.render.char, role: cmd.render.role }
}

function parseTokens(tokens: string[], ctx: CommandContext): { items: RenderItem[]; deferred: string[] } {
    const standaloneKey = findStandaloneKeyCommand(tokens, ctx)
    if (standaloneKey) {
        return { items: [commandToRenderItem(standaloneKey)], deferred: [] }
    }

    const items: RenderItem[] = []
    let i = 0

    while (i < tokens.length) {
        const match = findLongestCommand(tokens, i, ctx)
        if (match) {
            items.push(commandToRenderItem(match.cmd))
            i += match.len
            continue
        }

        const remaining = tokens.slice(i)
        if (ctx.streamEnabled && isCommandPrefix(remaining, ctx)) {
            return { items, deferred: remaining }
        }

        items.push({ kind: "word", text: tokens[i]! })
        i++
    }

    return { items, deferred: [] }
}

function needsSpaceBeforeWord(boundary: TextBoundary): boolean {
    return boundary === "word" || boundary === "afterPunctuation" || boundary === "afterOpeningQuote"
}

function renderItems(items: RenderItem[]): string {
    let text = ""
    let boundary: TextBoundary = "none"

    for (const item of items) {
        if (item.kind === "key") {
            if (boundary !== "none" && boundary !== "afterNewline") {
                boundary = "afterPunctuation"
            }
            continue
        }

        if (item.kind === "word") {
            if (needsSpaceBeforeWord(boundary)) text += " "
            text += item.text
            boundary = "word"
            continue
        }

        if (item.kind === "punctuation") {
            if (item.role === "openingQuote") {
                if (boundary === "word") text += " "
                text += item.char
                boundary = "afterOpeningQuote"
            } else {
                text += item.char
                boundary = "afterPunctuation"
            }
            continue
        }

        if (item.kind === "newline") {
            text += "\n"
            boundary = "afterNewline"
        }
    }

    return text.replace(SENTENCE_CAPITALIZE, (_match, sep, letter) => sep + letter.toUpperCase())
}

function extractKeyCommands(items: RenderItem[]): TranscriptCommand[] {
    return items
        .filter((item): item is { kind: "key"; chord: DotoolKeyChord } => item.kind === "key")
        .map((item) => ({ kind: "key", chord: item.chord }))
}

function capitalizeFirst(text: string): string {
    return text.replace(FIRST_LETTER, (letter) => letter.toUpperCase())
}

function endsSentence(text: string): boolean {
    return ENDS_SENTENCE.test(text)
}

function needsBoundaryLeadingSpace(items: RenderItem[]): boolean {
    const first = items[0]
    if (!first) return false
    if (first.kind === "word") return true
    if (first.kind === "punctuation" && first.role === "openingQuote") return true
    return false
}

function endsWithVisibleContent(text: string): boolean {
    return /\S/.test(text) && !text.endsWith("\n")
}

// Whether the next segment's text should be prefixed with a space after a WSA pause.
// Sentence-ending punctuation (.?!) attaches directly; newlines already separate;
// deferred multi-word commands use needsBoundaryLeadingSpace() to skip before newlines.
function needsCrossSegmentSpace(text: string): boolean {
    if (!text || text.endsWith("\n")) return false
    if (/[.?!]$/.test(text)) return false
    return /\S$/.test(text)
}

export function createEnglishTransformerSession(streamEnabled: boolean): TranscriptTransformerSession {
    const commandCtx = buildCommandContext(streamEnabled)
    let capitalizeNext = true
    let lastRenderedText = ""
    let carriedDeferred: string[] = []
    let segmentEndDeferred: string[] = []
    let pendingBoundarySpace = false
    let lastSegmentItems: RenderItem[] = []

    return {
        transform(rawText: string): TransformResult {
            let rawTokens = tokenize(rawText)
            if (carriedDeferred.length > 0) {
                rawTokens = [...carriedDeferred, ...rawTokens]
            }

            const { items, deferred } = parseTokens(rawTokens, commandCtx)
            segmentEndDeferred = deferred
            lastSegmentItems = items

            let text = renderItems(items)
            if (pendingBoundarySpace && text.length > 0 && needsBoundaryLeadingSpace(items)) {
                text = " " + text
            }

            if (capitalizeNext) text = capitalizeFirst(text)
            lastRenderedText = text

            return { text, commands: [] }
        },
        onSegmentFinalized(): TranscriptCommand[] {
            const deferredForNext = segmentEndDeferred
            segmentEndDeferred = []

            const commands = extractKeyCommands(lastSegmentItems)
            carriedDeferred = deferredForNext
            lastSegmentItems = []

            const hadVisibleText = endsWithVisibleContent(lastRenderedText)
            if (carriedDeferred.length > 0) {
                pendingBoundarySpace = hadVisibleText
            } else {
                pendingBoundarySpace = hadVisibleText && needsCrossSegmentSpace(lastRenderedText)
            }

            if (lastRenderedText !== "") {
                capitalizeNext = endsSentence(lastRenderedText)
                lastRenderedText = ""
            }
            return commands
        },
        reset(): void {
            capitalizeNext = true
            lastRenderedText = ""
            carriedDeferred = []
            segmentEndDeferred = []
            pendingBoundarySpace = false
            lastSegmentItems = []
        },
    }
}
