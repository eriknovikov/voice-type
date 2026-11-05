import { expect, it } from "vitest";
import { meterTarget, smoothMeter } from "./MicrophoneMeter";
it("maps quiet speech visibly and bounds silence and invalid inputs", () => {
  expect(meterTarget(0)).toBe(0);
  expect(meterTarget(NaN)).toBe(0);
  expect(meterTarget(0.01)).toBeCloseTo(1 / 3);
  expect(meterTarget(0.1)).toBeCloseTo(2 / 3);
  expect(meterTarget(1)).toBe(1);
  expect(meterTarget(2)).toBe(1);
});
it("rises quickly, releases gradually, and is independent of frame rate", () => {
  expect(smoothMeter(0, 1, 50)).toBeGreaterThan(0.7);
  expect(smoothMeter(1, 0, 50)).toBeGreaterThan(0.7);
  const once = smoothMeter(0, 1, 32);
  const twice = smoothMeter(smoothMeter(0, 1, 16), 1, 16);
  expect(twice).toBeCloseTo(once);
  expect(smoothMeter(0.4, 0.4, 16)).toBe(0.4);
});
