// Spoken-operation ids denied at runtime. Empty = all operations allowed.
// Future: load from config / GUI instead of this hardcoded list.
export const DISALLOWED_TRANSCRIPT_OPERATIONS: string[] = []

export function isOperationAllowed(operationId: string): boolean {
    return !DISALLOWED_TRANSCRIPT_OPERATIONS.includes(operationId)
}
