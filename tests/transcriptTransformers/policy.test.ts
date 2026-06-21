import { describe, expect, test } from "bun:test"
import { isOperationAllowed } from "../../src/transcriptTransformers/policy.js"

describe("isOperationAllowed", () => {
    test("allows known operations by default", () => {
        expect(isOperationAllowed("comma")).toBe(true)
        expect(isOperationAllowed("control enter")).toBe(true)
        expect(isOperationAllowed("new line")).toBe(true)
    })
})
