import { expect, it } from "vitest";
import { shortcutFromKey } from "./ShortcutPicker";
it("captures both default accelerators and ignores modifier-only input", () => {
  const event = {
    key: " ",
    ctrlKey: true,
    altKey: false,
    shiftKey: false,
    metaKey: false,
  };
  expect(shortcutFromKey(event)).toBe("Ctrl+Space");
  expect(shortcutFromKey({ ...event, altKey: true })).toBe("Ctrl+Alt+Space");
  expect(shortcutFromKey({ ...event, key: "Control" })).toBeNull();
  expect(shortcutFromKey({ ...event, key: "Escape" })).toBeNull();
});
