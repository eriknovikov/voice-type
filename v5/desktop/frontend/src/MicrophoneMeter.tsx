import { memo, useEffect, useRef } from "react";
import { Events } from "@wailsio/runtime";

export function meterTarget(amplitude: number): number {
  if (!Number.isFinite(amplitude) || amplitude <= 0.001) return 0;
  return Math.min(1, Math.max(0, (20 * Math.log10(amplitude) + 60) / 60));
}
export function smoothMeter(
  current: number,
  target: number,
  elapsed: number,
): number {
  const timeConstant = target > current ? 35 : 220;
  return current + (target - current) * (1 - Math.exp(-elapsed / timeConstant));
}
// Audio frames update a ref. Animation touches only a composited transform;
// neither audio frames nor animation frames trigger React renders.
export const MicrophoneMeter = memo(function MicrophoneMeter({
  active,
}: {
  active: boolean;
}) {
  const bar = useRef<HTMLDivElement>(null);
  const meter = useRef<HTMLDivElement>(null);
  const target = useRef(0);
  const received = useRef(0);
  useEffect(
    () =>
      Events.On("microphone-level", (event) => {
        const value = Array.isArray(event.data) ? event.data[0] : event.data;
        target.current = meterTarget(Number(value));
        received.current = performance.now();
      }),
    [],
  );
  useEffect(() => {
    target.current = 0;
    received.current = 0;
    if (!active) {
      if (bar.current) bar.current.style.transform = "scaleX(0)";
      meter.current?.setAttribute("aria-valuenow", "0");
      return;
    }
    let frame = 0;
    let previous = performance.now();
    let current = 0;
    let accessibilityUpdate = 0;
    const animate = (now: number) => {
      // Decay on a stalled connection or a suspended audio source too.
      const next = now - received.current > 200 ? 0 : target.current;
      current = smoothMeter(current, next, Math.min(100, now - previous));
      previous = now;
      if (current < 0.0005) current = 0;
      if (bar.current) bar.current.style.transform = `scaleX(${current})`;
      if (now - accessibilityUpdate > 100) {
        meter.current?.setAttribute(
          "aria-valuenow",
          String(Math.round(current * 100)),
        );
        accessibilityUpdate = now;
      }
      frame = requestAnimationFrame(animate);
    };
    frame = requestAnimationFrame(animate);
    return () => cancelAnimationFrame(frame);
  }, [active]);
  return (
    <div
      ref={meter}
      className="meter"
      role="meter"
      aria-label="Microphone level"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={0}
    >
      <div ref={bar} />
    </div>
  );
});
