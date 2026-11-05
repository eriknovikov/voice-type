// @vitest-environment jsdom
import React, { Profiler } from "react";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { MicrophoneMeter } from "./MicrophoneMeter";
const events = vi.hoisted(() => ({
  listener: null as null | ((event: { data: unknown }) => void),
  unsubscribe: vi.fn(),
}));
vi.mock("@wailsio/runtime", () => ({
  Events: {
    On: (_name: string, callback: (event: { data: unknown }) => void) => {
      events.listener = callback;
      return events.unsubscribe;
    },
  },
}));
afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});
it("animates streamed levels without committing React renders and cancels on unmount", () => {
  const frames = new Map<number, FrameRequestCallback>();
  let sequence = 0;
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) => {
    frames.set(++sequence, callback);
    return sequence;
  });
  vi.stubGlobal("cancelAnimationFrame", (id: number) => frames.delete(id));
  vi.spyOn(performance, "now").mockReturnValue(0);
  const commits = vi.fn();
  const view = render(
    <Profiler id="meter" onRender={commits}>
      <MicrophoneMeter active />
    </Profiler>,
  );
  const initialCommits = commits.mock.calls.length;
  const advance = (timestamp: number) => {
    const [id, callback] = frames.entries().next().value!;
    frames.delete(id);
    act(() => callback(timestamp));
  };
  act(() => events.listener?.({ data: [0.1] }));
  advance(16);
  const bar = screen.getByRole("meter").firstElementChild as HTMLElement;
  expect(bar.style.transform).not.toBe("scaleX(0)");
  const first = bar.style.transform;
  advance(48);
  expect(bar.style.transform).not.toBe(first);
  expect(commits.mock.calls.length).toBe(initialCommits);
  view.unmount();
  expect(frames.size).toBe(0);
  expect(events.unsubscribe).toHaveBeenCalled();
});
