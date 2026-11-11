import { expect, it } from "vitest";
import { meterTarget, smoothMeter } from "./MicrophoneMeter";
it("maps quiet speech visibly and bounds silence and invalid inputs", () => {
  expect(meterTarget(0)).toBe(0);
  expect(meterTarget(NaN)).toBe(0);
  expect(meterTarget(0.01)).toBeCloseTo(5 / 39);
  expect(meterTarget(0.1)).toBeCloseTo(25 / 39);
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

it("leaves quiet noise at zero and tracks a two-second crescendo", () => {
  expect(meterTarget(0.003)).toBe(0);
  let current = 0;
  let previous = 0;
  for (let frame = 1; frame <= 120; frame++) {
    const target = meterTarget(0.004 + (frame / 120) * 0.3);
    current = smoothMeter(current, target, 1000 / 60);
    expect(current).toBeGreaterThanOrEqual(previous);
    previous = current;
  }
  expect(current).toBeGreaterThan(0.8);
});
