// Deterministic spoken-punctuation transformation, applied to the full
// transcript BEFORE diffing so prevText and currText always live in the same
// transformed space (interim corrections backspace/retype cleanly).

// Spoken word -> punctuation. The \s* eats the space WSA puts before the word
// ("hello comma" -> "hello,"). \b keeps "commander"/"periodic" intact.
const PUNCTUATION_RULES: Array<[RegExp, string]> = [
    [/\s*\bcomma\b/gi, ","],
    [/\s*\bperiod\b/gi, "."],
    [/\s*\bquestion\s+mark\b/gi, "?"],
    [/\s*\bexclamation\s+(?:mark|point)\b/gi, "!"],
    [/\s*\bsemi[ -]?colon\b/gi, ";"],
    [/\s*\bcolon\b/gi, ":"],
]

export function transformTranscript(text: string): string {
    let result = text
    for (const [pattern, replacement] of PUNCTUATION_RULES) {
        result = result.replace(pattern, replacement)
    }
    // Capitalize the first letter after a sentence-ending punctuation mark
    result = result.replace(/([.?!]\s+)(\p{Ll})/gu, (_match, sep, letter) => sep + letter.toUpperCase())
    return result
}

// Capitalize the first letter in the text (used at the start of a new
// sentence when the previous segment's punctuation isn't part of this string)
export function capitalizeFirst(text: string): string {
    return text.replace(/\p{L}/u, (letter) => letter.toUpperCase())
}

export function endsSentence(text: string): boolean {
    return /[.?!]\s*$/.test(text)
}
