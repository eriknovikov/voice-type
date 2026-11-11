// @vitest-environment jsdom
import React from "react";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { FAQ } from "./FAQ";

afterEach(cleanup);

const groups = [
  "Getting started",
  "Choosing a provider",
  "API keys",
  "Local models",
  "Model memory",
  "Languages",
  "Dictation modes",
  "Microphone and shortcuts",
  "Privacy and data",
  "Troubleshooting",
];

describe("FAQ", () => {
  it("renders closed accordions", () => {
    render(<FAQ />);
    const items = document.querySelectorAll<HTMLDetailsElement>(
      "details.faq-item",
    );
    expect(items.length).toBeGreaterThan(0);
    for (const item of items) expect(item.open).toBe(false);
  });

  it("covers every group", () => {
    render(<FAQ />);
    for (const name of groups)
      expect(screen.getByRole("heading", { level: 2, name })).toBeTruthy();
  });

  it("answers common questions", () => {
    render(<FAQ />);
    expect(screen.getByText("Ctrl+Space")).toBeTruthy();
    expect(
      screen.getByText(/downloads about 731 MB/),
    ).toBeTruthy();
    expect(
      screen.getByText(/downloads about 740 MB/),
    ).toBeTruthy();
    expect(
      screen.getByText(
        "Cloud profiles upload audio to the selected provider. Local profiles never upload audio.",
      ),
    ).toBeTruthy();
    expect(screen.getByText("Which local model should I choose?")).toBeTruthy();
    expect(screen.getByText("How do I change the shortcut?")).toBeTruthy();
    expect(screen.getByText("Recordings are capped at ten minutes.")).toBeTruthy();
  });

  it("links to the key pages in a new tab", () => {
    render(<FAQ />);
    const openai = screen.getByRole("link", {
      name: "platform.openai.com/api-keys",
    });
    expect(openai.getAttribute("href")).toBe(
      "https://platform.openai.com/api-keys",
    );
    expect(openai.getAttribute("target")).toBe("_blank");
    expect(openai.getAttribute("rel")).toBe("noreferrer");
    const openrouter = screen.getByRole("link", {
      name: "openrouter.ai/keys",
    });
    expect(openrouter.getAttribute("href")).toBe(
      "https://openrouter.ai/keys",
    );
    expect(openrouter.getAttribute("target")).toBe("_blank");
    expect(openrouter.getAttribute("rel")).toBe("noreferrer");
  });
});
